package gota_test

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/repr"
	"github.com/carlosxfelipe/gota/typescript"
)

func TestTypeScriptAdapter_REPRAndNonREPRRoutes(t *testing.T) {
	type healthStatus struct {
		Status string `json:"status" example:"ok"`
	}
	type createNoteReq struct {
		Text string `json:"text"`
	}
	type createNoteRes struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}

	app := gota.New(repr.Config{Title: "Mixed API", Version: "1.0.0"})

	// Raw app.Handle stays invisible in TypeScript SDK.
	app.Handle(http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	}))

	app.Get("/health", func(ctx *repr.Context) error {
		return ctx.JSON(healthStatus{Status: "ok"})
	}).Summary("Health check").Tags("System").Response(http.StatusOK, healthStatus{}, "Healthy")

	app.HandleHTTP(http.MethodGet, "/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong"))
	})).Summary("Ping").Tags("System").ResponseMedia(http.StatusOK, "text/plain", nil, "Pong")

	api := app.Group("/api").Security("BearerAuth")
	api.Post("/notes/{bucket}", func(ctx *repr.Context) error {
		return ctx.NoContent()
	}).
		Summary("Create note").
		Param("query", "tag", "", false, "Optional tag").
		Request(createNoteReq{}, "Note payload").
		Response(http.StatusCreated, createNoteRes{}, "Created")

	api.Get("/secret", func(ctx *repr.Context) error {
		return ctx.NoContent()
	}).Hidden()

	v1 := app.Group("/api/v1").Tags("Catalog")
	items := v1.Group("/items")
	repr.Get(items, "/list", func(_ *repr.Context, _ repr.Empty) (string, error) {
		return "list", nil
	})
	repr.Get(items, "/special", func(_ *repr.Context, _ repr.Empty) (string, error) {
		return "special", nil
	}).Tags("Special")

	sdkFiles, err := gota.GenerateTypeScriptSDK(app)
	if err != nil {
		t.Fatalf("GenerateTypeScriptSDK failed: %v", err)
	}

	sysEP := sdkFiles["endpoints/system.ts"]
	notesEP := sdkFiles["endpoints/notes.ts"]
	for _, want := range []string{
		`"GET /health"`,
		`response: healthStatus;`,
		`"GET /ping"`,
		`response: string;`,
	} {
		if !strings.Contains(sysEP, want) {
			t.Errorf("expected %q in endpoints/system.ts:\n%s", want, sysEP)
		}
	}
	for _, want := range []string{
		`"POST /api/notes/{bucket}"`,
		`params: { bucket: string };`,
		`query?: { tag?: string };`,
		`body: createNoteReq;`,
		`response: createNoteRes;`,
	} {
		if !strings.Contains(notesEP, want) {
			t.Errorf("expected %q in endpoints/notes.ts:\n%s", want, notesEP)
		}
	}
	if _, ok := sdkFiles["endpoints/catalog.ts"]; !ok {
		t.Errorf("expected inherited group tag endpoints/catalog.ts")
	}
	if _, ok := sdkFiles["endpoints/special.ts"]; !ok {
		t.Errorf("expected explicit route tag override endpoints/special.ts")
	}
	allSDK := strings.Join([]string{sdkFiles["index.ts"], sysEP, notesEP}, "\n")
	if strings.Contains(allSDK, `"GET /"`) || strings.Contains(allSDK, `"/api/secret"`) {
		t.Errorf("unexpected hidden/raw routes in TypeScript SDK:\n%s", allSDK)
	}

	sdkDir := filepath.Join(t.TempDir(), "sdk")
	if handled, err := gota.RunCodegen(app, []string{"--ts-sdk=" + sdkDir}); !handled || err != nil {
		t.Fatalf("gota.RunCodegen failed: handled=%v err=%v", handled, err)
	}
	if err := gota.CheckTypeScriptSDK(app, sdkDir); err != nil {
		t.Fatalf("gota.CheckTypeScriptSDK failed: %v", err)
	}
}

func TestTypeScript_NodeRuntimeE2E(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping Node E2E SDK execution")
	}

	type ItemReq struct {
		ID string `param:"id" validate:"required"`
	}
	type CreateReq struct {
		Title string `json:"title" validate:"required,min=3"`
	}
	type Item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}

	app := gota.New(repr.Config{Title: "E2E API"})
	repr.Get(app, "/items/{id}", func(_ *repr.Context, req ItemReq) (Item, error) {
		if req.ID == "missing" {
			return Item{}, repr.NotFound("Item not found")
		}
		return Item{ID: req.ID, Title: "Found"}, nil
	}).ErrorResponse(http.StatusNotFound, "Not found")

	repr.Post(app, "/items", func(_ *repr.Context, req CreateReq) (Item, error) {
		return Item{ID: "new_1", Title: req.Title}, nil
	}).Status(http.StatusCreated)

	type NumericReq struct {
		ID string `param:"id" validate:"required"`
	}
	repr.Get(app, "/users/{id:[0-9]+}", func(_ *repr.Context, req NumericReq) (Item, error) {
		return Item{ID: req.ID, Title: "User"}, nil
	})

	repr.Get(app, "/slow", func(ctx *repr.Context, _ repr.Empty) (Item, error) {
		select {
		case <-time.After(2 * time.Second):
			return Item{ID: "slow", Title: "Done"}, nil
		case <-ctx.Request.Context().Done():
			return Item{}, ctx.Request.Context().Err()
		}
	})

	repr.Get(app, "/stream", func(_ *repr.Context, _ repr.Empty) (repr.SSEStream, error) {
		ch := make(chan repr.SSEvent, 2)
		ch <- repr.SSEvent{Event: "tick", Data: "one"}
		ch <- repr.SSEvent{Event: "tick", Data: "two"}
		close(ch)
		return repr.SSEStream{Events: ch}, nil
	})

	type UploadForm struct {
		Name   string                `form:"name" validate:"required"`
		Avatar *multipart.FileHeader `form:"avatar" validate:"required"`
	}
	type UploadRes struct {
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	repr.Post(app, "/upload", func(_ *repr.Context, req UploadForm) (UploadRes, error) {
		f, err := req.Avatar.Open()
		if err != nil {
			return UploadRes{}, err
		}
		defer f.Close()
		raw, err := io.ReadAll(f)
		if err != nil {
			return UploadRes{}, err
		}
		return UploadRes{
			Name:     req.Name,
			Filename: req.Avatar.Filename,
			Content:  string(raw),
		}, nil
	})

	type LoginForm struct {
		Username string `form:"username" validate:"required"`
		Remember bool   `form:"remember"`
	}
	type LoginRes struct {
		Username string `json:"username"`
		Remember bool   `json:"remember"`
		CType    string `json:"ctype"`
	}
	repr.Post(app, "/form", func(ctx *repr.Context, req LoginForm) (LoginRes, error) {
		return LoginRes{
			Username: req.Username,
			Remember: req.Remember,
			CType:    ctx.Request.Header.Get("Content-Type"),
		}, nil
	})

	var retryAttempts atomic.Int32
	repr.Get(app, "/retry-after", func(ctx *repr.Context, _ repr.Empty) (Item, error) {
		retryAttempts.Add(1)
		ctx.SetHeader("Retry-After", "5")
		return Item{}, repr.NewHTTPError(http.StatusServiceUnavailable, "busy")
	})

	var retryDateAttempts atomic.Int32
	repr.Get(app, "/retry-past-iso", func(ctx *repr.Context, _ repr.Empty) (Item, error) {
		if retryDateAttempts.Add(1) == 1 {
			ctx.SetHeader("Retry-After", "2020-01-01T00:00:00Z")
			return Item{}, repr.NewHTTPError(http.StatusServiceUnavailable, "retry")
		}
		return Item{ID: "ok", Title: "Recovered"}, nil
	})

	var retryBoolAttempts atomic.Int32
	repr.Get(app, "/retry-bool", func(_ *repr.Context, _ repr.Empty) (Item, error) {
		if retryBoolAttempts.Add(1) < 3 {
			return Item{}, repr.NewHTTPError(http.StatusServiceUnavailable, "try again")
		}
		return Item{ID: "ok-bool", Title: "Recovered"}, nil
	})

	var retryObjDefaultAttempts atomic.Int32
	repr.Get(app, "/retry-obj-default", func(_ *repr.Context, _ repr.Empty) (Item, error) {
		if retryObjDefaultAttempts.Add(1) < 3 {
			return Item{}, repr.NewHTTPError(http.StatusServiceUnavailable, "try again")
		}
		return Item{ID: "ok-obj", Title: "Recovered"}, nil
	})

	app.HandleHTTP(http.MethodGet, "/upper-json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "Application/JSON; charset=utf-8")
		_, _ = w.Write([]byte(`{"id":"u1","title":"Upper"}`))
	})).Tags("Items").Response(http.StatusOK, Item{})

	app.HandleHTTP(http.MethodGet, "/bad-json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{broken`))
	})).Tags("Items").Response(http.StatusOK, Item{})

	srv := httptest.NewServer(app)
	defer srv.Close()

	deadSrv := httptest.NewServer(http.NewServeMux())
	deadURL := deadSrv.URL
	deadSrv.Close()

	tmpDir := t.TempDir()
	sdkDir := filepath.Join(tmpDir, "sdk")
	if err := gota.ExportTypeScriptSDK(app, sdkDir, typescript.Options{ImportExtension: ".ts"}); err != nil {
		t.Fatalf("ExportTypeScriptSDK failed: %v", err)
	}

	runtimeBytes, err := os.ReadFile(filepath.Join(sdkDir, "runtime.ts"))
	if err != nil {
		t.Fatalf("failed reading runtime.ts: %v", err)
	}
	if strings.Contains(string(runtimeBytes), `kind !== ":"`) || strings.Contains(string(runtimeBytes), `segment === "*"`) {
		t.Errorf("runtime.ts resolvePath should use only idiomatic Chi {param} syntax")
	}

	runnerScript := `
import assert from "node:assert/strict";
import {
  createClient,
  isAbortError,
  isEndpointError,
  isHttpError,
  parseError,
} from "./sdk/index.ts";
import { parseError as parseRuntimeOnlyError } from "./sdk/runtime.ts";

const baseUrl = process.env.BASE_URL;
const api = createClient(baseUrl);

// 1. GET with {id} path param
const item = await api.fetch("GET /items/{id}", { params: { id: "abc 123" } });
assert.equal(item.id, "abc 123");
assert.equal(item.title, "Found");

// 2. GET with {id:regex} Chi path param
const user = await api.fetch("GET /users/{id:[0-9]+}", { params: { id: "42" } });
assert.equal(user.id, "42");

// 3. POST with body
const created = await api.fetch("POST /items", { body: { title: "Hello World" } });
assert.equal(created.id, "new_1");
assert.equal(created.title, "Hello World");

// 4. 404 error narrowing & parseError
await assert.rejects(
  () => api.fetch("GET /items/{id}", { params: { id: "missing" } }),
  (err) => {
    const parsed = parseError(err);
    return (
      isHttpError(err) &&
      isEndpointError(err, "GET /items/{id}") &&
      !isEndpointError(err, "POST /items") &&
      err.status === 404 &&
      err.message === "Item not found" &&
      parsed.kind === "http" &&
      parsed.status === 404 &&
      parsed.message === "Item not found" &&
      parsed.endpoint === "GET /items/{id}" &&
      Object.keys(parsed.fieldErrors).length === 0
    );
  }
);

// 5. 422 validation error narrowing & fieldErrors
await assert.rejects(
  () => api.fetch("POST /items", { body: { title: "ab" } }),
  (err) => {
    const parsed = parseError(err);
    return (
      isHttpError(err) &&
      err.status === 422 &&
      err.message === "Validation failed" &&
      parsed.kind === "validation" &&
      parsed.status === 422 &&
      Array.isArray(parsed.fieldErrors["title"]) &&
      parsed.fieldErrors["title"].length > 0
    );
  }
);

// 6. AbortController cancellation skips onError hook & is identified by isAbortError/parseError
let onErrorCalled = 0;
const controller = new AbortController();
setTimeout(() => controller.abort(), 30);
await assert.rejects(
  () =>
    api.fetch("GET /slow", {
      signal: controller.signal,
      onError: () => {
        onErrorCalled++;
      },
    }),
  (err) => {
    const parsed = parseRuntimeOnlyError(err);
    return isAbortError(err) && parsed.kind === "abort";
  }
);
assert.equal(onErrorCalled, 0);

// 7. Timeout is distinguished from caller AbortController cancellation
await assert.rejects(
  () => api.fetch("GET /slow", { timeoutMs: 30 }),
  (err) => !isAbortError(err) && parseError(err).kind === "timeout"
);

// 8. queryOptions builds stable queryKey and forwards signal to queryFn
const qOpts = api.queryOptions("GET /items/{id}", { params: { id: "q1" } });
assert.deepEqual(qOpts.queryKey, ["GET /items/{id}", { params: { id: "q1" } }]);
const qRes = await qOpts.queryFn();
assert.equal(qRes.id, "q1");

const qSlow = api.queryOptions("GET /slow");
const qController = new AbortController();
setTimeout(() => qController.abort(), 30);
await assert.rejects(
  () => qSlow.queryFn({ signal: qController.signal }),
  (err) => isAbortError(err)
);

// 9. SSE stream returns ReadableStream<string>
const stream = await api.fetch("GET /stream");
const reader = stream.getReader();
let sseText = "";
while (true) {
  const { done, value } = await reader.read();
  if (done) break;
  sseText += value;
}
assert.ok(sseText.includes("event: tick"), sseText);
assert.ok(sseText.includes("data: one"), sseText);
assert.ok(sseText.includes("data: two"), sseText);

// 10. multipart/form-data from plain object containing a Blob
const uploaded = await api.fetch("POST /upload", {
  body: {
    name: "avatar-pic",
    avatar: new Blob(["hello-blob"], { type: "text/plain" }),
  },
});
assert.equal(uploaded.name, "avatar-pic");
assert.equal(uploaded.content, "hello-blob");

// 11. application/x-www-form-urlencoded from plain object
const formRes = await api.fetch("POST /form", {
  body: { username: "alice", remember: true },
});
assert.equal(formRes.username, "alice");
assert.equal(formRes.remember, true);
assert.ok(formRes.ctype.includes("application/x-www-form-urlencoded"), formRes.ctype);

// 12. Retry-After backoff is cancelled early by AbortSignal
const retryController = new AbortController();
setTimeout(() => retryController.abort(), 50);
const retryStarted = Date.now();
await assert.rejects(
  () =>
    api.fetch("GET /retry-after", {
      retry: { attempts: 3, methods: ["GET"] },
      signal: retryController.signal,
    }),
  (err) => isAbortError(err)
);
const retryElapsed = Date.now() - retryStarted;
assert.ok(retryElapsed < 500, "expected Retry-After sleep to abort early, took " + retryElapsed + "ms");

// 13. Real network failure maps to parseError(...).kind === "network"
const deadApi = createClient(process.env.DEAD_URL);
await assert.rejects(
  () => deadApi.fetch("GET /items/{id}", { params: { id: "1" }, retry: false }),
  (err) => !isAbortError(err) && parseError(err).kind === "network"
);

// 14. Case-insensitive Content-Type parsing for Application/JSON
const upperRes = await api.fetch("GET /upper-json");
assert.equal(upperRes.id, "u1");
assert.equal(upperRes.title, "Upper");

// 15. clientOptions Content-Type: application/json does not break form-urlencoded requests
const jsonDefaultApi = createClient(baseUrl, { headers: { "Content-Type": "application/json" } });
const formOverrideRes = await jsonDefaultApi.fetch("POST /form", {
  body: { username: "bob", remember: false },
});
assert.equal(formOverrideRes.username, "bob");
assert.ok(formOverrideRes.ctype.includes("application/x-www-form-urlencoded"), formOverrideRes.ctype);

// 16. onError hook is invoked when response body fails to parse
let parseErrHookCalls = 0;
await assert.rejects(
  () =>
    api.fetch("GET /bad-json", {
      onError: () => {
        parseErrHookCalls++;
      },
    }),
  (err) => err instanceof SyntaxError
);
assert.equal(parseErrHookCalls, 1);

// 17. Retry-After starting with digits (ISO date in the past) is not parsed as 2020 seconds
const pastStart = Date.now();
const recovered = await api.fetch("GET /retry-past-iso", {
  retry: { attempts: 1, delayMs: 1, methods: ["GET"] },
});
assert.equal(recovered.id, "ok");
assert.ok(Date.now() - pastStart < 1000, "Retry-After ISO date should not sleep as integer seconds");

// 18. retry: true and retry: { delayMs } without attempts default to DEFAULT_RETRY_ATTEMPTS (2)
const boolRecovered = await api.fetch("GET /retry-bool", { retry: true });
assert.equal(boolRecovered.id, "ok-bool");
const objRecovered = await api.fetch("GET /retry-obj-default", { retry: { delayMs: 1 } });
assert.equal(objRecovered.id, "ok-obj");
`
	runnerPath := filepath.Join(tmpDir, "run.mjs")
	if err := os.WriteFile(runnerPath, []byte(runnerScript), 0o644); err != nil {
		t.Fatalf("failed writing run.mjs: %v", err)
	}

	cmd := exec.Command(nodePath, runnerPath)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BASE_URL="+srv.URL, "DEAD_URL="+deadURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node E2E SDK test failed (node=%s): %v\n%s", nodePath, err, string(out))
	}
	if got := retryAttempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt before Retry-After abort, got %d", got)
	}
}
