package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sseFrame(t *testing.T, event string, data any) string {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx := NewContext(rec, httptest.NewRequest("GET", "/", nil), nil)
	if err := ctx.SSEvent(event, data); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

func TestSSEventBasicFrames(t *testing.T) {
	if got := sseFrame(t, "ping", "hello"); got != "event: ping\ndata: hello\n\n" {
		t.Errorf("unexpected frame %q", got)
	}
	if got := sseFrame(t, "", Map{"a": 1}); got != "data: {\"a\":1}\n\n" {
		t.Errorf("unexpected json frame %q", got)
	}
}

func TestSSEventPreventsEventNameInjection(t *testing.T) {
	got := sseFrame(t, "x\ndata: forged\n\nevent: admin", "ok")
	want := "event: xdata: forgedevent: admin\ndata: ok\n\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSSEventPreventsDataInjection(t *testing.T) {
	got := sseFrame(t, "msg", "line1\n\nevent: forged\r\ndata: x\rend")
	want := "event: msg\ndata: line1\ndata: \ndata: event: forged\ndata: data: x\ndata: end\n\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// []byte payloads are sanitized too.
	if got := sseFrame(t, "", []byte("a\nb")); got != "data: a\ndata: b\n\n" {
		t.Errorf("got %q", got)
	}
}

type deadlineTrackingResponseWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (d *deadlineTrackingResponseWriter) SetWriteDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}

func TestSSEventSetsAndClearsPerFrameWriteDeadline(t *testing.T) {
	dw := &deadlineTrackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	ctx := NewContext(dw, httptest.NewRequest(http.MethodGet, "/events", nil), nil)
	if err := ctx.SSEvent("tick", "1"); err != nil {
		t.Fatalf("SSEvent failed: %v", err)
	}
	if len(dw.deadlines) != 2 {
		t.Fatalf("expected 2 SetWriteDeadline calls (set then clear), got %d", len(dw.deadlines))
	}
	if dw.deadlines[0].IsZero() {
		t.Error("expected non-zero write deadline before writing SSE frame")
	}
	if !dw.deadlines[1].IsZero() {
		t.Error("expected write deadline to be cleared (zero) after flushing SSE frame")
	}
}
