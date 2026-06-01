# CLAUDE.md

> Instructions for Claude Code agents working in `poli-page/gin`.

## 1. Repo at a glance

| Field        | Value |
| ------------ | ----- |
| Repository   | `poli-page/gin` |
| Type         | Framework integration (Gin middleware + response helpers) |
| Language     | Go 1.25+ |
| Gin          | `v1.9` \|\| `v1.10` (CI matrix covers both) |
| Registry     | Go modules — `github.com/poli-page/gin` (pkg.go.dev) |
| Depends on   | `github.com/poli-page/sdk-go` |
| Roadmap slot | Go integration (post-P3 in the SDK roadmap; first Go integration to ship) |

**Source-of-truth docs (read first):**
- `docs/spec/gin-integration-specification.md` — full design spec for v0.1.0
- `docs/plan/2026-06-01-implementation.md` — step-by-step implementation plan
- `/Users/mickael/Projects/INTEGRATIONS_PLAN.md` — cross-repo umbrella note, esp. §"Cross-cutting DX patterns"
- `/Users/mickael/Projects/nestjs/CLAUDE.md` + `docs/spec/nestjs-implementation.md` — most recent sibling integration (different language, same SDK shape). Reuse decisions where applicable.
- `/Users/mickael/Projects/symfony-bundle/CLAUDE.md` + `docs/spec/bundle-specification.md` — reference for response factory headers, CLI command shape, integration test gating.
- `/Users/mickael/Projects/sdk-go/` — the SDK this package wraps. `polipage.go`, `documents.go`, `render.go`, `option/`, `error.go` are the surface. **The SDK wins** every time a snippet contradicts it.

## 2. The package's job

This package is a **thin Gin-flavored wrapper** around the official Poli Page Go SDK (`github.com/poli-page/sdk-go`, source at `/Users/mickael/Projects/sdk-go/`). It provides:

- `polipagegin.Middleware(client)` — attaches `*polipage.Client` to `*gin.Context` via `c.Set(contextKey, client)`.
- `polipagegin.ClientFrom(c)` — typed accessor for the per-request client.
- Response helpers — `PDF`, `PDFStream`, `Preview`, `DocumentRedirect` — that set `Content-Type`, `Cache-Control`, `X-Content-Type-Options`, and RFC 5987 `Content-Disposition` so handlers never write headers by hand.
- `polipagegin.ErrorMiddleware()` (+ `WriteError`) that converts `*polipage.Error` to JSON responses with stable status mapping (4xx → same, 5xx → same, network → 502).
- `polipagegin.Config` + `FromEnv()` for env-driven configuration.
- `cmd/polipage-render` — smoke-test binary mirroring `bin/console poli-page:render`.
- An example Gin app at `example-app/` with the interactive demo UI served at `GET /`.

**This package does NOT** reimplement HTTP transport, retries, error classification, idempotency keys, stream chunking, or anything else the SDK already does. Bug in those areas? Fix it in `sdk-go`, not here.

**This package does NOT** ship: a generic interface that wraps the entire `*polipage.Client` (consumers define their own narrow domain interfaces — that's the idiomatic Go pattern for testability), an OpenAPI/Swagger generator, an `httptest` server replacement (use Gin's test mode + a stubbed interface), or any code that depends on a specific Gin renderer plugin beyond the `Context.Data`/`Context.DataFromReader`/`Context.String` primitives.

## 3. Working language

- **Code, comments, file names, commit messages, PR descriptions, repository documentation**: English.
- **Day-to-day conversation with Mickael/Xavier**: French, tutoiement.
- **Conversation in this Claude Code session**: French is fine for the chat; artifacts stay English.

## 4. TDD is mandatory

RED → GREEN → refactor for every change. Tests live next to source (`*_test.go`) and use the standard `testing` package + `github.com/stretchr/testify` for assertions where it materially shortens the test. Integration tests live in `tests/integration/` behind a `//go:build integration` build tag and are gated on `POLI_PAGE_API_KEY`.

### What to test (integration-specific!)

- **`Middleware` + `ClientFrom`**: middleware attaches the client; `ClientFrom` returns it; `ClientFrom` panics with a clear message when the middleware was not installed.
- **Response helpers**: every helper sets `Content-Type`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`, and an RFC 5987-encoded `Content-Disposition` with both `filename="…"` (sanitized ASCII) and `filename*=UTF-8''…` (percent-encoded UTF-8) where appropriate.
- **`PDFStream`**: passes through bytes from an `io.Reader`; closes the source when the handler returns; surfaces a mid-stream read error as `c.Errors` (the response is already partial, but the failure is visible).
- **`ErrorMiddleware` + `WriteError`**: `*polipage.Error` with status 4xx/5xx maps to same-status JSON `{code, message, requestId}`; network/timeout (predicates `IsNetworkError()`) maps to 502 with `code: "NETWORK_ERROR"`; non-`*polipage.Error` values are left untouched.
- **`FromEnv` validation**: missing `POLI_PAGE_API_KEY`, wrong prefix, unparseable `POLI_PAGE_TIMEOUT`/`POLI_PAGE_RETRY_DELAY`, out-of-range `POLI_PAGE_MAX_RETRIES` return an error naming the offending variable.
- **`cmd/polipage-render`**: flag parsing, JSON data unmarshal, exit codes for `*polipage.Error` families (auth → 1, validation → 2, network → 3, anything else → 4).

### What NOT to test (the SDK already does)

- HTTP transport behaviour (`net/http` transport edge cases, TLS).
- Retry policy (backoff, max attempts, `Retry-After`, never-retry-4xx, never-retry on context cancel).
- 4xx / 5xx → `*polipage.Error` mapping inside the SDK.
- Idempotency-Key generation.
- Stream chunking correctness from the SDK side.
- API contract drift — the SDK's contract tests own that.

Re-testing these here doubles maintenance burden. **If you find yourself spinning up an `httptest.Server` that fakes the Poli Page API, stop — you're doing the SDK's job.** Use the SDK directly against `api-develop.poli.page` for the one smoke test.

## 5. Robustness over shortcuts

Mickael's hard rule: **no hacks to make a test pass or a corner case go away**. Fix root causes. If a workaround is genuinely required (framework bug, SDK quirk), document it inline with a `// Why:` comment naming the constraint.

Concretely: don't `_ =` errors to silence linters, don't `panic` for control flow, don't widen `ErrorMiddleware` to catch generic `error` values (it would swallow non-Poli Page failures and destroy observability), don't expose an interface that mirrors the entire SDK just to make mocking easier.

## 6. Code conventions

- **`gofmt`** — required, CI fails on any unformatted file.
- **`go vet ./...`** + **`golangci-lint run`** with the project's `.golangci.yml`. Pinned linter set: `errcheck`, `govet`, `staticcheck`, `revive`, `gosec`, `unused`, `gocritic`. No exceptions added without a `//nolint:<linter> // Why: …` comment.
- **No commented-out code, no `TODO` without a linked issue, no `fmt.Println` debug prints.**
- **Default to no comments.** Add one only when the *why* is non-obvious. Comments restating *what* the code does are noise.
- **Exported names get a godoc comment** (one sentence, starts with the symbol name). Internal helpers stay uncommented unless the *why* is non-obvious.
- **Errors wrap with `fmt.Errorf("...: %w", err)`** — never use `errors.New` for wrapping. Consumers should be able to `errors.As(err, &polipage.Error{})`.

## 7. Commits and PRs

- **Conventional Commits**: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`.
- **One concern per PR**, reviewable in under 30 minutes.
- PR description: what changed, why, how it was tested.
- CI must be green before merge.

## 8. CI

Workflow: `.github/workflows/ci.yml`. Matrix: Go `1.25` × Gin `v1.9` / `v1.10`, plus Go `tip` × Gin `v1.10` (allowed to fail). Each step auto-skips if the relevant config file is missing (so a freshly scaffolded repo is green from day one). Don't change that behaviour.

Local mirror:
```bash
go mod download
gofmt -l .                  # must produce no output
go vet ./...
golangci-lint run
go test -race ./...
go test -tags=integration ./tests/integration/...   # needs POLI_PAGE_API_KEY
go build ./...
```

## 9. Unpublished-SDK / workspace note

`github.com/poli-page/sdk-go` is **not yet published**. For local dev we rely on a Go-native pattern: a `replace` directive in `go.mod`.

```
// go.mod
replace github.com/poli-page/sdk-go => ../sdk-go
```

For multi-repo work the cleaner alternative is `go work init ./sdk-go ./gin` at `/Users/mickael/Projects/`; that file stays out of the integration repo. Either way the integration's published `go.mod` keeps a plain `require github.com/poli-page/sdk-go vX.Y.Z` so a tagged release builds cleanly off `proxy.golang.org` once the SDK ships. The `replace` line lives behind a `// Why: SDK not yet published` comment and is deleted (single commit) at SDK publish.

## 10. Known gotchas (battle-tested — don't relearn the hard way)

These caught us once. Recorded so future agents don't burn a session rediscovering them.

### 10.1 `ClientFrom` must panic loudly when the middleware is missing

The natural temptation is to return `nil` and let the handler crash with a nil-pointer deref three frames deep in `polipage.Client.Render`. Don't. `ClientFrom` panics with a message naming the missing middleware: `polipagegin: Middleware(client) not installed; add r.Use(polipagegin.Middleware(client))`. The panic is recovered by Gin's default recovery middleware and surfaces in logs immediately. Cf. how `getsentry/sentry-go/gin`'s `sentrygin.GetHubFromContext` returns `nil` and bites users — that's the failure mode we're avoiding.

**Hard prerequisite: `gin.Recovery()` must be installed.** `gin.Default()` includes it; `gin.New()` does NOT. The README, `docs/middleware.md`, and the example app's `main.go` all show the Recovery wiring; tests assert that the panic surfaces as a 500 (not a process crash) when Recovery is present. Do NOT change `ClientFrom` to a soft-fail just because a user forgot Recovery — fix the user-facing docs instead. The fail-loud stance is the load-bearing decision; weakening it makes "I forgot the middleware" a silent prod bug.

### 10.2 Cobra (and `flag`) reserve option names

The `cmd/polipage-render` binary originally defined `--version` and Cobra silently shadowed it with the global flag — the command "ran" but printed the wrong thing. Renamed to `--template-version`. Same hazard for `--help` (always reserved). Audit before adding any CLI option. See `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §3 for the cross-framework list.

### 10.3 `PDFStream` must close the source on every exit path

The handler controls the lifetime of the `io.ReadCloser` returned by `Render.PDFStream`. `polipagegin.PDFStream` accepts an `io.Reader` (not `io.ReadCloser`) so the helper does NOT close it — closing is the caller's job, via `defer body.Close()` in the handler immediately after the SDK call. This matches the symfony-bundle's "the caller owns the stream" decision (spec §8.3). Document this in the helper's godoc and assert in tests that a leaking `io.ReadCloser` is the caller's bug, not the library's.

### 10.4 `c.Errors` ordering matters for `ErrorMiddleware`

Gin's `c.Errors` is a slice, not a single value. `ErrorMiddleware` reads `c.Errors.Last()` (most recent) and only acts when its `Err` is a `*polipage.Error`. If a handler queues multiple errors, only the last one drives the response. Document the rule in `errors.go` and link to the Gin docs on `c.Error`/`c.Errors`. Do NOT iterate the whole slice looking for the first `*polipage.Error` — that creates non-obvious response shapes when downstream middleware also pushes errors.

**`c.Errors.Last()` returns `*gin.Error`, not `error`.** The `*polipage.Error` lives inside the `.Err` field. `errors.As(c.Errors.Last(), &pe)` silently fails (the inner `Err` is not unwrapped by `*gin.Error`'s default `As` chain — Gin wraps but doesn't expose `Unwrap`). The correct call is `errors.As(c.Errors.Last().Err, &pe)`. Tests must cover both shapes (the right one and the easy-to-typo wrong one) so a future agent doesn't "simplify" the working call back into the broken one.

### 10.5 `ErrorMiddleware` only handles `*polipage.Error`

The middleware is **narrow on purpose**. Any other error in `c.Errors` is left untouched for Gin's default handler (or your custom logger middleware) to deal with. Do NOT widen the catch to `error` — generic error swallowing destroys observability and violates the "thin wrapper" stance (§2). Same rationale as the NestJS `PoliPageExceptionFilter`'s `@Catch(PoliPageError)` decision.

### 10.6 Single root `.env`, no per-app `.env.local`

The example app's `main.go` reads the workspace root `.env` (`/Users/mickael/Projects/.env`) at bootstrap and pushes values into the environment only when they are not already set. Real shell exports always win.

**Do NOT** introduce a `.env.local` in `example-app/` or instruct users to `cp .env .env.local`. This was an explicit hard requirement from Mickael during the symfony-bundle session. See `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §2.

### 10.7 The interactive demo UI is mandatory, not optional

`GET /` in the example app returns a single-page HTML dashboard with one button per SDK feature, inline `<iframe>` previews, JSON pretty-print, and a document-lifecycle state machine in client JS. Aesthetic copied from `/Users/mickael/Projects/symfony-bundle/example-app/templates/demo.html` (warm paper background, process-cyan accent, Fraunces + IBM Plex Sans + JetBrains Mono). Implemented as a static `embed.FS` file served via `c.FileFromFS` — no Go template engine dep beyond the standard library. See `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §1.

### 10.8 Context propagation is non-negotiable

Every SDK call from a handler **must** take `c.Request.Context()` (not `context.Background()`). When the client disconnects, the context cancels, the SDK aborts with `polipage.ErrAborted`, and the handler does not pile retries onto a request nobody is waiting for. Tests for handlers should assert that a canceled `c.Request.Context()` propagates into the SDK call. See spec §6.

## 11. When stuck

- Re-read `docs/spec/gin-integration-specification.md` first; most "open questions" are answered there or in §17 "Resolved decisions".
- Compare with the SDK source at `/Users/mickael/Projects/sdk-go/` — `polipage.go`, `render.go`, `documents.go`, `error.go`, `option/` are the surface.
- Compare with `/Users/mickael/Projects/symfony-bundle/` (response factory, CLI command, integration test gate) and `/Users/mickael/Projects/nestjs/` (exception filter scope, options validation).
- Look at industry benchmarks: `getsentry/sentry-go/gin`, `gin-contrib/sessions`, `gin-contrib/cors`, `swaggo/gin-swagger`. Their middleware shape + context-key pattern is the bar.
- Ask Mickael early. A two-line message is faster than a half-day rebuilding the wrong thing.
- If a CI failure looks unrelated to your change, check `main` first before assuming you caused it.
