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
