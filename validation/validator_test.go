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
	}
	for name, v := range cases {
		if err := CheckTags(reflect.TypeOf(v)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
