package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resolveTSCCmd returns the tsc invocation if available (direct tsc binary or via npx),
// or nil if TypeScript is not installed on the system.
func resolveTSCCmd() []string {
	if path, err := exec.LookPath("tsc"); err == nil {
		return []string{path}
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return []string{path, "--yes", "-p", "typescript", "tsc"}
	}
	return nil
}

func TestTypeScript_StrictTSCTypeCheck(t *testing.T) {
	tscCmd := resolveTSCCmd()
	if tscCmd == nil {
		t.Skip("TypeScript compiler (tsc/npx) not found on PATH; skipping strict E2E type-check")
	}

	app := New(Config{
		Title:       "TSC Validation API",
		Version:     "1.0.0",
		Description: "End-to-end verification that generated SDK and declarations pass strict tsc.",
	})

	type ComplexQueryReq struct {
		Q       string `query:"q" validate:"required"`
		Page    *int   `query:"page"`
		OrderBy string `query:"order_by" validate:"oneof=asc desc"`
	}

	type OptionalQueryReq struct {
		Limit *int `query:"limit"`
	}

	type ItemPayload struct {
		_           struct{} `doc:"Payload for creating a new catalog item."`
		Title       string   `json:"title" validate:"required"`
		Tags        []string `json:"tags,omitempty"`
		Description *string  `json:"description"`
		IsSpecial   bool     `json:"is-special"` // hyphenated prop name
	}

	type ItemResponse struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Description *string `json:"description"` // response pointer -> string | null
		IsSpecial   bool    `json:"is-special"`
	}

	type ErrorDetail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	// 1. Endpoint without inputs
	app.Get("/health", func(ctx *Context) error {
		return ctx.Send("ok")
	}).Response(200, "ok")

	// 2. Endpoint with only optional query
	app.Get("/items/optional", func(ctx *Context) error {
		return ctx.Json([]ItemResponse{})
	}).
		Query("limit", 10).
		Response(200, []ItemResponse{})

	// 3. Endpoint with required query + path param
	catRoute := app.Get("/categories/:category/items", func(ctx *Context) error {
		return ctx.Json([]ItemResponse{})
	}).
		Param("category", "").
		Query("q", "").
		Response(200, []ItemResponse{})
	// Mark 'q' as required in Doc
	catRoute.Node.Doc.Parameters[1].Required = true

	// 4. Endpoint with required body + typed error responses
	app.Post("/items", func(ctx *Context) error {
		return ctx.Json(ItemResponse{})
	}).
		Body(ItemPayload{}).
		Response(201, ItemResponse{}).
		Response(400, ErrorDetail{})

	sdkContent := app.GenerateTypeScriptSDK()
	typesContent := app.GenerateTypeScript()

	tmpDir := t.TempDir()
	sdkPath := filepath.Join(tmpDir, "sdk.ts")
	typesPath := filepath.Join(tmpDir, "types.d.ts")
	consumerPath := filepath.Join(tmpDir, "consumer.ts")

	if err := os.WriteFile(sdkPath, []byte(sdkContent), 0644); err != nil {
		t.Fatalf("failed to write sdk.ts: %v", err)
	}
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatalf("failed to write types.d.ts: %v", err)
	}

	// Verify struct-level JSDoc was generated
	if !strings.Contains(typesContent, "* Payload for creating a new catalog item.") {
		t.Errorf("expected struct-level JSDoc in types.d.ts, got:\n%s", typesContent)
	}

	// Consumer script asserting end-to-end type safety:
	// - endpoints without inputs can be called with 0 arguments
	// - endpoints with only optional query can be called with 0 arguments
	// - endpoints with required query cannot omit options or required query key
	// - endpoints with required body cannot omit body
	// - ResponseError resolves to typed union
	// - response fields with pointer become T | null
	consumerTS := `
import { createGotaClient, GotaHttpError, isGotaHttpError, DEFAULT_RETRY_METHODS, type ResponseError, type ResponseData } from "./sdk.ts";

async function testTyping() {
  const api = createGotaClient("https://api.example.com", {
    next: { revalidate: 60 },
    onRequest: (cfg) => {
      cfg.init.headers.set("X-Client", "gota");
    },
    onResponse: (res) => {
      void res.status;
    },
    onError: (err) => {
      void err.message;
    }
  });

  // 1. Endpoint without inputs: compiles with 0 args
  const h: string = await api.fetch("GET /health");

  // 2. Endpoint with only optional query: compiles with 0 args or optional query
  const optList1: ResponseData<"GET /items/optional"> = await api.fetch("GET /items/optional");
  const optList2 = await api.fetch("GET /items/optional", {
    query: { limit: 5 },
    next: { tags: ["items"] },
    retry: { attempts: 2, methods: [...DEFAULT_RETRY_METHODS, "PATCH"] },
    onRequest: (cfg) => {
      cfg.init.headers.set("X-Req", "1");
    }
  });

  // 3. Endpoint with required query + path param
  const catItems = await api.fetch("GET /categories/:category/items", {
    params: { category: "books" },
    query: { q: "golang" }
  });

  // 4. Endpoint with body
  const created = await api.fetch("POST /items", {
    body: {
      title: "Clean Code",
      "is-special": true,
      description: "A great book"
    }
  });
  const id: string = created.id;
  const desc: string | null = created.description;
  const special: boolean = created["is-special"];

  // 5. Error handling with isGotaHttpError<T>
  try {
    await api.fetch("POST /items", {
      body: { title: "Test", "is-special": false }
    });
  } catch (err) {
    if (isGotaHttpError<ResponseError<"POST /items">>(err)) {
      const errData = err.data;
      if (typeof errData === "object" && errData !== null && "code" in errData) {
        const code: string = errData.code;
        console.log(code);
      }
    }
    if (err instanceof GotaHttpError) {
      console.log(err.status);
    }
  }

  // Type assertion tests (verify compilation errors via comments if needed)
  // @ts-expect-error Missing required path & query arguments
  await api.fetch("GET /categories/:category/items");

  // @ts-expect-error Missing required body argument
  await api.fetch("POST /items");
}
`
	if err := os.WriteFile(consumerPath, []byte(consumerTS), 0644); err != nil {
		t.Fatalf("failed to write consumer.ts: %v", err)
	}

	flags := []string{
		"--noEmit",
		"--strict",
		"--exactOptionalPropertyTypes",
		"--noUncheckedIndexedAccess",
		"--target", "es2022",
		"--lib", "es2022,dom,dom.iterable",
		"--skipLibCheck",
		"--allowImportingTsExtensions",
	}

	// Test 1: consumer.ts + sdk.ts typecheck
	args := append(append([]string{}, tscCmd[1:]...), flags...)
	args = append(args, "consumer.ts")
	cmd := exec.Command(tscCmd[0], args...)
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc failed on consumer.ts/sdk.ts:\n%s", string(out))
	}

	// Test 2: types.d.ts typecheck
	dtsArgs := append(append([]string{}, tscCmd[1:]...), flags...)
	dtsArgs = append(dtsArgs, "types.d.ts")
	dtsCmd := exec.Command(tscCmd[0], dtsArgs...)
	dtsCmd.Dir = tmpDir
	dtsOut, dtsErr := dtsCmd.CombinedOutput()
	if dtsErr != nil {
		t.Fatalf("tsc failed on types.d.ts:\n%s", string(dtsOut))
	}
}

func TestTypeScript_DeterminismAndOrderIndependence(t *testing.T) {
	type SharedModel struct {
		Name  string  `json:"name"`
		Bio   *string `json:"bio"`
		Token string  `json:"token,omitempty"`
	}

	makeApp1 := func() *App {
		a := New(Config{Title: "Order1"})
		// Route 1 (POST input) registered first
		a.Post("/input", func(ctx *Context) error { return nil }).
			Body(SharedModel{}).Response(200, "ok")
		// Route 2 (GET output) registered second
		a.Get("/output", func(ctx *Context) error { return ctx.Json(SharedModel{}) }).
			Response(200, SharedModel{})
		return a
	}

	makeApp2 := func() *App {
		a := New(Config{Title: "Order1"})
		// Route 2 (GET output) registered first
		a.Get("/output", func(ctx *Context) error { return ctx.Json(SharedModel{}) }).
			Response(200, SharedModel{})
		// Route 1 (POST input) registered second
		a.Post("/input", func(ctx *Context) error { return nil }).
			Body(SharedModel{}).Response(200, "ok")
		return a
	}

	app1 := makeApp1()
	app2 := makeApp2()

	sdk1 := app1.GenerateTypeScriptSDK()
	sdk2 := app2.GenerateTypeScriptSDK()

	if sdk1 != sdk2 {
		t.Errorf("expected deterministic, registration-order-independent TypeScript output, but outputs differed")
	}

	// Since SharedModel is used in both request and response, it should NOT be response-only.
	// Therefore bio should be bio?: string (optional), not bio: string | null.
	if !strings.Contains(sdk1, "bio?: string;") {
		t.Errorf("expected shared model (used in request) to retain request validation optionality (bio?: string), got:\n%s", sdk1)
	}
}
