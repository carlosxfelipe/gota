package core

import (
	"testing"
)

func TestRouterStaticAndParams(t *testing.T) {
	r := NewRouter()

	r.Add("GET", "/users", func(ctx *Context) error {
		return ctx.Send("list users")
	})

	r.Add("GET", "/users/:id", func(ctx *Context) error {
		return ctx.Send("user " + ctx.Param("id"))
	})

	r.Add("GET", "/users/:id/posts/:slug", func(ctx *Context) error {
		return ctx.Send("post " + ctx.Param("slug"))
	})

	r.Add("GET", "/files/*path", func(ctx *Context) error {
		return ctx.Send("file " + ctx.Param("path"))
	})

	// Match static
	h, _, ok := r.Match("GET", "/users")
	if !ok || h == nil {
		t.Fatalf("expected to match /users")
	}

	// Match param
	h, params, ok := r.Match("GET", "/users/42")
	if !ok || h == nil {
		t.Fatalf("expected to match /users/42")
	}
	if params["id"] != "42" {
		t.Errorf("expected param id=42, got %s", params["id"])
	}

	// Match multiple params
	h, params, ok = r.Match("GET", "/users/10/posts/golang-repr")
	if !ok || h == nil {
		t.Fatalf("expected to match /users/10/posts/golang-repr")
	}
	if params["id"] != "10" || params["slug"] != "golang-repr" {
		t.Errorf("expected id=10, slug=golang-repr, got %+v", params)
	}

	// Match wildcard
	h, params, ok = r.Match("GET", "/files/docs/images/logo.png")
	if !ok || h == nil {
		t.Fatalf("expected to match /files/*path")
	}
	if params["path"] != "docs/images/logo.png" {
		t.Errorf("expected path=docs/images/logo.png, got %s", params["path"])
	}
}

func TestRouterURLFor(t *testing.T) {
	r := NewRouter()

	node := r.Add("GET", "/users/:id/orders/:orderId", func(ctx *Context) error { return nil })
	r.SetName("users.orders.show", node)

	url, err := r.URLFor("users.orders.show", map[string]string{
		"id":      "100",
		"orderId": "500",
		"tab":     "items",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/users/100/orders/500?tab=items"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestRouterRejectsDuplicateAndConflictingRoutes(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		fn()
	}
	h := func(ctx *Context) error { return nil }

	r := NewRouter()
	r.Add("GET", "/x", h)
	mustPanic("duplicate route", func() { r.Add("GET", "/x", h) })

	r.Add("GET", "/users/:id", h)
	mustPanic("conflicting param name", func() { r.Add("GET", "/users/:userId/posts", h) })

	// Same path with a different method is fine.
	r.Add("POST", "/x", h)
}

func TestRouterAllowed(t *testing.T) {
	r := NewRouter()
	h := func(ctx *Context) error { return nil }
	r.Add("POST", "/items/:id", h)
	r.Add("GET", "/items/:id", h)

	allowed := r.Allowed("/items/7")
	if len(allowed) != 2 || allowed[0] != "GET" || allowed[1] != "POST" {
		t.Errorf("expected [GET POST], got %v", allowed)
	}
	if got := r.Allowed("/nothing"); len(got) != 0 {
		t.Errorf("expected no methods, got %v", got)
	}
}

func TestDisplayAddr(t *testing.T) {
	cases := map[string][2]string{
		":3000":          {":3000", "localhost:3000"},
		"3000":           {":3000", "localhost:3000"},
		"0.0.0.0:8080":   {"0.0.0.0:8080", "localhost:8080"},
		"127.0.0.1:3000": {"127.0.0.1:3000", "127.0.0.1:3000"},
	}
	for in, want := range cases {
		listen, display := displayAddr(in)
		if listen != want[0] || display != want[1] {
			t.Errorf("displayAddr(%q) = (%q, %q), want (%q, %q)", in, listen, display, want[0], want[1])
		}
	}
}
