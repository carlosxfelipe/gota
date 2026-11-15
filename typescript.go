package repr

import (
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	tsSchemaNamePathPrefix = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)
	tsSchemaNamePkgQual    = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)
	tsSchemaNameInvalid    = regexp.MustCompile(`[^A-Za-z0-9_]+`)
	tsIdentifier           = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	tsReservedNames        = buildTSReservedNames()
	tsRawBodyPrefix        = []string{"image/", "audio/", "video/"}
	tsRawBodyExact         = []string{"application/octet-stream", "application/pdf", "application/zip"}
)

const tsDateTimeFormat = "date-time"

// sanitizeTSTypeName ensures a Go struct type name is a valid TypeScript identifier.
func sanitizeTSTypeName(name string) string {
	name = tsSchemaNamePathPrefix.ReplaceAllString(name, "")
	name = tsSchemaNamePkgQual.ReplaceAllString(name, "")
	name = tsSchemaNameInvalid.ReplaceAllString(name, "_")
	return strings.Trim(name, "_")
}

// TypeScriptGenerator inspects registered repr routes and types to produce TypeScript declarations and SDKs.
type TypeScriptGenerator struct {
	config       Config
	schemas      map[string]string
	schemaTypes  map[string]reflect.Type
	typeNames    map[reflect.Type]string
	responseOnly map[reflect.Type]bool
}

// NewTypeScriptGenerator initializes a new TypeScriptGenerator.
func NewTypeScriptGenerator(cfg Config) *TypeScriptGenerator {
	g := &TypeScriptGenerator{config: cfg}
	g.reset()
	return g
}

func (g *TypeScriptGenerator) reset() {
	g.schemas = make(map[string]string)
	g.schemaTypes = make(map[string]reflect.Type)
	g.typeNames = make(map[reflect.Type]string)
	g.responseOnly = make(map[reflect.Type]bool)
}

// buildTSReservedNames returns identifiers that generated schema types must never shadow.
func buildTSReservedNames() map[string]bool {
	names := []string{
		"any", "unknown", "never", "void", "null", "undefined", "object", "string", "number",
		"boolean", "symbol", "bigint", "true", "false", "this", "type", "interface", "class",
		"enum", "function", "const", "let", "var", "new", "delete", "default", "export",
		"import", "extends", "implements", "package", "private", "protected", "public",
		"static", "yield", "in", "instanceof", "typeof", "return", "super", "switch", "case",
		"break", "continue", "do", "else", "for", "if", "while", "with", "try", "catch",
		"finally", "throw", "debugger",
		"Array", "Record", "Partial", "Required", "Readonly", "Pick", "Omit", "Promise", "Date",
		"Error", "Map", "Set", "Object", "String", "Number", "Boolean", "Symbol", "JSON", "Math",
		"ArrayBuffer", "Blob", "File", "FormData", "Headers", "Response", "Request", "RequestInit",
		"RequestCache", "RequestCredentials", "BodyInit", "URLSearchParams", "ReadableStream",
		"AbortSignal", "AbortController", "TextDecoderStream", "DOMException",
	}
	reserved := make(map[string]bool, len(names)+len(tsRuntimeExportedNames))
	for _, n := range names {
		reserved[n] = true
	}
	for _, n := range tsRuntimeExportedNames {
		reserved[n] = true
	}
	return reserved
}

func tsPropName(name string) string {
	if tsIdentifier.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

func tsNeedsParens(ts string) bool {
	depth := 0
	inString := false
	for i := 0; i < len(ts); i++ {
		c := ts[i]
		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '<', '(', '{', '[':
			depth++
		case '>', ')', '}', ']':
			depth--
		case '|', '&':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func tsWrap(ts string) string {
	if tsNeedsParens(ts) {
		return "(" + ts + ")"
	}
	return ts
}

func tsDocLines(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(strings.TrimRight(line, " \t\r"), "*/", "*\\/")
	}
	return lines
}

func writeTSDoc(sb *strings.Builder, lines []string, indent string) {
	if len(lines) == 0 {
		return
	}
	sb.WriteString(indent + "/**\n")
	for _, line := range lines {
		sb.WriteString(strings.TrimRight(indent+" * "+line, " ") + "\n")
	}
	sb.WriteString(indent + " */\n")
}

func (g *TypeScriptGenerator) schemaName(t reflect.Type) string {
	if name, ok := g.typeNames[t]; ok {
		return name
	}
	taken := func(n string) bool {
		_, used := g.schemaTypes[n]
		return used || tsReservedNames[n]
	}
	base := sanitizeTSTypeName(t.Name())
	if base == "" {
		base = "AnonymousModel"
	}
	if base[0] >= '0' && base[0] <= '9' {
		base = "_" + base
	}
	name := base
	if taken(name) {
		if qualified := sanitizeTSTypeName(path.Base(t.PkgPath()) + "_" + t.Name()); qualified != "" && qualified != base {
			name = qualified
		}
	}
	candidate := name
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s_%d", candidate, i)
	}
	g.schemaTypes[name] = t
	g.typeNames[t] = name
	return name
}

func parseOneOfChoices(validateTag string) []string {
	if validateTag == "" {
		return nil
	}
	for _, rule := range strings.Split(validateTag, ",") {
		rule = strings.TrimSpace(rule)
		if val, ok := strings.CutPrefix(rule, "oneof="); ok {
			return strings.Fields(val)
		}
	}
	return nil
}

func oneOfUnion(validateTag string, t reflect.Type) string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.String {
		return ""
	}
	choices := parseOneOfChoices(validateTag)
	if len(choices) == 0 {
		return ""
	}
	formatted := make([]string, len(choices))
	for i, c := range choices {
		formatted[i] = strconv.Quote(c)
	}
	return strings.Join(formatted, " | ")
}

func hasJSONOmitOption(jsonTag string) bool {
	if jsonTag == "" {
		return false
	}
	parts := strings.Split(jsonTag, ",")
	for _, opt := range parts[1:] {
		opt = strings.TrimSpace(opt)
		if opt == "omitempty" || opt == "omitzero" {
			return true
		}
	}
	return false
}

func isRawContentType(ct string) bool {
	for _, p := range tsRawBodyPrefix {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	for _, e := range tsRawBodyExact {
		if ct == e {
			return true
		}
	}
	return false
}

func modelType(model any) reflect.Type {
	t := reflect.TypeOf(model)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isEmptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0
}

// analyzeRoutes identifies struct types reachable only from responses so they use
// serialization semantics (non-pointer fields always present, pointers nullable).
func (g *TypeScriptGenerator) analyzeRoutes(routes []*Route) {
	requestTypes := make(map[reflect.Type]bool)
	responseTypes := make(map[reflect.Type]bool)
	for _, rt := range routes {
		if !tsVisibleRoute(rt) || rt.doc == nil {
			continue
		}
		for _, p := range rt.doc.Parameters {
			walkTSTypes(reflect.TypeOf(p.Model), requestTypes)
		}
		if rt.doc.RequestBody != nil {
			walkTSTypes(reflect.TypeOf(rt.doc.RequestBody.Model), requestTypes)
		}
		for _, resp := range rt.doc.Responses {
			if resp != nil {
				walkTSTypes(reflect.TypeOf(resp.Model), responseTypes)
			}
		}
	}
	for t := range responseTypes {
		if !requestTypes[t] {
			g.responseOnly[t] = true
		}
	}
}

func walkTSTypes(t reflect.Type, seen map[reflect.Type]bool) {
	for t != nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
			continue
		}
		break
	}
	if t == nil || t.Kind() != reflect.Struct || t == timeType || t == fileHeaderType || seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("json") == "-" || field.Tag.Get("form") == "-" {
			continue
		}
		if ft, ok := embeddedStructType(field); ok {
			walkTSTypes(ft, seen)
			continue
		}
		if field.IsExported() {
			walkTSTypes(field.Type, seen)
		}
	}
}

func tsVisibleRoute(rt *Route) bool {
	return rt.doc == nil || !rt.doc.Hidden
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
		elem := t.Elem()
		if elem.Kind() == reflect.Uint8 {
			return "string"
		}
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem == fileHeaderType {
			return "File[] | Blob[]"
		}
		return tsWrap(g.typeToTS(t.Elem(), seen)) + "[]"
	case reflect.Map:
		return fmt.Sprintf("Record<string, %s>", g.typeToTS(t.Elem(), seen))
	case reflect.Interface:
		return "any"
	case reflect.Struct:
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
	name     string
	tsType   string
	required bool
	doc      []string
}

func (g *TypeScriptGenerator) collectStructFields(t reflect.Type, fields *[]structFieldDoc, seen map[reflect.Type]bool, embedding map[reflect.Type]bool, isResponse bool) {
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

		if ft, ok := embeddedStructType(field); ok {
			var sub []structFieldDoc
			g.collectStructFields(ft, &sub, seen, embedding, isResponse)
			promotedFields = append(promotedFields, sub...)
			continue
		}

		if !field.IsExported() {
			continue
		}

		warnIfSensitiveField(t, field, g.config.SilenceTagWarnings)

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
		isPointer := field.Type.Kind() == reflect.Pointer
		omits := hasJSONOmitOption(jsonTag)
		isRequired := hasRequiredRule(valTag)
		nullable := false
		if isResponse && !omits {
			isRequired = true
			nullable = isPointer
		}

		tsType := oneOfUnion(valTag, field.Type)
		if tsType == "" {
			tsType = g.typeToTS(field.Type, seen)
		}
		if nullable {
			tsType += " | null"
		}

		own[name] = true
		ownFields = append(ownFields, structFieldDoc{
			name:     name,
			tsType:   tsType,
			required: isRequired,
			doc:      fieldDocLines(field),
		})
	}

	*fields = append(*fields, ownFields...)
	for _, pf := range promotedFields {
		if !own[pf.name] {
			*fields = append(*fields, pf)
		}
	}
}

func fieldDocLines(field reflect.StructField) []string {
	lines := tsDocLines(field.Tag.Get("doc"))
	if ex := field.Tag.Get("example"); ex != "" {
		lines = append(lines, tsDocLines("@example "+ex)...)
	}
	if def := field.Tag.Get("default"); def != "" {
		lines = append(lines, tsDocLines("@default "+def)...)
	}
	format := field.Tag.Get("format")
	if format == "" {
		ft := field.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft == timeType {
			format = tsDateTimeFormat
		}
	}
	if format != "" && format != "binary" {
		lines = append(lines, tsDocLines("@format "+format)...)
	}
	if field.Tag.Get("deprecated") == "true" {
		lines = append(lines, "@deprecated")
	}
	return lines
}

func (g *TypeScriptGenerator) inlineStructTS(t reflect.Type, seen map[reflect.Type]bool) string {
	if seen[t] {
		return "Record<string, any>"
	}
	seen[t] = true
	defer delete(seen, t)

	var fields []structFieldDoc
	g.collectStructFields(t, &fields, seen, map[reflect.Type]bool{}, g.responseOnly[t])
	if len(fields) == 0 {
		return "Record<string, never>"
	}

	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		opt := "?"
		if f.required {
			opt = ""
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", tsPropName(f.name), opt, f.tsType))
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func structDocLines(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if m, ok := t.MethodByName("Doc"); ok && m.Type.NumIn() == 1 && m.Type.NumOut() == 1 && m.Type.Out(0).Kind() == reflect.String {
		val := reflect.New(t).Elem()
		res := m.Func.Call([]reflect.Value{val})
		if str := res[0].String(); str != "" {
			return tsDocLines(str)
		}
	}
	if m, ok := reflect.PointerTo(t).MethodByName("Doc"); ok && m.Type.NumIn() == 1 && m.Type.NumOut() == 1 && m.Type.Out(0).Kind() == reflect.String {
		val := reflect.New(t)
		res := m.Func.Call([]reflect.Value{val})
		if str := res[0].String(); str != "" {
			return tsDocLines(str)
		}
	}
	if m, ok := t.MethodByName("Description"); ok && m.Type.NumIn() == 1 && m.Type.NumOut() == 1 && m.Type.Out(0).Kind() == reflect.String {
		val := reflect.New(t).Elem()
		res := m.Func.Call([]reflect.Value{val})
		if str := res[0].String(); str != "" {
			return tsDocLines(str)
		}
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Name == "_" && f.Tag.Get("doc") != "" {
			return tsDocLines(f.Tag.Get("doc"))
		}
	}
	return nil
}

func (g *TypeScriptGenerator) generateInterface(name string, t reflect.Type, seen map[reflect.Type]bool) {
	var fields []structFieldDoc
	g.collectStructFields(t, &fields, seen, map[reflect.Type]bool{}, g.responseOnly[t])

	var sb strings.Builder
	writeTSDoc(&sb, structDocLines(t), "")
	sb.WriteString(fmt.Sprintf("export interface %s {\n", name))
	for _, f := range fields {
		writeTSDoc(&sb, f.doc, "  ")
		opt := "?"
		if f.required {
			opt = ""
		}
		sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", tsPropName(f.name), opt, f.tsType))
	}
	sb.WriteString("}\n")
	g.schemas[name] = sb.String()
}

type routeTS struct {
	method       string
	path         string
	key          string
	summary      string
	description  string
	deprecated   bool
	paramsTS     string
	queryTS      string
	bodyTS       string
	responseTS   string
	errorTS      string
	requestTS    string
	bodyEncoding string
}

type tsParamField struct {
	name     string
	tsType   string
	required bool
}

func joinTSFields(fields []tsParamField) string {
	sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
	parts := make([]string, len(fields))
	for i, f := range fields {
		opt := "?"
		if f.required {
			opt = ""
		}
		parts[i] = fmt.Sprintf("%s%s: %s", tsPropName(f.name), opt, f.tsType)
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func anyRequired(fields []tsParamField) bool {
	for _, f := range fields {
		if f.required {
			return true
		}
	}
	return false
}

func (g *TypeScriptGenerator) inspectRoute(rt *Route) routeTS {
	method := strings.ToUpper(rt.method)
	r := routeTS{
		method:       method,
		path:         rt.path,
		key:          fmt.Sprintf("%s %s", method, rt.path),
		paramsTS:     "void",
		queryTS:      "void",
		bodyTS:       "void",
		responseTS:   "unknown",
		errorTS:      "unknown",
		bodyEncoding: "json",
	}
	doc := rt.doc
	if doc == nil {
		doc = &RouteDoc{}
	}
	r.summary = doc.Summary
	r.description = doc.Description
	r.deprecated = doc.Deprecated

	pathTypes := make(map[string]string)
	var queryFields, headerFields []tsParamField
	for _, p := range doc.Parameters {
		pType := "string"
		if p.Model != nil {
			pType = oneOfUnion(p.Validate, reflect.TypeOf(p.Model))
			if pType == "" {
				pType = g.typeToTS(reflect.TypeOf(p.Model), map[reflect.Type]bool{})
			}
		}
		switch p.In {
		case "path":
			pathTypes[p.Name] = pType
		case "query":
			queryFields = append(queryFields, tsParamField{p.Name, pType, p.Required})
		case "header":
			headerFields = append(headerFields, tsParamField{p.Name, "string", p.Required})
		}
	}
	for _, name := range extractPathParams(rt.path) {
		if _, ok := pathTypes[name]; !ok {
			pathTypes[name] = "string"
		}
	}
	var pathFields []tsParamField
	for name, ts := range pathTypes {
		pathFields = append(pathFields, tsParamField{name, ts, true})
	}
	if len(pathFields) > 0 {
		r.paramsTS = joinTSFields(pathFields)
	}
	if len(queryFields) > 0 {
		r.queryTS = joinTSFields(queryFields)
	}

	bodyRequired := false
	if body := doc.RequestBody; body != nil && body.Model != nil {
		bodyRequired = body.Required
		mType := modelType(body.Model)
		hasFields := mType != nil && mType.Kind() == reflect.Struct && mType.NumField() > 0
		switch ct := body.ContentType; {
		case ct == "multipart/form-data":
			r.bodyEncoding = "multipart"
			if hasFields {
				r.bodyTS = "FormData | " + g.typeToTS(mType, map[reflect.Type]bool{})
			} else {
				r.bodyTS = "FormData | Record<string, unknown>"
			}
		case ct == "application/x-www-form-urlencoded":
			r.bodyEncoding = "form"
			if hasFields {
				r.bodyTS = "URLSearchParams | " + g.typeToTS(mType, map[reflect.Type]bool{})
			} else {
				r.bodyTS = "URLSearchParams | Record<string, string | number | boolean>"
			}
		case isRawContentType(ct):
			r.bodyEncoding = "raw"
			r.bodyTS = "Blob | ArrayBuffer | ArrayBufferView | ReadableStream | string"
		default:
			if mType != nil && !isEmptyStruct(mType) {
				r.bodyTS = g.typeToTS(mType, map[reflect.Type]bool{})
			}
		}
	}

	var successStatuses, errorStatuses []int
	for code := range doc.Responses {
		switch {
		case code >= 200 && code < 300:
			successStatuses = append(successStatuses, code)
		case code >= 400:
			errorStatuses = append(errorStatuses, code)
		}
	}
	sort.Ints(successStatuses)
	sort.Ints(errorStatuses)

	if len(successStatuses) > 0 {
		code := successStatuses[0]
		resp := doc.Responses[code]
		mType := modelType(resp.Model)
		switch {
		case code == httpStatusNoContent || code == httpStatusResetContent:
			r.responseTS = "void"
		case resp.ContentType == "text/event-stream":
			r.responseTS = "ReadableStream<string>"
		case isRawContentType(resp.ContentType):
			r.responseTS = "Blob"
		case mType == nil || isEmptyStruct(mType):
			if strings.HasPrefix(resp.ContentType, "text/") {
				r.responseTS = "string"
			} else {
				r.responseTS = "void"
			}
		default:
			r.responseTS = g.typeToTS(mType, map[reflect.Type]bool{})
		}
	}

	var errorTypes []string
	seenErrorTypes := make(map[string]bool)
	for _, code := range errorStatuses {
		resp := doc.Responses[code]
		if resp == nil {
			continue
		}
		mType := modelType(resp.Model)
		if mType == nil || isEmptyStruct(mType) {
			continue
		}
		tsType := g.typeToTS(mType, map[reflect.Type]bool{})
		if !seenErrorTypes[tsType] {
			seenErrorTypes[tsType] = true
			errorTypes = append(errorTypes, tsType)
		}
	}
	if len(errorTypes) > 0 {
		r.errorTS = strings.Join(errorTypes, " | ")
	}

	request := make([]string, 0, 4)
	if len(pathFields) > 0 {
		request = append(request, "params: "+r.paramsTS)
	} else {
		request = append(request, "params?: never")
	}
	switch {
	case len(queryFields) == 0:
		request = append(request, "query?: never")
	case anyRequired(queryFields):
		request = append(request, "query: "+r.queryTS)
	default:
		request = append(request, "query?: "+r.queryTS)
	}
	if len(headerFields) > 0 {
		opt := "?"
		if anyRequired(headerFields) {
			opt = ""
		}
		request = append(request, "headers"+opt+": "+joinTSFields(headerFields))
	}
	switch {
	case r.bodyTS == "void":
		request = append(request, "body?: never")
	case bodyRequired:
		request = append(request, "body: "+r.bodyTS)
	default:
		request = append(request, "body?: "+r.bodyTS)
	}
	r.requestTS = "{ " + strings.Join(request, "; ") + " }"

	return r
}

const (
	httpStatusNoContent    = 204
	httpStatusResetContent = 205
)

// Generate produces full TypeScript type definitions (.d.ts) for all registered routes.
func (g *TypeScriptGenerator) Generate(routes []*Route) string {
	return g.render(routes, true)
}

// GenerateSDK produces a full standalone TypeScript client SDK, including the createGotaClient implementation.
func (g *TypeScriptGenerator) GenerateSDK(routes []*Route) string {
	return g.render(routes, false)
}

func (g *TypeScriptGenerator) render(routes []*Route, declarationsOnly bool) string {
	g.reset()
	g.analyzeRoutes(routes)

	var routeItems []routeTS
	for _, rt := range routes {
		if tsVisibleRoute(rt) {
			routeItems = append(routeItems, g.inspectRoute(rt))
		}
	}
	sort.Slice(routeItems, func(i, j int) bool {
		return routeItems[i].key < routeItems[j].key
	})

	var sb strings.Builder
	header := []string{"Auto-generated by Gota Framework \U0001F4A7. Do not edit by hand."}
	if g.config.Title != "" {
		api := "API: " + g.config.Title
		if g.config.Version != "" {
			api += " (" + g.config.Version + ")"
		}
		header = append(header, tsDocLines(api)...)
	}
	header = append(header, tsDocLines(g.config.Description)...)
	writeTSDoc(&sb, header, "")
	sb.WriteString("\n")

	schemaNames := make([]string, 0, len(g.schemas))
	for name := range g.schemas {
		schemaNames = append(schemaNames, name)
	}
	sort.Strings(schemaNames)
	for _, name := range schemaNames {
		if content := g.schemas[name]; content != "" {
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("export interface GotaEndpoints {\n")
	for _, r := range routeItems {
		doc := tsDocLines(r.summary)
		if r.description != "" && r.description != r.summary {
			doc = append(doc, tsDocLines(r.description)...)
		}
		if r.deprecated {
			doc = append(doc, "@deprecated")
		}
		writeTSDoc(&sb, doc, "  ")
		sb.WriteString(fmt.Sprintf("  %q: {\n", r.key))
		sb.WriteString(fmt.Sprintf("    method: %q;\n", r.method))
		sb.WriteString(fmt.Sprintf("    path: %q;\n", r.path))
		sb.WriteString(fmt.Sprintf("    params: %s;\n", r.paramsTS))
		sb.WriteString(fmt.Sprintf("    query: %s;\n", r.queryTS))
		sb.WriteString(fmt.Sprintf("    body: %s;\n", r.bodyTS))
		sb.WriteString(fmt.Sprintf("    response: %s;\n", r.responseTS))
		sb.WriteString(fmt.Sprintf("    error: %s;\n", r.errorTS))
		sb.WriteString(fmt.Sprintf("    request: %s;\n", r.requestTS))
		sb.WriteString("  };\n")
	}
	sb.WriteString("}\n\n")

	sb.WriteString(tsRuntimeTypes)
	if declarationsOnly {
		sb.WriteString(tsRuntimeDeclarations)
		return sb.String()
	}

	sb.WriteString("\nconst BODY_ENCODINGS: Record<string, BodyEncoding | undefined> = {")
	first := true
	for _, r := range routeItems {
		if r.bodyEncoding == "json" {
			continue
		}
		if first {
			sb.WriteString("\n")
			first = false
		}
		sb.WriteString(fmt.Sprintf("  %q: %q,\n", r.key, r.bodyEncoding))
	}
	sb.WriteString("};\n")
	sb.WriteString(tsRuntimeImplementation)
	return sb.String()
}
