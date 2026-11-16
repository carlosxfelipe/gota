package typescript

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	// tsGeneratedMarker is the first line of every generated TypeScript file.
	tsGeneratedMarker = "Auto-generated TypeScript SDK. Do not edit by hand."
	// tsManagedDirMarker identifies files written by the modular SDK generator. Only files
	// carrying both markers are ever deleted when they become stale.
	tsManagedDirMarker = "This directory is managed by TypeScript SDK codegen"

	tsDirNote = tsManagedDirMarker + ": files are overwritten and stale ones removed on every regeneration.\n" +
		"Keep customizations in a wrapper module outside of this directory."

	// tsSharedModule holds schemas referenced by more than one module.
	tsSharedModule = "_shared"
	// tsDefaultModule groups routes without tags or static resource segments.
	tsDefaultModule = "default"
)

func normalizeTSImportExtension(ext string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case "", "none":
		return "", nil
	case ".js", "js":
		return ".js", nil
	case ".ts", "ts":
		return ".ts", nil
	case "auto":
		return "auto", nil
	default:
		return "", fmt.Errorf("typescript: unsupported import extension %q (use \"\", \".js\", \".ts\", or \"auto\")", ext)
	}
}

func resolveTSOptionsForDir(outDir string, opts []Options) (Options, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	norm, err := normalizeTSImportExtension(opt.ImportExtension)
	if err != nil {
		return opt, err
	}
	if norm == "auto" {
		opt.ImportExtension = DetectImportExtension(outDir)
	} else {
		opt.ImportExtension = norm
	}
	return opt, nil
}

// DetectImportExtension inspects deno.json(c), tsconfig.json, and package.json
// walking up from dir and returns ".ts", ".js", or "" (extensionless bundler default).
func DetectImportExtension(dir string) string {
	cur := strings.TrimSpace(dir)
	if cur == "" {
		cur = "."
	}
	if abs, err := filepath.Abs(cur); err == nil {
		cur = abs
	}
	if info, err := os.Stat(cur); err == nil && !info.IsDir() {
		cur = filepath.Dir(cur)
	}

	for {
		if fileExists(filepath.Join(cur, "deno.json")) || fileExists(filepath.Join(cur, "deno.jsonc")) {
			return ".ts"
		}
		if ext, decided := detectFromTSConfig(filepath.Join(cur, "tsconfig.json")); decided {
			return ext
		}
		if ext, decided := detectFromPackageJSON(filepath.Join(cur, "package.json")); decided {
			return ext
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func detectFromTSConfig(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var cfg struct {
		CompilerOptions struct {
			Module                     string `json:"module"`
			ModuleResolution           string `json:"moduleResolution"`
			AllowImportingTsExtensions bool   `json:"allowImportingTsExtensions"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return "", false
	}
	if cfg.CompilerOptions.AllowImportingTsExtensions {
		return ".ts", true
	}
	switch strings.ToLower(strings.TrimSpace(cfg.CompilerOptions.ModuleResolution)) {
	case "node16", "nodenext":
		return ".js", true
	case "bundler", "node", "node10", "classic":
		return "", true
	}
	switch strings.ToLower(strings.TrimSpace(cfg.CompilerOptions.Module)) {
	case "node16", "nodenext":
		return ".js", true
	}
	return "", false
}

func detectFromPackageJSON(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var pkg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(pkg.Type), "module") {
		return ".js", true
	}
	return "", true
}

// stripJSONC removes // comments, /* */ comments, and trailing commas from tsconfig JSONC.
func stripJSONC(src []byte) []byte {
	noComments := make([]byte, 0, len(src))
	inStr := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			noComments = append(noComments, c)
			if c == '\\' && i+1 < len(src) {
				i++
				noComments = append(noComments, src[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			noComments = append(noComments, c)
			continue
		}
		if c == '/' && i+1 < len(src) {
			if src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				if i < len(src) {
					noComments = append(noComments, '\n')
				}
				continue
			}
			if src[i+1] == '*' {
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		noComments = append(noComments, c)
	}

	out := make([]byte, 0, len(noComments))
	inStr = false
	for i := 0; i < len(noComments); i++ {
		c := noComments[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(noComments) {
				i++
				out = append(out, noComments[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(noComments) && (noComments[j] == ' ' || noComments[j] == '\t' || noComments[j] == '\r' || noComments[j] == '\n') {
				j++
			}
			if j < len(noComments) && (noComments[j] == '}' || noComments[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func tsRouteKey(rt Route) string {
	return strings.ToUpper(rt.Method) + " " + rt.Path
}

// tsAccentFold maps common accented Latin letters to ASCII so tags such as "Usuários" become "usuarios".
var tsAccentFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// tsSlugify turns a tag into a lowercase kebab-case module name ("UserProfiles" -> "user-profiles").
func tsSlugify(s string) string {
	var b strings.Builder
	pendingDash := false
	prevLowerOrDigit := false
	for _, r := range s {
		if unicode.IsUpper(r) && prevLowerOrDigit {
			pendingDash = true
		}
		r = unicode.ToLower(r)
		if folded, ok := tsAccentFold[r]; ok {
			r = folded
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			prevLowerOrDigit = true
			continue
		}
		pendingDash = true
		prevLowerOrDigit = false
	}
	return b.String()
}

func isVersionOrAPIPrefix(seg string) bool {
	lower := strings.ToLower(seg)
	if lower == "api" {
		return true
	}
	if len(lower) >= 2 && lower[0] == 'v' {
		for i := 1; i < len(lower); i++ {
			if lower[i] < '0' || lower[i] > '9' {
				return false
			}
		}
		return true
	}
	return false
}

func tsModuleSlug(rt Route) string {
	for _, tag := range rt.Tags {
		if slug := tsSlugify(tag); slug != "" {
			return slug
		}
	}
	var staticSegs []string
	for _, seg := range strings.Split(rt.Path, "/") {
		if seg == "" || seg == "*" || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			continue
		}
		if slug := tsSlugify(seg); slug != "" {
			staticSegs = append(staticSegs, slug)
		}
	}
	for len(staticSegs) > 1 && isVersionOrAPIPrefix(staticSegs[0]) {
		staticSegs = staticSegs[1:]
	}
	if len(staticSegs) > 0 {
		return staticSegs[0]
	}
	return tsDefaultModule
}

func tsPascal(slug string) string {
	var b strings.Builder
	for _, part := range strings.Split(slug, "-") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "_" + name
	}
	return name
}

type tsModule struct {
	slug   string
	iface  string
	routes []routeTS
}

type tsBuild struct {
	routes      []routeTS
	modules     []*tsModule
	schemaNames []string
	// owner maps each schema to the module file that declares it (or tsSharedModule).
	owner map[string]string
}

// build inspects the visible routes in a stable order (sorted by "METHOD /path"), so that
// schema naming and output never depend on route registration order.
func (g *Generator) build(routes []Route) *tsBuild {
	g.reset()

	visible := make([]Route, 0, len(routes))
	for _, rt := range routes {
		if !rt.Hidden {
			visible = append(visible, rt)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool {
		return tsRouteKey(visible[i]) < tsRouteKey(visible[j])
	})
	g.analyzeRoutes(visible)

	bySlug := make(map[string]*tsModule)
	slugs := make([]string, len(visible))
	for i, rt := range visible {
		slugs[i] = tsModuleSlug(rt)
		if _, ok := bySlug[slugs[i]]; !ok {
			bySlug[slugs[i]] = &tsModule{slug: slugs[i]}
		}
	}
	b := &tsBuild{owner: make(map[string]string)}
	for _, m := range bySlug {
		b.modules = append(b.modules, m)
	}
	sort.Slice(b.modules, func(i, j int) bool { return b.modules[i].slug < b.modules[j].slug })

	// Module interface names are reserved before any schema is named.
	usedIfaces := make(map[string]bool)
	for _, m := range b.modules {
		base := tsPascal(m.slug) + "Endpoints"
		if tsReservedNames[base] {
			base = tsPascal(m.slug) + "ModuleEndpoints"
		}
		name := base
		for i := 2; usedIfaces[name] || tsReservedNames[name]; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		usedIfaces[name] = true
		m.iface = name
		g.reserved[name] = true
	}

	for i, rt := range visible {
		r := g.inspectRoute(rt)
		r.module = slugs[i]
		b.routes = append(b.routes, r)
		m := bySlug[slugs[i]]
		m.routes = append(m.routes, r)
	}

	for name, content := range g.schemas {
		if content != "" {
			b.schemaNames = append(b.schemaNames, name)
		}
	}
	sort.Strings(b.schemaNames)

	// A schema lives in a module file when only that module reaches it (directly or
	// transitively); otherwise it goes to the shared file. Schemas reachable from a shared
	// schema are therefore shared as well, so module files never import each other.
	owners := make(map[string]map[string]bool)
	for _, m := range b.modules {
		visited := make(map[string]bool)
		var visit func(string)
		visit = func(name string) {
			if visited[name] {
				return
			}
			visited[name] = true
			for dep := range g.schemaDeps[name] {
				visit(dep)
			}
		}
		for _, r := range m.routes {
			for ref := range r.refs {
				visit(ref)
			}
		}
		for name := range visited {
			if owners[name] == nil {
				owners[name] = make(map[string]bool)
			}
			owners[name][m.slug] = true
		}
	}
	for _, name := range b.schemaNames {
		b.owner[name] = tsSharedModule
		if len(owners[name]) == 1 {
			for slug := range owners[name] {
				b.owner[name] = slug
			}
		}
	}
	return b
}

// GenerateSDK produces the modular TypeScript client SDK files, keyed by slash-separated paths
// relative to the output directory:
//
//	index.ts            entry point: re-exports everything, ApiEndpoints, createClient
//	runtime.ts          route-independent client core (changes only when the runtime changes)
//	types/<tag>.ts      schemas used by a single tag
//	types/_shared.ts    schemas used by several tags
//	endpoints/<tag>.ts  endpoint map of each tag (untagged routes use their path resource or "default")
func (g *Generator) GenerateSDK(routes []Route, opts ...Options) (map[string]string, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	ext, err := normalizeTSImportExtension(opt.ImportExtension)
	if err != nil {
		return nil, err
	}
	if ext == "auto" {
		ext = DetectImportExtension(".")
	}
	b := g.build(routes)

	byOwner := make(map[string][]string)
	for _, name := range b.schemaNames {
		byOwner[b.owner[name]] = append(byOwner[b.owner[name]], name)
	}

	files := make(map[string]string, len(byOwner)+len(b.modules)+2)
	files["runtime.ts"] = g.renderRuntimeFile()
	for owner, names := range byOwner {
		files["types/"+owner+".ts"] = g.renderTypesFile(b, owner, names, ext)
	}
	for _, m := range b.modules {
		files["endpoints/"+m.slug+".ts"] = g.renderEndpointsFile(b, m, ext)
	}
	files["index.ts"] = g.renderIndexFile(b, byOwner, ext)
	return files, nil
}

// Generate produces the modular TypeScript client SDK files for spec.
func Generate(spec Spec, opts ...Options) (map[string]string, error) {
	return NewGenerator(spec.Config).GenerateSDK(spec.Routes, opts...)
}

// Export writes the modular TypeScript client SDK into outDir, updating changed
// files and removing stale files previously created by TypeScript SDK codegen.
func Export(spec Spec, outDir string, opts ...Options) error {
	resolved, err := resolveTSOptionsForDir(outDir, opts)
	if err != nil {
		return err
	}
	files, err := Generate(spec, resolved)
	if err != nil {
		return err
	}
	return exportTSDir(outDir, files)
}

// Check verifies that outDir matches the modular TypeScript SDK for spec
// with no missing, modified, or stale generated files.
func Check(spec Spec, outDir string, opts ...Options) error {
	resolved, err := resolveTSOptionsForDir(outDir, opts)
	if err != nil {
		return err
	}
	files, err := Generate(spec, resolved)
	if err != nil {
		return err
	}
	return checkTSDir(outDir, files)
}

// RunCodegen processes CLI arguments (e.g. from os.Args[1:]) to export or verify the TypeScript SDK.
// Supported flags:
//
//	--ts-sdk[=<dir>]        writes modular client SDK directory to dir (default ./dist)
//	--ts-import-ext=<ext>   sets relative import suffix in --ts-sdk ("", ".js", ".ts", or "auto")
//	--check                 verifies exported SDK directory is up to date without modifying disk (for CI)
func RunCodegen(spec Spec, args []string) (bool, error) {
	sdkDir := ""
	importExt := ""
	hasImportExt := false
	check := false
	matched := false

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--ts-sdk="):
			sdkDir = strings.TrimPrefix(arg, "--ts-sdk=")
			matched = true
		case arg == "--ts-sdk":
			sdkDir = "./dist"
			matched = true
		case strings.HasPrefix(arg, "--ts-import-ext="):
			importExt = strings.TrimPrefix(arg, "--ts-import-ext=")
			hasImportExt = true
			matched = true
		case arg == "--check":
			check = true
			matched = true
		}
	}

	if !matched {
		return false, nil
	}

	if hasImportExt {
		if _, err := normalizeTSImportExtension(importExt); err != nil {
			return true, err
		}
	}

	sdkOpts := Options{ImportExtension: importExt}

	if check {
		if sdkDir == "" {
			return true, fmt.Errorf("typescript codegen: --check requires --ts-sdk")
		}
		if err := Check(spec, sdkDir, sdkOpts); err != nil {
			return true, err
		}
		return true, nil
	}

	if sdkDir != "" {
		if err := Export(spec, sdkDir, sdkOpts); err != nil {
			return true, fmt.Errorf("failed to export TypeScript SDK: %w", err)
		}
	}

	return true, nil
}

func (g *Generator) writeFileHeader(sb *strings.Builder, withAPI bool, note string) {
	lines := []string{tsGeneratedMarker}
	if withAPI {
		if g.config.Title != "" {
			api := "API: " + g.config.Title
			if g.config.Version != "" {
				api += " (" + g.config.Version + ")"
			}
			lines = append(lines, tsDocLines(api)...)
		}
		lines = append(lines, tsDocLines(g.config.Description)...)
	}
	lines = append(lines, tsDocLines(note)...)
	writeTSDoc(sb, lines, "")
	sb.WriteString("/* eslint-disable */\n\n")
}

func writeTSEndpointEntries(sb *strings.Builder, routes []routeTS) {
	for _, r := range routes {
		doc := tsDocLines(r.summary)
		if r.description != "" && r.description != r.summary {
			doc = append(doc, tsDocLines(r.description)...)
		}
		if r.deprecated {
			doc = append(doc, "@deprecated")
		}
		writeTSDoc(sb, doc, "  ")
		fmt.Fprintf(sb, "  %q: {\n", r.key)
		fmt.Fprintf(sb, "    method: %q;\n", r.method)
		fmt.Fprintf(sb, "    path: %q;\n", r.path)
		fmt.Fprintf(sb, "    params: %s;\n", r.paramsTS)
		fmt.Fprintf(sb, "    query: %s;\n", r.queryTS)
		fmt.Fprintf(sb, "    body: %s;\n", r.bodyTS)
		fmt.Fprintf(sb, "    response: %s;\n", r.responseTS)
		fmt.Fprintf(sb, "    error: %s;\n", r.errorTS)
		fmt.Fprintf(sb, "    request: %s;\n", r.requestTS)
		sb.WriteString("  };\n")
	}
}

func writeTSBodyEncodings(sb *strings.Builder, routes []routeTS) {
	sb.WriteString("const BODY_ENCODINGS: BodyEncodingMap = {")
	first := true
	for _, r := range routes {
		if r.bodyEncoding == "json" {
			continue
		}
		if first {
			sb.WriteString("\n")
			first = false
		}
		fmt.Fprintf(sb, "  %q: %q,\n", r.key, r.bodyEncoding)
	}
	sb.WriteString("};\n")
}

// writeTSTypeImports writes sorted `import type` statements from a specifier -> names map.
func writeTSTypeImports(sb *strings.Builder, imports map[string]map[string]bool) {
	if len(imports) == 0 {
		return
	}
	specs := make([]string, 0, len(imports))
	for spec := range imports {
		specs = append(specs, spec)
	}
	sort.Strings(specs)
	for _, spec := range specs {
		names := make([]string, 0, len(imports[spec]))
		for name := range imports[spec] {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(sb, "import type { %s } from %q;\n", strings.Join(names, ", "), spec)
	}
	sb.WriteString("\n")
}

func addTSImport(imports map[string]map[string]bool, spec, name string) {
	if imports[spec] == nil {
		imports[spec] = make(map[string]bool)
	}
	imports[spec][name] = true
}

func (g *Generator) renderRuntimeFile() string {
	var sb strings.Builder
	g.writeFileHeader(&sb, false, tsDirNote)
	sb.WriteString(tsRuntimeTypes)
	sb.WriteString(tsRuntimeImplementation)
	return sb.String()
}

func (g *Generator) renderTypesFile(b *tsBuild, owner string, names []string, ext string) string {
	imports := make(map[string]map[string]bool)
	for _, name := range names {
		for dep := range g.schemaDeps[name] {
			if depOwner, ok := b.owner[dep]; ok && depOwner != owner {
				addTSImport(imports, "./"+depOwner+ext, dep)
			}
		}
	}

	var sb strings.Builder
	g.writeFileHeader(&sb, false, tsDirNote)
	writeTSTypeImports(&sb, imports)
	for i, name := range names {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(g.schemas[name])
	}
	return sb.String()
}

func (g *Generator) renderEndpointsFile(b *tsBuild, m *tsModule, ext string) string {
	imports := make(map[string]map[string]bool)
	for _, r := range m.routes {
		for ref := range r.refs {
			if owner, ok := b.owner[ref]; ok {
				addTSImport(imports, "../types/"+owner+ext, ref)
			}
		}
	}

	var sb strings.Builder
	g.writeFileHeader(&sb, false, tsDirNote)
	writeTSTypeImports(&sb, imports)
	fmt.Fprintf(&sb, "export interface %s {\n", m.iface)
	writeTSEndpointEntries(&sb, m.routes)
	sb.WriteString("}\n")
	return sb.String()
}

func (g *Generator) renderIndexFile(b *tsBuild, byOwner map[string][]string, ext string) string {
	runtimeSpec := "./runtime" + ext

	var sb strings.Builder
	g.writeFileHeader(&sb, true, tsDirNote)

	fmt.Fprintf(&sb, "import { createClientFor, isHttpError } from %q;\n", runtimeSpec)
	fmt.Fprintf(&sb, "import type { ApiClientFor, BodyEncodingMap, ClientOptions, HttpError, RequestOptionsFor } from %q;\n", runtimeSpec)
	ifaces := make([]string, 0, len(b.modules))
	for _, m := range b.modules {
		fmt.Fprintf(&sb, "import type { %s } from %q;\n", m.iface, "./endpoints/"+m.slug+ext)
		ifaces = append(ifaces, m.iface)
	}
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "export * from %q;\n", runtimeSpec)
	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		fmt.Fprintf(&sb, "export type * from %q;\n", "./types/"+owner+ext)
	}
	if len(ifaces) > 0 {
		fmt.Fprintf(&sb, "export type { %s };\n", strings.Join(ifaces, ", "))
	}
	sb.WriteString("\n")

	switch len(ifaces) {
	case 0:
		sb.WriteString("export interface ApiEndpoints {}\n\n")
	case 1:
		fmt.Fprintf(&sb, "export interface ApiEndpoints extends %s {}\n\n", ifaces[0])
	default:
		sb.WriteString("export interface ApiEndpoints\n  extends ")
		sb.WriteString(strings.Join(ifaces, ",\n    "))
		sb.WriteString(" {}\n\n")
	}

	sb.WriteString(tsBindingTypes)
	sb.WriteString("\n")
	writeTSBodyEncodings(&sb, b.routes)
	sb.WriteString(tsBindingImplementation)
	return sb.String()
}

func isManagedGeneratedTSFile(data []byte) bool {
	s := string(data)
	return strings.Contains(s, tsGeneratedMarker) && strings.Contains(s, tsManagedDirMarker)
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func exportTSDir(outDir string, files map[string]string) error {
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		return fmt.Errorf("typescript: SDK output directory cannot be empty")
	}
	if info, err := os.Stat(outDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("typescript: %s exists and is not a directory", outDir)
		}
		var stale []string
		err := filepath.WalkDir(outDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || filepath.Ext(p) != ".ts" {
				return nil
			}
			rel, err := filepath.Rel(outDir, p)
			if err != nil {
				return err
			}
			if _, keep := files[filepath.ToSlash(rel)]; keep {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if isManagedGeneratedTSFile(data) {
				stale = append(stale, p)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, p := range stale {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, rel := range sortedMapKeys(files) {
		target := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := writeExportFile(target, []byte(files[rel])); err != nil {
			return err
		}
	}

	for _, sub := range []string{"types", "endpoints"} {
		dir := filepath.Join(outDir, sub)
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return nil
}

func checkTSDir(outDir string, files map[string]string) error {
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		return fmt.Errorf("typescript: SDK directory cannot be empty")
	}
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("missing TypeScript SDK directory %s", outDir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("typescript: %s is not a directory", outDir)
	}

	var missing, changed, stale []string
	for _, rel := range sortedMapKeys(files) {
		target := filepath.Join(outDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, rel)
				continue
			}
			return err
		}
		if string(data) != files[rel] {
			changed = append(changed, rel)
		}
	}

	err = filepath.WalkDir(outDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(p) != ".ts" {
			return nil
		}
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		if _, keep := files[slashRel]; keep {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if isManagedGeneratedTSFile(data) {
			stale = append(stale, slashRel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(stale)

	if len(missing) == 0 && len(changed) == 0 && len(stale) == 0 {
		return nil
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing ["+strings.Join(missing, ", ")+"]")
	}
	if len(changed) > 0 {
		parts = append(parts, "changed ["+strings.Join(changed, ", ")+"]")
	}
	if len(stale) > 0 {
		parts = append(parts, "stale ["+strings.Join(stale, ", ")+"]")
	}
	return fmt.Errorf("out-of-date TypeScript SDK in %s: %s", outDir, strings.Join(parts, "; "))
}
