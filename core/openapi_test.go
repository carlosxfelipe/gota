package core

import (
	"net/http"
	"reflect"
	"regexp"
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

type PageDTO[T any] struct {
	Items []T `json:"items"`
}

var validSchemaName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func TestOpenAPIGenericSchemaNamesAreValid(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	ref := g.registerOrRef(PageDTO[AddressDTO]{})

	if ref["$ref"] != "#/components/schemas/PageDTO_AddressDTO" {
		t.Fatalf("expected sanitized generic ref, got %v", ref["$ref"])
	}
	for name := range g.schemas {
		if !validSchemaName.MatchString(name) {
			t.Errorf("invalid components/schemas key %q", name)
		}
	}
}

func TestOpenAPIGenericSchemaNamesNeverCollide(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})

	// Both sanitize to "PageDTO_Cookie" but are different types.
	a := g.registerOrRef(PageDTO[Cookie]{})
	b := g.registerOrRef(PageDTO[http.Cookie]{})
	if a["$ref"] == b["$ref"] {
		t.Fatalf("different generic instantiations must not share a ref: %v", a["$ref"])
	}
	// Registering again keeps a stable name per type.
	if again := g.registerOrRef(PageDTO[Cookie]{}); again["$ref"] != a["$ref"] {
		t.Errorf("expected stable ref, got %v vs %v", again["$ref"], a["$ref"])
	}
	for name := range g.schemas {
		if !validSchemaName.MatchString(name) {
			t.Errorf("invalid components/schemas key %q", name)
		}
	}
}

func TestSanitizeSchemaName(t *testing.T) {
	for in, want := range map[string]string{
		"User":                                  "User",
		"Page[github.com/acme/api/model.User]":  "Page_User",
		"Pair[string,int]":                      "Pair_string_int",
		"Box[[]github.com/acme/api/model.User]": "Box_User",
		"Map[map[string]github.com/a/b.V]":      "Map_map_string_V",
	} {
		if got := sanitizeSchemaName(in); got != want {
			t.Errorf("sanitizeSchemaName(%q) = %q, want %q", in, got, want)
		}
	}
}

type embBase struct {
	ID      int    `json:"id" validate:"required"`
	Created string `json:"created"`
}

type EmbAudit struct {
	CreatedBy string `json:"createdBy"`
}

type embSelf struct {
	*embSelf
	Label string `json:"label"`
}

type EmbDerived struct {
	embBase          // unexported embedded struct: exported fields are still promoted
	*EmbAudit        // embedded pointer: promoted too
	Name      string `json:"name"`
	Created   string `json:"created" doc:"outer wins"` // shadows the promoted field
}

type EmbTagged struct {
	EmbAudit `json:"audit"` // tagged embedded struct is a regular property
	Name     string         `json:"name"`
}

func TestOpenAPIEmbeddedStructsAreFlattened(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	g.registerOrRef(EmbDerived{})

	schema := g.schemas["EmbDerived"]
	props := schema["properties"].(map[string]any)

	for _, want := range []string{"id", "created", "createdBy", "name"} {
		if _, ok := props[want]; !ok {
			t.Errorf("expected promoted/own property %q, got %v", want, props)
		}
	}
	for _, bad := range []string{"embBase", "EmbAudit"} {
		if _, ok := props[bad]; ok {
			t.Errorf("embedded struct must not appear as property %q", bad)
		}
	}
	if props["created"].(map[string]any)["description"] != "outer wins" {
		t.Errorf("outer field must shadow the promoted one, got %v", props["created"])
	}
	if req, _ := schema["required"].([]string); len(req) != 1 || req[0] != "id" {
		t.Errorf("expected required [id] promoted from embedded struct, got %v", schema["required"])
	}
	if _, ok := g.schemas["embBase"]; ok {
		t.Errorf("flattened embedded types must not be registered as schemas")
	}
}

func TestOpenAPITaggedEmbeddedStructStaysProperty(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	g.registerOrRef(EmbTagged{})

	props := g.schemas["EmbTagged"]["properties"].(map[string]any)
	audit, ok := props["audit"].(map[string]any)
	if !ok || audit["$ref"] != "#/components/schemas/EmbAudit" {
		t.Errorf("tagged embedded struct must stay a $ref property, got %v", props)
	}
	if _, ok := props["createdBy"]; ok {
		t.Errorf("tagged embedded struct must not be flattened")
	}
}

func TestOpenAPISelfEmbeddingDoesNotRecurseForever(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	g.registerOrRef(embSelf{})

	props := g.schemas["embSelf"]["properties"].(map[string]any)
	if _, ok := props["label"]; !ok {
		t.Errorf("expected label property, got %v", props)
	}
}

type MapAndAnyDTO struct {
	AnyField      any                   `json:"anyField"`
	GenericMap    map[string]any        `json:"genericMap"`
	TypedIntMap   map[string]int        `json:"typedIntMap"`
	TypedModelMap map[string]AddressDTO `json:"typedModelMap"`
}

func TestOpenAPIMapAndAnySchemas(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	g.registerOrRef(MapAndAnyDTO{})

	schema := g.schemas["MapAndAnyDTO"]
	props := schema["properties"].(map[string]any)

	// 1. anyField must be an empty schema {} allowing any type
	anyField, ok := props["anyField"].(map[string]any)
	if !ok || len(anyField) != 0 {
		t.Errorf("expected empty schema for anyField, got: %v", props["anyField"])
	}

	// 2. genericMap must have additionalProperties: true
	genericMap := props["genericMap"].(map[string]any)
	if genericMap["additionalProperties"] != true {
		t.Errorf("expected additionalProperties: true for map[string]any, got: %v", genericMap)
	}

	// 3. typedIntMap must have additionalProperties: {"type": "integer"}
	intMap := props["typedIntMap"].(map[string]any)
	intAddProps, ok := intMap["additionalProperties"].(map[string]any)
	if !ok || intAddProps["type"] != "integer" {
		t.Errorf("expected additionalProperties with integer type, got: %v", intMap)
	}

	// 4. typedModelMap must have additionalProperties with $ref
	modelMap := props["typedModelMap"].(map[string]any)
	modelAddProps, ok := modelMap["additionalProperties"].(map[string]any)
	if !ok || modelAddProps["$ref"] != "#/components/schemas/AddressDTO" {
		t.Errorf("expected additionalProperties with $ref to AddressDTO, got: %v", modelMap)
	}
}

type ValidatedSchemaDTO struct {
	Username string   `json:"username" validate:"required,min=3,max=20"`
	Pin      string   `json:"pin" validate:"len=6"`
	Age      int      `json:"age" validate:"min=18,max=120"`
	Price    float64  `json:"price" validate:"min=0.5,max=999.99"`
	Tags     []string `json:"tags" validate:"min=1,max=5"`
	Role     string   `json:"role" validate:"oneof=Admin,Customer,Manager,required"`
	Priority *int     `json:"priority" validate:"oneof=1,2,3"`
	Email    string   `json:"email" validate:"email"`
	Website  string   `json:"website" validate:"url"`
	UserID   string   `json:"userId" validate:"uuid"`
}

func TestOpenAPIValidationRulesInSchema(t *testing.T) {
	g := NewOpenAPIGenerator(Config{})
	g.registerOrRef(ValidatedSchemaDTO{})

	schema := g.schemas["ValidatedSchemaDTO"]
	props := schema["properties"].(map[string]any)

	username := props["username"].(map[string]any)
	if username["minLength"] != 3 || username["maxLength"] != 20 {
		t.Errorf("expected username minLength=3 maxLength=20, got %v", username)
	}

	pin := props["pin"].(map[string]any)
	if pin["minLength"] != 6 || pin["maxLength"] != 6 {
		t.Errorf("expected pin minLength=6 maxLength=6, got %v", pin)
	}

	age := props["age"].(map[string]any)
	if age["minimum"] != 18 || age["maximum"] != 120 {
		t.Errorf("expected age minimum=18 maximum=120, got %v", age)
	}

	price := props["price"].(map[string]any)
	if price["minimum"] != 0.5 || price["maximum"] != 999.99 {
		t.Errorf("expected price minimum=0.5 maximum=999.99, got %v", price)
	}

	tags := props["tags"].(map[string]any)
	if tags["minItems"] != 1 || tags["maxItems"] != 5 {
		t.Errorf("expected tags minItems=1 maxItems=5, got %v", tags)
	}

	role := props["role"].(map[string]any)
	roleEnum, ok := role["enum"].([]any)
	if !ok || len(roleEnum) != 3 || roleEnum[0] != "Admin" || roleEnum[2] != "Manager" {
		t.Errorf("expected role enum [Admin, Customer, Manager], got %v", role["enum"])
	}

	priority := props["priority"].(map[string]any)
	prioEnum, ok := priority["enum"].([]any)
	if !ok || len(prioEnum) != 3 || prioEnum[0] != 1 || prioEnum[2] != 3 {
		t.Errorf("expected priority typed int enum [1, 2, 3], got %v", priority["enum"])
	}

	if props["email"].(map[string]any)["format"] != "email" {
		t.Errorf("expected email format=email, got %v", props["email"])
	}
	if props["website"].(map[string]any)["format"] != "uri" {
		t.Errorf("expected website format=uri, got %v", props["website"])
	}
	if props["userId"].(map[string]any)["format"] != "uuid" {
		t.Errorf("expected userId format=uuid, got %v", props["userId"])
	}
}
