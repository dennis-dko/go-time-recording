# CLAUDE.md - Core Directives

## 1. CI/CD, Git & Branching

* **Strict CI Rule:** No merging to `main` without green CI on the PR. Releases are cut exclusively from `main` after a second CI pass.
* **Flakes:** A red check is a red check. Re-run and merge only when green. Do not chain merge commands.
* **Branching First:** NEVER commit directly to `main`. Always create a descriptively named branch (e.g., `audit/error-handling`, `fix/race-condition`) BEFORE making any code changes.
* **Atomic Commits:** Commit iteratively. Do not bundle multiple fixes into one massive commit. You MUST create a commit immediately after completing *each logical task* or individual fix.
* **Safe Staging:** Strictly avoid `git add .`. Always run `git status` and `git diff` before staging. Use `git add <specific-file>` to prevent accidental commits of debug outputs or scratchpads like `AUDIT_REPORT.md`.
* **Commit Messages:** Explain **why**, not *what*. Use conventional formats (e.g., `fix: ...`, `refactor: ...`). Commit subject = single sentence; body = prose explaining the problem and context (not just a changelog).

## 2. Development, Audits & Anti-Hallucination

* **Prove Changes:** Write a failing test against unfixed code -> Fix -> Verify pass. Commit must document this.
* **Reproduce First:** Always reproduce bugs (especially timeouts) to capture actual state before diagnosing. 
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
* **Iterative Audit Limits:** When asked to loop until the code is "professional", use hard metrics. Stop automatically ONLY when: `golangci-lint` outputs 0 warnings, all tests pass with the race detector, OR after a hard limit of **3 iterations** (to prevent infinite loops).
* **Idiomatic Go:** Strictly follow [Effective Go](https://go.dev/doc/effective_go) principles for all new code and refactorings.
* **Context & Session Protection:** To prevent session limit crashes:
  1. **No parallel agents:** Do NOT spawn multiple background tasks or try to analyze multiple domains simultaneously. Work STRICTLY sequentially: one step, one file, write to report, move on.
  2. **Limit CLI output:** NEVER run unbounded `grep` or `cat` on large or multiple files. Always use limits (e.g., `grep -m 20`, `head -n 50`) to prevent flooding the context window.
  3. **Frequent state saving:** Flush your findings to `AUDIT_REPORT.md` after EVERY single step in the manual deep dive. Do not hold all findings in memory until the end.
* **Living Document:** `CLAUDE.md` reflects the current reality. If you modify the project's folder layout, introduce a new standard test command, or change a core architectural convention during a task, you MUST automatically update the relevant sections (e.g., Layout, Commands) in this `CLAUDE.md` file.

## 3. Testing & Commands

* `task test` (Unit), `task test:integration` (SQLite/PG/MySQL), `task test:browser` (Chrome/ chromedp), `task test:firefox` (BiDi), `task test:all`.
* `golangci-lint run --build-tags "browser integration firefox" ./...`
* **Race Detector:** `go test -race ./...` is mandatory. **Fallback:** If this fails locally due to missing CGO (e.g., on Windows), automatically fallback to Docker (`docker run --rm -v ${PWD}:/app -w /app golang:latest go test -race ./...`) or WSL (`wsl go test -race ./...`).
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

## 5. Backend, Architecture & Concurrency

* **Layout:** `cmd/` (main), `internal/domain/` (models/repos), `internal/application/` (services), `internal/infrastructure/` (config/DB), `internal/interface/` (REST/UI), `internal/pkg/` (helpers), `test/`, `deploy/`.
* **Clean Architecture:** Strictly separate handlers, services, and repositories. Business logic MUST NOT leak into HTTP handlers.
* **Dependency Injection:** Inject all external dependencies (DBs, APIs) via constructors/structs to ensure code remains testable.
* **Context Propagation:** Always pass `context.Context` as the very first parameter to all functions handling business logic, DB queries, and external calls. Respect context cancellations.
* **Goroutine Safety:** Never spawn raw goroutines without managing their lifecycle. Prevent goroutine leaks using contexts, channels, or `golang.org/x/sync/errgroup`.
* **Database Resource Management:** Ensure database connection pools are explicitly tuned (`SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`) to prevent connection starvation.
* **Errors:** API uses closed catalogue (`internal/pkg/apperror`), rendered client-side via `t('err.' + code)`. **Mandatory:** New codes require a German translation in tests. Never silently drop errors; always wrap them (`%w`) for context.
* **`go fix`:** Always run as `go fix -embedlit=false ./...` (prevents rewriting promoted fields).
* **GoFr:** Config read in `gofr.New()`. Changing metrics/trace/DB configs requires storing state and triggering a restart.

## 6. Releases

* **Auto-Patch:** Every merge to `main` cuts a patch release.
* **Manual Tag:** `task release VERSION=vX.Y.Z` for major/minor.
* **Artifacts:** Builds `linux/amd64`, `linux/arm64` images, and 4 platform binaries. Images are verified on both architectures before publishing.

## 7. Enterprise SDLC & Production Readiness

* **Zero-Downtime Migrations:** Database schema changes MUST be backward compatible. Use multi-phase deployments (e.g., add column -> dual write -> switch read -> drop column). Never drop or rename columns in a single release.
* **Secret Management:** Strictly forbid hardcoded secrets, API keys, or private credentials in code or configs. Use environment variables or secret stores exclusively.
* **Observability & Logging:** Use structured logging (e.g., standard library `slog` or project logger) with contextual keys (request_id, user_id). Avoid raw `fmt.Println` in production code. Ensure health/readiness endpoints (`/healthz`, `/readyz`) are maintained.
* **API Backward Compatibility:** Do not introduce breaking changes to existing API request/response contracts without a versioning strategy. Payload fields must be treated as immutable once public.
* **Security & OWASP:** Validate and sanitize all external inputs at application boundaries. Prevent common vulnerabilities (SQL injection via parameterized queries, XSS in HTML/JS, CSRF).
* **Architecture Decision Records (ADRs):** For major structural or architectural changes proposed during audits or refactorings, document the rationale, alternatives considered, and consequences in a lightweight ADR format (or within the PR description).