package core

import (
	"fmt"
	"net/http"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RouteDoc holds OpenAPI metadata for an individual route.
type RouteDoc struct {
	Summary     string
	Description string
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

// OpenAPISpec represents the root OpenAPI 3.1.0 document.
type OpenAPISpec struct {
	OpenAPI    string                          `json:"openapi"`
	Info       OpenAPIInfo                     `json:"info"`
	Servers    []OpenAPIServer                 `json:"servers,omitempty"`
	Paths      map[string]map[string]OpenAPIOp `json:"paths"`
	Components OpenAPIComponents               `json:"components,omitempty"`
}

type OpenAPIInfo struct {
	Title          string `json:"title"`
	Version        string `json:"version"`
	Description    string `json:"description,omitempty"`
	TermsOfService string `json:"termsOfService,omitempty"`
}

type OpenAPIServer struct {
	URL         string `json:"url"`
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

// OpenAPIGenerator converts registered routes into an OpenAPI 3.1 schema.
type OpenAPIGenerator struct {
	config          Config
	schemas         map[string]map[string]any
	schemaTypes     map[string]reflect.Type
	securitySchemes map[string]OpenAPISecurityScheme
}

func NewOpenAPIGenerator(cfg Config) *OpenAPIGenerator {
	secSchemes := make(map[string]OpenAPISecurityScheme, len(cfg.SecuritySchemes))
	for k, v := range cfg.SecuritySchemes {
		secSchemes[k] = v
	}
	return &OpenAPIGenerator{
		config:          cfg,
		schemas:         make(map[string]map[string]any),
		schemaTypes:     make(map[string]reflect.Type),
		securitySchemes: secSchemes,
	}
}

var (
	schemaNamePathPrefix = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)
	schemaNamePkgQual    = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)
	schemaNameInvalid    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// sanitizeSchemaName makes a Go type name usable as a components/schemas key, which OpenAPI
// restricts to [A-Za-z0-9._-]. Generic instantiations such as
// "Page[github.com/acme/api/model.User]" become "Page_User"; a "/" would also break $ref
// resolution because it is the JSON Pointer separator.
func sanitizeSchemaName(name string) string {
	name = schemaNamePathPrefix.ReplaceAllString(name, "")
	name = schemaNamePkgQual.ReplaceAllString(name, "")
	name = schemaNameInvalid.ReplaceAllString(name, "_")
	return strings.Trim(name, "_")
}

// schemaName returns the components/schemas key for t. The plain (sanitized) type name is
// kept unless a different type already uses it (same name in another package, or generic
// instantiations that sanitize to the same name), in which case the package name is
// prefixed and, as a last resort, a numeric suffix is added so two schemas never overwrite
// each other.
func (g *OpenAPIGenerator) schemaName(t reflect.Type) string {
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

func (g *OpenAPIGenerator) Generate(routes []*Node) *OpenAPISpec {
	spec := &OpenAPISpec{
		OpenAPI: "3.1.0",
		Info: OpenAPIInfo{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
		},
		Paths: make(map[string]map[string]OpenAPIOp),
	}

	for _, s := range g.config.Servers {
		spec.Servers = append(spec.Servers, OpenAPIServer{URL: s.URL, Description: s.Description})
	}

	for _, rt := range routes {
		if rt.Doc != nil && rt.Doc.Hidden {
			continue
		}

		openAPIPath := toOpenAPIPath(rt.FullPath)
		if _, exists := spec.Paths[openAPIPath]; !exists {
			spec.Paths[openAPIPath] = make(map[string]OpenAPIOp)
		}

		op := g.buildOperation(rt)
		methodLower := strings.ToLower(rt.Method)
		spec.Paths[openAPIPath][methodLower] = op
	}

	if len(g.schemas) > 0 || len(g.securitySchemes) > 0 {
		spec.Components = OpenAPIComponents{
			Schemas:         g.schemas,
			SecuritySchemes: g.securitySchemes,
		}
	}

	return spec
}

func toOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if name, ok := pathParamName(part); ok {
			parts[i] = "{" + name + "}"
		}
	}
	res := strings.Join(parts, "/")
	if res == "" {
		return "/"
	}
	return res
}

func (g *OpenAPIGenerator) ensureSecurityScheme(scheme string) {
	if _, exists := g.securitySchemes[scheme]; exists {
		return
	}
	switch scheme {
	case "BearerAuth":
		g.securitySchemes[scheme] = OpenAPISecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		}
	case "BasicAuth":
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

func (g *OpenAPIGenerator) buildOperation(rt *Node) OpenAPIOp {
	doc := rt.Doc
	if doc == nil {
		doc = &RouteDoc{Responses: make(map[int]*ResponseDoc)}
	}

	op := OpenAPIOp{
		Summary:     doc.Summary,
		Description: doc.Description,
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

	// 1. Path parameters from route URL (:id)
	pathParams := extractPathParams(rt.FullPath)
	documentedParams := make(map[string]bool)

	for _, p := range doc.Parameters {
		documentedParams[p.Name] = true
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
		param := OpenAPIParam{
			Name:        p.Name,
			In:          p.In,
			Description: p.Description,
			Required:    p.Required || p.In == "path",
			Example:     p.Example,
			Schema:      pSchema,
		}
		op.Parameters = append(op.Parameters, param)
	}

	for _, pName := range pathParams {
		if !documentedParams[pName] {
			op.Parameters = append(op.Parameters, OpenAPIParam{
				Name:        pName,
				In:          "path",
				Required:    true,
				Description: fmt.Sprintf("Path parameter %s", pName),
				Schema:      map[string]any{"type": "string"},
			})
		}
	}

	// 2. Request body
	if doc.RequestBody != nil && doc.RequestBody.Model != nil {
		contentType := doc.RequestBody.ContentType
		if contentType == "" {
			contentType = "application/json"
		}

		schema := g.registerOrRef(doc.RequestBody.Model)
		op.RequestBody = &OpenAPIRequestBody{
			Description: doc.RequestBody.Description,
			Required:    doc.RequestBody.Required,
			Content: map[string]OpenAPIMediaType{
				contentType: {Schema: schema},
			},
		}
	}

	// 3. Responses
	for status, respDoc := range doc.Responses {
		statusStr := strconv.Itoa(status)
		description := respDoc.Description
		if description == "" {
			description = http.StatusText(status)
		}

		openAPIResp := OpenAPIResponse{
			Description: description,
		}

		if respDoc.Model != nil {
			cType := respDoc.ContentType
			if cType == "" {
				cType = "application/json"
			}
			if hdrs := g.extractResponseHeaders(reflect.TypeOf(respDoc.Model), map[reflect.Type]bool{}); len(hdrs) > 0 {
				openAPIResp.Headers = hdrs
			}
			schema := g.registerOrRef(respDoc.Model)
			openAPIResp.Content = map[string]OpenAPIMediaType{
				cType: {Schema: schema},
			}
		}

		op.Responses[statusStr] = openAPIResp
	}

	if len(op.Responses) == 0 {
		op.Responses["200"] = OpenAPIResponse{Description: "OK"}
	}

	return op
}

func extractPathParams(path string) []string {
	var params []string
	for _, p := range strings.Split(path, "/") {
		if name, ok := pathParamName(p); ok {
			params = append(params, name)
		}
	}
	return params
}

func pathParamName(segment string) (string, bool) {
	if strings.HasPrefix(segment, ":") {
		return segment[1:], true
	}
	if strings.HasPrefix(segment, "*") {
		if len(segment) > 1 {
			return segment[1:], true
		}
		return "wildcard", true
	}
	return "", false
}

func (g *OpenAPIGenerator) extractResponseHeaders(t reflect.Type, embedding map[reflect.Type]bool) map[string]OpenAPIHeader {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || embedding[t] {
		return nil
	}
	embedding[t] = true
	defer delete(embedding, t)

	var headers map[string]OpenAPIHeader
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if ft, ok := EmbeddedStructType(f); ok {
			for k, v := range g.extractResponseHeaders(ft, embedding) {
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
			ex = ParseExample(exTag, f.Type)
		}
		headers[hdrName] = OpenAPIHeader{
			Description: f.Tag.Get("doc"),
			Schema:      g.schemaForType(f.Type),
			Example:     ex,
		}
	}
	return headers
}

func (g *OpenAPIGenerator) registerOrRef(model any) map[string]any {
	if model == nil {
		return map[string]any{"type": "object"}
	}

	var t reflect.Type
	if rt, ok := model.(reflect.Type); ok {
		t = rt
	} else {
		t = reflect.TypeOf(model)
	}

	return g.schemaForType(t)
}

func (g *OpenAPIGenerator) structToSchema(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	required := make(map[string]bool)
	g.collectStructFields(t, properties, required, map[reflect.Type]bool{})

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		names := make([]string, 0, len(required))
		for name := range required {
			names = append(names, name)
		}
		sort.Strings(names)
		schema["required"] = names
	}
	return schema
}

// collectStructFields adds the documented fields of t to properties/required. Untagged
// embedded structs are flattened like encoding/json promotes them: fields declared on the
// outer struct win over promoted ones, whatever their declaration order. embedding tracks
// the current embedding chain so self-embedding through pointers cannot recurse forever.
func (g *OpenAPIGenerator) collectStructFields(t reflect.Type, properties map[string]any, required map[string]bool, embedding map[reflect.Type]bool) {
	if embedding[t] {
		return
	}
	embedding[t] = true
	defer delete(embedding, t)

	// Promoted fields are collected apart so the outer struct's own fields always win.
	promoted := make(map[string]any)
	promotedRequired := make(map[string]bool)
	own := make(map[string]bool)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")
		if jsonTag == "-" || formTag == "-" {
			continue
		}

		if ft, ok := EmbeddedStructType(field); ok {
			sub := make(map[string]any)
			subRequired := make(map[string]bool)
			g.collectStructFields(ft, sub, subRequired, embedding)
			for name, schema := range sub {
				if _, exists := promoted[name]; !exists {
					promoted[name] = schema
					if subRequired[name] {
						promotedRequired[name] = true
					}
				}
			}
			continue
		}

		if !field.IsExported() {
			continue
		}

		paramTag := field.Tag.Get("param")
		queryTag := field.Tag.Get("query")
		headerTag := field.Tag.Get("header")
		if (paramTag != "" || queryTag != "" || headerTag != "") && jsonTag == "" && formTag == "" {
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
			fieldSchema["example"] = ParseExample(exTag, field.Type)
		}
		if formatTag := field.Tag.Get("format"); formatTag != "" {
			fieldSchema["format"] = formatTag
		}
		if defTag := field.Tag.Get("default"); defTag != "" {
			fieldSchema["default"] = ParseExample(defTag, field.Type)
		}

		valTag := field.Tag.Get("validate")
		isRequired := false
		for _, rule := range splitOpenAPIRules(valTag) {
			if strings.TrimSpace(rule) == "required" {
				isRequired = true
				break
			}
		}
		if valTag != "" {
			applyValidationRulesToSchema(fieldSchema, field.Type, valTag)
		}

		properties[name] = fieldSchema
		own[name] = true
		if isRequired {
			required[name] = true
		} else {
			delete(required, name)
		}
	}

	for name, schema := range promoted {
		if own[name] {
			continue
		}
		properties[name] = schema
		if promotedRequired[name] {
			required[name] = true
		}
	}
}

func (g *OpenAPIGenerator) schemaForType(t reflect.Type) map[string]any {
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
			// []byte is encoded by encoding/json as a base64 string.
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{
			"type":  "array",
			"items": g.schemaForType(t.Elem()),
		}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Map:
		prop := any(true)
		if t.Elem().Kind() != reflect.Interface {
			prop = g.schemaForType(t.Elem())
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": prop,
		}
	case reflect.Struct:
		typeName := t.Name()
		if typeName == "" || strings.HasPrefix(typeName, "struct {") {
			return g.structToSchema(t)
		}

		typeName = g.schemaName(t)
		if _, exists := g.schemas[typeName]; !exists {
			placeholder := map[string]any{"type": "object"}
			g.schemas[typeName] = placeholder
			built := g.structToSchema(t)
			for k, v := range built {
				placeholder[k] = v
			}
		}

		return map[string]any{
			"$ref": "#/components/schemas/" + typeName,
		}
	default:
		return map[string]any{"type": "string"}
	}
}

// ParseExample converts a string tag value (such as `example` or `default`) into a typed
// scalar value matching t when possible, falling back to the raw string.
func ParseExample(val string, t reflect.Type) any {
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

func isKnownOpenAPIRule(s string) bool {
	if s == "required" || s == "email" || s == "url" || s == "uuid" || s == "nowarn" || s == "omitempty" {
		return true
	}
	parts := strings.SplitN(s, "=", 2)
	switch parts[0] {
	case "min", "max", "len", "oneof":
		return true
	default:
		return false
	}
}

func splitOpenAPIRules(tag string) []string {
	var rules []string
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(rules) > 0 && strings.HasPrefix(rules[len(rules)-1], "oneof=") && !isKnownOpenAPIRule(part) {
			rules[len(rules)-1] += "," + part
		} else {
			rules = append(rules, part)
		}
	}
	return rules
}

func parseNumericConstraint(param string, t reflect.Type) (any, bool) {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if i, err := strconv.Atoi(param); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(param, 64); err == nil {
			return f, true
		}
	case reflect.Float32, reflect.Float64:
		if f, err := strconv.ParseFloat(param, 64); err == nil {
			return f, true
		}
	}
	return nil, false
}

// applyValidationRulesToSchema enriches an OpenAPI property or parameter schema with constraints
// derived from the struct field's `validate` tag (`min`, `max`, `len`, `oneof`, `email`, `url`, `uuid`).
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

	for _, rule := range splitOpenAPIRules(valTag) {
		parts := strings.SplitN(rule, "=", 2)
		ruleName := parts[0]
		ruleParam := ""
		if len(parts) > 1 {
			ruleParam = parts[1]
		}

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
				if v, ok := parseNumericConstraint(ruleParam, t); ok {
					schema["minimum"] = v
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
				if v, ok := parseNumericConstraint(ruleParam, t); ok {
					schema["maximum"] = v
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
			var rawOpts []string
			if strings.Contains(ruleParam, ",") {
				rawOpts = strings.Split(ruleParam, ",")
			} else {
				rawOpts = strings.Fields(ruleParam)
			}
			enumVals := make([]any, 0, len(rawOpts))
			for _, opt := range rawOpts {
				opt = strings.TrimSpace(opt)
				if opt != "" {
					enumVals = append(enumVals, ParseExample(opt, t))
				}
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
