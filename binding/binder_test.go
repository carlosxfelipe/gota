package binding

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/carlosxfelipe/gota/core"
)

type TestBindDTO struct {
	ID    int    `param:"id"`
	Page  int    `query:"page"`
	Auth  string `header:"Authorization"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestBinder(t *testing.T) {
	body := []byte(`{"name":"Alex","email":"alex@example.com"}`)
	req := httptest.NewRequest("POST", "/users/42?page=3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	w := httptest.NewRecorder()
	ctx := core.NewContext(w, req, map[string]string{"id": "42"})

	var dto TestBindDTO
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
}

type TestBoolDTO struct {
	Active bool `query:"active"`
}

func TestBinderBool(t *testing.T) {
	bind := func(query string) (TestBoolDTO, error) {
		req := httptest.NewRequest("GET", "/x?active="+query, nil)
		ctx := core.NewContext(httptest.NewRecorder(), req, nil)
		var dto TestBoolDTO
		err := BindRequest(ctx, &dto)
		return dto, err
	}

	for _, v := range []string{"true", "1", "yes"} {
		if dto, err := bind(v); err != nil || !dto.Active {
			t.Errorf("%q: expected true, got %v (err=%v)", v, dto.Active, err)
		}
	}
	for _, v := range []string{"false", "0", "no"} {
		if dto, err := bind(v); err != nil || dto.Active {
			t.Errorf("%q: expected false, got %v (err=%v)", v, dto.Active, err)
		}
	}
	if _, err := bind("banana"); err == nil {
		t.Errorf("expected error for invalid boolean value")
	}
}
