package gota

import "github.com/carlosxfelipe/gota/typescript"

// TypeScriptSpec converts a registered gota/repr App into a decoupled typescript.Spec.
func TypeScriptSpec(app *App) typescript.Spec {
	if app == nil {
		return typescript.Spec{}
	}
	cfg := app.Config()
	appRoutes := app.Routes()
	routes := make([]typescript.Route, 0, len(appRoutes))
	for _, rt := range appRoutes {
		if rt == nil {
			continue
		}
		tr := typescript.Route{
			Method: rt.Method(),
			Path:   rt.Path(),
		}
		if doc := rt.Doc(); doc != nil {
			tr.Summary = doc.Summary
			tr.Description = doc.Description
			tr.Tags = append([]string(nil), doc.Tags...)
			tr.Deprecated = doc.Deprecated
			tr.Hidden = doc.Hidden
			if len(doc.Parameters) > 0 {
				tr.Parameters = make([]typescript.Param, len(doc.Parameters))
				for i, p := range doc.Parameters {
					tr.Parameters[i] = typescript.Param{
						Name:     p.Name,
						In:       p.In,
						Required: p.Required,
						Validate: p.Validate,
						Model:    p.Model,
					}
				}
			}
			if doc.RequestBody != nil {
				tr.RequestBody = &typescript.RequestBody{
					ContentType: doc.RequestBody.ContentType,
					Required:    doc.RequestBody.Required,
					Model:       doc.RequestBody.Model,
				}
			}
			if len(doc.Responses) > 0 {
				tr.Responses = make(map[int]*typescript.Response, len(doc.Responses))
				for code, resp := range doc.Responses {
					if resp == nil {
						continue
					}
					tr.Responses[code] = &typescript.Response{
						ContentType: resp.ContentType,
						Model:       resp.Model,
					}
				}
			}
		}
		routes = append(routes, tr)
	}
	return typescript.Spec{
		Config: typescript.Config{
			Title:              cfg.Title,
			Version:            cfg.Version,
			Description:        cfg.Description,
			SilenceTagWarnings: cfg.SilenceTagWarnings,
		},
		Routes: routes,
	}
}

// GenerateTypeScriptSDK generates the modular TypeScript client SDK files for app.
func GenerateTypeScriptSDK(app *App, opts ...typescript.Options) (map[string]string, error) {
	return typescript.Generate(TypeScriptSpec(app), opts...)
}

// ExportTypeScriptSDK writes the modular TypeScript client SDK for app into outDir.
func ExportTypeScriptSDK(app *App, outDir string, opts ...typescript.Options) error {
	return typescript.Export(TypeScriptSpec(app), outDir, opts...)
}

// CheckTypeScriptSDK verifies that outDir matches the modular TypeScript SDK for app.
func CheckTypeScriptSDK(app *App, outDir string, opts ...typescript.Options) error {
	return typescript.Check(TypeScriptSpec(app), outDir, opts...)
}

// RunCodegen processes CLI arguments (e.g. from os.Args[1:]) to export or verify the TypeScript SDK for app.
func RunCodegen(app *App, args []string) (bool, error) {
	return typescript.RunCodegen(TypeScriptSpec(app), args)
}
