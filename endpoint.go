package gota

import (
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/carlosxfelipe/gota/binding"
	"github.com/carlosxfelipe/gota/core"
	"github.com/carlosxfelipe/gota/validation"
)

var (
	loggedTagWarnings   sync.Map
	emptyType           = reflect.TypeOf(Empty{})
	emptyStructType     = reflect.TypeOf(struct{}{})
	streamerType        = reflect.TypeOf((*Streamer)(nil)).Elem()
	sseStreamType       = reflect.TypeOf(SSEStream{})
	sseStreamPtrType    = reflect.TypeOf(&SSEStream{})
	fileHeaderPtrType   = reflect.TypeOf((*multipart.FileHeader)(nil))
	fileHeaderSliceType = reflect.TypeOf(([]*multipart.FileHeader)(nil))
)

func isEmptyType(t reflect.Type) bool {
	return t == emptyType || t == emptyStructType
}

func hasResponseHeaders(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || isEmptyType(t) {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if ft, ok := core.EmbeddedStructType(f); ok {
			if hasResponseHeaders(ft) {
				return true
			}
			continue
		}
		if f.IsExported() && f.Tag.Get("header") != "" {
			return true
		}
	}
	return false
}

func writeResponseHeaders(ctx *core.Context, val reflect.Value) {
	for val.IsValid() && val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}
	if !val.IsValid() || val.Kind() != reflect.Struct {
		return
	}
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fv := val.Field(i)
		if _, ok := core.EmbeddedStructType(f); ok {
			writeResponseHeaders(ctx, fv)
			continue
		}
		if !f.IsExported() {
			continue
		}
		hdr := f.Tag.Get("header")
		if hdr == "" {
			continue
		}
		for fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				break
			}
			fv = fv.Elem()
		}
		if !fv.IsValid() || fv.Kind() == reflect.Pointer || fv.IsZero() {
			continue
		}
		ctx.SetHeader(hdr, fmt.Sprintf("%v", fv.Interface()))
	}
}

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
// It returns as soon as the client disconnects, even if the channel is never closed.
func (s SSEStream) Stream(ctx *core.Context) error {
	done := ctx.Request.Context().Done()
	for {
		select {
		case <-done:
			return nil
		case evt, ok := <-s.Events:
			if !ok {
				return nil
			}
			if err := ctx.SSEvent(evt.Event, evt.Data); err != nil {
				return err
			}
		}
	}
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

// WrapEndpoint converts a generic REPR handler into a standard Gota HandlerFunc
// with automatic parameter binding, validation, and OpenAPI documentation.
func WrapEndpoint[Req any, Res any](handler EndpointFunc[Req, Res], defaultStatus int) (core.HandlerFunc, *core.RouteDoc) {
	reqType := reflect.TypeOf(*new(Req))

	// Fail fast: an invalid tag (e.g. unknown rule, min=abc, path:"id") would cause silent bugs.
	if err := validation.CheckTags(reqType); err != nil {
		panic(fmt.Sprintf("gota: invalid tag in %T: %v", *new(Req), err))
	}

	// Emit non-fatal warnings for suspicious validation rules (e.g. required on primitive bool/numbers).
	if !validation.SilenceTagWarnings {
		for _, w := range validation.CheckTagWarnings(reqType) {
			if _, already := loggedTagWarnings.LoadOrStore(w, true); !already {
				if validation.TagWarningLogger != nil {
					validation.TagWarningLogger(w)
				} else {
					log.Printf("⚠️  gota: %s", w)
				}
			}
		}
	}

	doc := introspectEndpoint[Req, Res](defaultStatus)
	resHasHeaders := hasResponseHeaders(reflect.TypeOf(*new(Res)))

	handlerFunc := func(ctx *core.Context) error {
		var req Req
		reqType := reflect.TypeOf(req)

		// Perform auto-binding and validation if Req is not Empty
		if !isEmptyType(reqType) {
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
			// Long-lived streams (SSE, chunked) must not be cut by the server WriteTimeout.
			// Errors are ignored: not every ResponseWriter supports deadlines.
			_ = http.NewResponseController(ctx.ResponseWriter).SetWriteDeadline(time.Time{})
			return streamer.Stream(ctx)
		}

		if !ctx.Written() {
			if resHasHeaders {
				writeResponseHeaders(ctx, reflect.ValueOf(res))
			}
			resType := reflect.TypeOf(res)
			if isEmptyType(resType) {
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

	if reqType != nil && !isEmptyType(reqType) && reqType.Kind() == reflect.Struct {
		inspectRequestStruct(reqType, &doc.Parameters, &hasBodyFields, &hasFormFields, &hasFileFields, nil, map[reflect.Type]bool{})

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

	if isEmptyType(resType) {
		doc.Responses[http.StatusNoContent] = &core.ResponseDoc{
			Description: "No Content",
		}
	} else if resType != nil && (resType.Implements(streamerType) || reflect.PointerTo(resType).Implements(streamerType)) {
		cType := "application/octet-stream"
		if resType == sseStreamType || resType == sseStreamPtrType {
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

// inspectRequestStruct extracts path, query, and header parameters from t (including
// untagged embedded structs, with outer fields shadowing promoted ones) and reports
// whether t has any request body, form, or file upload fields.
func inspectRequestStruct(t reflect.Type, params *[]core.ParamDoc, hasBodyFields, hasFormFields, hasFileFields *bool, shadowed map[string]bool, embedding map[reflect.Type]bool) {
	if embedding[t] {
		return
	}
	embedding[t] = true
	defer delete(embedding, t)

	mergedShadowed := make(map[string]bool, len(shadowed)+t.NumField()*3)
	for k, v := range shadowed {
		mergedShadowed[k] = v
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if _, ok := core.EmbeddedStructType(f); ok {
			continue
		}
		for _, tag := range [...]string{"param", "query", "header"} {
			if v := f.Tag.Get(tag); v != "" {
				mergedShadowed[tag+":"+v] = true
			}
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")

		if ft, ok := core.EmbeddedStructType(field); ok {
			if jsonTag != "-" && formTag != "-" {
				inspectRequestStruct(ft, params, hasBodyFields, hasFormFields, hasFileFields, mergedShadowed, embedding)
			}
			continue
		}

		if !field.IsExported() {
			continue
		}

		var ex any
		if exTag := field.Tag.Get("example"); exTag != "" {
			ex = core.ParseExample(exTag, field.Type)
		}
		var def any
		if defTag := field.Tag.Get("default"); defTag != "" {
			def = core.ParseExample(defTag, field.Type)
		}
		formatTag := field.Tag.Get("format")
		validateTag := field.Tag.Get("validate")

		// Path param
		if paramTag := field.Tag.Get("param"); paramTag != "" {
			if !shadowed["param:"+paramTag] {
				*params = append(*params, core.ParamDoc{
					In:          "path",
					Name:        paramTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    true,
					Example:     ex,
					Default:     def,
					Format:      formatTag,
					Validate:    validateTag,
				})
			}
			if (jsonTag == "" || jsonTag == "-") && (formTag == "" || formTag == "-") {
				continue
			}
		}

		// Query param
		if queryTag := field.Tag.Get("query"); queryTag != "" {
			if !shadowed["query:"+queryTag] {
				*params = append(*params, core.ParamDoc{
					In:          "query",
					Name:        queryTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    hasRequiredRule(validateTag),
					Example:     ex,
					Default:     def,
					Format:      formatTag,
					Validate:    validateTag,
				})
			}
			if (jsonTag == "" || jsonTag == "-") && (formTag == "" || formTag == "-") {
				continue
			}
		}

		// Header param
		if headerTag := field.Tag.Get("header"); headerTag != "" {
			if !shadowed["header:"+headerTag] {
				*params = append(*params, core.ParamDoc{
					In:          "header",
					Name:        headerTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    hasRequiredRule(validateTag),
					Example:     ex,
					Default:     def,
					Format:      formatTag,
					Validate:    validateTag,
				})
			}
			if (jsonTag == "" || jsonTag == "-") && (formTag == "" || formTag == "-") {
				continue
			}
		}

		if jsonTag == "-" || formTag == "-" {
			continue
		}

		if formTag != "" {
			*hasFormFields = true
			if field.Type == fileHeaderPtrType || field.Type == fileHeaderSliceType {
				*hasFileFields = true
			}
		}

		*hasBodyFields = true
	}
}

func registerEndpoint[Req any, Res any](target RouterTarget, method, path string, handler EndpointFunc[Req, Res]) *core.Route {
	h, doc := WrapEndpoint(handler, http.StatusOK)
	route := target.RegisterRoute(method, path, h)
	route.Doc(*doc)
	return route
}

// Get registers an HTTP GET REPR endpoint on App or RouteGroup.
func Get[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	return registerEndpoint(target, http.MethodGet, path, handler)
}

// Post registers an HTTP POST REPR endpoint on App or RouteGroup.
func Post[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	return registerEndpoint(target, http.MethodPost, path, handler)
}

// Put registers an HTTP PUT REPR endpoint on App or RouteGroup.
func Put[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	return registerEndpoint(target, http.MethodPut, path, handler)
}

// Delete registers an HTTP DELETE REPR endpoint on App or RouteGroup.
func Delete[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	return registerEndpoint(target, http.MethodDelete, path, handler)
}

// Patch registers an HTTP PATCH REPR endpoint on App or RouteGroup.
func Patch[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *core.Route {
	return registerEndpoint(target, http.MethodPatch, path, handler)
}

// HandleEndpoint registers a struct-based REPR endpoint implementing Endpoint[Req, Res].
func HandleEndpoint[Req any, Res any](target RouterTarget, method, path string, ep Endpoint[Req, Res]) *core.Route {
	return registerEndpoint(target, method, path, ep.Handle)
}
