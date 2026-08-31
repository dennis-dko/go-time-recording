# CLAUDE.md - Core Directives

**What this is:** `Go-Time-Recording` — project time tracking as **a single self-contained binary**. The REST API and the web interface live in the same executable; the UI assets are embedded with `go:embed`, so starting it needs no asset directory and no migration step. Built on [GoFr](https://gofr.dev), structured after [gogs](https://github.com/gogs/gogs). With no database configured it serves an installer on the same port and waits, because there is nowhere to put an account, a project or an hour until a database exists.

## 1. CI/CD, Git & Branching

* **Strict CI Rule:** No merging to `main` without green CI on the PR. Releases are cut exclusively from `main` after a second CI pass.
* **Flakes:** A red check is a red check. Re-run and merge only when green. Do not chain merge commands.
* **Branching First:** NEVER commit directly to `main`. Always create a descriptively named branch (e.g., `audit/error-handling`, `fix/race-condition`) BEFORE making any code changes.
* **Atomic Commits:** Commit iteratively. Do not bundle multiple fixes into one massive commit. You MUST create a commit immediately after completing *each logical task* or individual fix.
* **Safe Staging:** Strictly avoid `git add .`. Always run `git status` and `git diff` before staging. Use `git add <specific-file>` to prevent accidental commits of debug outputs or scratchpads like `AUDIT_REPORT.md` (which is git-ignored and stays out of a commit on its own — do not add it back).
* **Commit Messages:** Explain **why**, not *what*.
  * **Subject:** a conventional prefix and one plain sentence (`fix: ...`, `feat: ...`, `docs: ...`, `refactor: ...`). This repository uses those prefixes throughout its history; keep them.
  * **Body:** prose explaining the problem, what had to survive the change, and **what was verified rather than assumed** ("measured on the rendered page rather than guessed at", "the importer never sends one - checked, not assumed"). Not a changelog. A commit that fixes a bug says which test failed against the unfixed code.
  * Keep the `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` trailer.
* **Line endings are decided by `.gitattributes`, not by anyone's `core.autocrlf`.** Load-bearing here, not cosmetic, and the file says why for each rule:
  * `*.sh`, `*.ldif`, `*.conf`, `*.template`, `Dockerfile*`, `*.yml`/`*.yaml` are pinned to LF because they are read by Linux inside a container, where a stray carriage return is not whitespace — a shell script fails as `bad interpreter: /bin/sh^M`, and an LDIF imports as empty, which looks exactly like a wrong base DN.
  * `*.env` and the two `.env*.example` files are read by GoFr's parser rather than by a shell, so `LOG_LEVEL=INFO` would become `INFO\r`.
  * `*.js`, `*.css`, `*.html`, `*.json`, `*.md` are the embedded web assets, served byte for byte.
  * `.golangci-lint-version` is read by golangci-lint-action, which wants a bare version and nothing else on the line.
  * If a file turns up with CRLF or mixed endings (`git ls-files --eol`), fix it with `git add --renormalize .` in a commit of its own.

## 2. Development, Audits & Anti-Hallucination

* **Prove Changes:** Write a failing test against unfixed code -> Fix -> Verify pass. Commit must document this.
* **Reproduce First:** Always reproduce bugs (especially timeouts) to capture actual state before diagnosing.
* **Zero Hallucination Audits:** Never guess or assume implementation details. You MUST use CLI tools (`grep`, `cat`, `rg`, `ls`) to read actual files before reviewing. **Burden of Proof:** Every issue you find MUST include the exact file path and line number. Do not critique code you haven't explicitly read into context.
* **Doc comments are load-bearing here.** This codebase states *why* in the comment above the thing, at length — `internal/infrastructure/announce`, `rest.PermissionRevision`, `rest.EventStream`, `service.PrincipalByID`, `noticePermissionChange` and `mutate` in `app.js`. Read the comment before changing the code: it usually already contains the reason the obvious simplification is wrong, and often the incident that produced the rule. When you change such code, the comment is part of the change.
* **Agentic Auditing Workflow:** Always document findings sequentially in an `AUDIT_REPORT.md` scratchpad.
  1. **Automated first:** Run `golangci-lint run --build-tags "browser integration firefox" ./...`, `go test -race ./...`, `go mod tidy` and `govulncheck ./...`. Document hard facts.
  2. **Manual Deep Dive:** Actively read files and search for:
     - *Architecture:* Read at least 3 core services to check domain boundaries.
     - *Errors:* Grep for `return err` / `fmt.Errorf` to enforce `apperror` usage and missing DE translations.
     - *State:* Grep for package-level `var` and unprotected maps (race conditions).
     - *Frontend:* Check JS/HTML for inline styles, `script-src` violations, and UI DOM quirks.
     - *Duplicates:* Search for redundant business logic or duplicated UI controls.
  3. **Stop & Ask:** Once the report is full, summarize findings and ask the user which exact file/issue to fix first using TDD.
* **Iterative Audit Limits:** When asked to loop until the code is "professional", use hard metrics. Stop automatically ONLY when: `golangci-lint` outputs 0 warnings, all tests pass with the race detector, OR after a hard limit of **3 iterations** (to prevent infinite loops).
* **Idiomatic Go:** Strictly follow [Effective Go](https://go.dev/doc/effective_go) principles for all new code and refactorings. Match the formatting the file you are editing already uses rather than a house style imported from elsewhere; `gofmt` and `goimports` are the arbiters, and `task check:format`-style rewriting of untouched code does not belong in a commit about something else.
* **Context & Session Protection:** To prevent session limit crashes:
  1. **No parallel agents:** Do NOT spawn multiple background tasks or try to analyze multiple domains simultaneously. Work STRICTLY sequentially: one step, one file, write to report, move on.
  2. **Limit CLI output:** NEVER run unbounded `grep` or `cat` on large or multiple files. Always use limits (e.g., `grep -m 20`, `head -n 50`, `sed -n 'a,bp'`) to prevent flooding the context window. `app.js` and `index.html` are tens of thousands of lines; never read one whole.
  3. **Frequent state saving:** Flush your findings to `AUDIT_REPORT.md` after EVERY single step in the manual deep dive. Do not hold all findings in memory until the end.
* **Living Document:** `CLAUDE.md` reflects the current reality. If you modify the project's folder layout, introduce a new standard test command, or change a core architectural convention during a task, you MUST automatically update the relevant sections (e.g., Layout, Commands) in this `CLAUDE.md` file — in the same commit. A directive here that has stopped being true is worse than no directive.

## 3. Testing & Commands

* **Task runner:** [Task](https://taskfile.dev/) via `Taskfile.yml` at the repo root; `.taskrc.yaml` holds the run settings. `task --list` shows every task and `task --summary <name>` its rationale — those summaries are the authoritative explanation of why a step exists, so read one before changing that task.
* `task test` (Unit), `task test:integration` (SQLite/PG/MySQL), `task test:browser` (Chrome/chromedp), `task test:firefox` (BiDi), `task test:ldap` (a real directory), `task test:traces`, `task test:all`.
* **Run the task, not the tool underneath.** They wrap the same `go test`, but they are not interchangeable with it. `task test` depends on `tidy` and sets `GOTMPDIR` under the project directory, because Go writes each test binary to `%TEMP%\go-build` and real-time virus scanning on Windows intermittently locks those files — "Access is denied" on a package that compiles perfectly well. A bare `go test ./...` outside a task is fine for a quick single-package check; use `GOTMPDIR` if it starts failing for no reason.
* `golangci-lint run --build-tags "browser integration firefox" ./...` — the tags are not optional. Without them the integration, browser and firefox suites are invisible to the linter, which is roughly fifteen hundred lines that decide whether a release is trustworthy. `.golangci.yml` already sets them for a bare `golangci-lint run`.
* **Race Detector:** `go test -race ./...` is mandatory. **Fallback:** If this fails locally due to missing CGO (e.g., on Windows), automatically fallback to Docker (`docker run --rm -v ${PWD}:/app -w /app golang:latest go test -race ./...`) or WSL (`wsl go test -race ./...`).
* `go mod tidy` & `govulncheck ./...` (Mandatory dependency checks).
* **Scoping:** Run *integration* for handler/service/migration changes. Run *browser* for UI changes.
* **Test style:** plain `testing` — there is no `testify/suite` here and testify is only an indirect dependency. Use the in-memory repositories in `internal/infrastructure/persistence/memory` and the `newFixture` helper in the service package rather than hand-rolling a mock; `test/harness` builds the real binary once per suite and starts an instance per case, which is what `test/integration` (HTTP), `test/browser` (chromedp) and `test/firefox` (BiDi) all drive. A case that measures something visual saves the picture it measured — `test/browser/*.png` is git-ignored.
* **Browser Suite Notes:** Runs parallel and is load-sensitive. Under a full-suite run a case can fail with `page load error net::ERR_ABORTED` or a download that never lands; re-run the case on its own before believing it. Fix missing waits instead of bumping timeouts, and never turn a real failure into a longer timeout.
* `task clean`: Cleans artifacts and orphaned headless browsers.

## 4. Frontend & Pitfalls

* **Vanilla JS UI:** No build step, `go:embed`, strict CSP (`script-src 'self'`). No inline handlers/styles. Setting a style from script goes through the CSSOM (`el.style.minWidth = ...`), never through a `style` attribute.
* **Every user-facing string goes through `t('English literal')` — the English literal *is* the key.** `TRANSLATIONS.en` is deliberately empty, so a key nobody has translated still renders, in English, rather than falling through to a language the reader may not know. Markup carries its English in the element and names its key with `data-i18n`.
  * A missing translation therefore fails **silently**. After touching any string, run `go test ./internal/interface/web/`: `TestEveryTranslationCoversTheMarkup` and `TestEveryTranslationCoversTheCodeLookups` check that every key has German, `TestNoTranslationIsUnused` that none is orphaned, `TestCodeFallbacksAreEnglish` that the fallback is the source language, and `TestEveryServerErrorCodeIsTranslated` that every code the API can return has a sentence.
  * New API error codes need their German entry in the same edit. The client renders them as ``t(`err.${err.code}`, err.message)``.
* **A state gets a banner; news gets a toast.** A toast fades after a few seconds, which is right for "Saved" and wrong for anything that goes on being true after nobody is looking — a newer release, a restart that is coming, maintenance, rights that have changed. Those are banners in `#standing-notices`, they are not dismissable, and they go down when they stop being true. Do not report a state with a toast.
* **State/Refills:** Background loads/saves must not overwrite a partially filled form or read stale server state. `beingEdited(form)` is what tells a loader to leave a half-filled form alone.
* **Duplication:** Search for existing UI controls (e.g., password reveals) before adding new ones. One question gets one control: the role is changed from the dropdown in the user's row and nowhere else, which is why the account form puts that field away while it is editing.
* **One form, two jobs.** Creating and correcting share a form, told apart by a hidden `id` input — the booking form, the role form, the account form and the project form all work this way. A form in edit mode swaps its title and its submit label and shows its `cancel editing` button; saving calls the form's own reset so the next new record is not written over the one just saved.
* **DOM Quirks:**
  * **SVG `hidden`:** SVGs lack this property. Use the attribute + `display: none`.
  * **`form.id`:** Fails if a child input is named `id`. Always use `getAttribute('id')`.
  * **`<select>`:** Has no default empty option; restoring a form defaults to the first entry unless handled. A hidden control that is still `required` cannot be submitted past and cannot be scrolled to, so the form silently does nothing — clear `required` when you hide a field.
  * **Date fields are two boxes.** `enhanceDateFields` keeps a visible text field in the reader's own convention beside the native `input[type=date]`, which stays the named element. Set one from code with `setDateField`; writing `.value` directly changes the box nobody looks at.

## 5. Backend, Architecture & Concurrency

* **Layout:** `cmd/` (main), `internal/domain/` (models/repos), `internal/application/` (services), `internal/infrastructure/` (config/DB/announce/selfupdate), `internal/interface/` (REST/UI/installer/CLI), `internal/pkg/` (helpers), `test/`, `deploy/`, `build/`.
* **Clean Architecture:** Strictly separate handlers, services, and repositories. Business logic MUST NOT leak into HTTP handlers.
* **Dependency Injection:** Inject all external dependencies (DBs, APIs) via constructors/structs to ensure code remains testable.
* **Context Propagation:** Always pass `context.Context` as the very first parameter to all functions handling business logic, DB queries, and external calls. Respect context cancellations.
* **Goroutine Safety:** Never spawn raw goroutines without managing their lifecycle. Prevent goroutine leaks using contexts, channels, or `golang.org/x/sync/errgroup`.
* **Database Resource Management:** Ensure database connection pools are explicitly tuned (`SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`) to prevent connection starvation.
* **Errors:** API uses closed catalogue (`internal/pkg/apperror`), rendered client-side via `t('err.' + code)`. **Mandatory:** New codes require a German translation in tests. Never silently drop errors; always wrap them (`%w`) for context.
  * **No Panics:** Never use `panic()` for business logic, control flow, or validation errors. Return wrapped errors up the stack. `panic` is reserved for a programmer error that cannot be recovered from, and there is exactly one in production code — `internal/interface/web/web.go`, where the embedded assets are missing, which means the binary was built wrongly.
* **Partial updates say three things, not two.** An update DTO of pointers can express "leave it alone" and "set it to this" and nothing else, so a field that has to be *emptiable* needs the third state said out loud — `UpdateProjectCommand.ClearEndDate`, and an end date that arrives present but empty. Never overload nil to mean "clear": the callers that say nothing rely on nil meaning nothing.
* **`go fix`:** Always run as `go fix -embedlit=false ./...` (prevents rewriting promoted fields).
* **GoFr:** Config read in `gofr.New()`. Changing metrics/trace/DB configs requires storing state and triggering a restart. Anything needing the raw `http.ResponseWriter` — a connection held open and written to over minutes, like the event stream — is middleware rather than a GoFr handler, because "return a value, return an error" is the wrong shape for it by construction.

## 6. Releases

* **Auto-Patch:** Every merge to `main` cuts a patch release.
* **Manual Tag:** `task release VERSION=vX.Y.Z` for major/minor.
* **Artifacts:** Builds `linux/amd64`, `linux/arm64` images, and 4 platform binaries. Images are verified on both architectures before publishing.

## 7. Enterprise SDLC & Production Readiness

* **Zero-Downtime Migrations:** Database schema changes MUST be backward compatible. Use multi-phase deployments (e.g., add column -> dual write -> switch read -> drop column). Never drop or rename columns in a single release.
* **Secret Management:** Strictly forbid hardcoded secrets, API keys, or private credentials in code or configs. Use environment variables or secret stores exclusively. `configs/datasource.json` and `deploy/.env` hold real passwords and are git-ignored; the `.example` files beside them are what belongs in the repository.
* **Observability & Logging:** Use structured logging (e.g., standard library `slog` or project logger) with contextual keys (request_id, user_id). Avoid raw `fmt.Println` in production code. The health endpoint is GoFr's `/.well-known/health` — keep it reachable, and keep it out of any middleware that would turn it away.
* **API Backward Compatibility:** Do not introduce breaking changes to existing API request/response contracts without a versioning strategy. Payload fields must be treated as immutable once public. A field a browser already reads is part of that contract even when no external client exists.
* **Security & OWASP:** Validate and sanitize all external inputs at application boundaries. Prevent common vulnerabilities (SQL injection via parameterized queries, XSS in HTML/JS, CSRF).
* **Authorisation is enforced server-side and only *reflected* on screen.** Every request resolves who is calling from the database, so a withdrawn right is refused on the very next call whatever the browser believes. What the interface adds is telling somebody: the revision travels on every response, the open event stream carries a change to a page nobody is touching, and the once-a-minute poll is the fallback under both. Never hide a control instead of enforcing a rule, and never offer one the server would refuse.
* **The installer's token is the sign-in.** Answering the installer proves more than a password does — it is read from the process log, and it decides where the data lives — so the browser that answered it is handed a session rather than being sent to type `changeme123`. Two conditions carry that, and both must survive any change to it: an installation that never served an installer has no token and the claim then has no working path at all (an empty configured token must be refused *before* any comparison, or an absent header matches it), and the claim stops working the moment the built-in administrator's password is changed — the same moment the documented password stops working.
* **Design rationale lives in the code and in the commit, not in an `adr/` directory** — there isn't one, and adding a parallel format would split the record. The convention is a package or declaration doc comment stating why the non-obvious choice was made, plus a commit body recording the alternatives and what was verified. Follow that for structural changes.

## 8. Agent Triggers (Prompt Cheatsheet)

* **The Full-Spectrum Audit & Fix Loop (All-in-One Master):**
  `"Execute a full-spectrum iterative Code Audit and Refactoring cycle based strictly on CLAUDE.md. Create a feature branch first. Actively analyze, refactor, and fix all of the following sequentially with TDD and atomic commits: (1) Duplicated logic and duplicated UI controls, (2) Dead/legacy code and orphaned translations, (3) Misplaced business logic (move it out of handlers and out of app.js into services), (4) Missing German translations and untranslated error codes, and (5) Performance bottlenecks, redundant allocations, and goroutine lifetimes. Loop until hitting 'Iterative Audit Limits' (0 golangci-lint warnings, green go test -race, max 3 iterations)."`

* **The Deep Audit (Analysis Only):**
  `"Run a comprehensive Code Audit strictly following the Agentic Auditing Workflow in CLAUDE.md. Create a feature branch first, use the AUDIT_REPORT.md scratchpad, run the automated checks (golangci-lint with the build tags, go test -race, go mod tidy, govulncheck), then do the manual deep dive sequentially. Specifically look for duplicated logic, orphaned or missing translations, and business logic that has leaked into handlers or into app.js. Stop for TDD planning when the report is full."`

* **The Autonomous Fix Loop (Standard Refactoring):**
  `"Execute an iterative Code Audit and Refactoring cycle based on CLAUDE.md. Create a feature branch, fix issues sequentially with atomic commits in this repository's commit style. Focus on consolidating duplicated logic and duplicated UI controls, removing dead code, and fixing missing or unreachable translations. Loop until we hit the 'Iterative Audit Limits' (0 lint warnings, green race detector, max 3 iterations)."`

* **The Performance & Architecture Loop (Deep Clean):**
  `"Run a strict Code Audit and Refactoring cycle focusing on Architecture and Performance. Verify business logic sits in the application services rather than in REST handlers or in app.js. Actively look for performance bottlenecks, unnecessary allocations, unmanaged goroutines and unprotected shared state. Fix sequentially with atomic commits and loop until 'Iterative Audit Limits' are met (max 3 iterations)."`

* **The UI Change:**
  `"Change <X> in the web interface following CLAUDE.md. Write the browser case first and watch it fail against the unfixed screen. Every new string needs its German entry, and go test ./internal/interface/web/ has to stay green. Report a state with a banner and news with a toast. Commit atomically."`
