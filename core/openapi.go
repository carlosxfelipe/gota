package core

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RouteDoc holds OpenAPI metadata for an individual route.
type RouteDoc struct {
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Hidden      bool
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

type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

type OpenAPIMediaType struct {
	Schema  map[string]any `json:"schema,omitempty"`
	Example any            `json:"example,omitempty"`
}

type OpenAPIComponents struct {
	Schemas map[string]map[string]any `json:"schemas,omitempty"`
}

// OpenAPIGenerator converts registered routes into an OpenAPI 3.1 schema.
type OpenAPIGenerator struct {
	config       Config
	schemas      map[string]map[string]any
	schemaTypes  map[string]reflect.Type
	visitedTypes map[reflect.Type]bool
}

func NewOpenAPIGenerator(cfg Config) *OpenAPIGenerator {
	return &OpenAPIGenerator{
		config:       cfg,
		schemas:      make(map[string]map[string]any),
		schemaTypes:  make(map[string]reflect.Type),
		visitedTypes: make(map[reflect.Type]bool),
	}
}

// schemaName returns the components/schemas key for t. The plain type name is kept unless
// a different type already uses it (same name in another package), in which case the
// package name is prefixed so the two schemas do not overwrite each other.
func (g *OpenAPIGenerator) schemaName(t reflect.Type) string {
	name := t.Name()
	if prev, taken := g.schemaTypes[name]; taken && prev != t {
		name = path.Base(t.PkgPath()) + "_" + t.Name()
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

	if len(g.schemas) > 0 {
		spec.Components = OpenAPIComponents{
			Schemas: g.schemas,
		}
	}

	return spec
}

func toOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		} else if strings.HasPrefix(part, "*") {
			name := "wildcard"
			if len(part) > 1 {
				name = part[1:]
			}
			parts[i] = "{" + name + "}"
		}
	}
	res := strings.Join(parts, "/")
	if res == "" {
		return "/"
	}
	return res
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

	// 1. Path parameters from route URL (:id)
	pathParams := extractPathParams(rt.FullPath)
	documentedParams := make(map[string]bool)

	for _, p := range doc.Parameters {
		documentedParams[p.Name] = true
		param := OpenAPIParam{
			Name:        p.Name,
			In:          p.In,
			Description: p.Description,
			Required:    p.Required || p.In == "path",
			Example:     p.Example,
			Schema:      g.schemaForType(reflect.TypeOf(p.Model)),
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
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ":") {
			params = append(params, p[1:])
		} else if strings.HasPrefix(p, "*") {
			name := "wildcard"
			if len(p) > 1 {
				name = p[1:]
			}
			params = append(params, name)
		}
	}
	return params
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
	if g.visitedTypes[t] {
		return map[string]any{
			"type": "object",
		}
	}
	g.visitedTypes[t] = true
	defer func() {
		delete(g.visitedTypes, t)
	}()

	properties := make(map[string]any)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")
		if jsonTag == "-" || formTag == "-" {
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
			parts := strings.Split(formTag, ",")
			if parts[0] != "" {
				name = parts[0]
			}
		} else if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				name = parts[0]
			}
		}

		fieldSchema := g.schemaForType(field.Type)

		if docTag := field.Tag.Get("doc"); docTag != "" {
			fieldSchema["description"] = docTag
		}
		if exTag := field.Tag.Get("example"); exTag != "" {
			fieldSchema["example"] = parseExample(exTag, field.Type)
		}
		if formatTag := field.Tag.Get("format"); formatTag != "" {
			fieldSchema["format"] = formatTag
		}
		if defTag := field.Tag.Get("default"); defTag != "" {
			fieldSchema["default"] = parseExample(defTag, field.Type)
		}

		valTag := field.Tag.Get("validate")
		isRequired := false
		for _, rule := range strings.Split(valTag, ",") {
			if strings.TrimSpace(rule) == "required" {
				isRequired = true
				break
			}
		}
		if isRequired {
			required = append(required, name)
		}

		properties[name] = fieldSchema
	}

	sort.Strings(required)

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (g *OpenAPIGenerator) schemaForType(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "string"}
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t == reflect.TypeOf(multipart.FileHeader{}) {
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
	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
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
