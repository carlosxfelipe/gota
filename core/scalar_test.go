package core

import (
	"strings"
	"testing"
)

func TestRenderScalarHTMLEscapesValues(t *testing.T) {
	out, err := RenderScalarHTML(ScalarConfig{
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

func TestRenderScalarHTML_Defaults(t *testing.T) {
	out, err := RenderScalarHTML(ScalarConfig{
		SpecURL: "/openapi.json",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, DefaultScalarCDN) {
		t.Errorf("expected default CDN URL %q in output, got: %s", DefaultScalarCDN, out)
	}
	if !strings.Contains(out, `integrity="`+DefaultScalarSRI+`"`) {
		t.Errorf("expected default SRI in output, got: %s", out)
	}
	if !strings.Contains(out, `crossorigin="anonymous"`) {
		t.Errorf("expected crossorigin attribute when SRI is set")
	}
}

func TestRenderScalarHTML_CustomScriptAndSRI(t *testing.T) {
	// Custom script URL without SRI
	out, err := RenderScalarHTML(ScalarConfig{
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

	// Custom script URL with custom SRI
	outWithSRI, err := RenderScalarHTML(ScalarConfig{
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
