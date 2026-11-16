package typescript_test

import (
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carlosxfelipe/gota/typescript"
)

func TestGenerator_ModelsAndEndpoints(t *testing.T) {
	type EmbeddedMeta struct {
		CreatedAt time.Time `json:"createdAt"`
	}

	type CreateItemRequest struct {
		_ struct{} `doc:"Payload for creating a catalog item."`
		EmbeddedMeta
		Category    string                `param:"category" validate:"required"`
		DryRun      bool                  `query:"dry_run"`
		TraceID     string                `header:"X-Trace-ID"`
		Title       string                `json:"title" doc:"Item title" example:"Clean Code" validate:"required"`
		Priority    string                `json:"priority" default:"normal" validate:"omitempty,oneof=low normal high"`
		HyphenField bool                  `json:"is-featured"`
		Hidden      string                `json:"-"`
		Attachment  *multipart.FileHeader `form:"-"`
	}

	type ItemResponse struct {
		ID          string  `json:"id" example:"item_1"`
		Title       string  `json:"title"`
		Description *string `json:"description"`
		Priority    string  `json:"priority" validate:"oneof=low normal high"`
	}

	type UploadFormRequest struct {
		Avatar *multipart.FileHeader `form:"avatar" validate:"required"`
		Label  string                `form:"label"`
	}

	type UploadResponse struct {
		URL string `json:"url"`
	}

	type ConflictError struct {
		Error  string `json:"error"`
		Status int    `json:"status"`
	}

	spec := typescript.Spec{
		Config: typescript.Config{
			Title:       "Catalog API",
			Version:     "2.0.0",
			Description: "Catalog endpoints",
		},
		Routes: []typescript.Route{
			{
				Method:  http.MethodGet,
				Path:    "/health",
				Summary: "Health check",
				Responses: map[int]*typescript.Response{
					http.StatusOK: {ContentType: "application/json"},
				},
			},
			{
				Method:  http.MethodPost,
				Path:    "/categories/{category}/items",
				Summary: "Create item",
				Parameters: []typescript.Param{
					{Name: "category", In: "path", Required: true, Validate: "required", Model: ""},
					{Name: "dry_run", In: "query", Required: false, Model: false},
					{Name: "X-Trace-ID", In: "header", Required: false, Model: ""},
				},
				RequestBody: &typescript.RequestBody{
					ContentType: "application/json",
					Required:    true,
					Model:       CreateItemRequest{},
				},
				Responses: map[int]*typescript.Response{
					http.StatusCreated:  {ContentType: "application/json", Model: ItemResponse{}},
					http.StatusConflict: {ContentType: "application/json", Model: ConflictError{}},
				},
			},
			{
				Method:  http.MethodPost,
				Path:    "/upload",
				Summary: "Upload avatar",
				RequestBody: &typescript.RequestBody{
					ContentType: "multipart/form-data",
					Required:    true,
					Model:       UploadFormRequest{},
				},
				Responses: map[int]*typescript.Response{
					http.StatusOK: {ContentType: "application/json", Model: UploadResponse{}},
				},
			},
			{
				Method:  http.MethodGet,
				Path:    "/events",
				Summary: "Event stream",
				Responses: map[int]*typescript.Response{
					http.StatusOK: {ContentType: "text/event-stream"},
				},
			},
			{
				Method: http.MethodGet,
				Path:   "/internal",
				Hidden: true,
			},
		},
	}

	sdkFiles, err := typescript.Generate(spec)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var allTS strings.Builder
	for _, content := range sdkFiles {
		allTS.WriteString(content)
		allTS.WriteByte('\n')
	}
	combined := allTS.String()

	checks := []string{
		"Auto-generated TypeScript SDK. Do not edit by hand.",
		"API: Catalog API (2.0.0)",
		"* Payload for creating a catalog item.",
		"title: string;",
		`priority?: "low" | "normal" | "high";`,
		`"is-featured"?: boolean;`,
		"createdAt?: string;",
		"description: string | null;",
		`priority: "low" | "normal" | "high";`,
		"avatar: File | Blob;",
		`"GET /health":`,
		`"POST /categories/{category}/items":`,
		`params: { category: string };`,
		`headers?: { "X-Trace-ID"?: string };`,
		`body: CreateItemRequest;`,
		`error: ConflictError;`,
		`"POST /upload":`,
		`body: FormData | UploadFormRequest;`,
		`"GET /events":`,
		`response: ReadableStream<string>;`,
	}
	for _, want := range checks {
		if !strings.Contains(combined, want) {
			t.Errorf("expected %q in generated SDK files, got:\n%s", want, combined)
		}
	}

	if strings.Contains(combined, "Hidden") {
		t.Errorf("expected json:\"-\" field Hidden to be omitted from generated SDK")
	}
	if strings.Contains(combined, "/internal") {
		t.Errorf("expected Hidden route to be omitted from generated SDK")
	}

	indexTS := sdkFiles["index.ts"]
	runtimeTS := sdkFiles["runtime.ts"]
	for _, want := range []string{
		"export function createClient",
		"export function isEndpointError",
		`"POST /upload": "multipart"`,
	} {
		if !strings.Contains(indexTS, want) {
			t.Errorf("expected %q in generated index.ts", want)
		}
	}
	for _, want := range []string{
		"export function isHttpError",
		"export function isAbortError",
		"export function parseError",
		"export class HttpError",
		"export const DEFAULT_RETRY_METHODS",
	} {
		if !strings.Contains(runtimeTS, want) {
			t.Errorf("expected %q in generated runtime.ts", want)
		}
	}
}

func TestReservedNameCollision(t *testing.T) {
	type Promise struct {
		Value string `json:"value"`
	}

	spec := typescript.Spec{
		Config: typescript.Config{Title: "Collision API"},
		Routes: []typescript.Route{
			{
				Method: http.MethodGet,
				Path:   "/p",
				Responses: map[int]*typescript.Response{
					http.StatusOK: {ContentType: "application/json", Model: Promise{}},
				},
			},
		},
	}

	sdkFiles, err := typescript.Generate(spec)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	typesP := sdkFiles["types/p.ts"]
	if strings.Contains(typesP, "export interface Promise {") {
		t.Errorf("generated schema shadowed reserved global Promise:\n%s", typesP)
	}
	if !strings.Contains(typesP, "export interface typescript_test_Promise {") && !strings.Contains(typesP, "export interface Promise_2 {") {
		t.Errorf("expected disambiguated Promise interface name in:\n%s", typesP)
	}
}

func TestDeterminismAndOrderIndependence(t *testing.T) {
	type SharedModel struct {
		Name  string  `json:"name"`
		Bio   *string `json:"bio"`
		Token string  `json:"token,omitempty"`
	}

	postRoute := typescript.Route{
		Method:      http.MethodPost,
		Path:        "/input",
		RequestBody: &typescript.RequestBody{ContentType: "application/json", Required: true, Model: SharedModel{}},
		Responses:   map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json"}},
	}
	getRoute := typescript.Route{
		Method:    http.MethodGet,
		Path:      "/output",
		Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: SharedModel{}}},
	}

	sdk1, err := typescript.Generate(typescript.Spec{
		Config: typescript.Config{Title: "Order"},
		Routes: []typescript.Route{postRoute, getRoute},
	})
	if err != nil {
		t.Fatalf("Generate(1) failed: %v", err)
	}
	sdk2, err := typescript.Generate(typescript.Spec{
		Config: typescript.Config{Title: "Order"},
		Routes: []typescript.Route{getRoute, postRoute},
	})
	if err != nil {
		t.Fatalf("Generate(2) failed: %v", err)
	}

	if !maps.Equal(sdk1, sdk2) {
		t.Errorf("expected deterministic registration-order-independent SDK output")
	}
	sharedTypes := sdk1["types/_shared.ts"]
	if !strings.Contains(sharedTypes, "bio?: string;") {
		t.Errorf("expected shared model to keep request optionality (bio?: string), got:\n%s", sharedTypes)
	}
}

func TestExportAndRunCodegen(t *testing.T) {
	spec := typescript.Spec{
		Config: typescript.Config{Title: "Export API"},
		Routes: []typescript.Route{
			{
				Method:    http.MethodGet,
				Path:      "/ping",
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: ""}},
			},
		},
	}

	tmpDir := t.TempDir()
	sdkOut := filepath.Join(tmpDir, "nested", "sdk")

	handled, err := typescript.RunCodegen(spec, []string{"--ts-sdk=" + sdkOut})
	if !handled || err != nil {
		t.Fatalf("RunCodegen failed: handled=%v err=%v", handled, err)
	}

	indexBytes, err := os.ReadFile(filepath.Join(sdkOut, "index.ts"))
	if err != nil || !strings.Contains(string(indexBytes), "createClient") {
		t.Fatalf("failed reading exported sdk/index.ts: err=%v", err)
	}
	runtimeBytes, err := os.ReadFile(filepath.Join(sdkOut, "runtime.ts"))
	if err != nil || !strings.Contains(string(runtimeBytes), "createClientFor") {
		t.Fatalf("failed reading exported sdk/runtime.ts: err=%v", err)
	}

	// Bare --ts-sdk should default to ./dist.
	t.Chdir(tmpDir)
	handled, err = typescript.RunCodegen(spec, []string{"--ts-sdk"})
	if !handled || err != nil {
		t.Fatalf("RunCodegen --ts-sdk failed: handled=%v err=%v", handled, err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "dist", "index.ts")); err != nil {
		t.Fatalf("expected bare --ts-sdk to write ./dist/index.ts, err=%v", err)
	}

	handled, err = typescript.RunCodegen(spec, []string{"--port=8080"})
	if handled || err != nil {
		t.Errorf("expected unrelated flags to return handled=false, err=nil; got handled=%v err=%v", handled, err)
	}
}

func TestSDKFilesLayoutAndSharing(t *testing.T) {
	type SharedLeaf struct {
		Code string `json:"code"`
	}
	type SharedMeta struct {
		Leaf SharedLeaf `json:"leaf"`
	}
	type RecursiveNode struct {
		ID   string         `json:"id"`
		Next *RecursiveNode `json:"next,omitempty"`
	}
	type TodoItem struct {
		ID   string         `json:"id"`
		Meta SharedMeta     `json:"meta"`
		Node *RecursiveNode `json:"node,omitempty"`
	}
	// Intentionally collides with the generated module interface name for tag "Todos".
	type TodosEndpoints struct {
		Marker string `json:"marker"`
	}
	type ProfileRes struct {
		Meta SharedMeta `json:"meta"`
	}

	todosRoutes := []typescript.Route{
		{
			Method:    http.MethodGet,
			Path:      "/todos",
			Tags:      []string{"Todos"},
			Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: []TodoItem(nil)}},
		},
		{
			Method:      http.MethodPost,
			Path:        "/todos/meta",
			Tags:        []string{"Todos"},
			RequestBody: &typescript.RequestBody{ContentType: "application/json", Required: true, Model: TodosEndpoints{}},
			Responses:   map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json"}},
		},
	}
	usersRoute := typescript.Route{
		Method:    http.MethodGet,
		Path:      "/profiles",
		Tags:      []string{"User Profiles"},
		Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: ProfileRes{}}},
	}
	accentedRoute := typescript.Route{
		Method:    http.MethodGet,
		Path:      "/usuarios",
		Tags:      []string{"Usuários"},
		Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json"}},
	}
	untaggedRoutes := []typescript.Route{
		{
			Method:    http.MethodGet,
			Path:      "/ping",
			Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: ""}},
		},
		{
			Method:    http.MethodGet,
			Path:      "/",
			Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: ""}},
		},
	}

	buildSpec := func(reverse bool) typescript.Spec {
		var routes []typescript.Route
		if reverse {
			routes = append(routes, untaggedRoutes...)
			routes = append(routes, accentedRoute, usersRoute)
			routes = append(routes, todosRoutes...)
		} else {
			routes = append(routes, todosRoutes...)
			routes = append(routes, usersRoute, accentedRoute)
			routes = append(routes, untaggedRoutes...)
		}
		return typescript.Spec{
			Config: typescript.Config{Title: "Modular API", Version: "1.0.0"},
			Routes: routes,
		}
	}

	files1, err := typescript.Generate(buildSpec(false))
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	files2, err := typescript.Generate(buildSpec(true))
	if err != nil {
		t.Fatalf("Generate(reverse) failed: %v", err)
	}
	if !maps.Equal(files1, files2) {
		t.Fatalf("expected deterministic route-order-independent SDK files")
	}

	expectedPaths := []string{
		"index.ts",
		"runtime.ts",
		"types/_shared.ts",
		"types/todos.ts",
		"types/user-profiles.ts",
		"endpoints/default.ts",
		"endpoints/ping.ts",
		"endpoints/todos.ts",
		"endpoints/user-profiles.ts",
		"endpoints/usuarios.ts",
	}
	for _, p := range expectedPaths {
		if _, ok := files1[p]; !ok {
			t.Errorf("expected generated file %q, got keys: %v", p, keysOf(files1))
		}
	}

	// SharedMeta and its transitive dependency SharedLeaf must both live in _shared.ts.
	shared := files1["types/_shared.ts"]
	if !strings.Contains(shared, "export interface SharedMeta") || !strings.Contains(shared, "export interface SharedLeaf") {
		t.Errorf("expected SharedMeta and SharedLeaf in types/_shared.ts, got:\n%s", shared)
	}
	if strings.Contains(shared, "import ") {
		t.Errorf("types/_shared.ts must not import module files:\n%s", shared)
	}

	// TodoItem and RecursiveNode belong only to todos.ts, and RecursiveNode must not self-import.
	todosTypes := files1["types/todos.ts"]
	if !strings.Contains(todosTypes, "export interface TodoItem") || !strings.Contains(todosTypes, "export interface RecursiveNode") {
		t.Errorf("expected TodoItem and RecursiveNode in types/todos.ts, got:\n%s", todosTypes)
	}
	if !strings.Contains(todosTypes, `import type { SharedMeta } from "./_shared";`) {
		t.Errorf("expected types/todos.ts to import SharedMeta from ./_shared, got:\n%s", todosTypes)
	}
	if strings.Contains(todosTypes, `from "./todos"`) {
		t.Errorf("types/todos.ts must not self-import:\n%s", todosTypes)
	}

	// Struct named TodosEndpoints must be disambiguated so it doesn't collide with TodosEndpoints module interface.
	todosEP := files1["endpoints/todos.ts"]
	if !strings.Contains(todosEP, "export interface TodosEndpoints {") {
		t.Errorf("expected module interface TodosEndpoints in endpoints/todos.ts")
	}
	if strings.Contains(todosTypes, "export interface TodosEndpoints {") {
		t.Errorf("schema TodosEndpoints should have been disambiguated:\n%s", todosTypes)
	}

	// Tree-shaking checks.
	if !strings.Contains(files1["runtime.ts"], "/* @__PURE__ */") {
		t.Errorf("expected /* @__PURE__ */ annotation in runtime.ts for tree-shaking")
	}
	if !strings.Contains(files1["index.ts"], `export type * from "./types/todos";`) {
		t.Errorf("expected export type * in index.ts")
	}

	// ImportExtension validation and support.
	if _, err := typescript.Generate(buildSpec(false), typescript.Options{ImportExtension: ".mjs"}); err == nil {
		t.Errorf("expected error for unsupported ImportExtension")
	}
	jsFiles, err := typescript.Generate(buildSpec(false), typescript.Options{ImportExtension: ".js"})
	if err != nil {
		t.Fatalf("Generate(.js) failed: %v", err)
	}
	if !strings.Contains(jsFiles["index.ts"], `from "./runtime.js"`) ||
		!strings.Contains(jsFiles["endpoints/todos.ts"], `from "../types/todos.js"`) {
		t.Errorf("expected .js import specifiers when ImportExtension is .js")
	}
}

func TestTagsFallbackAndAutoImportExtension(t *testing.T) {
	spec := typescript.Spec{
		Config: typescript.Config{Title: "Group & Ext API"},
		Routes: []typescript.Route{
			{Method: http.MethodGet, Path: "/api/v1/items/list", Tags: []string{"Catalog"}},
			{Method: http.MethodGet, Path: "/api/v1/items/special", Tags: []string{"Special", "Catalog"}},
			{Method: http.MethodGet, Path: "/api/v1/admin/stats", Tags: []string{"Admin", "Catalog"}},
			{Method: http.MethodGet, Path: "/api/v2/orders/{id}"},
			{Method: http.MethodGet, Path: "/health"},
			{Method: http.MethodGet, Path: "/{tenant}"},
		},
	}

	files, err := typescript.Generate(spec)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	for _, want := range []string{
		"endpoints/catalog.ts",
		"endpoints/special.ts",
		"endpoints/admin.ts",
		"endpoints/orders.ts",
		"endpoints/health.ts",
		"endpoints/default.ts",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("expected generated module %q, got keys: %v", want, keysOf(files))
		}
	}

	// DetectImportExtension walks ancestor config files.
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "empty", want: ""},
		{name: "deno", files: map[string]string{"deno.json": `{}`}, want: ".ts"},
		{
			name: "tsconfig allowImportingTsExtensions (JSONC)",
			files: map[string]string{
				"tsconfig.json": `{
					// comment
					"compilerOptions": {
						"moduleResolution": "bundler",
						"allowImportingTsExtensions": true, /* trailing */
					}
				}`,
			},
			want: ".ts",
		},
		{
			name: "tsconfig bundler",
			files: map[string]string{
				"tsconfig.json": `{"compilerOptions": {"moduleResolution": "bundler"}}`,
			},
			want: "",
		},
		{
			name: "tsconfig nodenext",
			files: map[string]string{
				"tsconfig.json": `{"compilerOptions": {"moduleResolution": "nodenext"}}`,
			},
			want: ".js",
		},
		{
			name: "package module",
			files: map[string]string{
				"package.json": `{"type": "module"}`,
			},
			want: ".js",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			outDir := filepath.Join(root, "src", "sdk")
			if got := typescript.DetectImportExtension(outDir); got != tc.want {
				t.Errorf("DetectImportExtension() = %q, want %q", got, tc.want)
			}
		})
	}

	// RunCodegen --ts-import-ext=auto exports & checks consistently against nearest package.json.
	autoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(autoRoot, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	autoSDK := filepath.Join(autoRoot, "client", "sdk")
	if handled, err := typescript.RunCodegen(spec, []string{"--ts-sdk=" + autoSDK, "--ts-import-ext=auto"}); !handled || err != nil {
		t.Fatalf("RunCodegen --ts-import-ext=auto failed: handled=%v err=%v", handled, err)
	}
	indexBytes, err := os.ReadFile(filepath.Join(autoSDK, "index.ts"))
	if err != nil || !strings.Contains(string(indexBytes), `from "./runtime.js"`) {
		t.Fatalf("expected .js import specifier from auto-detection, err=%v content:\n%s", err, string(indexBytes))
	}
	if handled, err := typescript.RunCodegen(spec, []string{"--ts-sdk=" + autoSDK, "--ts-import-ext=auto", "--check"}); !handled || err != nil {
		t.Fatalf("RunCodegen --ts-import-ext=auto --check failed: handled=%v err=%v", handled, err)
	}
}

func TestExportDirStaleCleanupAndCheck(t *testing.T) {
	type Item struct {
		ID string `json:"id"`
	}

	spec := typescript.Spec{
		Config: typescript.Config{Title: "Check API"},
		Routes: []typescript.Route{
			{
				Method:    http.MethodGet,
				Path:      "/items",
				Tags:      []string{"Items"},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: []Item(nil)}},
			},
		},
	}

	tmpDir := t.TempDir()
	sdkDir := filepath.Join(tmpDir, "sdk")

	// Export everything via RunCodegen.
	handled, err := typescript.RunCodegen(spec, []string{
		"--ts-sdk=" + sdkDir,
	})
	if !handled || err != nil {
		t.Fatalf("RunCodegen export failed: handled=%v err=%v", handled, err)
	}

	// --check should succeed right after export.
	if handled, err := typescript.RunCodegen(spec, []string{
		"--ts-sdk=" + sdkDir,
		"--check",
	}); !handled || err != nil {
		t.Fatalf("RunCodegen --check failed: handled=%v err=%v", handled, err)
	}

	// Write both a user-owned file (no markers) and a stale managed file inside sdkDir.
	customFile := filepath.Join(sdkDir, "custom.ts")
	if err := os.WriteFile(customFile, []byte("export const myHelper = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(sdkDir, "endpoints", "legacy.ts")
	staleContent := "/**\n * Auto-generated TypeScript SDK. Do not edit by hand.\n * This directory is managed by TypeScript SDK codegen\n */\nexport interface LegacyEndpoints {}\n"
	if err := os.WriteFile(staleFile, []byte(staleContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Modify index.ts so --check detects both changed and stale files.
	indexPath := filepath.Join(sdkDir, "index.ts")
	if err := os.WriteFile(indexPath, []byte("// dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := typescript.Check(spec, sdkDir); err == nil ||
		!strings.Contains(err.Error(), "changed [index.ts]") ||
		!strings.Contains(err.Error(), "stale [endpoints/legacy.ts]") {
		t.Fatalf("expected Check to report changed index.ts and stale endpoints/legacy.ts, got: %v", err)
	}

	// Re-exporting should remove staleFile, restore index.ts, and preserve custom.ts.
	if err := typescript.Export(spec, sdkDir); err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("expected stale managed file %s to be deleted, stat err=%v", staleFile, err)
	}
	if _, err := os.Stat(customFile); err != nil {
		t.Errorf("expected user file %s to be preserved, stat err=%v", customFile, err)
	}
	if err := typescript.Check(spec, sdkDir); err != nil {
		t.Errorf("expected Check to pass after re-export, got: %v", err)
	}

	// Invalid --check flag combinations.
	if handled, err := typescript.RunCodegen(spec, []string{"--check"}); !handled || err == nil {
		t.Errorf("expected --check without target to return handled=true and error")
	}
	if handled, err := typescript.RunCodegen(spec, []string{"--ts-import-ext=.bad"}); !handled || err == nil {
		t.Errorf("expected invalid --ts-import-ext to return handled=true and error")
	}
}

func TestEdgeCasesAndStrictTypeCheck(t *testing.T) {
	type BaseA struct {
		ID    string `json:"id"`
		OnlyA string `json:"onlyA"`
	}
	type BaseB struct {
		FormID int    `form:"id"`
		OnlyB  string `json:"onlyB"`
	}
	type Combined struct {
		BaseA
		BaseB
	}

	spec := typescript.Spec{
		Config: typescript.Config{Title: "Edge Cases API"},
		Routes: []typescript.Route{
			{
				Method:    http.MethodGet,
				Path:      "/combined",
				Tags:      []string{"Edge"},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: Combined{}}},
			},
			{
				Method:    http.MethodGet,
				Path:      "/any-json",
				Tags:      []string{"Edge"},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json"}},
			},
			{
				Method: http.MethodGet,
				Path:   "/dup-params",
				Tags:   []string{"Edge"},
				Parameters: []typescript.Param{
					{Name: "q", In: "query", Required: false, Model: ""},
					{Name: "q", In: "query", Required: true, Model: ""},
					{Name: "X-Req", In: "header", Required: false, Model: ""},
					{Name: "X-Req", In: "header", Required: true, Model: ""},
				},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json"}},
			},
			{
				Method:    http.MethodGet,
				Path:      "/gzip",
				Tags:      []string{"Edge"},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/gzip; charset=binary"}},
			},
			{
				Method:    http.MethodGet,
				Path:      "/xml",
				Tags:      []string{"Edge"},
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "Application/XML; charset=utf-8"}},
			},
		},
	}

	files, err := typescript.Generate(spec, typescript.Options{ImportExtension: ".ts"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	edgeTypes := files["types/edge.ts"]
	if strings.Count(edgeTypes, "id: ") != 1 {
		t.Errorf("expected deduplicated id property in Combined, got:\n%s", edgeTypes)
	}

	edgeEP := files["endpoints/edge.ts"]
	for _, want := range []string{
		"query: { q: string };",
		`headers: { "X-Req": string };`,
		`response: unknown;`,
		`response: Blob;`,
		`response: string;`,
	} {
		if !strings.Contains(edgeEP, want) {
			t.Errorf("expected %q in endpoints/edge.ts, got:\n%s", want, edgeEP)
		}
	}

	if denoPath, err := exec.LookPath("deno"); err == nil {
		tmpDir := t.TempDir()
		sdkDir := filepath.Join(tmpDir, "sdk")
		if err := typescript.Export(spec, sdkDir, typescript.Options{ImportExtension: ".ts"}); err != nil {
			t.Fatalf("Export failed: %v", err)
		}
		denoConfig := filepath.Join(tmpDir, "deno.json")
		cfgJSON := `{"compilerOptions":{"strict":true,"exactOptionalPropertyTypes":true,"noUncheckedIndexedAccess":true,"erasableSyntaxOnly":true}}`
		if err := os.WriteFile(denoConfig, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		consumerPath := filepath.Join(tmpDir, "consumer.ts")
		consumerTS := `import { createClient } from "./sdk/index.ts";
const api = createClient("http://localhost", {
  retry: false,
  async onUnauthorized() {
    return false;
  },
});
async function verify() {
  const c = await api.fetch("GET /combined", { retry: false });
  const c2 = await api.fetch("GET /combined", { retry: true });
  const c3 = await api.fetch("GET /combined", { retry: { delayMs: 100 } });
  const a: unknown = await api.fetch("GET /any-json");
  const b: Blob = await api.fetch("GET /gzip");
  const x: string = await api.fetch("GET /xml");
  const d: unknown = await api.fetch("GET /dup-params", { query: { q: "1" }, headers: { "X-Req": "v" } });
  void [c.id, c2.id, c3.id, c.onlyA, c.onlyB, a, b.size, x.length, d];
}
void verify;
`
		if err := os.WriteFile(consumerPath, []byte(consumerTS), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(denoPath, "check", "--config", denoConfig, consumerPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("deno check failed: %v\n%s", err, string(out))
		}
	}
}

func TestOnUnauthorizedRefresh(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping Node E2E SDK execution")
	}

	type CreateReq struct {
		Title string `json:"title" validate:"required"`
	}
	type ItemRes struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}

	spec := typescript.Spec{
		Config: typescript.Config{Title: "Auth Refresh API"},
		Routes: []typescript.Route{
			{
				Method:      http.MethodPost,
				Path:        "/items",
				RequestBody: &typescript.RequestBody{ContentType: "application/json", Required: true, Model: CreateReq{}},
				Responses:   map[int]*typescript.Response{http.StatusCreated: {ContentType: "application/json", Model: ItemRes{}}},
			},
			{
				Method:    http.MethodGet,
				Path:      "/items",
				Responses: map[int]*typescript.Response{http.StatusOK: {ContentType: "application/json", Model: []ItemRes(nil)}},
			},
		},
	}

	tmpDir := t.TempDir()
	sdkDir := filepath.Join(tmpDir, "sdk")
	if err := typescript.Export(spec, sdkDir, typescript.Options{ImportExtension: ".ts"}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	script := `import assert from "node:assert/strict";
import { createClient, isHttpError } from "./sdk/index.ts";

let token = "expired";
let refreshCalls = 0;
let onRequestCalls = 0;
let onErrorCalls = 0;
const seenBodies: string[] = [];

const client = createClient("https://example.test", {
  headers: () => ({ Authorization: "Bearer " + token }),
  onRequest() {
    onRequestCalls++;
  },
  onError() {
    onErrorCalls++;
  },
  async onUnauthorized() {
    refreshCalls++;
    if (token === "reject-refresh") return false;
    if (token === "throw-refresh") throw new Error("Refresh failed");
    token = "fresh";
    return true;
  },
  async fetch(_url, init) {
    const auth = init.headers.get("Authorization");
    if (init.body) seenBodies.push(String(init.body));
    const mode = init.headers.get("X-Case");
    if (mode === "still401" || auth === "Bearer expired") {
      return new Response(JSON.stringify({ message: "Token expired" }), {
        status: 401,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response(JSON.stringify({ id: "1", title: "Ship" }), {
      status: 201,
      headers: { "content-type": "application/json" },
    });
  },
});

const created = await client.fetch("POST /items", { body: { title: "Ship" } });
assert.equal(created.id, "1");
assert.equal(refreshCalls, 1);
assert.equal(onRequestCalls, 2);
assert.equal(seenBodies.length, 2);
assert.equal(seenBodies[0], seenBodies[1]);

token = "fresh";
await assert.rejects(
  () => client.fetch("GET /items", { headers: { "X-Case": "still401" } }),
  (err) => isHttpError(err) && err.status === 401 && (err.data as { message?: string }).message === "Token expired",
);
assert.equal(refreshCalls, 2);

token = "reject-refresh";
await assert.rejects(
  () => client.fetch("GET /items", { headers: { "X-Case": "still401" } }),
  (err) => isHttpError(err) && err.status === 401 && (err.data as { message?: string }).message === "Token expired",
);
assert.equal(refreshCalls, 3);

token = "throw-refresh";
const beforeErrors = onErrorCalls;
await assert.rejects(
  () => client.fetch("GET /items", { headers: { "X-Case": "still401" } }),
  (err) => err instanceof Error && err.message === "Refresh failed",
);
assert.equal(onErrorCalls, beforeErrors + 1);
`
	runnerPath := filepath.Join(tmpDir, "refresh_test.mts")
	if err := os.WriteFile(runnerPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(nodePath, "--experimental-strip-types", runnerPath)
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node refresh E2E failed: %v\n%s", err, string(out))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
