# TODO — Open Design Trade-offs

## 1. Default `ImportExtension` (`""` vs `"auto"`)

- [ ] **Evaluate switching the default `ImportExtension` from `""` to `"auto"` (or keeping opt-in):**
  - **Current behavior:** `ExportTypeScriptSDK`, `CheckTypeScriptSDK`, and `RunCodegen` (`--ts-sdk`) default to `ImportExtension: ""` (extensionless imports aimed at bundlers like Next.js, Vite, and webpack). Autodetection via `DetectTypeScriptImportExtension(dir)` runs only when `ImportExtension: "auto"` or `--ts-import-ext=auto` is passed explicitly (`repr/app.go`, `repr/typescript_render.go`).
  - **Why keep `""` as default (pros):**
    - Completely predictable and deterministic regardless of parent directory files (`deno.json`, `tsconfig.json`, `package.json`).
    - Generating into a standalone staging folder (e.g., `./dist` outside a TS workspace) or beneath an unrelated root `package.json` will never unexpectedly switch specifiers between `""`, `".js"`, and `".ts"`.
    - Matches `--ts-import-ext=none` without requiring explicit overrides in bundler-first setups.
  - **Why `"auto"` as default could help (cons of current default):**
    - Projects using `node16`/`nodenext` ESM, Deno, Bun, or Node type stripping have to remember `--ts-import-ext=auto` (or `.js`/`.ts`) on first setup instead of getting working imports out of the box when exporting inside an existing JS/TS workspace.
  - **Decision criteria:** Monitor real-world usage across Node ESM, Deno/Bun, and monorepo setups (where root config files may differ from sub-package `tsconfig.json` files) before deciding whether to keep `""` or promote `"auto"` to the default in CLI codegen.
