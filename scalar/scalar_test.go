package scalar

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestRenderHTMLEscapesValues(t *testing.T) {
	out, err := RenderHTML(Config{
		Title:     `</title><script>alert(1)</script>`,
		SpecURL:   `/x"><script>alert(2)</script>`,
		CustomCSS: `body{content:'x'} ' onload='alert(3)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script>alert(1)", `"><script>alert(2)`, `' onload='alert(3)`} {
		if strings.Contains(out, bad) {
			t.Errorf("unescaped value in HTML: %q", bad)
		}
	}
}

func TestRenderHTMLDefaults(t *testing.T) {
	out, err := RenderHTML(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, DefaultCDN) {
		t.Errorf("expected default CDN URL %q in output", DefaultCDN)
	}
	if !strings.Contains(out, `data-url="/openapi.json"`) {
		t.Errorf("expected default SpecURL /openapi.json in output")
	}
	if !strings.Contains(out, `integrity="`+DefaultSRI+`"`) {
		t.Errorf("expected default SRI in output")
	}
	if !strings.Contains(out, `crossorigin="anonymous"`) {
		t.Errorf("expected crossorigin attribute when SRI is set")
	}
	if !strings.Contains(out, `"theme":"default"`) && !strings.Contains(out, `&#34;theme&#34;:&#34;default&#34;`) {
		t.Errorf("expected default theme %q in output", ThemeDefault)
	}
	if !strings.Contains(out, `"layout":"modern"`) && !strings.Contains(out, `&#34;layout&#34;:&#34;modern&#34;`) {
		t.Errorf("expected default layout %q in output", "modern")
	}
}

func TestRenderHTMLCustomScriptAndSRI(t *testing.T) {
	out, err := RenderHTML(Config{
		SpecURL:   "/openapi.json",
		ScriptURL: "/static/scalar.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `src="/static/scalar.js"`) {
		t.Errorf("expected custom script URL in output")
	}
	if strings.Contains(out, "integrity=") {
		t.Errorf("did not expect integrity attribute when custom script has no SRI")
	}

	outWithSRI, err := RenderHTML(Config{
		SpecURL:   "/openapi.json",
		ScriptURL: "https://custom.cdn/scalar.js",
		SRI:       "sha384-customhash123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outWithSRI, `src="https://custom.cdn/scalar.js"`) {
		t.Errorf("expected custom script URL in output")
	}
	if !strings.Contains(outWithSRI, `integrity="sha384-customhash123"`) {
		t.Errorf("expected custom SRI in output")
	}
}

func TestRenderHTMLSRIWithoutScriptURLIsIgnored(t *testing.T) {
	out, err := RenderHTML(Config{
		SpecURL: "/openapi.json",
		SRI:     "sha384-wronghash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "wronghash") {
		t.Errorf("custom SRI must not be applied to default script")
	}
	if !strings.Contains(out, `integrity="`+DefaultSRI+`"`) {
		t.Errorf("expected default SRI to stay in place")
	}
}

func TestConstantsConsistency(t *testing.T) {
	if DefaultCDN != CDNURL(DefaultVersion) {
		t.Errorf("expected DefaultCDN %q, got %q", CDNURL(DefaultVersion), DefaultCDN)
	}
	sriPattern := regexp.MustCompile(`^sha(256|384|512)-[A-Za-z0-9+/=]+$`)
	if !sriPattern.MatchString(DefaultSRI) {
		t.Errorf("invalid SRI hash format %q", DefaultSRI)
	}
}

func TestHandlerSecurityHeaders(t *testing.T) {
	h := Handler(Config{Title: "My API"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options DENY, got %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "cdn.jsdelivr.net") {
		t.Errorf("expected enforcing CSP header with cdn.jsdelivr.net, got %q", got)
	}

	roHandler := Handler(Config{Title: "My API", CSPReportOnly: true})
	roRec := httptest.NewRecorder()
	roHandler.ServeHTTP(roRec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if got := roRec.Header().Get("Content-Security-Policy-Report-Only"); !strings.Contains(got, "cdn.jsdelivr.net") {
		t.Errorf("expected report-only CSP header with cdn.jsdelivr.net, got %q", got)
	}
	if got := roRec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("expected no enforcing CSP header in report-only mode, got %q", got)
	}
}
