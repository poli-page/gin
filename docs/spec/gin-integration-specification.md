# `poli-page/gin` — implementation specification

> Source of truth for what we build, in what shape, and explicitly what we don't. Mirrors `nestjs/docs/spec/nestjs-implementation.md` and `symfony-bundle/docs/spec/bundle-specification.md` so reviewers can cross-reference. Read `INTEGRATIONS_PLAN.md` first; this is the per-repo expansion of the first Go integration slot.

**Roadmap slot**: first Go integration (after the four P0–P2 frameworks ship).
**Target**: ship v0.1.0 as a working Go module, not a recipe.
**Stance**: thin idiomatic Gin wrapper over `github.com/poli-page/sdk-go`. Anything the SDK already does — HTTP, retries, error classification, idempotency, stream handling — does NOT get reimplemented here.

---

## 1. What this package is, and what it isn't

### Is

- A Gin-native wrapper around `github.com/poli-page/sdk-go` that gives Gin users:
  - `polipagegin.Middleware(client)` — a `gin.HandlerFunc` that attaches a shared `*polipage.Client` to `*gin.Context`.
  - `polipagegin.ClientFrom(c)` — the typed accessor handlers use.
  - Response helpers `PDF`, `PDFStream`, `Preview`, `DocumentRedirect` that set `Content-Type`, `Cache-Control`, `X-Content-Type-Options`, and RFC 5987 `Content-Disposition`.
  - `polipagegin.ErrorMiddleware()` + `WriteError` — opt-in mapping of `*polipage.Error` to JSON responses.
  - `polipagegin.Config` + `FromEnv()` — env-driven configuration loader.
  - `cmd/polipage-render` — smoke-test binary.
  - An example Gin app at `example-app/` with the interactive demo UI at `GET /`.

### Isn't

- **A wrapper around every `*polipage.Client` method.** Consumers define their own narrow domain interfaces for testability (the idiomatic Go pattern). We do not ship a `Renderer interface { ... }`.
- **An ORM-like abstraction.** No `polipagegin.Render(c, ProjectModeInput{...})` convenience that hides the client — that would obscure context, options, and error paths.
- **A replacement for Gin's error middleware.** `ErrorMiddleware` is narrow on purpose: it only handles `*polipage.Error`. Everything else falls through to whatever else is in the chain.
- **A request-scoped client factory.** One `*polipage.Client` per process is the default and the SDK is built for it (`polipage.go` makes the goroutine-safety guarantee explicit). Multiple clients are supported via per-group middleware (§7.4), not via per-request construction.
- **A code generator.** No `openapi-generator`-style plugin, no Swagger emission.
- **A re-implementation of SDK behaviour.** Tests do not cover transport, retries, 4xx mapping, idempotency, or stream chunking — `sdk-go`'s test suite owns those.

### Quality bar

`getsentry/sentry-go/gin`, `gin-contrib/sessions`, `gin-contrib/cors`, `swaggo/gin-swagger`. Their middleware shape, context-key pattern, and CI matrix are the bar. Same DX patterns we copy from Stripe across all integrations: typed error hierarchy surfaced cleanly, idempotency-key plumbing (already in the SDK), `x-request-id` always logged, helpful validation messages on misconfiguration, `pp_test_*` vs `pp_live_*` prefix discipline.

---

## 2. Required reading (concrete file paths)

Before touching code, read in this order:

1. `/Users/mickael/Projects/INTEGRATIONS_PLAN.md` — cross-repo plan, scope verdicts, cross-cutting DX patterns (§"Cross-cutting DX patterns" is the most relevant section).
2. `/Users/mickael/Projects/nestjs/docs/spec/nestjs-implementation.md` — most recent sibling integration. Same SDK shape, different language. §10 (exception filter scope) and §12 (unpublished-SDK workaround) carry directly.
3. `/Users/mickael/Projects/symfony-bundle/docs/spec/bundle-specification.md` — reference for the response factory header contract (§8), CLI command shape (§9), and integration test gating (§13).
4. `/Users/mickael/Projects/sdk-go/polipage.go` — the SDK entry point. `NewClient(opts ...option.RequestOption) *Client` with `.Render` and `.Documents` namespaces.
5. `/Users/mickael/Projects/sdk-go/render.go` and `documents.go` — every public method we plumb through.
6. `/Users/mickael/Projects/sdk-go/option/` — every construction-time and per-call option. The `Config` loader (§7.2) maps env vars onto these.
7. `/Users/mickael/Projects/sdk-go/error.go` — `*polipage.Error` with `Code`, `StatusCode`, `Message`, `RequestID`, `Cause`, and predicate methods. The opt-in middleware (§11) uses `IsAuthError()`, `IsRateLimitError()`, `IsValidationError()`, `IsNetworkError()`, `IsRetryable()`.
8. `/Users/mickael/Projects/symfony-bundle/example-app/templates/demo.html` — reference interactive-demo UI we replicate in the Gin example app.
9. Gin internals: `gin.Context.Set`/`Get`, `gin.Context.Data`, `gin.Context.DataFromReader`, `gin.Context.Header`, `gin.Context.AbortWithStatusJSON`. The pkg.go.dev reference for `github.com/gin-gonic/gin` is the source of truth.
10. Reference modules to compare patterns against: `getsentry/sentry-go/gin`, `gin-contrib/cors`, `gin-contrib/sessions`. Their middleware shape, context-key conventions, and panic-on-misuse philosophy are the bar.

---

## 3. Version targets

| Field | Value |
|---|---|
| Module path | `github.com/poli-page/gin` |
| Package name | `polipagegin` (consumers `import polipagegin "github.com/poli-page/gin"`) |
| Initial version | `v0.1.0` |
| Go | `1.25+` (matches `sdk-go`'s `go.mod` directive) |
| Gin | `v1.9.0 - v1.10.x` |
| Runtime support | Server-side Go only — no wasm, no Edge. |

`github.com/poli-page/sdk-go` is consumed via a `require` directive resolving to the published module path; during the unpublished window it resolves via a local `replace` (see §12).

CI matrix: Go `1.25` × Gin `v1.9.x` / `v1.10.x`, plus Go `tip` × Gin `v1.10.x` (allowed to fail). Three required cells, one informational cell — matches the breadth `getsentry/sentry-go/gin` ships.

---

## 4. Architecture style

Plain Go, no DI container, no reflection-driven wiring. The package surface is five primitives plus one binary.

1. **`Middleware(client *polipage.Client) gin.HandlerFunc`** — sets `c.Set(ContextKey, client)` and calls `c.Next()`. Zero-allocation per request beyond the map entry Gin already maintains.
2. **`ClientFrom(c *gin.Context) *polipage.Client`** — typed accessor. Panics with a fixed message if `Middleware` was not installed (see §10.1).
3. **Response helpers** — `PDF`, `PDFStream`, `Preview`, `DocumentRedirect`. All pure functions; no shared state.
4. **`ErrorMiddleware() gin.HandlerFunc`** + **`WriteError(c, err)`** — convert `*polipage.Error` in `c.Errors` (or the explicit `err` argument) to a JSON response. The middleware version reads `c.Errors.Last()` (see §11.2). Both narrow on purpose: non-`*polipage.Error` values are untouched.
5. **`Config` struct + `FromEnv() (Config, error)`** + **`Config.Options() []option.RequestOption`** — env-driven loader that produces the slice the consumer passes to `polipage.NewClient`.

Plus:

6. **`cmd/polipage-render`** — Cobra-driven smoke-test CLI (see §9).

That's the public surface. No middleware variants, no per-method response factories, no Gin renderer plugin.

---

## 5. File layout

```
gin/
├── go.mod
├── go.sum
├── doc.go                              # package-level godoc
├── middleware.go                       # Middleware + ClientFrom + ContextKey constant
├── responses.go                        # PDF + PDFStream + Preview + DocumentRedirect
├── headers.go                          # internal: RFC 5987 encoder, content-disposition builder
├── errors.go                           # ErrorMiddleware + WriteError + status mapping
├── config.go                           # Config + FromEnv + Options
├── middleware_test.go
├── responses_test.go
├── headers_test.go
├── errors_test.go
├── config_test.go
├── cmd/
│   └── polipage-render/
│       ├── main.go                     # Cobra root + persistent flags
│       ├── root.go                     # command definitions
│       └── main_test.go                # exit-code matrix
├── example-app/
│   ├── go.mod                          # separate module (depends on parent via replace)
│   ├── main.go                         # gin router + middleware install
│   ├── handlers.go                     # one handler per SDK demo step
│   ├── templates/
│   │   └── demo.html                   # interactive UI (embed.FS)
│   ├── static/
│   │   └── demo.js                     # vanilla JS, no build step
│   └── README.md
├── tests/
│   └── integration/
│       └── render_against_develop_test.go   # //go:build integration
├── docs/
│   ├── events.md
│   ├── streaming.md
│   ├── testing.md
│   ├── middleware.md
│   ├── responses.md
│   ├── cli.md
│   ├── spec/
│   │   └── gin-integration-specification.md
│   └── plan/
│       └── 2026-06-01-implementation.md
├── CLAUDE.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── README.md
├── LICENSE
├── .gitignore
├── .golangci.yml
└── .github/
    └── workflows/
        └── ci.yml
```

Notable choices:

- **Single Go package at the root** (`polipagegin`). The CLI lives under `cmd/polipage-render/` per the standard Go layout. The example app is a separate module to keep the parent module's dependency graph minimal.
- **No `internal/` directory.** The package is small; everything that would live in `internal/` lives in unexported identifiers in the root package.
- **Tests sit next to source** (the Go convention), not in a sibling `tests/` directory. The one exception is the build-tagged integration test, which lives at `tests/integration/` so the build tag isolates it cleanly.

---

## 6. Architecture — the request lifecycle

The full happy path for a render request:

1. `main.go` sets `gin.SetMode(gin.ReleaseMode)` (production) and builds `client := polipage.NewClient(cfg.Options()...)` at startup.
2. `r := gin.New()` then `r.Use(gin.Recovery(), polipagegin.Middleware(client))` — `Recovery` is a hard prerequisite because `ClientFrom` panics on misuse (§7.1); installing it before `polipagegin.Middleware` is non-negotiable. `gin.Default()` includes `Recovery` already and is fine for examples.
3. Per-request, `Middleware` calls `c.Set("polipagegin.client", client)`.
4. A handler calls `client := polipagegin.ClientFrom(c)`.
5. The handler calls `client.Render.PDF(c.Request.Context(), input)` — the SDK uses the request context, so client disconnects cancel the upstream call.
6. On success, the handler calls `polipagegin.PDF(c, bytes, polipagegin.PDFOptions{Filename: "..."})` which writes headers and body.
7. On error, the handler either:
   - Calls `polipagegin.WriteError(c, err)` (per-handler), or
   - Calls `c.Error(err)` and returns (lets `ErrorMiddleware` produce the response).

`c.Request.Context()` is the single point of cancellation. Layering `context.WithTimeout` is fine when the application has its own deadline policy; the SDK's `option.WithTimeout` and `option.WithRequestTimeout` cover the request-budget case.

**Async callbacks must not close over `*gin.Context`.** `*gin.Context` is pooled and recycled after the handler returns; touching it from a goroutine that outlives the request reads the next request's state. Code paths that fan callbacks into goroutines use `c.Copy()` (returns a context safe for goroutine use) or — preferred — copy the values they need onto a handler-local struct before returning. The spec calls this out here and in `docs/events.md`; CLAUDE §10 records the gotcha.

---

## 7. Public API design

### 7.1 Middleware + context

```go
// middleware.go
package polipagegin

const ContextKey = "polipagegin.client"

// Middleware returns a gin.HandlerFunc that attaches client to the gin context.
// Install once on the router: r.Use(polipagegin.Middleware(client)).
func Middleware(client *polipage.Client) gin.HandlerFunc {
	if client == nil {
		panic("polipagegin: Middleware called with nil client")
	}
	return func(c *gin.Context) {
		c.Set(ContextKey, client)
		c.Next()
	}
}

// ClientFrom returns the *polipage.Client attached by Middleware.
// Panics if Middleware was not installed.
func ClientFrom(c *gin.Context) *polipage.Client {
	v, ok := c.Get(ContextKey)
	if !ok {
		panic("polipagegin: Middleware(client) not installed; add r.Use(polipagegin.Middleware(client))")
	}
	client, ok := v.(*polipage.Client)
	if !ok {
		panic("polipagegin: ContextKey holds a non-*polipage.Client value")
	}
	return client
}
```

The `panic` semantics are deliberate. Returning `nil` from `ClientFrom` would surface as a nil-pointer dereference three frames deep in `polipage.Render.PDF` — much harder to debug. Gin's `Recovery()` middleware catches the panic and surfaces a 500 with the message in logs.

**`gin.Recovery()` is a hard prerequisite.** `gin.Default()` registers it automatically; `gin.New()` does NOT. Tests assert that the panic surfaces as a 500 when Recovery is installed (and as a process crash when it isn't — that test exists to make the requirement load-bearing, not to support running without Recovery). README, `docs/middleware.md`, and the example app's `main.go` all show the wiring.

### 7.2 Configuration

```go
// config.go
package polipagegin

type Config struct {
	APIKey      string
	BaseURL     string
	Timeout     time.Duration
	MaxRetries  int
	RetryDelay  time.Duration
}

// FromEnv loads Config from environment variables and validates it.
// Returns a typed error naming the offending variable when validation fails.
func FromEnv() (Config, error) { /* ... */ }

// Options returns the slice to pass to polipage.NewClient.
func (c Config) Options() []option.RequestOption { /* ... */ }
```

Variables:

| Variable | Required | Parsed by | Default | Validation |
|---|---|---|---|---|
| `POLI_PAGE_API_KEY` | yes | string | — | Must match `^pp_(test\|live)_` |
| `POLI_PAGE_BASE_URL` | no | URL parse | SDK default | Must be `http(s)://…` |
| `POLI_PAGE_TIMEOUT` | no | `time.ParseDuration` | SDK default | `> 0 && <= 10m` |
| `POLI_PAGE_MAX_RETRIES` | no | `strconv.Atoi` | SDK default | `0 <= n <= 10` |
| `POLI_PAGE_RETRY_DELAY` | no | `time.ParseDuration` | SDK default | `>= 0 && <= 30s` |

Validation errors wrap a sentinel `polipagegin.ErrInvalidConfig` so callers can `errors.Is(err, polipagegin.ErrInvalidConfig)` if they want a single check.

### 7.3 Response helpers

```go
// responses.go
package polipagegin

type PDFOptions struct {
	Filename string // defaults to "document.pdf"
	Inline   bool   // false → attachment; true → inline (browser viewer)
}

func PDF(c *gin.Context, body []byte, opts PDFOptions)
func PDFStream(c *gin.Context, body io.Reader, opts PDFOptions)
func Preview(c *gin.Context, html string)
func DocumentRedirect(c *gin.Context, descriptor *polipage.DocumentDescriptor)
```

Header contract (§8 is the exhaustive table):

| Helper | `Content-Type` | `Cache-Control` | `X-Content-Type-Options` | `Content-Disposition` |
|---|---|---|---|---|
| `PDF` / `PDFStream` | `application/pdf` | `no-store, private` | `nosniff` | `attachment; filename="…"; filename*=UTF-8''…` (inline when `Inline: true`) |
| `Preview` | `text/html; charset=utf-8` | `no-store, private` | `nosniff` | — |
| `DocumentRedirect` | — | `no-store, private` | — | — (302 + `Location`) |

`PDFStream` takes `io.Reader` (not `io.ReadCloser`) on purpose so it does not assume ownership of close. The handler that obtained the reader from `Render.PDFStream` is responsible for `defer body.Close()`. See §10.3 for rationale.

### 7.4 Per-group clients

`Middleware` is installed via `Use` on anything that implements `gin.IRoutes` — `*gin.Engine` (global) or any `*gin.RouterGroup` (scoped). Multiple clients are supported by stacking middleware; the last `c.Set` wins for the duration of the request:

```go
// main.go
r.Use(polipagegin.Middleware(prodClient))

sandbox := r.Group("/sandbox")
sandbox.Use(polipagegin.Middleware(sandboxClient))
sandbox.GET("/preview", previewHandler) // sees sandboxClient
```

No additional API needed.

---

## 8. Response header contract

The headers below are non-negotiable defaults. Consumers override per-request by calling `c.Header(...)` *before* the helper.

### 8.1 PDF responses (`PDF`, `PDFStream`)

| Header | Value | Source |
|---|---|---|
| `Content-Type` | `application/pdf` | Always. |
| `Content-Length` | byte count | Only `PDF` (buffered). `PDFStream` uses chunked encoding. |
| `Cache-Control` | `no-store, private` | Tenant-scoped docs MUST NOT be proxy-cached. |
| `X-Content-Type-Options` | `nosniff` | Prevents browser type sniffing. |
| `Content-Disposition` | `attachment; filename="…"; filename*=UTF-8''…` | `inline` instead of `attachment` when `PDFOptions.Inline == true`. |
| `X-Accel-Buffering` | `no` | `PDFStream` only. Opts nginx (and conventions-compliant proxies) out of response buffering so the chunked stream actually streams. Safe no-op everywhere else. |

**Status code.** Both helpers set `http.StatusOK` unconditionally. A `c.Status(...)` call before the helper is silently overwritten — non-200 PDF responses (rare) need a hand-rolled `c.Header(...)` + `c.Data(status, "application/pdf", bytes)` path.

**Gzip composition.** PDFs are already compressed inside the container; routing them through `gin-contrib/gzip` is pure CPU waste and breaks viewers that read `Content-Encoding`. Consumers who install gzip globally must exclude PDF routes via `gzip.WithExcludedExtensions([]string{".pdf"})` or path filters. This package does not set `Content-Encoding` itself.

### 8.2 HTML preview (`Preview`)

| Header | Value |
|---|---|
| `Content-Type` | `text/html; charset=utf-8` |
| `Cache-Control` | `no-store, private` |
| `X-Content-Type-Options` | `nosniff` |

### 8.3 Stream ownership + flush mechanic

`PDFStream(c *gin.Context, body io.Reader, opts PDFOptions)` does NOT call `Close()` on the reader. The handler that obtained the `io.ReadCloser` from `client.Render.PDFStream(...)` `defer`s the close immediately after the SDK call, before invoking the helper. Rationale:

- Symfony bundle made the same call (`PoliPageResponseFactory::stream` accepts `StreamInterface` but does not close it — see `symfony-bundle/src/Http/PoliPageResponseFactory.php`).
- Splitting "owns the stream" between caller and helper produces bugs that survive code review (the lifetime depends on which method threw what error, when). Concentrating ownership in the handler makes the rule a one-liner: "the handler owns it from `body, err := ... ; defer body.Close()` until the function returns."
- The helper's signature taking `io.Reader` (not `io.ReadCloser`) advertises the policy.

**Flush mechanic.** `c.DataFromReader` is the idiomatic Gin call but it wraps `io.Copy` and does not flush, so behind a buffering proxy the entire response sits in the proxy buffer until EOF. `PDFStream` therefore writes headers (via the shared `writePDFHeaders` + the `X-Accel-Buffering: no` opt-out from §8.1), sets status 200, then runs a 32 KB `Read`/`Write`/`Flush` loop manually:

```go
const flushChunk = 32 * 1024
buf := make([]byte, flushChunk)
flusher, _ := c.Writer.(http.Flusher)
for {
    n, rerr := body.Read(buf)
    if n > 0 {
        if _, werr := c.Writer.Write(buf[:n]); werr != nil {
            _ = c.Error(fmt.Errorf("polipagegin: stream write failed: %w", werr))
            return
        }
        if flusher != nil { flusher.Flush() }
    }
    if rerr == io.EOF { return }
    if rerr != nil {
        _ = c.Error(fmt.Errorf("polipagegin: stream read failed: %w", rerr))
        return
    }
}
```

The 32 KB chunk matches `io.Copy`'s default and amortizes the per-flush syscall cost across a sensible byte count. `c.Writer.(http.Flusher)` is satisfied by Gin's default response writer; the helper falls through silently when an exotic wrapper hides it (bytes still go out, the kernel decides cadence). Tests cover both the flusher-present and flusher-absent paths.

### 8.4 Filename encoding (RFC 5987)

`Content-Disposition` carries the filename in two slots:

```
Content-Disposition: attachment; filename="invoice-42.pdf"; filename*=UTF-8''invoice-42.pdf
```

- `filename="…"` — ASCII fallback. Non-ASCII codepoints are replaced with `_`. Wrapped in double quotes; the double-quote and backslash characters are backslash-escaped.
- `filename*=UTF-8''…` — the actual UTF-8 filename, percent-encoded with `url.PathEscape` plus a small allowlist (per RFC 5987 §3.2.1). Browsers (and `curl -O -J`) use this in preference to `filename=`.

Empty `PDFOptions.Filename` defaults to `document.pdf`.

---

## 9. CLI — `cmd/polipage-render`

Smoke-test binary that renders a template end-to-end. Same shape as the Symfony bundle's `bin/console poli-page:render`.

### 9.1 Implementation

- **`spf13/cobra`** for argument parsing. `cobra` is the Go standard for CLI tools (`kubectl`, `gh`, `docker`, `hugo`).
- **One subcommand, `render`** — though Cobra wraps it as the root command for ergonomics (`polipage-render --project=... ` instead of `polipage-render render --project=...`).
- **No persistent state.** Each invocation builds a `*polipage.Client` from env + flags, renders once, exits.

### 9.2 Flags

See `docs/cli.md` for the canonical table. Spec-level rule: every flag is prefixed by the noun it describes (`--template-version`, not `--version`). Cobra reserves `--help` globally; redefining it silently shadows the global. The Symfony bundle hit the same hazard (see `symfony-bundle/CLAUDE.md` §10.4) — record once, never relearn.

### 9.3 Exit codes

| Code | Trigger |
|---|---|
| `0` | Success |
| `1` | `*polipage.Error.IsAuthError()` |
| `2` | `*polipage.Error.IsValidationError()` |
| `3` | `*polipage.Error.IsNetworkError()` |
| `4` | Anything else (rate limit, 5xx, SDK-internal) |
| `64` | Misuse — bad flag, missing required flag, invalid JSON in `--data` (matches `sysexits.h`'s `EX_USAGE`) |

### 9.4 Smoke-test usage in CI

The `.github/workflows/ci.yml` runs `go run ./cmd/polipage-render --project=getting-started --template=welcome --template-version=1.0.0 --data='{"name":"ci"}' -o /tmp/welcome.pdf` against `api-develop.poli.page` when `POLI_PAGE_API_KEY` is set. Skipped on PRs from forks (no secrets).

---

## 10. Error handling

### 10.1 What `*polipage.Error` looks like

```go
type Error struct {
	Code       string
	StatusCode int
	Message    string
	RequestID  string
	Cause      error
}

func (e *Error) Error() string
func (e *Error) IsAuthError() bool         // 401, 403
func (e *Error) IsRateLimitError() bool    // 429
func (e *Error) IsValidationError() bool   // 400
func (e *Error) IsNetworkError() bool      // network / TLS / timeout
func (e *Error) IsRetryable() bool         // 5xx, 429, network (SDK already retried)
```

Plus sentinels (`polipage.ErrUnauthorized`, `polipage.ErrRateLimit`, etc.) usable with `errors.Is`.

### 10.2 `ErrorMiddleware`

`c.Errors.Last()` returns `*gin.Error`, not `error` — the inner value lives in `.Err`. `errors.As(c.Errors.Last(), &pe)` silently fails (Gin doesn't implement an `Unwrap` chain that reaches `pe`). The correct call is `errors.As(ge.Err, &pe)`, asserted by tests so a future "cleanup" PR can't simplify it back into the broken form.

```go
// errors.go
func ErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		ge := c.Errors.Last()
		if ge == nil {
			return
		}
		var pe *polipage.Error
		if !errors.As(ge.Err, &pe) { // .Err, NOT ge — see note above
			return // not ours, leave it for downstream handlers
		}
		status, code := mapError(pe)
		c.AbortWithStatusJSON(status, gin.H{
			"code":      code,
			"message":   pe.Message,
			"requestId": pe.RequestID,
		})
	}
}

func mapError(pe *polipage.Error) (status int, code string) {
	switch {
	case pe.IsNetworkError():
		return http.StatusBadGateway, "NETWORK_ERROR"
	default:
		// 4xx and 5xx pass through
		return pe.StatusCode, pe.Code
	}
}
```

Reads `c.Errors.Last()` because Gin's `Errors` is a slice and handlers may queue several. The last is what the response should reflect — same rule downstream middleware (logging, tracing) typically follows.

### 10.3 `WriteError(c, err)`

The per-handler version of `ErrorMiddleware`. Produces the same status + body, lets handlers that want explicit error flow stay explicit:

```go
func WriteError(c *gin.Context, err error) {
	var pe *polipage.Error
	if !errors.As(err, &pe) {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	status, code := mapError(pe)
	c.AbortWithStatusJSON(status, gin.H{
		"code":      code,
		"message":   pe.Message,
		"requestId": pe.RequestID,
	})
}
```

### 10.4 Why narrow?

`ErrorMiddleware` only handles `*polipage.Error`. Widening to all `error` values would swallow non-Poli Page failures, violate the thin-wrapper stance, and destroy observability (consumers' existing logging middleware would never see the error). Same decision the NestJS package made with `@Catch(PoliPageError)` (see `nestjs/CLAUDE.md` §10.3) and the Symfony bundle made by not catching at all.

---

## 11. Lifecycle hooks → application observability

The SDK exposes `option.WithOnRetry(func(RetryEvent))` and `option.WithOnError(func(error))` on the client constructor. The integration does NOT wrap these in a custom event bus — Go already has `slog`, `log/slog`, OpenTelemetry, Prometheus, and any number of metrics libraries that compose with plain functions.

`docs/events.md` shows the three canonical patterns (slog, per-request audit via `*gin.Context`, Prometheus counters). The spec-level rule: the integration must not introduce a Gin-specific event abstraction over the SDK's hooks. If a future user needs one, they build it in their app.

---

## 12. Unpublished-SDK workaround (dev-time only)

`github.com/poli-page/sdk-go` is **not yet published**. Until it is, dev resolves via a `replace` directive in `go.mod`:

```go.mod
module github.com/poli-page/gin

go 1.25

require (
    github.com/gin-gonic/gin v1.10.0
    github.com/poli-page/sdk-go v0.0.0-00010101000000-000000000000
    github.com/spf13/cobra v1.8.0
    github.com/stretchr/testify v1.9.0
)

// Why: SDK not yet published. Remove on SDK v1.0.0 publish.
replace github.com/poli-page/sdk-go => ../sdk-go
```

For multi-repo workflows where contributors clone outside `/Users/mickael/Projects/`, a `go.work` at the workspace root is the cleaner alternative:

```go.work
go 1.25

use (
    ./sdk-go
    ./gin
)
```

The `go.work` lives outside the integration repo, so it does not affect the published manifest. When the SDK publishes:

1. Delete the `replace` line in `go.mod`.
2. `go get github.com/poli-page/sdk-go@v1.0.0` to pin a real version.
3. Tag v0.1.0.

**Source code is untouched** by the transition — only the dependency graph.

---

## 13. Testing strategy

Mirror the policies set in `symfony-bundle/docs/spec/bundle-specification.md` §13 and `nestjs/docs/spec/nestjs-implementation.md` §14. The integration tests focus on integration-specific concerns; SDK-level behaviour is the SDK's job.

### 13.1 What we test

- **`Middleware` + `ClientFrom`**: middleware attaches the client; `ClientFrom` returns it; `ClientFrom` panics with the documented message when the middleware was not installed.
- **`Middleware(nil)` panics at install time** (fail fast, not on first request).
- **Response helpers**: every helper sets `Content-Type`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`. PDF helpers set the RFC 5987 `Content-Disposition` correctly for ASCII, non-ASCII, double-quote, and backslash filenames.
- **`PDFStream`**: passes through bytes from an `io.Reader`; surfaces a mid-stream read error via `c.Error`.
- **`ErrorMiddleware` + `WriteError`**: every `*polipage.Error` predicate maps to the right status and code; non-`*polipage.Error` is left alone.
- **`FromEnv` validation**: each variable's failure mode (missing required key, wrong prefix, unparseable duration, out-of-range integer) returns an error naming the variable.
- **`cmd/polipage-render`**: flag parsing, JSON `--data` unmarshal, exit codes per the table in §9.3.
- **Race detector**: `go test -race ./...` must pass — middleware is on the request hot path, headers helpers touch `c.Writer`.

### 13.2 What we DO NOT test

- HTTP transport behaviour (`net/http` transport edge cases, TLS).
- Retry policy (backoff, max attempts, `Retry-After`, never-retry-4xx).
- 4xx / 5xx → `*polipage.Error` mapping inside the SDK.
- Idempotency-Key generation.
- Stream chunking correctness from the SDK side.
- API contract drift — the SDK's contract tests own that.

If a test ends up spinning an `httptest.Server` that fakes the Poli Page API, it's doing the SDK's job. Stop and stub at the interface level inside the consumer's domain (see `docs/testing.md`).

### 13.3 Integration test

One file, `tests/integration/render_against_develop_test.go`, behind `//go:build integration`. Calls the real `api-develop.poli.page` with a `pp_test_*` key, asserts a non-empty PDF comes back. Skips with `t.Skip` when `POLI_PAGE_API_KEY` is unset (does NOT fail). Run with `go test -tags=integration ./tests/integration/...`.

### 13.4 Test layout

Tests live next to source (`middleware_test.go`, `responses_test.go`, …) — Go convention. The build-tagged integration test is the one exception (lives at `tests/integration/`).

---

## 14. Example app

Lives at `example-app/` as a separate Go module (`example-app/go.mod`) so its dependencies (`html/template` is fine; nothing heavyweight) don't bleed into the parent module's graph.

### 14.1 Routes

One handler per SDK demo step:

| Route | SDK call | Helper used |
|---|---|---|
| `GET /` | — | `c.FileFromFS("templates/demo.html", ...)` |
| `GET /api/render/pdf` | `Render.PDF` | `polipagegin.PDF` |
| `GET /api/render/pdf-stream` | `Render.PDFStream` | `polipagegin.PDFStream` |
| `GET /api/render/preview` | `Render.Preview` | `polipagegin.Preview` |
| `POST /api/documents` | `Render.Document` | JSON of `{documentId, expiresAt}` |
| `GET /api/documents/:id` | `Documents.Get` | `polipagegin.DocumentRedirect` |
| `GET /api/documents/:id/preview` | `Documents.Preview` | `polipagegin.Preview` |
| `GET /api/documents/:id/thumbnails` | `Documents.Thumbnails` | JSON |
| `DELETE /api/documents/:id` | `Documents.Delete` | 204 |
| `GET /api/render/error` | render with bad version | triggers `ErrorMiddleware` |

### 14.2 UI

Single-page HTML at `GET /`. One button per SDK feature, inline `<iframe>` previews for PDFs, JSON pretty-print for stored-document operations, a document-lifecycle state machine in client-side JS (capture the ID into JS state; gate downstream buttons on its presence; clear it on Delete).

Aesthetic copied from `/Users/mickael/Projects/symfony-bundle/example-app/templates/demo.html` — warm paper background, process-cyan accent, Fraunces (headings) + IBM Plex Sans (body) + JetBrains Mono (code). Implemented as a static file served via `embed.FS` + `c.FileFromFS`; no Go template engine dependency beyond `html/template` for the one-off bootstrap data injection.

### 14.3 Env loading

`example-app/main.go` reads the workspace root `.env` (`/Users/mickael/Projects/.env`) at bootstrap and pushes values into the process environment only when they are not already set. Real shell exports always win.

Hand-rolled parser (~30 lines) — no `joho/godotenv` dependency for the example app. The integration's `Config.FromEnv` does NOT load `.env` either; it reads the process environment. Same policy as the Symfony bundle and NestJS package (see `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §2).

### 14.4 Production-shape bootstrap

The example app's `main.go` is wired the way a production binary would be — `gin.SetMode(gin.ReleaseMode)` to drop the debug-log spam, explicit `r.SetTrustedProxies(...)` (defaults to `nil` so `c.ClientIP()` returns the direct peer; if the demo runs behind a local proxy, set it accordingly), and a graceful shutdown driven by `signal.NotifyContext` + `http.Server.Shutdown`:

```go
// example-app/main.go
gin.SetMode(gin.ReleaseMode)
r := gin.New()
r.Use(gin.Logger(), gin.Recovery(), polipagegin.Middleware(client))
_ = r.SetTrustedProxies(nil) // explicit no-trust default

srv := &http.Server{Addr: ":8080", Handler: r}

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

go func() {
    if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Fatal(err)
    }
}()

<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
_ = srv.Shutdown(shutdownCtx)
```

Mid-stream PDFs handle this correctly because `Shutdown` waits for active connections to drain (up to the timeout) and `c.Request.Context()` cancels on the timeout, which propagates into the SDK call. This is the pattern users will copy into their own services; the example app is the reference.

### 14.5 Smoke

```bash
cd example-app
go run .
# → http://127.0.0.1:8080
```

---

## 15. CI and quality gates

Workflow: `.github/workflows/ci.yml`. Matrix: Go `1.25` × Gin `v1.9.x` / `v1.10.x` (required), Go `tip` × Gin `v1.10.x` (informational). Each step auto-skips if the relevant config is missing so a freshly scaffolded repo is green from day one.

| Step | Tool | Skip condition |
|---|---|---|
| Format | `gofmt -l .` | never |
| Vet | `go vet ./...` | never |
| Lint | `golangci-lint run` | missing `.golangci.yml` |
| Unit tests | `go test -race ./...` | never |
| Integration smoke | `go test -tags=integration ./tests/integration/...` | missing `POLI_PAGE_API_KEY` |
| Build | `go build ./...` | never |
| Render smoke (CLI) | `go run ./cmd/polipage-render ...` | missing `POLI_PAGE_API_KEY` |

`golangci-lint` config (`.golangci.yml`) enables: `errcheck`, `govet`, `staticcheck`, `revive`, `gosec`, `unused`, `gocritic`, `gofmt`, `goimports`. No `//nolint:...` without a `// Why: …` comment.

---

## 16. Out of scope for v0.1

Deferred to v0.2+ on real user demand.

- **OpenTelemetry integration as a typed wrapper.** Users currently wire `option.WithOnRetry`/`OnError` into their own OTel setup (see `docs/events.md`). A `polipagegin.OTelMiddleware()` would be a separate sub-package once the patterns settle.
- **Per-handler timeout overrides via decorator.** Currently a wrapper handler. A `polipagegin.WithTimeout(d)(handler)` decorator might land once people ask.
- **Multi-tenant client selection by header.** Per-group middleware (§7.4) handles the static case; dynamic key selection (read API key from `X-Tenant` header) is a v0.2 feature once one user actually needs it.
- **WebSocket / SSE streaming.** Out of scope — Poli Page renders documents, not live data.
- **`fiber` / `echo` / `chi` ports.** Each gets its own repo when prioritised. The `polipagegin` codebase is small enough that copy-paste-adapt is the right starting point for a sibling.

---

## 17. Resolved decisions

A short index of the "why" behind major choices. Useful when an alternative looks tempting six months from now.

| Decision | Alternative considered | Why we went this way |
|---|---|---|
| `ClientFrom` panics on missing middleware | Return `nil` with a boolean second value | Returning `nil` defers the failure to a nil-pointer deref deep in the SDK call. Panicking with a fixed message is louder, Gin's `Recovery` catches it. |
| `PDFStream` takes `io.Reader` (not `io.ReadCloser`) | Take `io.ReadCloser` and close it in the helper | Mirrors the symfony-bundle's caller-owns-stream choice. Splitting ownership across helper + caller produces bugs that survive review. |
| `ErrorMiddleware` only handles `*polipage.Error` | Catch every `error` and map to 500 | Widening swallows non-Poli Page failures and destroys observability. Same narrow scope as NestJS `@Catch(PoliPageError)` and Symfony "exceptions propagate". |
| No `Renderer` interface in this package | Ship one wrapping the full client | Idiomatic Go: consumers define narrow interfaces for the methods *they* call, not the library. The Big Generic Interface couples every test to every SDK change. |
| `Config` reads env vars only (no `.env` loading) | Bundle `joho/godotenv` | The integration runs inside processes orchestrated by Docker/k8s/systemd that already inject env. Per-app `.env` files violate the "one root `.env`" rule (`INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §2). |
| Cobra for the CLI | `flag` from the standard library | Cobra is the Go community standard (kubectl, gh, hugo). Flag completion, nested help, validators all come for free. |
| CLI flag is `--template-version`, not `--version` | `--version` | Cobra reserves `--version` globally on opted-in binaries; renaming avoids silent shadowing. Same hazard the Symfony bundle hit. |
| `Middleware(nil)` panics at install time | Return an error from `Middleware` | `gin.HandlerFunc` cannot return an error. Failing fast at startup is better than failing on first request. |
| Tests live next to source | Sibling `tests/` directory | Go convention; everyone clones expecting `middleware_test.go` next to `middleware.go`. Integration test is the lone exception. |
| Example app is a separate Go module | One module for everything | Keeps the published module's dependency graph minimal; no transitive `html/template` or example-only deps. |
| Use `slog` examples in docs | Use a custom logger interface | `slog` is the standard-library logger from Go 1.21+; no third-party dependency to teach. The SDK already accepts `*slog.Logger` via `option.WithLogger`. |
| `gin.Recovery()` is a documented prerequisite, not an internal install | Have `Middleware` chain `Recovery` for the user | Hiding Recovery breaks the principle of "the consumer's router is the consumer's router". A loud requirement in three doc surfaces (README, middleware.md, CLAUDE) + a test that asserts the panic-to-500 path is enough. |
| `PDFStream` runs a 32 KB read/write/flush loop, not `c.DataFromReader` | Use `c.DataFromReader(http.StatusOK, -1, ...)` | `DataFromReader` is one `io.Copy`, no flush. Behind a buffering proxy that defeats streaming entirely. The manual loop is ~10 lines and makes the flush cadence explicit. |
| `PDFStream` auto-sets `X-Accel-Buffering: no` | Leave proxy hints to the consumer | Setting a per-response opt-out costs one header line and works for the dominant proxy (nginx) without burdening consumers. Proxies that ignore it are no worse off. |
| Pass `OnRetry`/`OnError` per-call via `option.With*`, not via a per-request client | Build a new `*polipage.Client` per request when a trail is needed | Per-request client allocation throws away the SDK's connection pool. Per-call options give the same isolation without the cost. |
| `errors.As(c.Errors.Last().Err, &pe)` (not `.Last()` directly) | Implement `Unwrap` on a custom error type | Gin's `*gin.Error` does not chain through `Unwrap` to its inner `Err`. Calling `.Err` explicitly is the documented Gin idiom; we test both shapes to lock the rule in. |
| Graceful shutdown shown in the example app | Leave it to user discretion | In-flight PDF streams need `Shutdown(ctx)` to cancel cleanly. The example app is the reference users copy from; missing it produces "container kills mid-render" stories in support. |
| `gin.SetMode(gin.ReleaseMode)` in the example app's `main.go` | Default `gin.DebugMode` everywhere | Debug mode spams route registration to stdout and reports trusted-proxies warnings. Production-shape example sets the tone for consumers. |
