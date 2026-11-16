package typescript

// Config describes API metadata and generator options for TypeScript SDK generation.
type Config struct {
	Title              string
	Version            string
	Description        string
	SilenceTagWarnings bool
}

// Param describes an endpoint parameter (path, query, or header).
type Param struct {
	Name     string
	In       string // "path", "query", or "header"
	Required bool
	Validate string
	Model    any
}

// RequestBody describes the HTTP request payload for an endpoint.
type RequestBody struct {
	ContentType string
	Required    bool
	Model       any
}

// Response describes an HTTP response payload for a status code.
type Response struct {
	ContentType string
	Model       any
}

// Route describes a single HTTP endpoint to include in the generated TypeScript SDK.
type Route struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Hidden      bool
	Parameters  []Param
	RequestBody *RequestBody
	Responses   map[int]*Response
}

// Spec is the self-contained API specification consumed by the TypeScript SDK generator.
type Spec struct {
	Config Config
	Routes []Route
}

// Options configures the multi-file TypeScript SDK output.
type Options struct {
	// ImportExtension is appended to relative import specifiers between generated files:
	//   - "" (default) / "none": extensionless imports, for bundlers (Next.js, Vite, webpack);
	//   - ".js": for TypeScript moduleResolution "node16"/"nodenext" emitting ESM;
	//   - ".ts": for Deno, Bun and Node.js type stripping (or allowImportingTsExtensions);
	//   - "auto": detects the appropriate suffix from deno.json / tsconfig.json / package.json near outDir.
	ImportExtension string
}
