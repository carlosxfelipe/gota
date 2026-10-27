package validation

import (
	"errors"
	"reflect"
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
