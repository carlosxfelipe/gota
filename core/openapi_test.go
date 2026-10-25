package core

import (
	"net/http"
	"reflect"
	"testing"
)

// Cookie intentionally shares its name with http.Cookie.
type Cookie struct {
	Name string `json:"name"`
}

func TestOpenAPISchemaNameCollision(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})

	a := g.registerOrRef(Cookie{})
	b := g.registerOrRef(http.Cookie{})

	if a["$ref"] == b["$ref"] {
		t.Fatalf("different types must not share a schema ref: %v", a["$ref"])
	}
	if len(g.schemas) != 2 {
		t.Fatalf("expected 2 distinct schemas, got %d", len(g.schemas))
	}
	// Same type registered twice keeps a single, stable name.
	if again := g.registerOrRef(Cookie{}); again["$ref"] != a["$ref"] {
		t.Errorf("expected stable ref for same type, got %v vs %v", again["$ref"], a["$ref"])
	}
}

func TestOpenAPIByteSliceIsBase64String(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	for name, schema := range map[string]map[string]any{
		"top-level": g.registerOrRef([]byte(nil)),
		"field":     g.schemaForType(reflectTypeOf([]byte(nil))),
	} {
		if schema["type"] != "string" || schema["format"] != "byte" {
			t.Errorf("%s: expected string/byte, got %v", name, schema)
		}
	}
}

func reflectTypeOf(v any) reflect.Type { return reflect.TypeOf(v) }
