package gota

import (
	"net/http"
	"reflect"
)

// RouterTarget represents any entity capable of registering routes (App or RouteGroup).
type RouterTarget interface {
	registerRoute(method, path string, h HandlerFunc) *Route
}

func (a *App) registerRoute(method, path string, h HandlerFunc) *Route {
	return a.register(method, path, h)
}

func (g *RouteGroup) registerRoute(method, path string, h HandlerFunc) *Route {
	return g.register(method, path, h)
}

// Endpoint defines the contract for struct-based REPR endpoints.
type Endpoint[Req any, Res any] interface {
	Handle(ctx *Context, req Req) (Res, error)
}

// EndpointFunc defines the contract for function-based REPR endpoints.
type EndpointFunc[Req any, Res any] func(ctx *Context, req Req) (Res, error)

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
	Register(target RouterTarget) *Route
}

// WrapEndpoint converts a generic REPR handler into a standard Gota HandlerFunc
// with automatic parameter binding, validation, and OpenAPI documentation.
func WrapEndpoint[Req any, Res any](handler EndpointFunc[Req, Res], defaultStatus int) (HandlerFunc, *RouteDoc) {
	doc := introspectEndpoint[Req, Res](defaultStatus)

	handlerFunc := func(ctx *Context) error {
		var req Req
		reqType := reflect.TypeOf(req)

		// Perform auto-binding and validation if Req is not Empty
		if reqType != reflect.TypeOf(Empty{}) && reqType != reflect.TypeOf(struct{}{}) {
			if err := BindRequest(ctx, &req); err != nil {
				return ctx.Status(http.StatusBadRequest).Json(ErrorResponse{
					Error:  "invalid request: " + err.Error(),
					Status: http.StatusBadRequest,
				})
			}

			if valErr := ValidateStruct(ctx, &req); valErr != nil {
				return ctx.Status(valErr.Status).Json(valErr)
			}
		}

		res, err := handler(ctx, req)
		if err != nil {
			return err
		}

		if !ctx.Written() {
			resType := reflect.TypeOf(res)
			if resType == reflect.TypeOf(Empty{}) || resType == reflect.TypeOf(struct{}{}) {
				return ctx.NoContent()
			}
			return ctx.Status(defaultStatus).Json(res)
		}
		return nil
	}

	return handlerFunc, doc
}

// introspectEndpoint inspects Req and Res types to construct initial OpenAPI metadata.
func introspectEndpoint[Req any, Res any](defaultStatus int) *RouteDoc {
	doc := &RouteDoc{
		Responses: make(map[int]*ResponseDoc),
	}

	var req Req
	reqType := reflect.TypeOf(req)
	if reqType != nil && reqType.Kind() == reflect.Pointer {
		reqType = reqType.Elem()
	}

	hasBodyFields := false

	if reqType != nil && reqType != reflect.TypeOf(Empty{}) && reqType != reflect.TypeOf(struct{}{}) && reqType.Kind() == reflect.Struct {
		for i := 0; i < reqType.NumField(); i++ {
			field := reqType.Field(i)

			// Path param
			if paramTag := field.Tag.Get("param"); paramTag != "" {
				doc.Parameters = append(doc.Parameters, ParamDoc{
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
				doc.Parameters = append(doc.Parameters, ParamDoc{
					In:          "query",
					Name:        queryTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    field.Tag.Get("validate") == "required",
				})
				continue
			}

			// Header param
			if headerTag := field.Tag.Get("header"); headerTag != "" {
				doc.Parameters = append(doc.Parameters, ParamDoc{
					In:          "header",
					Name:        headerTag,
					Model:       reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"),
					Required:    field.Tag.Get("validate") == "required",
				})
				continue
			}

			if field.IsExported() {
				hasBodyFields = true
			}
		}

		if hasBodyFields {
			doc.RequestBody = &BodyDoc{
				Model:       *new(Req),
				Description: "Request body payload",
				Required:    true,
				ContentType: "application/json",
			}
		}
	}

	var res Res
	resType := reflect.TypeOf(res)
	if resType == reflect.TypeOf(Empty{}) || resType == reflect.TypeOf(struct{}{}) {
		doc.Responses[http.StatusNoContent] = &ResponseDoc{
			Description: "No Content",
		}
	} else {
		doc.Responses[defaultStatus] = &ResponseDoc{
			Model:       *new(Res),
			Description: http.StatusText(defaultStatus),
			ContentType: "application/json",
		}
	}

	// Always document standard error responses
	doc.Responses[http.StatusBadRequest] = &ResponseDoc{
		Model:       ErrorResponse{},
		Description: "Bad Request",
	}
	doc.Responses[http.StatusUnprocessableEntity] = &ResponseDoc{
		Model:       ValidationError{},
		Description: "Validation Error",
	}

	return doc
}

// Get registers an HTTP GET REPR endpoint on App or RouteGroup.
func Get[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc := WrapEndpoint[Req, Res](handler, http.StatusOK)
	route := target.registerRoute(http.MethodGet, path, h)
	route.Doc(*doc)
	return route
}

// Post registers an HTTP POST REPR endpoint on App or RouteGroup.
func Post[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc := WrapEndpoint[Req, Res](handler, http.StatusCreated)
	route := target.registerRoute(http.MethodPost, path, h)
	route.Doc(*doc)
	return route
}

// Put registers an HTTP PUT REPR endpoint on App or RouteGroup.
func Put[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc := WrapEndpoint[Req, Res](handler, http.StatusOK)
	route := target.registerRoute(http.MethodPut, path, h)
	route.Doc(*doc)
	return route
}

// Delete registers an HTTP DELETE REPR endpoint on App or RouteGroup.
func Delete[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc := WrapEndpoint[Req, Res](handler, http.StatusOK)
	route := target.registerRoute(http.MethodDelete, path, h)
	route.Doc(*doc)
	return route
}

// Patch registers an HTTP PATCH REPR endpoint on App or RouteGroup.
func Patch[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc := WrapEndpoint[Req, Res](handler, http.StatusOK)
	route := target.registerRoute(http.MethodPatch, path, h)
	route.Doc(*doc)
	return route
}

// HandleEndpoint registers a struct-based REPR endpoint implementing Endpoint[Req, Res].
func HandleEndpoint[Req any, Res any](target RouterTarget, method, path string, ep Endpoint[Req, Res]) *Route {
	status := http.StatusOK
	if method == http.MethodPost {
		status = http.StatusCreated
	}
	h, doc := WrapEndpoint[Req, Res](ep.Handle, status)
	route := target.registerRoute(method, path, h)
	route.Doc(*doc)
	return route
}

// Register registers a self-configuring endpoint that implements ConfigurableEndpoint.
func (a *App) Register(ep ConfigurableEndpoint) *Route {
	return ep.Register(a)
}

// Register registers a self-configuring endpoint on a RouteGroup.
func (g *RouteGroup) Register(ep ConfigurableEndpoint) *Route {
	return ep.Register(g)
}
