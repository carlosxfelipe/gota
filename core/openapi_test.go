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

type AddressDTO struct {
	Street string `json:"street" doc:"Street name"`
	City   string `json:"city"`
}

type UserDTO struct {
	Name    string     `json:"name"`
	Address AddressDTO `json:"address"`
	Tags    []string   `json:"tags"`
	Friends []UserDTO  `json:"friends"`
}

func TestOpenAPINestedNamedStruct(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	ref := g.registerOrRef(UserDTO{})

	if ref["$ref"] != "#/components/schemas/UserDTO" {
		t.Fatalf("expected ref to UserDTO, got %v", ref["$ref"])
	}

	userSchema, ok := g.schemas["UserDTO"]
	if !ok {
		t.Fatal("expected UserDTO in components.schemas")
	}

	props, ok := userSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties in UserDTO schema: %v", userSchema)
	}

	// Nested AddressDTO must be a $ref to AddressDTO
	addrProp, ok := props["address"].(map[string]any)
	if !ok || addrProp["$ref"] != "#/components/schemas/AddressDTO" {
		t.Errorf("expected address to ref AddressDTO, got: %v", props["address"])
	}

	// AddressDTO must exist with its own properties
	addrSchema, ok := g.schemas["AddressDTO"]
	if !ok {
		t.Fatal("expected AddressDTO in components.schemas")
	}
	addrProps := addrSchema["properties"].(map[string]any)
	if _, ok := addrProps["street"]; !ok {
		t.Errorf("expected street property in AddressDTO")
	}
	if _, ok := addrProps["city"]; !ok {
		t.Errorf("expected city property in AddressDTO")
	}

	// Friends slice must have items ref to UserDTO
	friendsProp, ok := props["friends"].(map[string]any)
	if !ok || friendsProp["type"] != "array" {
		t.Fatalf("expected friends to be array, got: %v", props["friends"])
	}
	items := friendsProp["items"].(map[string]any)
	if items["$ref"] != "#/components/schemas/UserDTO" {
		t.Errorf("expected friends items to ref UserDTO, got: %v", items)
	}
}

type AnonymousContainerDTO struct {
	Title string `json:"title"`
	Meta  struct {
		Page  int `json:"page"`
		Total int `json:"total"`
	} `json:"meta"`
}

func TestOpenAPINestedAnonymousStruct(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	_ = g.registerOrRef(AnonymousContainerDTO{})

	container, ok := g.schemas["AnonymousContainerDTO"]
	if !ok {
		t.Fatal("expected AnonymousContainerDTO in schemas")
	}

	props := container["properties"].(map[string]any)
	meta, ok := props["meta"].(map[string]any)
	if !ok || meta["type"] != "object" {
		t.Fatalf("expected meta property to be object, got %v", props["meta"])
	}

	metaProps, ok := meta["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta to contain properties, got %v", meta)
	}
	if _, ok := metaProps["page"]; !ok {
		t.Errorf("expected page property in meta")
	}
	if _, ok := metaProps["total"]; !ok {
		t.Errorf("expected total property in meta")
	}
}

type RecursiveNode struct {
	ID   int            `json:"id"`
	Next *RecursiveNode `json:"next"`
}

func TestOpenAPIRecursiveStruct(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	ref := g.registerOrRef(RecursiveNode{})

	if ref["$ref"] != "#/components/schemas/RecursiveNode" {
		t.Fatalf("expected ref to RecursiveNode, got %v", ref)
	}

	nodeSchema, ok := g.schemas["RecursiveNode"]
	if !ok {
		t.Fatal("expected RecursiveNode in schemas")
	}

	props := nodeSchema["properties"].(map[string]any)
	nextProp, ok := props["next"].(map[string]any)
	if !ok || nextProp["$ref"] != "#/components/schemas/RecursiveNode" {
		t.Errorf("expected next to self-reference #/components/schemas/RecursiveNode, got %v", props["next"])
	}
}

type MutuallyRecursiveA struct {
	B *MutuallyRecursiveB `json:"b"`
}

type MutuallyRecursiveB struct {
	A *MutuallyRecursiveA `json:"a"`
}

func TestOpenAPIMutuallyRecursiveStructs(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	ref := g.registerOrRef(MutuallyRecursiveA{})

	if ref["$ref"] != "#/components/schemas/MutuallyRecursiveA" {
		t.Fatalf("expected ref to MutuallyRecursiveA, got %v", ref)
	}

	schemaA, ok := g.schemas["MutuallyRecursiveA"]
	if !ok {
		t.Fatal("expected MutuallyRecursiveA in schemas")
	}
	schemaB, ok := g.schemas["MutuallyRecursiveB"]
	if !ok {
		t.Fatal("expected MutuallyRecursiveB in schemas")
	}

	propsA := schemaA["properties"].(map[string]any)
	propB := propsA["b"].(map[string]any)
	if propB["$ref"] != "#/components/schemas/MutuallyRecursiveB" {
		t.Errorf("expected A.b to ref B, got %v", propB)
	}

	propsB := schemaB["properties"].(map[string]any)
	propA := propsB["a"].(map[string]any)
	if propA["$ref"] != "#/components/schemas/MutuallyRecursiveA" {
		t.Errorf("expected B.a to ref A, got %v", propA)
	}
}
