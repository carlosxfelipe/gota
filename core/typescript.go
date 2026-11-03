package core

import (
	"fmt"
	"log"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	tsSchemaNamePathPrefix = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)
	tsSchemaNamePkgQual    = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)
	tsSchemaNameInvalid    = regexp.MustCompile(`[^A-Za-z0-9_]+`)
)

// sanitizeTSTypeName ensures a Go struct type name is a valid TypeScript identifier.
func sanitizeTSTypeName(name string) string {
	name = tsSchemaNamePathPrefix.ReplaceAllString(name, "")
	name = tsSchemaNamePkgQual.ReplaceAllString(name, "")
	name = tsSchemaNameInvalid.ReplaceAllString(name, "_")
	return strings.Trim(name, "_")
}

var loggedSensitiveWarnings sync.Map

// IsSensitiveFieldName checks if an exported struct field name strongly resembles a sensitive secret
// that should generally not be exposed in JSON, TypeScript definitions, or OpenAPI contracts.
func IsSensitiveFieldName(name string) (bool, string) {
	lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	switch {
	case strings.Contains(lower, "passwordhash") || strings.Contains(lower, "passwdhash") || strings.Contains(lower, "pwdhash") || strings.Contains(lower, "passhash"):
		return true, "password hash"
	case strings.Contains(lower, "passwordsalt") || lower == "salt":
		return true, "cryptographic salt"
	case strings.Contains(lower, "clientsecret") || strings.Contains(lower, "apisecret") || strings.Contains(lower, "jwtsecret") || lower == "secret":
		return true, "secret key"
	case strings.Contains(lower, "resettoken") || strings.Contains(lower, "refreshtoken") || strings.Contains(lower, "authtoken") || strings.Contains(lower, "sessiontoken"):
		return true, "authentication/session token"
	default:
		return false, ""
	}
}

// AuditModel inspects a struct model (recursing into embedded structs) and returns warning messages
// for any exported fields that appear to expose sensitive secrets without a `json:"-"` or `form:"-"` tag.
// Useful in unit tests to ensure internal entity fields are not leaked to API contracts.
func AuditModel(model any) []string {
	if model == nil {
		return nil
	}
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var warnings []string
	auditModelRecursive(t, make(map[reflect.Type]bool), &warnings)
	return warnings
}

func auditModelRecursive(t reflect.Type, seen map[reflect.Type]bool, warnings *[]string) {
	if seen[t] {
		return
	}
	seen[t] = true

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")
		if jsonTag == "-" || formTag == "-" {
			continue
		}

		if ft, ok := EmbeddedStructType(field); ok {
			auditModelRecursive(ft, seen, warnings)
			continue
		}

		if !field.IsExported() {
			continue
		}

		if strings.Contains(field.Tag.Get("validate"), "nowarn") {
			continue
		}

		if isSens, reason := IsSensitiveFieldName(field.Name); isSens {
			*warnings = append(*warnings, fmt.Sprintf("field %q in struct %s appears to contain a %s but is not marked with `json:\"-\"`", field.Name, t.Name(), reason))
		}

		ft := field.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft != timeType && ft != fileHeaderType {
			auditModelRecursive(ft, seen, warnings)
		}
	}
}

// TypeScriptGenerator inspects registered Gota routes and types to produce TypeScript declarations and SDKs.
type TypeScriptGenerator struct {
	config      Config
	schemas     map[string]string
	schemaTypes map[string]reflect.Type
}

// NewTypeScriptGenerator initializes a new TypeScriptGenerator.
func NewTypeScriptGenerator(cfg Config) *TypeScriptGenerator {
	return &TypeScriptGenerator{
		config:      cfg,
		schemas:     make(map[string]string),
		schemaTypes: make(map[string]reflect.Type),
	}
}

func (g *TypeScriptGenerator) schemaName(t reflect.Type) string {
	name := sanitizeTSTypeName(t.Name())
	if name == "" {
		name = "AnonymousModel"
	}
	if prev, taken := g.schemaTypes[name]; taken && prev != t {
		name = sanitizeTSTypeName(path.Base(t.PkgPath()) + "_" + t.Name())
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

// parseOneOfChoices extracts string literal union choices from validate:"oneof=..." tags.
func parseOneOfChoices(validateTag string) []string {
	if validateTag == "" {
		return nil
	}
	for _, rule := range splitOpenAPIRules(validateTag) {
		if strings.HasPrefix(rule, "oneof=") {
			val := strings.TrimPrefix(rule, "oneof=")
			rawParts := strings.Fields(val)
			var choices []string
			for _, part := range rawParts {
				for _, sub := range strings.Split(part, ",") {
					sub = strings.TrimSpace(sub)
					if sub != "" {
						choices = append(choices, sub)
					}
				}
			}
			return choices
		}
	}
	return nil
}

func (g *TypeScriptGenerator) typeToTS(t reflect.Type, seen map[reflect.Type]bool) string {
	if t == nil {
		return "void"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == timeType {
		return "string"
	}
	if t == fileHeaderType {
		return "File | Blob"
	}

	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string"
		}
		if t.Elem() == fileHeaderType || reflect.PointerTo(t.Elem()) == reflect.PointerTo(fileHeaderType) {
			return "File[] | Blob[]"
		}
		elemTS := g.typeToTS(t.Elem(), seen)
		if strings.Contains(elemTS, " ") && !strings.HasPrefix(elemTS, "{") && !strings.HasPrefix(elemTS, "Record<") {
			return "(" + elemTS + ")[]"
		}
		return elemTS + "[]"
	case reflect.Map:
		valTS := g.typeToTS(t.Elem(), seen)
		return fmt.Sprintf("Record<string, %s>", valTS)
	case reflect.Interface:
		return "any"
	case reflect.Struct:
		if t == timeType {
			return "string"
		}
		if t.NumField() == 0 {
			return "Record<string, never>"
		}

		name := t.Name()
		if name == "" || strings.HasPrefix(name, "struct {") {
			return g.inlineStructTS(t, seen)
		}

		sName := g.schemaName(t)
		if _, exists := g.schemas[sName]; !exists {
			g.schemas[sName] = "" // placeholder for recursive types
			g.generateInterface(sName, t, seen)
		}
		return sName
	default:
		return "any"
	}
}

type structFieldDoc struct {
	name        string
	tsType      string
	required    bool
	description string
	example     string
}

func (g *TypeScriptGenerator) collectStructFields(t reflect.Type, fields *[]structFieldDoc, seen map[reflect.Type]bool, embedding map[reflect.Type]bool) {
	if embedding[t] {
		return
	}
	embedding[t] = true
	defer delete(embedding, t)

	own := make(map[string]bool)
	var ownFields []structFieldDoc
	var promotedFields []structFieldDoc

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		formTag := field.Tag.Get("form")
		if jsonTag == "-" || formTag == "-" {
			continue
		}

		if ft, ok := EmbeddedStructType(field); ok {
			var sub []structFieldDoc
			g.collectStructFields(ft, &sub, seen, embedding)
			promotedFields = append(promotedFields, sub...)
			continue
		}

		if !field.IsExported() {
			continue
		}

		if !g.config.SilenceTagWarnings && !strings.Contains(field.Tag.Get("validate"), "nowarn") {
			if isSens, reason := IsSensitiveFieldName(field.Name); isSens {
				warnKey := fmt.Sprintf("%s.%s", t.String(), field.Name)
				if _, loaded := loggedSensitiveWarnings.LoadOrStore(warnKey, true); !loaded {
					log.Printf("⚠️  gota [SECURITY]: field %q in struct %s appears to contain a %s but is not marked with `json:\"-\"`. It will be exposed in TypeScript and OpenAPI contracts. Add `json:\"-\"` to hide it, or silence with Config.SilenceTagWarnings.", field.Name, t.String(), reason)
				}
			}
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

		valTag := field.Tag.Get("validate")
		isRequired := false
		for _, rule := range splitOpenAPIRules(valTag) {
			if strings.TrimSpace(rule) == "required" {
				isRequired = true
				break
			}
		}

		underlyingType := field.Type
		for underlyingType.Kind() == reflect.Pointer {
			underlyingType = underlyingType.Elem()
		}

		// TypeScript type resolution
		tsType := ""
		if choices := parseOneOfChoices(valTag); len(choices) > 0 && underlyingType.Kind() == reflect.String {
			var formatted []string
			for _, c := range choices {
				formatted = append(formatted, fmt.Sprintf("%q", c))
			}
			tsType = strings.Join(formatted, " | ")
		} else {
			tsType = g.typeToTS(field.Type, seen)
		}

		own[name] = true
		ownFields = append(ownFields, structFieldDoc{
			name:        name,
			tsType:      tsType,
			required:    isRequired,
			description: field.Tag.Get("doc"),
			example:     field.Tag.Get("example"),
		})
	}

	*fields = append(*fields, ownFields...)
	for _, pf := range promotedFields {
		if !own[pf.name] {
			*fields = append(*fields, pf)
		}
	}
}

func (g *TypeScriptGenerator) inlineStructTS(t reflect.Type, seen map[reflect.Type]bool) string {
	if seen[t] {
		return "Record<string, any>"
	}
	seen[t] = true
	defer delete(seen, t)

	var fields []structFieldDoc
	g.collectStructFields(t, &fields, seen, map[reflect.Type]bool{})
	if len(fields) == 0 {
		return "Record<string, never>"
	}

	var parts []string
	for _, f := range fields {
		opt := "?"
		if f.required {
			opt = ""
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", f.name, opt, f.tsType))
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func (g *TypeScriptGenerator) generateInterface(name string, t reflect.Type, seen map[reflect.Type]bool) {
	var fields []structFieldDoc
	g.collectStructFields(t, &fields, seen, map[reflect.Type]bool{})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("export interface %s {\n", name))
	for _, f := range fields {
		if f.description != "" || f.example != "" {
			sb.WriteString("  /**\n")
			if f.description != "" {
				sb.WriteString(fmt.Sprintf("   * %s\n", f.description))
			}
			if f.example != "" {
				sb.WriteString(fmt.Sprintf("   * @example %s\n", f.example))
			}
			sb.WriteString("   */\n")
		}
		opt := "?"
		if f.required {
			opt = ""
		}
		sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", f.name, opt, f.tsType))
	}
	sb.WriteString("}\n")
	g.schemas[name] = sb.String()
}

type routeTS struct {
	method      string
	path        string
	key         string
	summary     string
	description string
	paramsTS    string
	queryTS     string
	bodyTS      string
	responseTS  string
}

func (g *TypeScriptGenerator) inspectRoute(rt *Node) routeTS {
	r := routeTS{
		method:     strings.ToUpper(rt.Method),
		path:       rt.FullPath,
		key:        fmt.Sprintf("%s %s", strings.ToUpper(rt.Method), rt.FullPath),
		paramsTS:   "void",
		queryTS:    "void",
		bodyTS:     "void",
		responseTS: "void",
	}

	if rt.Doc == nil {
		return r
	}

	r.summary = rt.Doc.Summary
	r.description = rt.Doc.Description

	// 1. Path & Query Parameters
	documentedParams := make(map[string]bool)
	var pathFields []string
	var queryFields []string
	if rt.Doc != nil {
		for _, p := range rt.Doc.Parameters {
			documentedParams[p.Name] = true
			mType := reflect.TypeOf(p.Model)
			pType := "string"
			if mType != nil {
				pType = g.typeToTS(mType, map[reflect.Type]bool{})
			}
			opt := "?"
			if p.Required || p.In == "path" {
				opt = ""
			}
			fieldStr := fmt.Sprintf("%s%s: %s", p.Name, opt, pType)
			switch p.In {
			case "path":
				pathFields = append(pathFields, fieldStr)
			case "query":
				queryFields = append(queryFields, fieldStr)
			}
		}
	}

	for _, pName := range extractPathParams(rt.FullPath) {
		if !documentedParams[pName] {
			pathFields = append(pathFields, fmt.Sprintf("%s: string", pName))
		}
	}

	if len(pathFields) > 0 {
		sort.Strings(pathFields)
		r.paramsTS = "{ " + strings.Join(pathFields, "; ") + " }"
	}
	if len(queryFields) > 0 {
		sort.Strings(queryFields)
		r.queryTS = "{ " + strings.Join(queryFields, "; ") + " }"
	}

	// 2. Request Body
	if rt.Doc.RequestBody != nil && rt.Doc.RequestBody.Model != nil {
		if rt.Doc.RequestBody.ContentType == "multipart/form-data" {
			r.bodyTS = "FormData"
		} else {
			mType := reflect.TypeOf(rt.Doc.RequestBody.Model)
			if mType != nil && mType.Kind() == reflect.Pointer {
				mType = mType.Elem()
			}
			if mType != nil && mType.Kind() == reflect.Struct && mType.NumField() == 0 {
				r.bodyTS = "void"
			} else if mType != nil {
				r.bodyTS = g.typeToTS(mType, map[reflect.Type]bool{})
			}
		}
	}

	// 3. Response
	if len(rt.Doc.Responses) > 0 {
		// Prefer 200, 201, 204 or lowest 2xx status
		var successStatuses []int
		for code := range rt.Doc.Responses {
			if code >= 200 && code < 300 {
				successStatuses = append(successStatuses, code)
			}
		}
		sort.Ints(successStatuses)

		chosenCode := 0
		if len(successStatuses) > 0 {
			chosenCode = successStatuses[0]
		}

		if chosenCode > 0 {
			resp := rt.Doc.Responses[chosenCode]
			if chosenCode == 204 || resp.Model == nil {
				r.responseTS = "void"
			} else if resp.ContentType == "text/event-stream" {
				r.responseTS = "ReadableStream<string>"
			} else {
				mType := reflect.TypeOf(resp.Model)
				if mType != nil && mType.Kind() == reflect.Pointer {
					mType = mType.Elem()
				}
				if mType != nil && mType.Kind() == reflect.Struct && mType.NumField() == 0 {
					r.responseTS = "void"
				} else if mType != nil {
					r.responseTS = g.typeToTS(mType, map[reflect.Type]bool{})
				}
			}
		}
	}

	return r
}

// Generate produces full TypeScript type definitions (.d.ts) for all registered routes.
func (g *TypeScriptGenerator) Generate(routes []*Node) string {
	var routeItems []routeTS
	for _, rt := range routes {
		if rt.auto || (rt.Doc != nil && rt.Doc.Hidden) {
			continue
		}
		routeItems = append(routeItems, g.inspectRoute(rt))
	}

	sort.Slice(routeItems, func(i, j int) bool {
		return routeItems[i].key < routeItems[j].key
	})

	var sb strings.Builder
	sb.WriteString("/**\n")
	sb.WriteString(" * Auto-generated by Gota Framework \U0001F4A7\n")
	sb.WriteString(fmt.Sprintf(" * Application: %s (%s)\n", g.config.Title, g.config.Version))
	if g.config.Description != "" {
		sb.WriteString(fmt.Sprintf(" * Description: %s\n", g.config.Description))
	}
	sb.WriteString(" * Generated at runtime.\n")
	sb.WriteString(" */\n\n")

	// 1. Schemas/Models
	var schemaNames []string
	for name := range g.schemas {
		schemaNames = append(schemaNames, name)
	}
	sort.Strings(schemaNames)
	for _, name := range schemaNames {
		content := g.schemas[name]
		if content != "" {
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}

	// 2. Endpoint Map
	sb.WriteString("export interface GotaEndpoints {\n")
	for _, r := range routeItems {
		if r.summary != "" || r.description != "" {
			sb.WriteString("  /**\n")
			if r.summary != "" {
				sb.WriteString(fmt.Sprintf("   * %s\n", r.summary))
			}
			if r.description != "" {
				sb.WriteString(fmt.Sprintf("   * %s\n", r.description))
			}
			sb.WriteString("   */\n")
		}
		sb.WriteString(fmt.Sprintf("  %q: {\n", r.key))
		sb.WriteString(fmt.Sprintf("    method: %q;\n", r.method))
		sb.WriteString(fmt.Sprintf("    path: %q;\n", r.path))
		sb.WriteString(fmt.Sprintf("    params: %s;\n", r.paramsTS))
		sb.WriteString(fmt.Sprintf("    query: %s;\n", r.queryTS))
		sb.WriteString(fmt.Sprintf("    body: %s;\n", r.bodyTS))
		sb.WriteString(fmt.Sprintf("    response: %s;\n", r.responseTS))
		sb.WriteString("  };\n")
	}
	sb.WriteString("}\n\n")

	// 3. Client and Helper Type Declarations
	sb.WriteString(`export type EndpointKey = keyof GotaEndpoints;

export type RequestParams<K extends EndpointKey> = GotaEndpoints[K]["params"];
export type RequestQuery<K extends EndpointKey> = GotaEndpoints[K]["query"];
export type RequestBody<K extends EndpointKey> = GotaEndpoints[K]["body"];
export type ResponseData<K extends EndpointKey> = GotaEndpoints[K]["response"];

export type RequestOptions<K extends EndpointKey> =
  (RequestParams<K> extends void ? { params?: never } : { params: RequestParams<K> }) &
  (RequestQuery<K> extends void ? { query?: never } : { query?: RequestQuery<K> }) &
  (RequestBody<K> extends void ? { body?: never } : { body: RequestBody<K> }) & {
    headers?: Record<string, string>;
    signal?: AbortSignal;
  };

export interface GotaClient {
  readonly baseUrl: string;
  fetch<K extends EndpointKey>(
    endpoint: K,
    ...args: RequestOptions<K> extends { params?: never; query?: never; body?: never }
      ? [options?: RequestOptions<K>]
      : [options: RequestOptions<K>]
  ): Promise<ResponseData<K>>;
}

export declare function createGotaClient(baseUrl?: string): GotaClient;
`)

	return sb.String()
}

// GenerateSDK produces a full standalone TypeScript client SDK, including the createGotaClient implementation.
func (g *TypeScriptGenerator) GenerateSDK(routes []*Node) string {
	tsDefs := g.Generate(routes)

	// Replace the declare function with the actual implementation
	decl := "export declare function createGotaClient(baseUrl?: string): GotaClient;\n"
	impl := `export function createGotaClient(baseUrl = ""): GotaClient {
  return {
    baseUrl,
    async fetch(endpoint, ...args) {
      const options = (args[0] || {}) as any;
      const [method, pathTemplate] = endpoint.split(" ");
      let path = pathTemplate;
      if (options.params) {
        for (const [k, v] of Object.entries(options.params)) {
          path = path.replace(":" + k, encodeURIComponent(String(v)));
          path = path.replace("*" + k, String(v));
        }
      }
      let url = baseUrl + path;
      if (options.query) {
        const q = new URLSearchParams();
        for (const [k, v] of Object.entries(options.query)) {
          if (v !== undefined && v !== null) {
            q.set(k, String(v));
          }
        }
        const qs = q.toString();
        if (qs) url += (url.includes("?") ? "&" : "?") + qs;
      }
      const headers = new Headers(options.headers);
      let body: any = undefined;
      if (options.body !== undefined) {
        if (typeof FormData !== "undefined" && options.body instanceof FormData) {
          body = options.body;
        } else {
          headers.set("Content-Type", "application/json");
          body = JSON.stringify(options.body);
        }
      }
      const res = await fetch(url, {
        method,
        headers,
        body,
        signal: options.signal,
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({ message: res.statusText }));
        throw Object.assign(new Error((err && err.message) || res.statusText), { status: res.status, data: err });
      }
      if (res.status === 204) return undefined as any;
      const ctype = res.headers.get("content-type") || "";
      if (ctype.includes("application/json")) {
        return res.json();
      }
      return res.text() as any;
    }
  };
}
`

	return strings.Replace(tsDefs, decl, impl, 1)
}
