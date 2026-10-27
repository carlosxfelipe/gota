package core

import (
	"encoding/json"
	"fmt"
	"html"
)

// ScalarTheme defines available color themes for Scalar API Reference.
type ScalarTheme string

const (
	ThemeDefault    ScalarTheme = "default"
	ThemeAlternate  ScalarTheme = "alternate"
	ThemeMoon       ScalarTheme = "moon"
	ThemePurple     ScalarTheme = "purple"
	ThemeSolarized  ScalarTheme = "solarized"
	ThemeBluePlanet ScalarTheme = "bluePlanet"
	ThemeDeepSpace  ScalarTheme = "deepSpace"
	ThemeSaturn     ScalarTheme = "saturn"
	ThemeKepler     ScalarTheme = "kepler"
	ThemeElysiaJS   ScalarTheme = "elysiajs"
	ThemeFastify    ScalarTheme = "fastify"
	ThemeMars       ScalarTheme = "mars"
	ThemeLaserwave  ScalarTheme = "laserwave"
	ThemeNone       ScalarTheme = "none"
)

// Default Scalar CDN settings pinned to latest stable release with Subresource Integrity (SRI).
const (
	DefaultScalarVersion = "1.76.0"
	DefaultScalarCDN     = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.76.0/dist/browser/standalone.js"
	DefaultScalarSRI     = "sha384-oH1C0RwqcwbVXkC+r8A+4qRoyEUQdtxAzOwumkxZtQBBcsy61FOoPt7B50OVJYYQ"
)

// ScalarConfig holds visual and functional settings for Scalar API Reference.
type ScalarConfig struct {
	Title        string            `json:"title,omitempty"`
	SpecURL      string            `json:"specURL,omitempty"`
	Theme        ScalarTheme       `json:"theme,omitempty"`
	Layout       string            `json:"layout,omitempty"` // "classic" (default) or "modern"
	DarkMode     *bool             `json:"darkMode,omitempty"`
	ShowSidebar  *bool             `json:"showSidebar,omitempty"`
	ProxyURL     string            `json:"proxyUrl,omitempty"`
	CustomCSS    string            `json:"customCss,omitempty"`
	SearchHotKey string            `json:"searchHotKey,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	ScriptURL    string            `json:"scriptUrl,omitempty"`
	SRI          string            `json:"sri,omitempty"`
}

// RenderScalarHTML generates the standalone HTML page for Scalar API Reference.
// Default layout is "classic" (Swagger-like), as configured in Gota.
func RenderScalarHTML(cfg ScalarConfig) (string, error) {
	title := cfg.Title
	if title == "" {
		title = "Gota API Reference"
	}

	theme := cfg.Theme
	if theme == "" {
		theme = ThemeFastify
	}

	layout := cfg.Layout
	if layout == "" {
		layout = "classic"
	}

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

	optionsJSON, err := json.Marshal(scalarOptions)
	if err != nil {
		return "", err
	}

	scriptURL := cfg.ScriptURL
	sri := cfg.SRI
	if scriptURL == "" {
		// The default script is always paired with its own hash; a custom SRI only makes
		// sense together with a custom ScriptURL (otherwise the browser would block it).
		scriptURL = DefaultScalarCDN
		sri = DefaultScalarSRI
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
</html>`, html.EscapeString(title), html.EscapeString(cfg.SpecURL), html.EscapeString(string(optionsJSON)), html.EscapeString(scriptURL), integrityAttr)

	return page, nil
}
