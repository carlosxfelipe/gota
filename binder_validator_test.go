package gota

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"
)

type TestUserDTO struct {
	ID    int    `param:"id"`
	Page  int    `query:"page"`
	Auth  string `header:"Authorization"`
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

func TestBinderAndValidator(t *testing.T) {
	body := []byte(`{"name":"Alex","email":"alex@example.com"}`)
	req := httptest.NewRequest("POST", "/users/42?page=3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	w := httptest.NewRecorder()
	ctx := newContext(w, req, map[string]string{"id": "42"})

	var dto TestUserDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	if dto.ID != 42 {
		t.Errorf("expected ID=42, got %d", dto.ID)
	}
	if dto.Page != 3 {
		t.Errorf("expected Page=3, got %d", dto.Page)
	}
	if dto.Auth != "Bearer token123" {
		t.Errorf("expected Auth header, got %s", dto.Auth)
	}
	if dto.Name != "Alex" {
		t.Errorf("expected Name=Alex, got %s", dto.Name)
	}
	if dto.Email != "alex@example.com" {
		t.Errorf("expected Email=alex@example.com, got %s", dto.Email)
	}

	// Validate valid DTO
	valErr := ValidateStruct(ctx, &dto)
	if valErr != nil {
		t.Fatalf("expected validation to pass, got: %v", valErr)
	}

	// Test invalid DTO (name too short, invalid email)
	invalidDTO := TestUserDTO{
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

	// Test custom Validator interface
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
