package repr

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

var (
	emptyType        = reflect.TypeOf(Empty{})
	emptyStructType  = reflect.TypeOf(struct{}{})
	streamerType     = reflect.TypeOf((*Streamer)(nil)).Elem()
	sseStreamType    = reflect.TypeOf(SSEStream{})
	sseStreamPtrType = reflect.TypeOf(&SSEStream{})
)

func isEmptyType(t reflect.Type) bool {
	return t == emptyType || t == emptyStructType
}

// Streamer defines the contract for streaming responses in REPR endpoints.
type Streamer interface {
	Stream(ctx *Context) error
}

// StreamFunc adapts a function to the Streamer interface.
type StreamFunc func(ctx *Context) error

func (f StreamFunc) Stream(ctx *Context) error { return f(ctx) }

// SSEvent represents a Server-Sent Event item.
type SSEvent struct {
	Event string
	Data  any
}

// SSEStream streams Server-Sent Events from a channel until closed or client disconnects.
type SSEStream struct {
	Events <-chan SSEvent
}

func (s SSEStream) Stream(ctx *Context) error {
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

// Endpoint defines the contract for struct-based REPR endpoints.
type Endpoint[Req any, Res any] interface {
	Handle(ctx *Context, req Req) (Res, error)
}

// EndpointFunc defines the contract for function-based REPR endpoints.
type EndpointFunc[Req any, Res any] func(ctx *Context, req Req) (Res, error)

// RouterTarget represents an App or Group that can register REPR routes.
type RouterTarget interface {
	registerRoute(method, path string, h HandlerFunc, doc *RouteDoc) *Route
}

// HandlerFunc defines the internal handler signature returning an error handled by App.ErrorHandler.
type HandlerFunc func(ctx *Context) error

// Route holds metadata for a registered route and exposes fluent OpenAPI builders.
type Route struct {
	app           *App
	method        string
	path          string
	doc           *RouteDoc
	defaultStatus *int
}

func (r *Route) Summary(s string) *Route     { r.doc.Summary = s; r.app.invalidateSpec(); return r }
func (r *Route) Description(d string) *Route { r.doc.Description = d; r.app.invalidateSpec(); return r }
func (r *Route) OperationID(id string) *Route {
	r.doc.OperationID = id
	r.app.invalidateSpec()
	return r
}
func (r *Route) Tags(tags ...string) *Route {
	r.doc.Tags = append(r.doc.Tags, tags...)
	r.app.invalidateSpec()
	return r
}
func (r *Route) Security(schemes ...string) *Route {
	r.doc.Security = append(r.doc.Security, schemes...)
	r.app.invalidateSpec()
	return r
}
func (r *Route) Deprecated() *Route { r.doc.Deprecated = true; r.app.invalidateSpec(); return r }
func (r *Route) Hidden() *Route     { r.doc.Hidden = true; r.app.invalidateSpec(); return r }

// Status overrides the default success HTTP status code documented in OpenAPI.
// Note: the runtime handler also uses this default when ctx.Status was not set explicitly.
func (r *Route) Status(code int) *Route {
	if old, ok := r.doc.Responses[http.StatusOK]; ok && code != http.StatusOK {
		delete(r.doc.Responses, http.StatusOK)
		old.Description = http.StatusText(code)
		r.doc.Responses[code] = old
	}
	if r.defaultStatus != nil {
		*r.defaultStatus = code
	}
	r.app.invalidateSpec()
	return r
}

// ErrorResponse documents an additional HTTP error response for this route in OpenAPI.
func (r *Route) ErrorResponse(status int, description string, model ...any) *Route {
	var m any = ErrorResponse{}
	if len(model) > 0 && model[0] != nil {
		m = model[0]
	}
	r.doc.Responses[status] = &ResponseDoc{
		Model:       m,
		Description: description,
		ContentType: "application/json",
	}
	r.app.invalidateSpec()
	return r
}

// Response documents an HTTP response status and optional JSON model for OpenAPI and TypeScript.
func (r *Route) Response(status int, model any, description ...string) *Route {
	cType := ""
	if model != nil {
		cType = "application/json"
	}
	return r.ResponseMedia(status, cType, model, description...)
}

// ResponseMedia documents an HTTP response status with an explicit media content type and optional model.
func (r *Route) ResponseMedia(status int, contentType string, model any, description ...string) *Route {
	desc := http.StatusText(status)
	if len(description) > 0 && description[0] != "" {
		desc = description[0]
	}
	if status >= 200 && status < 300 && status != http.StatusOK {
		if placeholder, ok := r.doc.Responses[http.StatusOK]; ok && placeholder != nil && placeholder.Model == nil && placeholder.ContentType == "" {
			delete(r.doc.Responses, http.StatusOK)
		}
	}
	r.doc.Responses[status] = &ResponseDoc{
		Model:       model,
		Description: desc,
		ContentType: contentType,
	}
	r.app.invalidateSpec()
	return r
}

// Request documents a JSON request body model for OpenAPI and TypeScript on non-REPR routes.
func (r *Route) Request(model any, description ...string) *Route {
	if model == nil {
		r.doc.RequestBody = nil
		r.app.invalidateSpec()
		return r
	}
	desc := "Request body payload"
	if len(description) > 0 && description[0] != "" {
		desc = description[0]
	}
	r.doc.RequestBody = &BodyDoc{
		Model:       model,
		Description: desc,
		Required:    true,
		ContentType: "application/json",
	}
	r.app.invalidateSpec()
	return r
}

// Param documents a path, query, or header parameter for OpenAPI and TypeScript on non-REPR routes.
func (r *Route) Param(in, name string, model any, required bool, description ...string) *Route {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	if model == nil {
		model = ""
	}
	r.doc.Parameters = append(r.doc.Parameters, ParamDoc{
		In:          in,
		Name:        name,
		Model:       model,
		Description: desc,
		Required:    required || in == "path",
	})
	r.app.invalidateSpec()
	return r
}

// WrapEndpoint converts a typed REPR EndpointFunc into a HandlerFunc + RouteDoc.
func WrapEndpoint[Req any, Res any](handler EndpointFunc[Req, Res], defaultStatus int) (HandlerFunc, *RouteDoc, *int) {
	reqType := reflect.TypeOf(*new(Req))
	if err := checkStructTags(reqType, make(map[reflect.Type]bool)); err != nil {
		panic(fmt.Sprintf("repr: invalid tag in %T: %v", *new(Req), err))
	}

	compileBindingPlan(reqType)
	doc := introspectEndpoint[Req, Res](defaultStatus)
	resHasHeaders := hasResponseHeaders(reflect.TypeOf(*new(Res)))
	statusPtr := new(int)
	*statusPtr = defaultStatus

	h := func(ctx *Context) error {
		var req Req
		if !isEmptyType(reflect.TypeOf(req)) {
			if err := bindRequest(ctx, &req); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					return ctx.Status(http.StatusRequestEntityTooLarge).JSON(ErrorResponse{
						Error:     "request body too large",
						Status:    http.StatusRequestEntityTooLarge,
						RequestID: ctx.RequestID(),
					})
				}
				return ctx.Status(http.StatusBadRequest).JSON(ErrorResponse{
					Error:     "invalid request: " + err.Error(),
					Status:    http.StatusBadRequest,
					RequestID: ctx.RequestID(),
				})
			}

			if valErr := ValidateStruct(ctx, &req); valErr != nil {
				return ctx.Status(valErr.Status).JSON(valErr)
			}
		}

		res, err := handler(ctx, req)
		if err != nil {
			return err
		}

		if streamer, ok := any(res).(Streamer); ok {
			_ = http.NewResponseController(ctx.Writer).SetWriteDeadline(time.Time{})
			return streamer.Stream(ctx)
		}

		if !ctx.Written() {
			if resHasHeaders {
				writeResponseHeaders(ctx, reflect.ValueOf(res))
			}
			if isEmptyType(reflect.TypeOf(res)) {
				return ctx.NoContent()
			}
			status := *statusPtr
			if ctx.StatusExplicit() {
				status = ctx.StatusCode()
			}
			return ctx.Status(status).JSON(res)
		}
		return nil
	}

	return h, doc, statusPtr
}

func registerEndpoint[Req any, Res any](target RouterTarget, method, path string, handler EndpointFunc[Req, Res]) *Route {
	h, doc, statusPtr := WrapEndpoint(handler, http.StatusOK)
	rt := target.registerRoute(method, path, h, doc)
	rt.defaultStatus = statusPtr
	checkRoutePathParams[Req](method, rt.path, doc.Parameters)
	return rt
}

// Get registers a typed GET REPR endpoint.
func Get[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	return registerEndpoint(target, http.MethodGet, path, handler)
}

// Post registers a typed POST REPR endpoint.
func Post[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	return registerEndpoint(target, http.MethodPost, path, handler)
}

// Put registers a typed PUT REPR endpoint.
func Put[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	return registerEndpoint(target, http.MethodPut, path, handler)
}

// Delete registers a typed DELETE REPR endpoint.
func Delete[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	return registerEndpoint(target, http.MethodDelete, path, handler)
}

// Patch registers a typed PATCH REPR endpoint.
func Patch[Req any, Res any](target RouterTarget, path string, handler EndpointFunc[Req, Res]) *Route {
	return registerEndpoint(target, http.MethodPatch, path, handler)
}

// HandleEndpoint registers a struct-based REPR endpoint implementing Endpoint[Req, Res].
func HandleEndpoint[Req any, Res any](target RouterTarget, method, path string, ep Endpoint[Req, Res]) *Route {
	return registerEndpoint(target, method, path, ep.Handle)
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
		if ft, ok := embeddedStructType(f); ok {
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

func writeResponseHeaders(ctx *Context, val reflect.Value) {
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
		if _, ok := embeddedStructType(f); ok {
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

func hasRequiredRule(tag string) bool {
	for _, rule := range strings.Split(tag, ",") {
		if strings.TrimSpace(rule) == "required" {
			return true
		}
	}
	return false
}

func introspectEndpoint[Req any, Res any](defaultStatus int) *RouteDoc {
	doc := &RouteDoc{
		Responses: make(map[int]*ResponseDoc),
	}

	reqType := reflect.TypeOf(*new(Req))
	for reqType != nil && reqType.Kind() == reflect.Pointer {
		reqType = reqType.Elem()
	}

	var hasBody, hasForm, hasFile bool
	if reqType != nil && !isEmptyType(reqType) && reqType.Kind() == reflect.Struct {
		inspectRequestStruct(reqType, &doc.Parameters, &hasBody, &hasForm, &hasFile, nil, map[reflect.Type]bool{})
		if hasBody {
			cType := "application/json"
			if hasFile {
				cType = "multipart/form-data"
			} else if hasForm {
				cType = "application/x-www-form-urlencoded"
			}
			doc.RequestBody = &BodyDoc{
				Model:       *new(Req),
				Description: "Request body payload",
				Required:    true,
				ContentType: cType,
			}
		}
	}

	resType := reflect.TypeOf(*new(Res))
	if isEmptyType(resType) {
		doc.Responses[http.StatusNoContent] = &ResponseDoc{Description: "No Content"}
	} else if resType != nil && (resType.Implements(streamerType) || reflect.PointerTo(resType).Implements(streamerType)) {
		cType := "application/octet-stream"
		if resType == sseStreamType || resType == sseStreamPtrType {
			cType = "text/event-stream"
		}
		doc.Responses[defaultStatus] = &ResponseDoc{
			Description: http.StatusText(defaultStatus),
			ContentType: cType,
		}
	} else {
		doc.Responses[defaultStatus] = &ResponseDoc{
			Model:       *new(Res),
			Description: http.StatusText(defaultStatus),
			ContentType: "application/json",
		}
	}

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

func inspectRequestStruct(t reflect.Type, params *[]ParamDoc, hasBody, hasForm, hasFile *bool, shadowed map[string]bool, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	merged := make(map[string]bool, len(shadowed)+t.NumField()*3)
	for k, v := range shadowed {
		merged[k] = v
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if _, ok := embeddedStructType(f); ok {
			continue
		}
		for _, tag := range [...]string{"param", "query", "header"} {
			if v := f.Tag.Get(tag); v != "" {
				merged[tag+":"+v] = true
			}
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")

		if ft, ok := embeddedStructType(field); ok {
			if jsonTag != "-" && formTag != "-" {
				inspectRequestStruct(ft, params, hasBody, hasForm, hasFile, merged, seen)
			}
			continue
		}
		if !field.IsExported() {
			continue
		}

		var ex, def any
		if exTag := field.Tag.Get("example"); exTag != "" {
			ex = parseExample(exTag, field.Type)
		}
		if defTag := field.Tag.Get("default"); defTag != "" {
			def = parseExample(defTag, field.Type)
		}
		fmtTag := field.Tag.Get("format")
		valTag := field.Tag.Get("validate")

		skipBody := (jsonTag == "" || jsonTag == "-") && (formTag == "" || formTag == "-")
		if paramTag := field.Tag.Get("param"); paramTag != "" {
			if !shadowed["param:"+paramTag] {
				*params = append(*params, ParamDoc{
					In: "path", Name: paramTag, Model: reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"), Required: true,
					Example: ex, Default: def, Format: fmtTag, Validate: valTag,
				})
			}
			if skipBody {
				continue
			}
		}
		if queryTag := field.Tag.Get("query"); queryTag != "" {
			if !shadowed["query:"+queryTag] {
				*params = append(*params, ParamDoc{
					In: "query", Name: queryTag, Model: reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"), Required: hasRequiredRule(valTag),
					Example: ex, Default: def, Format: fmtTag, Validate: valTag,
				})
			}
			if skipBody {
				continue
			}
		}
		if headerTag := field.Tag.Get("header"); headerTag != "" {
			if !shadowed["header:"+headerTag] {
				*params = append(*params, ParamDoc{
					In: "header", Name: headerTag, Model: reflect.New(field.Type).Elem().Interface(),
					Description: field.Tag.Get("doc"), Required: hasRequiredRule(valTag),
					Example: ex, Default: def, Format: fmtTag, Validate: valTag,
				})
			}
			if skipBody {
				continue
			}
		}

		if jsonTag == "-" || formTag == "-" {
			continue
		}
		if formTag != "" {
			*hasForm = true
			if field.Type == fileHeaderPtrType || field.Type == fileHeaderSliceType {
				*hasFile = true
			}
		}
		*hasBody = true
	}
}

func checkRoutePathParams[Req any](method, fullPath string, params []ParamDoc) {
	routeParams := make(map[string]bool)
	for _, pName := range extractPathParams(fullPath) {
		routeParams[pName] = true
	}
	for _, p := range params {
		if p.In == "path" && !routeParams[p.Name] {
			panic(fmt.Sprintf("repr: %s %q: struct %T has param:%q tag, but route path does not contain {%s}", method, fullPath, *new(Req), p.Name, p.Name))
		}
	}
}
