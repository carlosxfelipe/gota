package binding

import (
	"bytes"
	"mime/multipart"
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

type TestUploadDTO struct {
	Title string                `form:"title"`
	File  *multipart.FileHeader `form:"file"`
	Tags  []string              `form:"tags"`
}

type TestMultipleFilesDTO struct {
	Files []*multipart.FileHeader `form:"files"`
}

func TestBinderMultipartAndForm(t *testing.T) {
	// 1. Test multipart/form-data with file upload
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	_ = w.WriteField("title", "My Document")
	_ = w.WriteField("tags", "doc")
	_ = w.WriteField("tags", "pdf")

	part, err := w.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte("Hello world file content"))
	_ = w.Close()

	req := httptest.NewRequest("POST", "/upload", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())

	ctx := core.NewContext(httptest.NewRecorder(), req, nil)

	var dto TestUploadDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind multipart failed: %v", err)
	}

	if dto.Title != "My Document" {
		t.Errorf("expected title 'My Document', got %q", dto.Title)
	}
	if len(dto.Tags) != 2 || dto.Tags[0] != "doc" || dto.Tags[1] != "pdf" {
		t.Errorf("expected tags [doc pdf], got %v", dto.Tags)
	}
	if dto.File == nil {
		t.Fatal("expected File to be bound, got nil")
	}
	if dto.File.Filename != "test.txt" {
		t.Errorf("expected filename 'test.txt', got %q", dto.File.Filename)
	}

	// 2. Test multiple files []*multipart.FileHeader
	var b2 bytes.Buffer
	w2 := multipart.NewWriter(&b2)
	part1, _ := w2.CreateFormFile("files", "file1.txt")
	_, _ = part1.Write([]byte("content 1"))
	part2, _ := w2.CreateFormFile("files", "file2.txt")
	_, _ = part2.Write([]byte("content 2"))
	_ = w2.Close()

	req2 := httptest.NewRequest("POST", "/upload-multi", &b2)
	req2.Header.Set("Content-Type", w2.FormDataContentType())

	ctx2 := core.NewContext(httptest.NewRecorder(), req2, nil)
	var multiDTO TestMultipleFilesDTO
	if err := BindRequest(ctx2, &multiDTO); err != nil {
		t.Fatalf("bind multi-file failed: %v", err)
	}
	if len(multiDTO.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(multiDTO.Files))
	}
	if multiDTO.Files[0].Filename != "file1.txt" || multiDTO.Files[1].Filename != "file2.txt" {
		t.Errorf("expected file1.txt and file2.txt, got %s, %s", multiDTO.Files[0].Filename, multiDTO.Files[1].Filename)
	}
}

func TestBinderJSONCannotSpoofNonBodyFields(t *testing.T) {
	body := []byte(`{"ID":1,"Page":9,"Auth":"forged","name":"Alex"}`)
	req := httptest.NewRequest("POST", "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := core.NewContext(httptest.NewRecorder(), req, nil)

	var dto TestBindDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.ID != 0 || dto.Page != 0 || dto.Auth != "" {
		t.Errorf("JSON body spoofed non-body fields: %+v", dto)
	}
	if dto.Name != "Alex" {
		t.Errorf("expected Name=Alex, got %q", dto.Name)
	}
}
