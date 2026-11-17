package repr

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// RouteDoc holds OpenAPI metadata for an individual route.
type RouteDoc struct {
	Summary     string
	Description string
	OperationID string
	Tags        []string
	Deprecated  bool
	Hidden      bool
	Security    []string
	RequestBody *BodyDoc
	Responses   map[int]*ResponseDoc
	Parameters  []ParamDoc
}

// BodyDoc defines metadata for an HTTP request body.
type BodyDoc struct {
	Model       any
	Description string
	Required    bool
	ContentType string
}

// ResponseDoc defines metadata for an HTTP response.
type ResponseDoc struct {
	Model       any
	Description string
	ContentType string
}

// ParamDoc defines metadata for path, query, or header parameters.
type ParamDoc struct {
	In          string // "path", "query", "header"
	Name        string
	Model       any
	Description string
	Required    bool
	Example     any
	Default     any
	Format      string
	Validate    string
}

// OpenAPISecurityScheme defines an OpenAPI 3.1 security scheme in components.securitySchemes.
type OpenAPISecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
	Name         string `json:"name,omitempty"`
	In           string `json:"in,omitempty"`
}

// OpenAPIServer defines an OpenAPI server entry.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// OpenAPISpec represents an OpenAPI 3.1.0 document.
type OpenAPISpec struct {
	OpenAPI    string                          `json:"openapi"`
	Info       OpenAPIInfo                     `json:"info"`
	Servers    []OpenAPIServer                 `json:"servers,omitempty"`
	Paths      map[string]map[string]OpenAPIOp `json:"paths"`
	Components OpenAPIComponents               `json:"components,omitempty"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type OpenAPIOp struct {
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	OperationID string                     `json:"operationId,omitempty"`
	Tags        []string                   `json:"tags,omitempty"`
	Security    []map[string][]string      `json:"security,omitempty"`
	Parameters  []OpenAPIParam             `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `json:"responses"`
	Deprecated  bool                       `json:"deprecated,omitempty"`
}

type OpenAPIParam struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
	Example     any            `json:"example,omitempty"`
}

type OpenAPIRequestBody struct {
	Description string                      `json:"description,omitempty"`
	Required    bool                        `json:"required,omitempty"`
	Content     map[string]OpenAPIMediaType `json:"content"`
}

type OpenAPIHeader struct {
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
	Example     any            `json:"example,omitempty"`
}

type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Headers     map[string]OpenAPIHeader    `json:"headers,omitempty"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

type OpenAPIMediaType struct {
	Schema  map[string]any `json:"schema,omitempty"`
	Example any            `json:"example,omitempty"`
}

type OpenAPIComponents struct {
	Schemas         map[string]map[string]any        `json:"schemas,omitempty"`
	SecuritySchemes map[string]OpenAPISecurityScheme `json:"securitySchemes,omitempty"`
}

var (
	schemaNamePathPrefix = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)
	schemaNamePkgQual    = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)
	schemaNameInvalid    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

type openAPIGenerator struct {
	config          Config
	schemas         map[string]map[string]any
	schemaTypes     map[string]reflect.Type
	securitySchemes map[string]OpenAPISecurityScheme
}

func newOpenAPIGenerator(cfg Config) *openAPIGenerator {
	sec := make(map[string]OpenAPISecurityScheme, len(cfg.SecuritySchemes))
	maps.Copy(sec, cfg.SecuritySchemes)
	return &openAPIGenerator{
		config:          cfg,
		schemas:         make(map[string]map[string]any),
		schemaTypes:     make(map[string]reflect.Type),
		securitySchemes: sec,
	}
}

func sanitizeSchemaName(name string) string {
	name = schemaNamePathPrefix.ReplaceAllString(name, "")
	name = schemaNamePkgQual.ReplaceAllString(name, "")
	name = schemaNameInvalid.ReplaceAllString(name, "_")
	return strings.Trim(name, "_")
}

func (g *openAPIGenerator) schemaName(t reflect.Type) string {
	name := sanitizeSchemaName(t.Name())
	if prev, taken := g.schemaTypes[name]; taken && prev != t {
		name = sanitizeSchemaName(path.Base(t.PkgPath()) + "_" + t.Name())
	}
	candidate := name
	for i := 2; ; i++ {
		prev, taken := g.schemaTypes[name]
		if !taken || prev == t {
			break
		}
		name = fmt.Sprintf("%s_%d", candidate, i)
	}
	g.schemaTypes[name] = t
	return name
}

func (g *openAPIGenerator) generate(routes []*Route) *OpenAPISpec {
	spec := &OpenAPISpec{
		OpenAPI: "3.1.0",
		Info: OpenAPIInfo{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
		},
		Servers: g.config.Servers,
		Paths:   make(map[string]map[string]OpenAPIOp),
	}

	for _, rt := range routes {
		if rt.doc != nil && rt.doc.Hidden {
			continue
		}
		oapiPath := toOpenAPIPath(rt.path)
		if _, ok := spec.Paths[oapiPath]; !ok {
			spec.Paths[oapiPath] = make(map[string]OpenAPIOp)
		}
		spec.Paths[oapiPath][strings.ToLower(rt.method)] = g.buildOperation(rt)
	}

	if len(g.schemas) > 0 || len(g.securitySchemes) > 0 {
		spec.Components = OpenAPIComponents{
			Schemas:         g.schemas,
			SecuritySchemes: g.securitySchemes,
		}
	}
	return spec
}

func pathParamName(segment string) (string, bool) {
	if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(segment) > 2 {
		inner := segment[1 : len(segment)-1]
		name, _, _ := strings.Cut(inner, ":")
		return name, name != ""
	}
	return "", false
}

func toOpenAPIPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if name, ok := pathParamName(seg); ok {
			parts[i] = "{" + name + "}"
		}
	}
	res := strings.Join(parts, "/")
	if res == "" {
		return "/"
	}
	return res
}

func extractPathParams(p string) []string {
	var params []string
	for seg := range strings.SplitSeq(p, "/") {
		if name, ok := pathParamName(seg); ok {
			params = append(params, name)
		}
	}
	return params
}

func (g *openAPIGenerator) ensureSecurityScheme(scheme string) {
	if _, exists := g.securitySchemes[scheme]; exists {
		return
	}
	switch scheme {
	case "BearerAuth", "bearerAuth":
		g.securitySchemes[scheme] = OpenAPISecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		}
	case "BasicAuth", "basicAuth":
		g.securitySchemes[scheme] = OpenAPISecurityScheme{
			Type:   "http",
			Scheme: "basic",
		}
	default:
		g.securitySchemes[scheme] = OpenAPISecurityScheme{
			Type:   "http",
			Scheme: "bearer",
		}
	}
}

func (g *openAPIGenerator) buildOperation(rt *Route) OpenAPIOp {
	doc := rt.doc
	if doc == nil {
		doc = &RouteDoc{Responses: make(map[int]*ResponseDoc)}
	}

	op := OpenAPIOp{
		Summary:     doc.Summary,
		Description: doc.Description,
		OperationID: doc.OperationID,
		Tags:        doc.Tags,
		Deprecated:  doc.Deprecated,
		Responses:   make(map[string]OpenAPIResponse),
	}

	if len(doc.Security) > 0 {
		seen := make(map[string]bool, len(doc.Security))
		for _, scheme := range doc.Security {
			if scheme == "" || seen[scheme] {
				continue
			}
			seen[scheme] = true
			g.ensureSecurityScheme(scheme)
			op.Security = append(op.Security, map[string][]string{scheme: {}})
		}
	}

	documented := make(map[string]bool)
	for _, p := range doc.Parameters {
		documented[p.Name] = true
		pType := reflect.TypeOf(p.Model)
		pSchema := g.schemaForType(pType)
		if p.Format != "" {
			pSchema["format"] = p.Format
		}
		if p.Default != nil {
			pSchema["default"] = p.Default
		}
		if p.Validate != "" {
			applyValidationRulesToSchema(pSchema, pType, p.Validate)
		}
		op.Parameters = append(op.Parameters, OpenAPIParam{
			Name:        p.Name,
			In:          p.In,
			Description: p.Description,
			Required:    p.Required || p.In == "path",
			Example:     p.Example,
			Schema:      pSchema,
		})
	}

	for _, pName := range extractPathParams(rt.path) {
		if !documented[pName] {
			op.Parameters = append(op.Parameters, OpenAPIParam{
				Name:        pName,
				In:          "path",
				Required:    true,
				Description: fmt.Sprintf("Path parameter %s", pName),
				Schema:      map[string]any{"type": "string"},
			})
		}
	}

	if doc.RequestBody != nil && doc.RequestBody.Model != nil {
		cType := cmp.Or(doc.RequestBody.ContentType, "application/json")
		op.RequestBody = &OpenAPIRequestBody{
			Description: doc.RequestBody.Description,
			Required:    doc.RequestBody.Required,
			Content: map[string]OpenAPIMediaType{
				cType: {Schema: g.registerOrRef(doc.RequestBody.Model)},
			},
		}
	}

	for status, respDoc := range doc.Responses {
		desc := respDoc.Description
		if desc == "" {
			desc = http.StatusText(status)
		}
		resp := OpenAPIResponse{Description: desc}
		if respDoc.Model != nil {
			cType := cmp.Or(respDoc.ContentType, "application/json")
			if hdrs := g.extractResponseHeaders(reflect.TypeOf(respDoc.Model), map[reflect.Type]bool{}); len(hdrs) > 0 {
				resp.Headers = hdrs
			}
			resp.Content = map[string]OpenAPIMediaType{
				cType: {Schema: g.registerOrRef(respDoc.Model)},
			}
		} else if respDoc.ContentType != "" && status != http.StatusNoContent && status != http.StatusResetContent {
			schema := map[string]any{}
			if strings.HasPrefix(respDoc.ContentType, "text/") {
				schema["type"] = "string"
			}
			resp.Content = map[string]OpenAPIMediaType{
				respDoc.ContentType: {Schema: schema},
			}
		}
		op.Responses[strconv.Itoa(status)] = resp
	}

	if len(op.Responses) == 0 {
		op.Responses["200"] = OpenAPIResponse{Description: "OK"}
	}
	return op
}

func (g *openAPIGenerator) extractResponseHeaders(t reflect.Type, seen map[reflect.Type]bool) map[string]OpenAPIHeader {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var headers map[string]OpenAPIHeader
	for i := range t.NumField() {
		f := t.Field(i)
		if ft, ok := embeddedStructType(f); ok {
			for k, v := range g.extractResponseHeaders(ft, seen) {
				if headers == nil {
					headers = make(map[string]OpenAPIHeader)
				}
				if _, exists := headers[k]; !exists {
					headers[k] = v
				}
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		hdrName := f.Tag.Get("header")
		if hdrName == "" {
			continue
		}
		if headers == nil {
			headers = make(map[string]OpenAPIHeader)
		}
		var ex any
		if exTag := f.Tag.Get("example"); exTag != "" {
			ex = parseExample(exTag, f.Type)
		}
		headers[hdrName] = OpenAPIHeader{
			Description: f.Tag.Get("doc"),
			Schema:      g.schemaForType(f.Type),
			Example:     ex,
		}
	}
	return headers
}

func (g *openAPIGenerator) registerOrRef(model any) map[string]any {
	if model == nil {
		return map[string]any{"type": "object"}
	}
	if rt, ok := model.(reflect.Type); ok {
		return g.schemaForType(rt)
	}
	return g.schemaForType(reflect.TypeOf(model))
}

func (g *openAPIGenerator) structToSchema(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	required := make(map[string]bool)
	g.collectStructFields(t, properties, required, map[reflect.Type]bool{})

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = slices.Sorted(maps.Keys(required))
	}
	return schema
}

func (g *openAPIGenerator) collectStructFields(t reflect.Type, properties map[string]any, required map[string]bool, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	promoted := make(map[string]any)
	promotedReq := make(map[string]bool)
	own := make(map[string]bool)

	for i := range t.NumField() {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")
		if jsonTag == "-" || formTag == "-" {
			continue
		}

		if ft, ok := embeddedStructType(field); ok {
			sub := make(map[string]any)
			subReq := make(map[string]bool)
			g.collectStructFields(ft, sub, subReq, seen)
			for n, s := range sub {
				if _, exists := promoted[n]; !exists {
					promoted[n] = s
					if subReq[n] {
						promotedReq[n] = true
					}
				}
			}
			continue
		}

		if !field.IsExported() {
			continue
		}

		warnIfSensitiveField(t, field, g.config.SilenceTagWarnings)

		if (field.Tag.Get("param") != "" || field.Tag.Get("query") != "" || field.Tag.Get("header") != "") &&
			jsonTag == "" && formTag == "" {
			continue
		}

		name := field.Name
		if formTag != "" {
			name = formTag
		} else if jsonName, _, _ := strings.Cut(jsonTag, ","); jsonName != "" {
			name = jsonName
		}

		fieldSchema := g.schemaForType(field.Type)
		if docTag := field.Tag.Get("doc"); docTag != "" {
			fieldSchema["description"] = docTag
		}
		if exTag := field.Tag.Get("example"); exTag != "" {
			fieldSchema["example"] = parseExample(exTag, field.Type)
		}
		if fmtTag := field.Tag.Get("format"); fmtTag != "" {
			fieldSchema["format"] = fmtTag
		}
		if defTag := field.Tag.Get("default"); defTag != "" {
			fieldSchema["default"] = parseExample(defTag, field.Type)
		}

		valTag := field.Tag.Get("validate")
		isReq := hasRequiredRule(valTag)
		if valTag != "" {
			applyValidationRulesToSchema(fieldSchema, field.Type, valTag)
		}

		properties[name] = fieldSchema
		own[name] = true
		if isReq {
			required[name] = true
		} else {
			delete(required, name)
		}
	}

	for n, s := range promoted {
		if own[n] {
			continue
		}
		properties[n] = s
		if promotedReq[n] {
			required[n] = true
		}
	}
}

func (g *openAPIGenerator) schemaForType(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "string"}
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t == fileHeaderType {
		return map[string]any{"type": "string", "format": "binary"}
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": g.schemaForType(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Map:
		prop := any(true)
		if t.Elem().Kind() != reflect.Interface {
			prop = g.schemaForType(t.Elem())
		}
		return map[string]any{"type": "object", "additionalProperties": prop}
	case reflect.Struct:
		typeName := t.Name()
		if typeName == "" || strings.HasPrefix(typeName, "struct {") {
			return g.structToSchema(t)
		}
		typeName = g.schemaName(t)
		if _, exists := g.schemas[typeName]; !exists {
			placeholder := map[string]any{"type": "object"}
			g.schemas[typeName] = placeholder
			maps.Copy(placeholder, g.structToSchema(t))
		}
		return map[string]any{"$ref": "#/components/schemas/" + typeName}
	default:
		return map[string]any{"type": "string"}
	}
}

func parseExample(val string, t reflect.Type) any {
	if t == nil {
		return val
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if u, err := strconv.ParseUint(val, 10, 64); err == nil {
			return int(u)
		}
	case reflect.Float32, reflect.Float64:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	case reflect.Bool:
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return val
}

func applyValidationRulesToSchema(schema map[string]any, t reflect.Type, valTag string) {
	if schema == nil || valTag == "" {
		return
	}
	if _, isRef := schema["$ref"]; isRef {
		return
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	for rawRule := range strings.SplitSeq(valTag, ",") {
		rawRule = strings.TrimSpace(rawRule)
		if rawRule == "" {
			continue
		}
		ruleName, ruleParam, _ := strings.Cut(rawRule, "=")
		switch ruleName {
		case "min":
			if t == nil || ruleParam == "" {
				continue
			}
			switch t.Kind() {
			case reflect.String:
				if n, err := strconv.Atoi(ruleParam); err == nil {
					schema["minLength"] = n
				}
			case reflect.Slice, reflect.Array:
				if n, err := strconv.Atoi(ruleParam); err == nil {
					schema["minItems"] = n
				}
			default:
				if f, err := strconv.ParseFloat(ruleParam, 64); err == nil {
					schema["minimum"] = f
				}
			}
		case "max":
			if t == nil || ruleParam == "" {
				continue
			}
			switch t.Kind() {
			case reflect.String:
				if n, err := strconv.Atoi(ruleParam); err == nil {
					schema["maxLength"] = n
				}
			case reflect.Slice, reflect.Array:
				if n, err := strconv.Atoi(ruleParam); err == nil {
					schema["maxItems"] = n
				}
			default:
				if f, err := strconv.ParseFloat(ruleParam, 64); err == nil {
					schema["maximum"] = f
				}
			}
		case "len":
			if t == nil || ruleParam == "" {
				continue
			}
			if n, err := strconv.Atoi(ruleParam); err == nil {
				switch t.Kind() {
				case reflect.String:
					schema["minLength"] = n
					schema["maxLength"] = n
				case reflect.Slice, reflect.Array:
					schema["minItems"] = n
					schema["maxItems"] = n
				}
			}
		case "oneof":
			if ruleParam == "" {
				continue
			}
			opts := strings.Fields(ruleParam)
			enumVals := make([]any, 0, len(opts))
			for _, opt := range opts {
				enumVals = append(enumVals, parseExample(opt, t))
			}
			if len(enumVals) > 0 {
				schema["enum"] = enumVals
			}
		case "email":
			if _, exists := schema["format"]; !exists {
				schema["format"] = "email"
			}
		case "url":
			if _, exists := schema["format"]; !exists {
				schema["format"] = "uri"
			}
		case "uuid":
			if _, exists := schema["format"]; !exists {
				schema["format"] = "uuid"
			}
		}
	}
}
