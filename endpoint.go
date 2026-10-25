package gota

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"

	"github.com/carlosxfelipe/gota/binding"
	"github.com/carlosxfelipe/gota/core"
	"github.com/carlosxfelipe/gota/validation"
)

// Streamer defines the contract for streaming responses in REPR endpoints.
type Streamer interface {
	Stream(ctx *core.Context) error
}

// StreamFunc adapts a function to the Streamer interface.
type StreamFunc func(ctx *core.Context) error

// Stream executes the stream function.
func (f StreamFunc) Stream(ctx *core.Context) error {
	return f(ctx)
}

// SSEvent represents a Server-Sent Event item.
type SSEvent struct {
	Event string
	Data  any
}

// SSEStream streams Server-Sent Events from a channel.
type SSEStream struct {
	Events <-chan SSEvent
}

// Stream sends each event in the channel and flushes it immediately.
func (s SSEStream) Stream(ctx *core.Context) error {
	for evt := range s.Events {
		if err := ctx.SSEvent(evt.Event, evt.Data); err != nil {
			return err
		}
	}
	return nil
}

// hasRequiredRule reports whether a validate tag contains the `required` rule.
func hasRequiredRule(tag string) bool {
	for _, rule := range strings.Split(tag, ",") {
		if strings.TrimSpace(rule) == "required" {
			return true
		}
	}
	return false
}

// RouterTarget represents any entity capable of registering routes (App or RouteGroup).
type RouterTarget interface {
	RegisterRoute(method, path string, h core.HandlerFunc) *core.Route
}

// Endpoint defines the contract for struct-based REPR endpoints.
type Endpoint[Req any, Res any] interface {
	Handle(ctx *core.Context, req Req) (Res, error)
}

// EndpointFunc defines the contract for function-based REPR endpoints.
type EndpointFunc[Req any, Res any] func(ctx *core.Context, req Req) (Res, error)

// RouteConfig holds metadata for self-configuring endpoints.
type RouteConfig struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Status      int
	Deprecated  bool
}

// ConfigurableEndpoint is implemented by endpoints that define their own route metadata.
type ConfigurableEndpoint interface {
	Route() RouteConfig
	Register(target RouterTarget) *core.Route
}

// WrapEndpoint converts a generic REPR handler into a standard Gota HandlerFunc
// with automatic parameter binding, validation, and OpenAPI documentation.
func WrapEndpoint[Req any, Res any](handler EndpointFunc[Req, Res], defaultStatus int) (core.HandlerFunc, *core.RouteDoc) {
	// Fail fast: a typo in a `validate` tag would otherwise silently disable the rule.
	if err := validation.CheckTags(reflect.TypeOf(*new(Req))); err != nil {
		panic(fmt.Sprintf("gota: invalid validate tag in %T: %v", *new(Req), err))
	}

	doc := introspectEndpoint[Req, Res](defaultStatus)

	handlerFunc := func(ctx *core.Context) error {
		var req Req
		reqType := reflect.TypeOf(req)

		// Perform auto-binding and validation if Req is not Empty
		if reqType != reflect.TypeOf(Empty{}) && reqType != reflect.TypeOf(struct{}{}) {
			if err := binding.BindRequest(ctx, &req); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					return ctx.Status(http.StatusRequestEntityTooLarge).Json(ErrorResponse{
						Error:     "request body too large",
						Status:    http.StatusRequestEntityTooLarge,
						RequestID: ctx.RequestID(),
					})
				}
				return ctx.Status(http.StatusBadRequest).Json(ErrorResponse{
					Error:     "invalid request: " + err.Error(),
					Status:    http.StatusBadRequest,
					RequestID: ctx.RequestID(),
				})
			}

			if valErr := validation.ValidateStruct(ctx, &req); valErr != nil {
				return ctx.Status(valErr.Status).Json(valErr)
			}
		}

		res, err := handler(ctx, req)
		if err != nil {
			return err
		}

		if streamer, ok := any(res).(Streamer); ok {
			return streamer.Stream(ctx)
		}

		if !ctx.Written() {
			resType := reflect.TypeOf(res)
			if resType == reflect.TypeOf(Empty{}) || resType == reflect.TypeOf(struct{}{}) {
				return ctx.NoContent()
			}
			status := defaultStatus
			if ctx.StatusExplicit() {
				status = ctx.StatusCode()
			}
			return ctx.Status(status).Json(res)
		}
		return nil
	}

	return handlerFunc, doc
}

// introspectEndpoint inspects Req and Res types to construct initial OpenAPI metadata.
func introspectEndpoint[Req any, Res any](defaultStatus int) *core.RouteDoc {
	doc := &core.RouteDoc{
		Responses: make(map[int]*core.ResponseDoc),
	}

	var req Req
	reqType := reflect.TypeOf(req)
	if reqType != nil && reqType.Kind() == reflect.Pointer {
		reqType = reqType.Elem()
	}

	hasBodyFields := false
	hasFormFields := false
	hasFileFields := false

	if reqType != nil && reqType != reflect.TypeOf(Empty{}) && reqType != reflect.TypeOf(struct{}{}) && reqType.Kind() == reflect.Struct {
		for i := 0; i < reqType.NumField(); i++ {
			field := reqType.Field(i)

			// Path param
			if paramTag := field.Tag.Get("param"); paramTag != "" {
				doc.Parameters = append(doc.Parameters, core.ParamDoc{
					In:          "path",
					Name:        paramTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    true,
				})
				continue
			}

			// Query param
			if queryTag := field.Tag.Get("query"); queryTag != "" {
				doc.Parameters = append(doc.Parameters, core.ParamDoc{
					In:          "query",
					Name:        queryTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    hasRequiredRule(field.Tag.Get("validate")),
				})
				continue
			}

			// Header param
			if headerTag := field.Tag.Get("header"); headerTag != "" {
				doc.Parameters = append(doc.Parameters, core.ParamDoc{
					In:          "header",
					Name:        headerTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    hasRequiredRule(field.Tag.Get("validate")),
				})
				continue
			}

			if formTag := field.Tag.Get("form"); formTag != "" {
				hasFormFields = true
				if field.Type == reflect.TypeOf((*multipart.FileHeader)(nil)) || field.Type == reflect.TypeOf(([]*multipart.FileHeader)(nil)) {
					hasFileFields = true
				}
			}

			if field.IsExported() {
				hasBodyFields = true
			}
		}

		if hasBodyFields {
			contentType := "application/json"
			if hasFileFields {
				contentType = "multipart/form-data"
			} else if hasFormFields {
				contentType = "application/x-www-form-urlencoded"
			}

			doc.RequestBody = &core.BodyDoc{
				Model:       *new(Req),
				Description: "Request body payload",
				Required:    true,
				ContentType: contentType,
			}
		}
	}

	var res Res
	resType := reflect.TypeOf(res)
	streamerType := reflect.TypeOf((*Streamer)(nil)).Elem()

	if resType == reflect.TypeOf(Empty{}) || resType == reflect.TypeOf(struct{}{}) {
		doc.Responses[http.StatusNoContent] = &core.ResponseDoc{
			Description: "No Content",
		}
	} else if resType != nil && (resType.Implements(streamerType) || reflect.PointerTo(resType).Implements(streamerType)) {
		cType := "application/octet-stream"
		if resType == reflect.TypeOf(SSEStream{}) || resType == reflect.TypeOf(&SSEStream{}) {
			cType = "text/event-stream"
		}
		doc.Responses[defaultStatus] = &core.ResponseDoc{
			Description: http.StatusText(defaultStatus),
			ContentType: cType,
		}
	} else {
		doc.Responses[defaultStatus] = &core.ResponseDoc{
			Model:       *new(Res),
			Description: http.StatusText(defaultStatus),
			ContentType: "application/json",
		}
	}

	// Always document standard error responses
	doc.Responses[http.StatusBadRequest] = &core.ResponseDoc{
		Model:       core.ErrorResponse{},
		Description: "Bad Request",
	}
	doc.Responses[http.StatusUnprocessableEntity] = &core.ResponseDoc{
		Model:       validation.ValidationError{},
		Description: "Validation Error",
	}

	return doc
}

// Get registers an HTTP GET REPR endpoint on App or RouteGroup.
func Get[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(http.MethodGet, path, h)
	route.Doc(*doc)
	return route
}

// Post registers an HTTP POST REPR endpoint on App or RouteGroup.
func Post[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(http.MethodPost, path, h)
	route.Doc(*doc)
	return route
}

// Put registers an HTTP PUT REPR endpoint on App or RouteGroup.
func Put[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(http.MethodPut, path, h)
	route.Doc(*doc)
	return route
}

// Delete registers an HTTP DELETE REPR endpoint on App or RouteGroup.
func Delete[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(http.MethodDelete, path, h)
	route.Doc(*doc)
	return route
}

// Patch registers an HTTP PATCH REPR endpoint on App or RouteGroup.
func Patch[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(http.MethodPatch, path, h)
	route.Doc(*doc)
	return route
}

// HandleEndpoint registers a struct-based REPR endpoint implementing Endpoint[Req, Res].
func HandleEndpoint[Req any, Res any](target RouterTarget, method, path string, ep Endpoint[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(ep.Handle, http.StatusOK)
	route := target.RegisterRoute(method, path, h)
	route.Doc(*doc)
	return route
}
