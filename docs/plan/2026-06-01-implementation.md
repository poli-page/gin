# `poli-page/gin` v0.1.0 Implementation Plan

> Step-by-step plan to ship v0.1.0. Each task is a single PR, reviewable in <30 min, RED→GREEN→refactor. The implementation spec at `docs/spec/gin-integration-specification.md` is the design contract; this is the execution order.

**Prerequisite**: read `docs/spec/gin-integration-specification.md` (entire), `CLAUDE.md`, and `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" before starting Task 1.

**Working directory throughout**: `/Users/mickael/Projects/gin/`

**Tech stack**: Go 1.25, Gin `v1.9` / `v1.10` (matrix), standard `testing` + `stretchr/testify` for assertions, `spf13/cobra` for the CLI, `golangci-lint` for linting, GitHub Actions for CI. Go modules only.

**Total scope**: 14 tasks, ~13 PRs (the pre-flight folds into Task 1's commit). Estimated effort 3–5 working days for a single contributor familiar with the SDK.

---

## Pre-flight: clean the inherited scaffold

**Goal**: start from a known clean slate. The `poli-page/gin` repo is currently an empty git repo (no commits) with only a remote pointing at `git@github.com:poli-page/gin.git`.

- [ ] **Step 0.1: Verify what's actually in the repo**

  ```bash
  cd /Users/mickael/Projects/gin
  git status
  git log --oneline -5
  ls -la
  ```

  Expected: no commits, no source files; `LICENSE`, `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `CLAUDE.md`, and `docs/` are present from the documentation-bootstrap session that created this plan.

- [ ] **Step 0.2: Confirm the SDK is reachable**

  ```bash
  ls /Users/mickael/Projects/sdk-go/polipage.go
  ls /Users/mickael/Projects/sdk-go/go.mod
  grep '^module' /Users/mickael/Projects/sdk-go/go.mod
  ```

  Expected: `polipage.go` exists, module path is `github.com/poli-page/sdk-go`. If absent, the SDK has been moved — stop and ask Mickael.

- [ ] **Step 0.3: Re-read the SDK's public surface so you don't invent API shape**

  Mandatory: skim
  - `/Users/mickael/Projects/sdk-go/polipage.go` — `NewClient(opts ...option.RequestOption) *Client`, with `.Render` and `.Documents` namespaces.
  - `/Users/mickael/Projects/sdk-go/render.go` — `Render.Preview`, `Render.Document`, `Render.PDF`, `Render.PDFStream`. Every signature.
  - `/Users/mickael/Projects/sdk-go/documents.go` — `Documents.Get`, `Documents.Preview`, `Documents.Thumbnails`, `Documents.Delete`.
  - `/Users/mickael/Projects/sdk-go/option/` — every `option.With*` constructor.
  - `/Users/mickael/Projects/sdk-go/error.go` — `*polipage.Error` shape and predicates.

  If a code snippet below contradicts the SDK source, **the SDK wins**. Adapt the snippet, do not invent fields.

---

## Task 1: Bootstrap `go.mod`, tooling configs, and CI workflow

**Files:**
- Create: `go.mod`
- Create: `.golangci.yml`
- Create: `.gitignore`
- Create: `.github/workflows/ci.yml`
- Create: `doc.go` (package-level godoc)
- Create: `smoke_test.go` (proves the test pipeline runs)

**Goal**: every `go *` command works on a freshly cloned repo. `go test ./...` passes (one no-op test). CI matrix is green.

- [ ] **Step 1.1: `go.mod`**

  ```go.mod
  module github.com/poli-page/gin

  go 1.25

  require (
      github.com/gin-gonic/gin v1.10.0
      github.com/poli-page/sdk-go v0.0.0-00010101000000-000000000000
      github.com/spf13/cobra v1.9.0
      github.com/stretchr/testify v1.10.0
  )

  // Why: SDK not yet published. Remove on SDK v1.0.0 publish.
  replace github.com/poli-page/sdk-go => ../sdk-go
  ```

  Run `go mod tidy` after the first source file lands. The `replace` line is documented in spec §12; the comment is required.

- [ ] **Step 1.2: `.golangci.yml`**

  ```yaml
  # .golangci.yml
  run:
    timeout: 5m
    go: "1.25"

  linters:
    disable-all: true
    enable:
      - errcheck
      - gocritic
      - gofmt
      - goimports
      - gosec
      - govet
      - revive
      - staticcheck
      - unused

  linters-settings:
    revive:
      severity: warning
      rules:
        - name: exported              # exported identifiers must have a comment
        - name: var-naming
        - name: error-return
        - name: error-naming
        - name: if-return
        - name: indent-error-flow
        - name: superfluous-else
        - name: unused-parameter
          disabled: true               # idiomatic ctx handler params trigger this
    gocritic:
      enabled-tags:
        - diagnostic
        - performance
        - style
      disabled-checks:
        - whyNoLint                    # paired with the //nolint Why: convention

  issues:
    exclude-rules:
      - path: _test\.go
        linters:
          - gosec                      # test fixtures may use weak crypto/hardcoded keys
      - path: cmd/polipage-render/
        linters:
          - gocritic                   # CLI shape often differs from library shape
  ```

  Mirrors the linter set named in `CLAUDE.md` §6. The two `disabled` rules each have a rationale comment — keep them or document why you removed.

- [ ] **Step 1.3: `.gitignore`**

  ```
  # Build output
  *.test
  *.out
  coverage.*
  /cmd/polipage-render/polipage-render
  /example-app/example-app

  # Go workspace files (kept out of integration repo per spec §12)
  go.work
  go.work.sum

  # IDE
  .idea/
  .vscode/
  *.swp

  # OS
  .DS_Store
  ```

- [ ] **Step 1.4: `.github/workflows/ci.yml`**

  Mirrors the symfony-bundle's auto-skip pattern (each step exits 0 with a "skipping" log when its config file doesn't exist yet), so the workflow is green from the first commit and lights up step-by-step as Tasks 2–11 land.

  ```yaml
  # .github/workflows/ci.yml
  name: CI
  on:
    push:
    pull_request:
      branches: [main]

  jobs:
    test:
      runs-on: ubuntu-latest
      strategy:
        fail-fast: false
        matrix:
          go: ['1.25']
          gin: ['v1.9.1', 'v1.10.0']
          include:
            - go: 'tip'
              gin: 'v1.10.0'
              experimental: true
      continue-on-error: ${{ matrix.experimental == true }}
      steps:
        - uses: actions/checkout@v4
          with:
            path: gin
        - uses: actions/checkout@v4
          with:
            repository: poli-page/sdk-go
            path: sdk-go
            ref: main
        - uses: actions/setup-go@v5
          with:
            go-version: ${{ matrix.go }}
            cache: false
        - name: Pin Gin version
          working-directory: gin
          run: |
            if [ -f go.mod ]; then
              go get github.com/gin-gonic/gin@${{ matrix.gin }}
              go mod tidy
            else
              echo "Skipping pin: no go.mod yet"
            fi
        - name: Format check
          working-directory: gin
          run: |
            if [ -f go.mod ]; then
              test -z "$(gofmt -l .)" || (gofmt -d .; exit 1)
            else
              echo "Skipping gofmt: no go.mod yet"
            fi
        - name: Vet
          working-directory: gin
          run: |
            if [ -f go.mod ]; then
              go vet ./...
            else
              echo "Skipping vet: no go.mod yet"
            fi
        - name: Lint
          working-directory: gin
          run: |
            if [ -f .golangci.yml ]; then
              curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
              $(go env GOPATH)/bin/golangci-lint run
            else
              echo "Skipping lint: no .golangci.yml yet"
            fi
        - name: Unit tests (race)
          working-directory: gin
          run: |
            if [ -f go.mod ]; then
              go test -race -count=1 ./...
            else
              echo "Skipping tests: no go.mod yet"
            fi
        - name: Build
          working-directory: gin
          run: |
            if [ -f go.mod ]; then
              go build ./...
            else
              echo "Skipping build: no go.mod yet"
            fi

    integration:
      runs-on: ubuntu-latest
      if: github.event_name == 'push' && github.ref == 'refs/heads/main'
      steps:
        - uses: actions/checkout@v4
          with:
            path: gin
        - uses: actions/checkout@v4
          with:
            repository: poli-page/sdk-go
            path: sdk-go
            ref: main
        - uses: actions/setup-go@v5
          with:
            go-version: '1.25'
        - name: Integration test (gated on POLI_PAGE_API_KEY)
          working-directory: gin
          env:
            POLI_PAGE_API_KEY: ${{ secrets.POLI_PAGE_API_KEY }}
            POLI_PAGE_BASE_URL: https://api-develop.poli.page
          run: |
            if [ -z "$POLI_PAGE_API_KEY" ]; then
              echo "POLI_PAGE_API_KEY not set; skipping integration test"
              exit 0
            fi
            if [ ! -d tests/integration ] || [ -z "$(ls -A tests/integration 2>/dev/null)" ]; then
              echo "No tests/integration yet; skipping"
              exit 0
            fi
            go test -tags=integration -count=1 ./tests/integration/...
        - name: CLI smoke test
          working-directory: gin
          env:
            POLI_PAGE_API_KEY: ${{ secrets.POLI_PAGE_API_KEY }}
            POLI_PAGE_BASE_URL: https://api-develop.poli.page
          run: |
            if [ -z "$POLI_PAGE_API_KEY" ]; then
              echo "POLI_PAGE_API_KEY not set; skipping CLI smoke"
              exit 0
            fi
            if [ ! -d cmd/polipage-render ]; then
              echo "No cmd/polipage-render yet; skipping"
              exit 0
            fi
            go run ./cmd/polipage-render \
                --project=getting-started \
                --template=welcome \
                --template-version=1.0.0 \
                --data='{"name":"ci"}' \
                -o /tmp/welcome.pdf
            test -s /tmp/welcome.pdf
  ```

  Two jobs: `test` runs on every push/PR with the full matrix, `integration` runs only on `main` pushes with secrets (no spurious red on PRs from forks).

- [ ] **Step 1.5: `doc.go`**

  ```go
  // Package polipagegin integrates the Poli Page Go SDK with the Gin HTTP framework.
  //
  // Install the middleware once on your router, then pull the per-request
  // *polipage.Client off the gin context with ClientFrom. See README for the
  // canonical example.
  package polipagegin
  ```

- [ ] **Step 1.6: `smoke_test.go` + `main_test.go`**

  `smoke_test.go`: one test that asserts `1 + 1 == 2`, plus the package compiles. Lets the test step in CI light up before any real code lands.

  `main_test.go`: package-level `TestMain` that pins `gin.SetMode(gin.TestMode)` for every subsequent test in the suite (CLAUDE-side rationale in `docs/testing.md`):

  ```go
  package polipagegin_test

  import (
      "os"
      "testing"

      "github.com/gin-gonic/gin"
  )

  func TestMain(m *testing.M) {
      gin.SetMode(gin.TestMode)
      os.Exit(m.Run())
  }
  ```

  Every later test file in this task list inherits the mode pin — no per-test `gin.SetMode` calls.

- [ ] **Step 1.7: Verify locally**

  ```bash
  go mod tidy
  gofmt -l .          # empty output
  go vet ./...
  golangci-lint run
  go test -race ./...
  ```

**Commit:** `chore: bootstrap module, tooling configs, and CI workflow`

---

## Task 2: Middleware + `ClientFrom`

**Files:**
- Create: `middleware.go`
- Create: `middleware_test.go`

**Goal**: install the middleware, pull the client back out, panic loudly when misused.

- [ ] **Step 2.1: RED — write the failing tests**

  Test cases (all in `middleware_test.go`):
  - `Middleware(client)` installed on a router; a handler calls `ClientFrom(c)` and gets the same pointer back.
  - `Middleware(nil)` panics at install time with a clear message.
  - `ClientFrom(c)` panics when no middleware was installed, message contains "Middleware(client) not installed".
  - `ClientFrom(c)` panics when `ContextKey` holds a non-`*polipage.Client` value (tampering case).
  - `Middleware` installed on a router group only — handlers outside the group panic, handlers inside do not.
  - **Recovery-prerequisite assertion**: a route that calls `ClientFrom(c)` without the middleware, with `r.Use(gin.Recovery())` installed first, returns HTTP 500 (not a process crash). The test exists so the prerequisite stays load-bearing — removing Recovery from the test must fail loudly.

- [ ] **Step 2.2: GREEN — implement**

  Per spec §7.1. Constants:

  ```go
  const ContextKey = "polipagegin.client"
  ```

- [ ] **Step 2.3: refactor**

  Single-responsibility check: `Middleware` does only the `c.Set`; the nil check fires at construction, not per-request. `ClientFrom` does one map lookup + one type assert + one panic path.

**Commit:** `feat: middleware and ClientFrom accessor`

---

## Task 3: RFC 5987 header builder

**Files:**
- Create: `headers.go` (internal, unexported)
- Create: `headers_test.go`

**Goal**: the header machinery the response helpers will use.

- [ ] **Step 3.1: RED — test matrix**

  Test inputs (all in `headers_test.go`):
  - ASCII filename `invoice-42.pdf` — `filename="invoice-42.pdf"; filename*=UTF-8''invoice-42.pdf`.
  - Non-ASCII filename `facture-élève.pdf` — `filename="facture-_l_ve.pdf"; filename*=UTF-8''facture-%C3%A9l%C3%A8ve.pdf`.
  - Filename with double quote `bad"name.pdf` — quote is backslash-escaped in the ASCII slot; percent-encoded in the UTF-8 slot.
  - Filename with backslash `a\b.pdf` — backslash escaped.
  - Empty filename — default `document.pdf`.
  - `Inline: true` — `inline; …` instead of `attachment; …`.

- [ ] **Step 3.2: GREEN — implement**

  Unexported helper: `func contentDisposition(filename string, inline bool) string`. Uses `url.PathEscape` for the UTF-8 slot, plus the small RFC 5987 allowlist.

- [ ] **Step 3.3: refactor**

  Extract the ASCII sanitizer (`asciiFallback(string) string`) so the test for "filename with quote" is independent of the encoder.

**Commit:** `feat: RFC 5987 Content-Disposition builder`

---

## Task 4: Response helpers — `PDF`, `Preview`, `DocumentRedirect`

**Files:**
- Create: `responses.go`
- Create: `responses_test.go`

**Goal**: the buffered response helpers. `PDFStream` lands in Task 5.

- [ ] **Step 4.1: RED — tests per spec §8 header table**

  For each helper: status code, every header (`Content-Type`, `Cache-Control`, `X-Content-Type-Options`, `Content-Disposition` where applicable), body bytes.

  `DocumentRedirect` test: takes a `*polipage.DocumentDescriptor`, response is 302 with `Location` equal to `descriptor.PresignedPDFURL`, body empty, `Cache-Control: no-store, private` set.

- [ ] **Step 4.2: GREEN — implement**

  Per spec §7.3 and §8.

  ```go
  type PDFOptions struct {
      Filename string
      Inline   bool
  }

  func PDF(c *gin.Context, body []byte, opts PDFOptions) { /* ... */ }
  func Preview(c *gin.Context, html string)              { /* ... */ }
  func DocumentRedirect(c *gin.Context, d *polipage.DocumentDescriptor) { /* ... */ }
  ```

- [ ] **Step 4.3: refactor**

  Extract a private `writePDFHeaders(c, opts)` so `PDF` and (later) `PDFStream` share the header setup.

**Commit:** `feat: PDF, Preview, DocumentRedirect response helpers`

---

## Task 5: `PDFStream` with periodic flush

**Files:**
- Update: `responses.go`
- Update: `responses_test.go`

**Goal**: streaming variant that does not own the reader, sets `X-Accel-Buffering: no`, and flushes every 32 KB so bytes leave the process as they arrive.

- [ ] **Step 5.1: RED — tests**

  - `PDFStream` writes status 200, all the same PDF headers as `PDF`, plus `X-Accel-Buffering: no` and no `Content-Length` (chunked).
  - Body equals `io.ReadAll(reader)`.
  - **Flush cadence**: install a `*flushingRecorder` (custom `http.ResponseWriter` that implements `http.Flusher` and records every `Flush()` call). Feed 100 KB through `PDFStream` and assert `Flush()` was called at least 3 times (100 KB / 32 KB = 3+).
  - **Flusher-absent fallback**: feed the same input through an `httptest.NewRecorder()` (which does NOT implement `http.Flusher`). Assert the body is still complete and no panic occurred.
  - When the reader returns an error mid-stream, `c.Errors.Last().Err` is that error (so logging middleware sees it), but the response body still contains the bytes that were successfully read before the failure.
  - Calling `PDFStream` with `nil` reader panics with a clear message (fail-loud stance).

- [ ] **Step 5.2: GREEN — implement**

  Per spec §8.3 — manual 32 KB read/write/flush loop (NOT `c.DataFromReader`, which doesn't flush). The chunk size constant lives in `responses.go` as `const pdfStreamChunk = 32 * 1024` so it's discoverable.

  ```go
  const pdfStreamChunk = 32 * 1024

  func PDFStream(c *gin.Context, body io.Reader, opts PDFOptions) {
      if body == nil {
          panic("polipagegin: PDFStream called with nil reader")
      }
      writePDFHeaders(c, opts)
      c.Header("X-Accel-Buffering", "no")
      c.Status(http.StatusOK)

      flusher, _ := c.Writer.(http.Flusher)
      buf := make([]byte, pdfStreamChunk)
      for {
          n, rerr := body.Read(buf)
          if n > 0 {
              if _, werr := c.Writer.Write(buf[:n]); werr != nil {
                  _ = c.Error(fmt.Errorf("polipagegin: stream write failed: %w", werr))
                  return
              }
              if flusher != nil {
                  flusher.Flush()
              }
          }
          if errors.Is(rerr, io.EOF) {
              return
          }
          if rerr != nil {
              _ = c.Error(fmt.Errorf("polipagegin: stream read failed: %w", rerr))
              return
          }
      }
  }
  ```

  Note: helper takes `io.Reader` per spec §8.3. The handler `defer body.Close()`s the `io.ReadCloser` it got from `Render.PDFStream`.

- [ ] **Step 5.3: refactor**

  Confirm `writePDFHeaders` is now shared between `PDF` and `PDFStream` (no duplicated header strings) and that `X-Accel-Buffering` lives only in the streaming path, not in `writePDFHeaders`.

**Commit:** `feat: PDFStream with periodic flush and proxy-buffering opt-out`

---

## Task 6: Error middleware + `WriteError`

**Files:**
- Create: `errors.go`
- Create: `errors_test.go`

**Goal**: opt-in mapping of `*polipage.Error` to JSON responses.

- [ ] **Step 6.1: RED — tests per spec §10**

  - Handler queues `&polipage.Error{Code:"VALIDATION_ERROR", StatusCode: 400, Message:"…", RequestID:"req_1"}` via `c.Error(...)` and returns. `ErrorMiddleware` responds 400 with JSON `{code:"VALIDATION_ERROR", message:"…", requestId:"req_1"}`.
  - 401 + `IsAuthError()` → 401 with code passed through.
  - 429 + `IsRateLimitError()` → 429.
  - 503 + `IsNetworkError()` → **502** with `code:"NETWORK_ERROR"` (the one transformation the middleware does).
  - Generic `errors.New("boom")` queued — `ErrorMiddleware` does NOT respond; another handler in the chain (or Gin's default) deals with it.
  - `WriteError(c, err)` produces the same response as the middleware for the same input.
  - `c.Errors` contains two errors, last is the `*polipage.Error` — the middleware uses the last one.
  - **Pitfall lock-in**: an explicit test that `errors.As(c.Errors.Last(), &pe)` returns `false` while `errors.As(c.Errors.Last().Err, &pe)` returns `true`. The test exists to keep the `.Err` access load-bearing — a future "cleanup" that drops `.Err` must fail this test (CLAUDE §10.4).

- [ ] **Step 6.2: GREEN — implement**

  Per spec §10.2 and §10.3. `mapError(*polipage.Error) (int, string)` extracted so both `ErrorMiddleware` and `WriteError` share it. The `errors.As(ge.Err, &pe)` call has a `// Why: gin.Error does not Unwrap to .Err — see CLAUDE §10.4` comment alongside it.

- [ ] **Step 6.3: refactor**

  Confirm narrowness: no `default` case widens the catch. Add a godoc note on `ErrorMiddleware` explaining the "narrow on purpose" decision.

**Commit:** `feat: ErrorMiddleware and WriteError for *polipage.Error`

---

## Task 7: `Config` + `FromEnv`

**Files:**
- Create: `config.go`
- Create: `config_test.go`

**Goal**: env-driven configuration that produces `[]option.RequestOption`.

- [ ] **Step 7.1: RED — tests**

  - All variables set with valid values → `Config` populated, `Options()` returns the corresponding `option.With*` slice (assert by constructing a `polipage.NewClient(cfg.Options()...)` and reading back via a stubbed test helper, or by recording the options slice if `option` exposes a comparable shape).
  - `POLI_PAGE_API_KEY` unset → error mentioning the variable name.
  - `POLI_PAGE_API_KEY=foo` (no prefix) → error mentioning the variable AND the expected prefix shape.
  - `POLI_PAGE_TIMEOUT=not-a-duration` → error mentioning the variable AND `time.ParseDuration`.
  - `POLI_PAGE_MAX_RETRIES=99` → error mentioning the variable AND the `[0,10]` range.
  - `POLI_PAGE_BASE_URL=://broken` → error mentioning the variable AND URL parse.
  - All errors wrap `polipagegin.ErrInvalidConfig` (checkable with `errors.Is`).

- [ ] **Step 7.2: GREEN — implement**

  Per spec §7.2. Use `os.LookupEnv` (not `os.Getenv`) so "set but empty" and "unset" can be distinguished where it matters.

- [ ] **Step 7.3: refactor**

  Each variable parsed in its own private function (`parseAPIKey`, `parseTimeout`, ...) — keeps `FromEnv` a flat list of validations and the test signal precise.

**Commit:** `feat: Config and FromEnv loader`

---

## Task 8: `cmd/polipage-render` — root command

**Files:**
- Create: `cmd/polipage-render/main.go`
- Create: `cmd/polipage-render/root.go`
- Create: `cmd/polipage-render/render.go`           (the pure function tests call directly)
- Create: `cmd/polipage-render/render_test.go`

**Goal**: the CLI binary per spec §9 and `docs/cli.md`.

**Test stance** — the CLI tests verify the *integration code we own*, not the SDK. Specifically: flag parsing, JSON `--data` unmarshal, exit-code mapping from `*polipage.Error` → integer, output routing (`-o` file vs `os.Stdout`). The end-to-end "does the SDK actually render?" check is the integration test in Task 9; the CLI smoke step in CI runs the binary against the develop API. We do NOT spin up an `httptest.Server` faking the API (CLAUDE §4).

- [ ] **Step 8.1: Extract a testable `render` function first**

  Before writing the Cobra command, define the pure function that does the work. Tests call it directly; the Cobra `RunE` is then a 5-line shim.

  ```go
  // cmd/polipage-render/render.go
  package main

  type renderRequest struct {
      Cfg   polipagegin.Config
      Input polipage.RenderInput     // ProjectModeInput or InlineModeInput
      Out   io.Writer
  }

  // render returns the process exit code per spec §9.3. err is non-nil only
  // for diagnostic logging; the exit code already encodes the outcome.
  func render(ctx context.Context, req renderRequest) (exitCode int, err error)
  ```

  This shape is what Step 8.3 produces; doing it first lets Step 8.2's tests target it directly.

- [ ] **Step 8.2: RED — exit-code + flag-parsing tests**

  Two test files, two distinct test surfaces:

  `render_test.go` — calls `render(...)` directly with constructed inputs:
  - Given a `req` whose `client.Render.PDF` (built from `req.Cfg`) returns `&polipage.Error{StatusCode: 401, Code: "INVALID_API_KEY"}` → exit `1`. **Implementation note**: building a `*polipage.Client` that returns a canned error means using `option.WithHTTPClient` to inject a `*http.Client` with a custom `RoundTripper` that returns a synthetic 401 response. This is allowed (we're testing our exit-code mapping, not the SDK's network code) — but if it starts feeling like "I'm rebuilding the SDK's response parser", stop and switch the test to call the exit-code mapping function in isolation.
  - 400 + `IsValidationError()` → exit `2`.
  - Synthesized network error (RoundTripper returns `&net.OpError{...}`) → exit `3`.
  - 429 → exit `4`.

  `flags_test.go` — calls Cobra's `SetArgs` + `Execute` and asserts that the resulting `renderRequest` (or the early exit) matches:
  - `--data='not json'` → Cobra error, exit `64`.
  - Neither `--project` nor `--inline-template` → exit `64`.
  - `--project=p --template=t --template-version=1.0.0 --data='{}'` → `req.Input` is a `polipage.ProjectModeInput` with those fields.
  - `--inline-template='<h1></h1>' --data='{}'` → `req.Input` is a `polipage.InlineModeInput`.
  - `-o /tmp/x.pdf` → `req.Out` writes to that path.

  **Easier option for the exit-code matrix**: skip the RoundTripper plumbing entirely and refactor the exit-code mapping into a pure `exitCodeFor(err error) int` function. Then `render_test.go` becomes a table-driven test over `*polipage.Error` values. Stronger separation of concerns; fewer moving parts in the test.

- [ ] **Step 8.3: GREEN — implement**

  Cobra root command in `root.go`, flags per spec §9.2 and `docs/cli.md`. `--template-version` (NOT `--version`). Output goes to `-o <path>` when provided, else `os.Stdout`. Diagnostics go to `os.Stderr`. `main.go` is `func main() { os.Exit(rootCmd.Execute()) }`-shaped.

- [ ] **Step 8.4: refactor**

  Confirm the boundary: `render.go` is pure (no Cobra, no `os.Exit`, no `os.Stderr`). `root.go` is the Cobra adapter. `main.go` is the entrypoint. Each layer has one reason to change.

**Commit:** `feat: polipage-render CLI binary`

---

## Task 9: Integration test against `api-develop.poli.page`

**Files:**
- Create: `tests/integration/render_against_develop_test.go`

**Goal**: one smoke test per spec §13.3.

- [ ] **Step 9.1: Implement**

  ```go
  //go:build integration

  package integration_test

  func TestRenderAgainstDevelopAPI(t *testing.T) {
      key := os.Getenv("POLI_PAGE_API_KEY")
      if key == "" {
          t.Skip("POLI_PAGE_API_KEY not set")
      }
      // ... per the snippet in docs/testing.md
  }
  ```

- [ ] **Step 9.2: Run locally**

  ```bash
  POLI_PAGE_API_KEY=pp_test_… \
  POLI_PAGE_BASE_URL=https://api-develop.poli.page \
  go test -tags=integration ./tests/integration/...
  ```

- [ ] **Step 9.3: Wire into CI**

  CI step from spec §15 — already present in `.github/workflows/ci.yml` from Task 1. Verify it skips cleanly on PRs from forks.

**Commit:** `test: integration smoke test against develop API`

---

## Task 10: Example app — server + routes

**Files:**
- Create: `example-app/go.mod`
- Create: `example-app/main.go`
- Create: `example-app/handlers.go`
- Create: `example-app/env.go` (the hand-rolled `.env` parser)

**Goal**: the runnable example app per spec §14. UI deferred to Task 11 — this task gets the routes wired and the dev experience pleasant.

- [ ] **Step 10.1: Bootstrap module**

  ```bash
  cd example-app
  go mod init github.com/poli-page/gin/example-app
  go mod edit -replace=github.com/poli-page/gin=..
  go mod edit -replace=github.com/poli-page/sdk-go=../../sdk-go
  go mod tidy
  ```

- [ ] **Step 10.2: `main.go` (production-shape bootstrap)**

  Per spec §14.4. The example app's `main.go` is the reference users will copy verbatim, so it has to model the right shape end-to-end:

  - `gin.SetMode(gin.ReleaseMode)` first (drops debug-log spam, sets the trusted-proxies warning to silent).
  - Load `/Users/mickael/Projects/.env` via the hand-rolled parser (`env.go`, see `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §2).
  - Build the client via `polipagegin.FromEnv()` + `polipage.NewClient(cfg.Options()...)`.
  - `r := gin.New()` then `r.Use(gin.Logger(), gin.Recovery(), polipagegin.Middleware(client), polipagegin.ErrorMiddleware())`. Recovery before Middleware (so ClientFrom panics surface as 500s); ErrorMiddleware last (it consumes `c.Errors` after handlers return).
  - `_ = r.SetTrustedProxies(nil)` — explicit no-trust default. If the demo lives behind a local reverse proxy at any point, the line changes; leaving it implicit hides the decision.
  - Build a `&http.Server{Addr: ":8080", Handler: r}` so shutdown can be controlled (the convenience `r.Run` doesn't expose it).
  - Wire `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` + `srv.Shutdown(shutdownCtx)` with a 30s timeout — see the snippet in spec §14.4. Mid-stream PDFs need this to drain cleanly.
  - Register routes from `handlers.go`.

- [ ] **Step 10.3: `handlers.go`**

  One handler per row of the table in spec §14.1. Every handler uses `polipagegin.ClientFrom(c)` + `c.Request.Context()`.

- [ ] **Step 10.4: Smoke**

  ```bash
  cd example-app
  go run .
  # In another terminal:
  curl -o /tmp/test.pdf http://127.0.0.1:8080/api/render/pdf
  test -s /tmp/test.pdf
  ```

**Commit:** `feat(example-app): server bootstrap and route wiring`

---

## Task 11: Example app — interactive UI

**Files:**
- Create: `example-app/templates/demo.html`
- Create: `example-app/static/demo.js`
- Create: `example-app/static/demo.css`
- Update: `example-app/main.go` (mount `embed.FS`, serve `/`, `/static/*`)

**Goal**: the single-page dashboard per spec §14.2 and `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §1.

**Time estimate**: this task is the largest of the v0.1.0 push — budget ~4–6 hours for a contributor who hasn't built the symfony-bundle demo before. The HTML/CSS is a port (no design work), but wiring the JS state machine + Blob URL plumbing + the visual polish to match the reference takes real time. If you're tight, ship Task 10 (server + routes) as a separate "alpha" PR and circle back to Task 11 — the rest of v0.1.0 doesn't depend on the demo UI.

- [ ] **Step 11.1: Copy the aesthetic from the Symfony reference**

  Files to read first:
  - `/Users/mickael/Projects/symfony-bundle/example-app/templates/demo.html` — the canonical reference. ~440 lines, inline `<style>` block.
  - `/Users/mickael/Projects/nestjs/example-app/src/` — most recent port; check if there's a `demo.html` there for the Node-flavored shape.

  Port the structure verbatim: same Google Fonts imports (Fraunces / IBM Plex Sans / JetBrains Mono), same warm paper background (`#F5F2ED`), same process-cyan accent (`#00A3E0`). The visual constants are not negotiable — these specimens go in the README screenshot and need to feel like the same product across stacks.

- [ ] **Step 11.2: Button → route map**

  The dashboard has exactly the buttons below, in this order, each one calling exactly the route from Task 10. Implement them in this order so each row works as soon as the corresponding handler exists:

  | # | Button label | Route called | Output rendering | State change |
  |---|---|---|---|---|
  | 1 | Render PDF | `GET /api/render/pdf` | `<iframe src="blob:…">` (PDF blob) | none |
  | 2 | Stream PDF | `GET /api/render/pdf-stream` | `<iframe src="blob:…">` | none |
  | 3 | Render preview (HTML) | `GET /api/render/preview` | `<iframe srcdoc="…">` | none |
  | 4 | Store document | `POST /api/documents` | pretty-printed JSON | capture `documentId` into JS state |
  | 5 | Get document (redirect) | `GET /api/documents/{id}` | `<iframe src="…">` of the followed redirect | requires `documentId` |
  | 6 | Document preview (HTML) | `GET /api/documents/{id}/preview` | `<iframe srcdoc="…">` | requires `documentId` |
  | 7 | Document thumbnails | `GET /api/documents/{id}/thumbnails` | `<img>` grid, decode base64 client-side | requires `documentId` |
  | 8 | Delete document | `DELETE /api/documents/{id}` | "204 deleted" status pill | requires `documentId`, then clears it |
  | 9 | Trigger validation error | `GET /api/render/error` | pretty-printed error JSON, status pill in red | none |

  Buttons 5–8 are `disabled` in HTML until `documentId` is set in JS state. Button 8 clears `documentId` (and re-disables 5–7) on success.

- [ ] **Step 11.3: Wire `embed.FS`**

  ```go
  //go:embed templates static
  var assets embed.FS
  ```

  Serve `/` via `c.FileFromFS("templates/demo.html", http.FS(assets))`, `/static/*filepath` via `c.FileFromFS(...)`.

  **Gotcha**: `//go:embed` fails at compile time if a named directory is empty (`pattern templates: no matching files found`). Create `templates/demo.html` and at least one file in `static/` (e.g. `static/demo.css` and `static/demo.js`) before running `go build`. Don't try to ship an empty `static/` and add files later — the build is broken until both directories contain at least one file.

- [ ] **Step 11.4: Client JS — state machine**

  Vanilla JS, no framework, no build step. Single `<script>` tag in `demo.html` or a sibling `static/demo.js`.

  State shape: `{ documentId: null | string }`. Mutations come from button-4 success (set) and button-8 success (clear); a `render()` function reads state and toggles `disabled` on buttons 5–8.

  PDF rendering: `fetch(route).then(r => r.blob()).then(b => iframe.src = URL.createObjectURL(b))`. Revoke the previous Blob URL before assigning a new one (`URL.revokeObjectURL(oldUrl)`) — otherwise the browser leaks the buffer for the page lifetime.

  JSON pretty-print: `pre.textContent = JSON.stringify(await r.json(), null, 2)`; swap a status pill to red on non-2xx.

- [ ] **Step 11.5: Smoke checklist**

  `go run .`, open `http://127.0.0.1:8080`, click each button in order:

  - [ ] Buttons 1, 2 render a PDF inside the iframe (no download prompt).
  - [ ] Button 3 shows the HTML preview rendered inside the iframe sandbox.
  - [ ] Button 4 prints JSON with a `documentId` field; buttons 5–8 transition from disabled to enabled.
  - [ ] Button 5 navigates the iframe to the presigned URL and the PDF renders.
  - [ ] Button 6 shows the stored document's HTML preview.
  - [ ] Button 7 shows a grid of decoded thumbnail PNGs.
  - [ ] Button 8 shows "204 deleted"; buttons 5–7 transition back to disabled.
  - [ ] Button 9 shows a red status pill and the validation-error JSON body.

**Commit:** `feat(example-app): interactive demo dashboard`

---

## Task 12: Documentation review pass

**Files:**
- Update: `README.md` (final code samples line up with shipped API)
- Update: `docs/middleware.md`, `docs/responses.md`, `docs/streaming.md`, `docs/events.md`, `docs/testing.md`, `docs/cli.md`
- Update: `CHANGELOG.md` (move v0.1.0 entries to a dated release header)

**Goal**: every documented identifier exists, every snippet compiles when copy-pasted, every link resolves.

- [ ] **Step 12.1: Identifier sweep**

  `grep -rn polipagegin\. docs/ README.md` — every symbol referenced must exist in `*.go` files. Fix the doc, not the code.

- [ ] **Step 12.2: Snippet compile pass**

  Pick three end-to-end snippets (the Quick start in README, the `PDFStream` example in `streaming.md`, the test example in `testing.md`). Drop them into `example-app/` (or a scratch module), `go build`, fix any compilation errors.

- [ ] **Step 12.3: Link check**

  `grep -rn '](.\+\.md' docs/ README.md` — every relative link resolves to an existing file.

- [ ] **Step 12.4: `CHANGELOG.md`**

  Move the v0.1.0 bullets out of `## [0.1.0] — TBD` into `## [0.1.0] — 2026-MM-DD` with today's date. Update the version links at the bottom.

**Commit:** `docs: align docs with shipped v0.1.0 API`

---

## Task 13: Release prep

**Files:**
- Update: `go.mod` (drop the `replace` if SDK has published)
- Create: a Git tag

**Goal**: cut v0.1.0.

- [ ] **Step 13.1: Decide on SDK pin**

  If `sdk-go` has tagged a `v1.0.0` (or any tagged version) by this point, drop the `replace` line in `go.mod` and pin: `go get github.com/poli-page/sdk-go@v1.0.0`. If it's still unpublished, keep the `replace` for now and ship v0.1.0 with the `replace` documented in `CLAUDE.md` §9 — the integration is then dev-only until the SDK ships.

- [ ] **Step 13.2: Final CI green**

  `git push` and wait for the full matrix. No skips allowed on the required cells. Forks-without-secrets skip is OK.

- [ ] **Step 13.3: Tag**

  ```bash
  git tag -a v0.1.0 -m "Initial release"
  git push origin v0.1.0
  ```

- [ ] **Step 13.4: Verify pkg.go.dev**

  Wait ~10 minutes, then open https://pkg.go.dev/github.com/poli-page/gin@v0.1.0. Confirm the godoc renders, all exported symbols show up, and the example block from `doc.go` renders.

**Commit:** _no commit — this task ends with the `v0.1.0` tag_

---

## Task 14 (optional, post-v0.1.0): announce

- [ ] Tweet/post from the Poli Page handle linking to https://pkg.go.dev/github.com/poli-page/gin and the example app.
- [ ] Open a PR against `INTEGRATIONS_PLAN.md` to add Gin to the verdict table with status "Shipped".
- [ ] If there are any v0.2 candidates surfaced during dogfooding (see spec §16), open issues for them so they're not lost in chat.

---

## Rollout checklist

Run through this before merging the v0.1.0 tag PR:

- [ ] `go test -race ./...` green locally and in CI.
- [ ] `golangci-lint run` returns zero findings (no `//nolint:...` without a `// Why:` comment).
- [ ] `go test -tags=integration ./tests/integration/...` green against `api-develop.poli.page` with a real `pp_test_*` key.
- [ ] `go run ./cmd/polipage-render --project=getting-started --template=welcome --template-version=1.0.0 --data='{"name":"World"}' -o /tmp/x.pdf` exits 0 and produces a non-empty PDF.
- [ ] `cd example-app && go run .` boots, `GET /` renders the dashboard, every button on the dashboard exercises a real round-trip.
- [ ] `README.md` Quick start example compiles and runs verbatim against the published module.
- [ ] `docs/` link-check passes (Task 12.3).
- [ ] `CLAUDE.md` §9 reflects the actual state of the SDK publish (still `replace`, or pinned to a tag).
- [ ] `CHANGELOG.md` v0.1.0 section has today's date.
- [ ] Tag pushed; pkg.go.dev shows the godoc.
