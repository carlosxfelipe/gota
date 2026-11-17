package scalar

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sync"
)

// Theme defines available color themes for Scalar API Reference.
type Theme string

const (
	ThemeDefault    Theme = "default"
	ThemeAlternate  Theme = "alternate"
	ThemeMoon       Theme = "moon"
	ThemePurple     Theme = "purple"
	ThemeSolarized  Theme = "solarized"
	ThemeBluePlanet Theme = "bluePlanet"
	ThemeDeepSpace  Theme = "deepSpace"
	ThemeSaturn     Theme = "saturn"
	ThemeKepler     Theme = "kepler"
	ThemeElysiaJS   Theme = "elysiajs"
	ThemeFastify    Theme = "fastify"
	ThemeMars       Theme = "mars"
	ThemeLaserwave  Theme = "laserwave"
	ThemeNone       Theme = "none"
)

// Default Scalar CDN settings pinned to a stable release with Subresource Integrity (SRI).
// To upgrade the version, update DefaultVersion and recompute DefaultSRI via:
//
//	curl -sL https://cdn.jsdelivr.net/npm/@scalar/api-reference@<version>/dist/browser/standalone.js | openssl dgst -sha384 -binary | openssl base64 -A | sed 's/^/sha384-/'
const (
	DefaultVersion = "1.76.0"
	DefaultCDN     = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@" + DefaultVersion + "/dist/browser/standalone.js"
	DefaultSRI     = "sha384-oH1C0RwqcwbVXkC+r8A+4qRoyEUQdtxAzOwumkxZtQBBcsy61FOoPt7B50OVJYYQ"
)

// CDNURL returns the standalone bundle URL for a given @scalar/api-reference version.
func CDNURL(version string) string {
	return "https://cdn.jsdelivr.net/npm/@scalar/api-reference@" + version + "/dist/browser/standalone.js"
}

// Config holds visual, security, and functional settings for Scalar API Reference.
type Config struct {
	Title        string            `json:"title,omitempty"`
	SpecURL      string            `json:"specUrl,omitempty"`
	Theme        Theme             `json:"theme,omitempty"`
	Layout       string            `json:"layout,omitempty"` // "modern" (default) or "classic"
	DarkMode     *bool             `json:"darkMode,omitempty"`
	ShowSidebar  *bool             `json:"showSidebar,omitempty"`
	ProxyURL     string            `json:"proxyUrl,omitempty"`
	CustomCSS    string            `json:"customCss,omitempty"`
	SearchHotKey string            `json:"searchHotKey,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	ScriptURL    string            `json:"scriptUrl,omitempty"`
	SRI          string            `json:"sri,omitempty"`

	// CSP overrides the Content-Security-Policy header sent by Handler.
	// Empty uses ContentSecurityPolicy; "-" omits the CSP header.
	CSP string `json:"-"`

	// CSPReportOnly sends Content-Security-Policy-Report-Only instead of enforcing Content-Security-Policy.
	CSPReportOnly bool `json:"-"`
}

// RenderHTML generates the standalone HTML page for Scalar API Reference.
func RenderHTML(cfg Config) (string, error) {
	title := cmp.Or(cfg.Title, "API Reference")
	specURL := cmp.Or(cfg.SpecURL, "/openapi.json")
	theme := cmp.Or(cfg.Theme, ThemeDefault)
	layout := cmp.Or(cfg.Layout, "modern")

	scalarOptions := map[string]any{
		"theme":  theme,
		"layout": layout,
	}

	if cfg.ProxyURL != "" {
		scalarOptions["proxyUrl"] = cfg.ProxyURL
	}
	if cfg.DarkMode != nil {
		scalarOptions["darkMode"] = *cfg.DarkMode
	}
	if cfg.ShowSidebar != nil {
		scalarOptions["showSidebar"] = *cfg.ShowSidebar
	}
	if cfg.SearchHotKey != "" {
		scalarOptions["searchHotKey"] = cfg.SearchHotKey
	}
	if cfg.CustomCSS != "" {
		scalarOptions["customCss"] = cfg.CustomCSS
	}
	if len(cfg.Metadata) > 0 {
		scalarOptions["metadata"] = cfg.Metadata
	}

	optionsJSON, err := json.Marshal(scalarOptions)
	if err != nil {
		return "", err
	}

	scriptURL := cfg.ScriptURL
	sri := cfg.SRI
	if scriptURL == "" {
		// The default script is always paired with its own hash; a custom SRI only makes
		// sense together with a custom ScriptURL (otherwise the browser would block it).
		scriptURL = DefaultCDN
		sri = DefaultSRI
	}

	var integrityAttr string
	if sri != "" {
		integrityAttr = fmt.Sprintf("\n      integrity=\"%s\"\n      crossorigin=\"anonymous\"", html.EscapeString(sri))
	}

	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <title>%s</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <link rel="icon" type="image/svg+xml" href="https://scalar.com/favicon.svg" />
    <style>
      body {
        margin: 0;
        padding: 0;
        min-height: 100vh;
      }
    </style>
  </head>
  <body>
    <script
      id="api-reference"
      data-url="%s"
      data-configuration='%s'
      src="%s"%s>
    </script>
  </body>
</html>`, html.EscapeString(title), html.EscapeString(specURL), html.EscapeString(string(optionsJSON)), html.EscapeString(scriptURL), integrityAttr)

	return page, nil
}

// ContentSecurityPolicy builds a Scalar-compatible Content-Security-Policy for the given script URL.
func ContentSecurityPolicy(scriptURL string) string {
	scriptURL = cmp.Or(scriptURL, DefaultCDN)
	scriptSrc := "'self'"
	if u, err := url.Parse(scriptURL); err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
		scriptSrc += " " + u.Scheme + "://" + u.Host
	}
	return "default-src 'self'; script-src " + scriptSrc +
		"; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data: https://fonts.scalar.com" +
		"; connect-src 'self' https://proxy.scalar.com; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

// Handler returns a standard net/http HandlerFunc serving the Scalar UI with security headers.
// Usable standalone with http.ServeMux, chi, or inside repr.App.
func Handler(cfg Config) http.HandlerFunc {
	var (
		once    sync.Once
		htmlDoc string
		render  error
	)
	return func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			htmlDoc, render = RenderHTML(cfg)
		})
		if render != nil {
			http.Error(w, "failed to render api docs", http.StatusInternalServerError)
			return
		}

		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")

		if cfg.CSP != "-" {
			csp := cfg.CSP
			if csp == "" {
				csp = ContentSecurityPolicy(cfg.ScriptURL)
			}
			headerName := "Content-Security-Policy"
			if cfg.CSPReportOnly {
				headerName = "Content-Security-Policy-Report-Only"
			}
			h.Set(headerName, csp)
		}

		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlDoc))
	}
}
