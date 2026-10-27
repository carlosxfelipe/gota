package validation

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/carlosxfelipe/gota/core"
)

type TestValidationDTO struct {
	Name  string `json:"name" validate:"required,min=3"`
	Email string `json:"email" validate:"required,email"`
}

type CustomValidatableDTO struct {
	Code string `json:"code"`
}

func (c CustomValidatableDTO) Validate() error {
	if c.Code != "SECRET" {
		return errors.New("code must be SECRET")
	}
	return nil
}

func TestValidator(t *testing.T) {
	ctx := &core.Context{}

	validDTO := TestValidationDTO{
		Name:  "Alex",
		Email: "alex@example.com",
	}
	valErr := ValidateStruct(ctx, &validDTO)
	if valErr != nil {
		t.Fatalf("expected validation to pass, got: %v", valErr)
	}

	invalidDTO := TestValidationDTO{
		Name:  "Al",
		Email: "not-an-email",
	}
	valErr = ValidateStruct(ctx, &invalidDTO)
	if valErr == nil {
		t.Fatalf("expected validation errors, got none")
	}
	if len(valErr.Errors["name"]) == 0 {
		t.Errorf("expected error on name")
	}
	if len(valErr.Errors["email"]) == 0 {
		t.Errorf("expected error on email")
	}

	cvInvalid := CustomValidatableDTO{Code: "WRONG"}
	valErr = ValidateStruct(ctx, &cvInvalid)
	if valErr == nil || len(valErr.Errors["_general"]) == 0 {
		t.Errorf("expected custom validation error for invalid code")
	}

	cvValid := CustomValidatableDTO{Code: "SECRET"}
	valErr = ValidateStruct(ctx, &cvValid)
	if valErr != nil {
		t.Errorf("expected valid custom validation, got: %v", valErr)
	}
}

type TestLimitsDTO struct {
	Name string `json:"name" validate:"min=4,max=4"`
	Qty  uint   `json:"qty" validate:"min=2,max=5"`
}

func TestValidatorLimits(t *testing.T) {
	ctx := &core.Context{}

	// "José" has 4 characters but 5 bytes.
	if valErr := ValidateStruct(ctx, &TestLimitsDTO{Name: "José", Qty: 3}); valErr != nil {
		t.Errorf("expected 4-character string and qty=3 to pass, got: %v", valErr)
	}

	valErr := ValidateStruct(ctx, &TestLimitsDTO{Name: "Jos", Qty: 1})
	if valErr == nil || len(valErr.Errors["name"]) == 0 || len(valErr.Errors["qty"]) == 0 {
		t.Errorf("expected errors on name and qty below min, got: %v", valErr)
	}

	valErr = ValidateStruct(ctx, &TestLimitsDTO{Name: "Jose", Qty: 9})
	if valErr == nil || len(valErr.Errors["qty"]) == 0 {
		t.Errorf("expected error on qty above max, got: %v", valErr)
	}
}

func TestCheckTags(t *testing.T) {
	type valid struct {
		A string `validate:"required,min=3,max=10,email"`
		B int    `validate:"min=1"`
		C string `validate:"url"`
		D string `validate:"uuid"`
		E string
	}
	if err := CheckTags(reflect.TypeOf(valid{})); err != nil {
		t.Errorf("expected valid tags to pass, got: %v", err)
	}
	if err := CheckTags(reflect.TypeOf(&valid{})); err != nil {
		t.Errorf("expected pointer to struct to pass, got: %v", err)
	}
	if err := CheckTags(reflect.TypeOf("not a struct")); err != nil {
		t.Errorf("expected non-struct to be ignored, got: %v", err)
	}

	cases := map[string]any{
		"unknown rule": struct {
			A string `validate:"requird"`
		}{},
		"min without param": struct {
			A string `validate:"min"`
		}{},
		"min bad param": struct {
			A string `validate:"min=abc"`
		}{},
		"max bad param": struct {
			A int `validate:"max=x"`
		}{},
		"len without param": struct {
			A string `validate:"len"`
		}{},
		"len bad param": struct {
			A string `validate:"len=xyz"`
		}{},
		"oneof without param": struct {
			A string `validate:"oneof"`
		}{},
		"nested struct bad tag": struct {
			Child struct {
				A string `validate:"bad_rule"`
			}
		}{},
	}
	for name, v := range cases {
		if err := CheckTags(reflect.TypeOf(v)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

type TestOneOfAndLenDTO struct {
	Role   string   `json:"role" validate:"required,oneof=Admin,Customer,Guest"`
	Code   string   `json:"code" validate:"len=6"`
	Tags   []string `json:"tags" validate:"len=2"`
	Status string   `json:"status" validate:"oneof=active inactive"`
}

func TestValidatorOneOfAndLen(t *testing.T) {
	ctx := &core.Context{}

	// Valid case
	valid := TestOneOfAndLenDTO{
		Role:   "Admin",
		Code:   "123456",
		Tags:   []string{"go", "api"},
		Status: "active",
	}
	if err := ValidateStruct(ctx, &valid); err != nil {
		t.Fatalf("expected valid DTO to pass, got: %v", err)
	}

	// Invalid case
	invalid := TestOneOfAndLenDTO{
		Role:   "Superuser",
		Code:   "123",
		Tags:   []string{"single"},
		Status: "deleted",
	}
	valErr := ValidateStruct(ctx, &invalid)
	if valErr == nil {
		t.Fatal("expected validation errors, got none")
	}
	if len(valErr.Errors["role"]) == 0 {
		t.Errorf("expected error on role")
	}
	if len(valErr.Errors["code"]) == 0 {
		t.Errorf("expected error on code")
	}
	if len(valErr.Errors["tags"]) == 0 {
		t.Errorf("expected error on tags")
	}
	if len(valErr.Errors["status"]) == 0 {
		t.Errorf("expected error on status")
	}
}

type AddressDTO struct {
	Street string `json:"street" validate:"required,min=3"`
	City   string `json:"city" validate:"required"`
}

type ItemDTO struct {
	Name  string  `json:"name" validate:"required"`
	Price float64 `json:"price" validate:"min=0.01"`
}

type OrderDTO struct {
	Customer string     `json:"customer" validate:"required"`
	Address  AddressDTO `json:"address"`
	Items    []ItemDTO  `json:"items" validate:"min=1"`
}

func TestValidatorNestedAndSlices(t *testing.T) {
	ctx := &core.Context{}

	valid := OrderDTO{
		Customer: "Carlos",
		Address: AddressDTO{
			Street: "Rua das Flores, 123",
			City:   "Porto Alegre",
		},
		Items: []ItemDTO{
			{Name: "Item 1", Price: 19.99},
			{Name: "Item 2", Price: 5.50},
		},
	}
	if err := ValidateStruct(ctx, &valid); err != nil {
		t.Fatalf("expected valid OrderDTO to pass, got: %v", err)
	}

	invalid := OrderDTO{
		Customer: "",
		Address: AddressDTO{
			Street: "R",
			City:   "",
		},
		Items: []ItemDTO{
			{Name: "Item 1", Price: 10.0},
			{Name: "", Price: 0.0},
		},
	}
	valErr := ValidateStruct(ctx, &invalid)
	if valErr == nil {
		t.Fatal("expected validation errors, got none")
	}
	if len(valErr.Errors["customer"]) == 0 {
		t.Errorf("expected error on customer")
	}
	if len(valErr.Errors["address.street"]) == 0 {
		t.Errorf("expected error on address.street")
	}
	if len(valErr.Errors["address.city"]) == 0 {
		t.Errorf("expected error on address.city")
	}
	if len(valErr.Errors["items[1].name"]) == 0 {
		t.Errorf("expected error on items[1].name")
	}
	if len(valErr.Errors["items[1].price"]) == 0 {
		t.Errorf("expected error on items[1].price")
	}
}

type TestPointerDTO struct {
	Name  *string `json:"name" validate:"min=3"`
	Age   *int    `json:"age" validate:"max=10"`
	Role  *string `json:"role" validate:"oneof=Admin,Customer"`
	Email *string `json:"email" validate:"email"`
	Count *int    `json:"count" validate:"min=1"`
}

func TestValidatorPointerFields(t *testing.T) {
	ctx := &core.Context{}
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }

	invalid := TestPointerDTO{Name: str("ab"), Age: num(99), Role: str("Hacker"), Email: str("nope")}
	valErr := ValidateStruct(ctx, &invalid)
	if valErr == nil {
		t.Fatal("expected pointer fields to be validated")
	}
	for _, f := range []string{"name", "age", "role", "email"} {
		if len(valErr.Errors[f]) == 0 {
			t.Errorf("expected error on %q", f)
		}
	}
	if len(valErr.Errors["count"]) != 0 {
		t.Errorf("nil pointer must not fail non-required rules")
	}

	valid := TestPointerDTO{Name: str("alice"), Age: num(5), Role: str("Admin"), Email: str("a@b.com")}
	if err := ValidateStruct(ctx, &valid); err != nil {
		t.Fatalf("expected valid pointer DTO to pass, got %v", err)
	}
}

type TestUnexportedDTO struct {
	Public string `json:"public" validate:"required"`
	secret string `validate:"oneof=a,b"`
}

func TestValidatorSkipsUnexportedFields(t *testing.T) {
	dto := TestUnexportedDTO{Public: "x", secret: "zzz"}
	if err := ValidateStruct(&core.Context{}, &dto); err != nil {
		t.Fatalf("unexported field must be skipped, got %v", err)
	}
}

type TestZeroAndFalseDTO struct {
	// Primitives: 0 and false are zero-values in Go, so required rejects them.
	PrimitiveBool bool `json:"primitiveBool" validate:"required"`
	PrimitiveInt  int  `json:"primitiveInt" validate:"required"`

	// Pointers: nil means absent (fails required), whereas *false and *0 are present (pass required).
	PointerBool *bool `json:"pointerBool" validate:"required"`
	PointerInt  *int  `json:"pointerInt" validate:"required"`
}

func TestValidatorRequired_PointersAllowZeroAndFalse(t *testing.T) {
	ctx := &core.Context{}
	f := false
	zero := 0

	// 1. Primitive false / 0 are rejected by required because they equal Go's zero-value.
	invalidPrimitive := TestZeroAndFalseDTO{
		PrimitiveBool: false,
		PrimitiveInt:  0,
		PointerBool:   &f,
		PointerInt:    &zero,
	}
	err := ValidateStruct(ctx, &invalidPrimitive)
	if err == nil {
		t.Fatal("expected primitive false and 0 to fail required")
	}
	if len(err.Errors["primitiveBool"]) == 0 {
		t.Errorf("expected error on primitiveBool")
	}
	if len(err.Errors["primitiveInt"]) == 0 {
		t.Errorf("expected error on primitiveInt")
	}
	// But pointer fields with &false and &0 MUST pass!
	if len(err.Errors["pointerBool"]) != 0 {
		t.Errorf("pointer to false must pass required")
	}
	if len(err.Errors["pointerInt"]) != 0 {
		t.Errorf("pointer to 0 must pass required")
	}

	// 2. Nil pointers are rejected by required because they indicate absence.
	tVal := true
	one := 1
	invalidNilPointers := TestZeroAndFalseDTO{
		PrimitiveBool: true,
		PrimitiveInt:  1,
		PointerBool:   nil,
		PointerInt:    nil,
	}
	errNil := ValidateStruct(ctx, &invalidNilPointers)
	if errNil == nil {
		t.Fatal("expected nil pointers to fail required")
	}
	if len(errNil.Errors["pointerBool"]) == 0 {
		t.Errorf("expected error on nil pointerBool")
	}
	if len(errNil.Errors["pointerInt"]) == 0 {
		t.Errorf("expected error on nil pointerInt")
	}

	// 3. Fully valid with pointers.
	valid := TestZeroAndFalseDTO{
		PrimitiveBool: true,
		PrimitiveInt:  1,
		PointerBool:   &tVal,
		PointerInt:    &one,
	}
	if err := ValidateStruct(ctx, &valid); err != nil {
		t.Fatalf("expected valid DTO to pass, got: %v", err)
	}
}

type EmbBaseQuery struct {
	Limit int `query:"limit" validate:"required"`
}

type EmbAuditQuery struct {
	Author string `query:"author" validate:"required"`
}

type EmbContainerDTO struct {
	EmbBaseQuery
	*EmbAuditQuery
	Name string `json:"name" validate:"required"`
}

func TestValidatorEmbeddedStructs(t *testing.T) {
	ctx := &core.Context{}

	// 1. Missing embedded fields: errors must use promoted field names, not EmbeddedType.field.
	invalid := EmbContainerDTO{
		Name: "Alice",
	}
	err := ValidateStruct(ctx, &invalid)
	if err == nil {
		t.Fatal("expected validation errors for missing limit and author")
	}
	if len(err.Errors["limit"]) == 0 {
		t.Errorf("expected error on promoted key 'limit', got errors: %v", err.Errors)
	}
	if len(err.Errors["author"]) == 0 {
		t.Errorf("expected error on promoted key 'author' from nil pointer, got errors: %v", err.Errors)
	}
	if len(err.Errors["EmbBaseQuery.limit"]) != 0 || len(err.Errors["EmbAuditQuery.author"]) != 0 {
		t.Errorf("embedded type name must not be used as prefix in error keys: %v", err.Errors)
	}

	// 2. Fully valid: all promoted fields provided
	valid := EmbContainerDTO{
		EmbBaseQuery:  EmbBaseQuery{Limit: 10},
		EmbAuditQuery: &EmbAuditQuery{Author: "Carlos"},
		Name:          "Alice",
	}
	if err := ValidateStruct(ctx, &valid); err != nil {
		t.Fatalf("expected valid embedded DTO to pass, got: %v", err)
	}
}

type ShadowValidateDTO struct {
	EmbBaseQuery
	Limit int `query:"limit" validate:"min=10"` // Shadows EmbBaseQuery.Limit
}

func TestValidatorEmbeddedShadowing(t *testing.T) {
	ctx := &core.Context{}

	// Limit=5 satisfies required (from EmbBaseQuery) but fails min=10 (outer field).
	// Shadowed field EmbBaseQuery.Limit should not run or duplicate errors.
	dto := ShadowValidateDTO{
		Limit: 5,
	}
	err := ValidateStruct(ctx, &dto)
	if err == nil {
		t.Fatal("expected error because outer Limit=5 fails min=10")
	}
	if len(err.Errors["limit"]) != 1 {
		t.Errorf("expected exactly 1 error on limit, got: %v", err.Errors["limit"])
	}
}

type CyclicNode struct {
	Next *CyclicNode `json:"next"`
	Name string      `json:"name" validate:"required"`
}

func TestValidatorCyclicSafety(t *testing.T) {
	if err := CheckTags(reflect.TypeOf(CyclicNode{})); err != nil {
		t.Fatalf("CheckTags on cyclic type failed: %v", err)
	}

	node1 := &CyclicNode{Name: "Node1"}
	node2 := &CyclicNode{Name: "Node2", Next: node1}
	node1.Next = node2 // cycle!

	err := ValidateStruct(&core.Context{}, node1)
	if err != nil {
		t.Fatalf("expected cyclic node with valid names to pass, got: %v", err)
	}
}

type WarningTestDTO struct {
	Active    bool    `json:"active" validate:"required"`
	Count     int     `json:"count" validate:"required"`
	Price     float64 `json:"price" validate:"required"`
	ValidPtrB *bool   `json:"validPtrB" validate:"required"`
	ValidPtrI *int    `json:"validPtrI" validate:"required"`
	ValidStr  string  `json:"validStr" validate:"required"`
	Silenced1 bool    `json:"silenced1" validate:"required,nowarn"`
	Silenced2 int     `json:"silenced2" validate:"required" gota:"nowarn"`
}

func TestCheckTagWarnings(t *testing.T) {
	warnings := CheckTagWarnings(reflect.TypeOf(WarningTestDTO{}))
	if len(warnings) != 3 {
		t.Fatalf("expected exactly 3 warnings for active, count, price, got %d: %v", len(warnings), warnings)
	}

	foundActive := false
	foundCount := false
	foundPrice := false
	for _, w := range warnings {
		if strings.Contains(w, `"active"`) && strings.Contains(w, "non-pointer bool") {
			foundActive = true
		}
		if strings.Contains(w, `"count"`) && strings.Contains(w, "non-pointer int") {
			foundCount = true
		}
		if strings.Contains(w, `"price"`) && strings.Contains(w, "non-pointer float64") {
			foundPrice = true
		}
	}
	if !foundActive || !foundCount || !foundPrice {
		t.Errorf("missing expected field warning: %v", warnings)
	}
}
