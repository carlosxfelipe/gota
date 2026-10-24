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
