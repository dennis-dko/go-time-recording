# CLAUDE.md - Core Directives

## 1. CI/CD & Workflow

* **Strict CI Rule:** No merging to `main` without green CI on the PR. Releases are cut exclusively from `main` after a second CI pass.
* **Flakes:** A red check is a red check. Re-run and merge only when green. Do not chain merge commands.

## 2. Development & Commits

* **Prove Changes:** Write a failing test against unfixed code -> Fix -> Verify pass. Commit must document this.
* **Reproduce First:** Always reproduce bugs (especially timeouts) to capture actual state before diagnosing. 
* **Comments & Commits:** Explain **why**, not *what*. Commit subject = sentence; body = prose explaining the problem (not a changelog).

## 3. Testing & Commands

* `task test` (Unit), `task test:integration` (SQLite/PG/MySQL), `task test:browser` (Chrome/ chromedp), `task test:firefox` (BiDi), `task test:all`.
* `golangci-lint run --build-tags "browser integration firefox" ./...`
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
