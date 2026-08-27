# CLAUDE.md - Core Directives

## 1. CI/CD & Workflow

* **Strict CI Rule:** No merging to `main` without green CI on the PR. Releases are cut exclusively from `main` after a second CI pass.
* **Flakes:** A red check is a red check. Re-run and merge only when green. Do not chain merge commands.

## 2. Development, Audits & Anti-Hallucination

* **Prove Changes:** Write a failing test against unfixed code -> Fix -> Verify pass. Commit must document this.
* **Reproduce First:** Always reproduce bugs (especially timeouts) to capture actual state before diagnosing. 
* **Comments & Commits:** Explain **why**, not *what*. Commit subject = sentence; body = prose explaining the problem (not a changelog).
* **Zero Hallucination Audits:** Never guess or assume implementation details. You MUST use CLI tools (`grep`, `cat`, `rg`, `ls`) to read actual files before reviewing. **Burden of Proof:** Every issue you find MUST include the exact file path and line number. Do not critique code you haven't explicitly read into context.
* **Agentic Auditing Workflow:** Always document findings sequentially in an `AUDIT_REPORT.md` scratchpad. 
  1. **Automated first:** Run `golangci-lint`, `go test -race ./...`, `go mod tidy` and `govulncheck`. Document hard facts.
  2. **Manual Deep Dive:** Actively read files and search for:
     - *Architecture:* Read at least 3 core services to check domain boundaries.
     - *Errors:* Grep for `return err` / `fmt.Errorf` to enforce `apperror` usage and missing DE translations.
     - *State:* Grep for package-level `var` and unprotected maps (race conditions).
     - *Frontend:* Check JS/HTML for inline styles, `script-src` violations, and UI DOM quirks.
     - *Duplicates:* Search for redundant business logic or duplicated UI controls.
  3. **Stop & Ask:** Once the report is full, summarize findings and ask the user which exact file/issue to fix first using TDD.
* **Idiomatic Go:** Strictly follow [Effective Go](https://go.dev/doc/effective_go) principles for all new code and refactorings.
* **Context & Session Protection:** To prevent session limit crashes:
  1. **No parallel agents:** Do NOT spawn multiple background tasks or try to analyze multiple domains simultaneously. Work STRICTLY sequentially: one step, one file, write to report, move on.
  2. **Limit CLI output:** NEVER run unbounded `grep` or `cat` on large or multiple files. Always use limits (e.g., `grep -m 20`, `head -n 50`) to prevent flooding the context window.
  3. **Frequent state saving:** Flush your findings to `AUDIT_REPORT.md` after EVERY single step in the manual deep dive. Do not hold all findings in memory until the end.

## 3. Testing & Commands

* `task test` (Unit), `task test:integration` (SQLite/PG/MySQL), `task test:browser` (Chrome/ chromedp), `task test:firefox` (BiDi), `task test:all`.
* `golangci-lint run --build-tags "browser integration firefox" ./...`
* `go test -race ./...` (Mandatory for checking race conditions during audits).
* `go mod tidy` & `govulncheck ./...` (Mandatory dependency checks).
* **Scoping:** Run *integration* for handler/service/migration changes. Run *browser* for UI changes. 
* **Browser Suite Notes:** Runs parallel (load-sensitive). Fix missing waits instead of bumping timeouts. 
* `task clean`: Cleans artifacts and orphaned headless browsers.

## 4. Frontend & Pitfalls

* **Vanilla JS UI:** No build step, `go:embed`, strict CSP (`script-src 'self'`). No inline handlers/styles.
* **State/Refills:** Background loads/saves must not overwrite a partially filled form or read stale server state.
* **Duplication:** Search for existing UI controls (e.g., password reveals) before adding new ones.
* **DOM Quirks:**
  * **SVG `hidden`:** SVGs lack this property. Use the attribute + `display: none`.
  * **`form.id`:** Fails if a child input is named `id`. Always use `getAttribute('id')`.
  * **`<select>`:** Has no default empty option; restoring a form defaults to the first entry unless handled.

## 5. Backend & Architecture

* **Layout:** `cmd/` (main), `internal/domain/` (models/repos), `internal/application/` (services), `internal/infrastructure/` (config/DB), `internal/interface/` (REST/UI), `internal/pkg/` (helpers), `test/`, `deploy/`.
* **Errors:** API uses closed catalogue (`internal/pkg/apperror`), rendered client-side via `t('err.' + code)`. **Mandatory:** New codes require a German translation in tests.
* **`go fix`:** Always run as `go fix -embedlit=false ./...` (prevents rewriting promoted fields).
* **GoFr:** Config read in `gofr.New()`. Changing metrics/trace/DB configs requires storing state and triggering a restart.

## 6. Releases

* **Auto-Patch:** Every merge to `main` cuts a patch release.
* **Manual Tag:** `task release VERSION=vX.Y.Z` for major/minor.
* **Artifacts:** Builds `linux/amd64`, `linux/arm64` images, and 4 platform binaries. Images are verified on both architectures before publishing.
