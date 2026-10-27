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

type TestPointerBindDTO struct {
	ID      *int    `param:"id"`
	Limit   *int    `query:"limit"`
	Active  *bool   `query:"active"`
	Token   *string `header:"X-Token"`
	Note    *string `form:"note"`
	Missing *int    `query:"missing"`
}

func TestBinderPointerFields(t *testing.T) {
	req := httptest.NewRequest("GET", "/x/0?limit=0&active=false", nil)
	req.Header.Set("X-Token", "abc")
	ctx := core.NewContext(httptest.NewRecorder(), req, map[string]string{"id": "0"})

	var dto TestPointerBindDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.ID == nil || *dto.ID != 0 {
		t.Errorf("expected ID=&0, got %v", dto.ID)
	}
	if dto.Limit == nil || *dto.Limit != 0 {
		t.Errorf("expected Limit=&0 (present, zero), got %v", dto.Limit)
	}
	if dto.Active == nil || *dto.Active {
		t.Errorf("expected Active=&false (present), got %v", dto.Active)
	}
	if dto.Token == nil || *dto.Token != "abc" {
		t.Errorf("expected Token=&abc, got %v", dto.Token)
	}
	if dto.Missing != nil {
		t.Errorf("absent query param must stay nil, got %v", *dto.Missing)
	}
	if dto.Note != nil {
		t.Errorf("absent form field must stay nil, got %v", *dto.Note)
	}
}

func TestBinderPointerInvalidValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/x?limit=abc", nil)
	ctx := core.NewContext(httptest.NewRecorder(), req, nil)
	var dto TestPointerBindDTO
	if err := BindRequest(ctx, &dto); err == nil {
		t.Fatal("expected error for invalid *int query value")
	}
}

func TestBinderPointerFormValue(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("note", "")
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/x", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ctx := core.NewContext(httptest.NewRecorder(), req, nil)
	var dto TestPointerBindDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.Note == nil || *dto.Note != "" {
		t.Errorf("expected Note=&\"\" (present, empty), got %v", dto.Note)
	}
}

type EmbeddedPageQuery struct {
	Limit int `query:"limit"`
	Page  int `query:"page"`
}

type EmbeddedAuditHeader struct {
	Token string `header:"X-Token"`
}

type EmbeddedReqDTO struct {
	EmbeddedPageQuery
	*EmbeddedAuditHeader
	ID   int    `param:"id"`
	Name string `json:"name"`
}

func TestBinderEmbeddedStruct(t *testing.T) {
	body := []byte(`{"name":"Alex"}`)
	req := httptest.NewRequest("POST", "/items/42?limit=10&page=2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", "secret123")
	ctx := core.NewContext(httptest.NewRecorder(), req, map[string]string{"id": "42"})

	var dto EmbeddedReqDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.ID != 42 {
		t.Errorf("expected ID=42, got %d", dto.ID)
	}
	if dto.Name != "Alex" {
		t.Errorf("expected Name=Alex, got %q", dto.Name)
	}
	if dto.Limit != 10 || dto.Page != 2 {
		t.Errorf("expected embedded Limit=10, Page=2, got %+v", dto.EmbeddedPageQuery)
	}
	if dto.EmbeddedAuditHeader == nil || dto.EmbeddedAuditHeader.Token != "secret123" {
		t.Errorf("expected embedded pointer to be allocated with Token=secret123, got %+v", dto.EmbeddedAuditHeader)
	}
}

func TestBinderEmbeddedPointerNilWhenAbsent(t *testing.T) {
	req := httptest.NewRequest("GET", "/items/1?limit=5", nil)
	ctx := core.NewContext(httptest.NewRecorder(), req, map[string]string{"id": "1"})

	var dto EmbeddedReqDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.EmbeddedAuditHeader != nil {
		t.Errorf("expected embedded pointer to stay nil when header is absent, got %+v", dto.EmbeddedAuditHeader)
	}
	if dto.Limit != 5 {
		t.Errorf("expected Limit=5, got %d", dto.Limit)
	}
}

type ShadowOuterDTO struct {
	EmbeddedPageQuery
	Limit int `query:"limit"` // Outer shadows EmbeddedPageQuery.Limit
}

func TestBinderEmbeddedShadowing(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?limit=99&page=1", nil)
	ctx := core.NewContext(httptest.NewRecorder(), req, nil)

	var dto ShadowOuterDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.Limit != 99 {
		t.Errorf("outer Limit should win, got %d", dto.Limit)
	}
	if dto.EmbeddedPageQuery.Limit != 0 {
		t.Errorf("shadowed embedded Limit must not be set, got %d", dto.EmbeddedPageQuery.Limit)
	}
	if dto.EmbeddedPageQuery.Page != 1 {
		t.Errorf("unshadowed embedded Page must be set to 1, got %d", dto.EmbeddedPageQuery.Page)
	}
}

func TestBinderEmbeddedJSONCannotSpoofNonBodyFields(t *testing.T) {
	body := []byte(`{"Limit":100,"Page":50,"Token":"spoofed","Name":"Sam"}`)
	req := httptest.NewRequest("POST", "/items/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := core.NewContext(httptest.NewRecorder(), req, map[string]string{"id": "1"})

	var dto EmbeddedReqDTO
	if err := BindRequest(ctx, &dto); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if dto.Limit != 0 || dto.Page != 0 {
		t.Errorf("JSON body must not spoof embedded non-body fields: %+v", dto.EmbeddedPageQuery)
	}
	if dto.EmbeddedAuditHeader != nil {
		t.Errorf("JSON body must not allocate embedded pointer for non-body fields: %+v", dto.EmbeddedAuditHeader)
	}
	if dto.Name != "Sam" {
		t.Errorf("expected Name=Sam, got %q", dto.Name)
	}
}
