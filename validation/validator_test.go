package validation

import (
	"errors"
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
