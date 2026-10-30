# Changelog

All notable changes to `poli-page/gin` are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Breaking:** API keys are `pp_{environment}_{32 lowercase hex}` for any environment (`pp_sandbox_…`, `pp_live_…`, `pp_staging_…`). `FromEnv` no longer rejects a key by its `pp_test_` / `pp_live_` prefix: a missing or empty `POLI_PAGE_API_KEY` is still an error, and any other value that does not match the key format logs one `log/slog` warning (it names the expected format and shows no part of the key, not even its `pp_{environment}_` part) and loads, so the API answers with its own error.
- **Breaking:** requires `github.com/poli-page/sdk-go` `v0.11.0`, the SDK release with environments: renders take a `polipage.RenderInput` with no `Version`, and the key selects the environment whose published copy is rendered. Both `go.mod` files require the published module instead of a `replace` to a sibling `../sdk-go` checkout.
- **Breaking:** `Middleware(client)` installs the error handler by default. A `*polipage.Error` queued with `c.Error(err)` is answered with `Content-Type: application/json`, the error's `ToPayload()` as the body (`{code, message, status, requestId}`, `null` when unknown) and its `status` as the HTTP status (`503` for `network_error`, `504` for `timeout`, `500` when the payload has no status). Turn it off with `Middleware(client, polipagegin.WithoutErrorHandler())`. A response the handler already wrote is never overwritten.
- **Breaking:** `ErrorMiddleware` and `WriteError` write the same body: an error with no status now answers `500` with `"status": null` in the body instead of `"status": 500`.
- **Breaking:** example app: the demo routes move from `/api/...` to the root (`GET /render/pdf`, `GET /documents/{id}`, `GET /errors/not-found`, ...). `GET /errors/not-found` replaces `GET /api/render/error`: it renders the `does-not-exist` template of `getting-started` with `{}` and answers `404` with code `TEMPLATE_NOT_FOUND` through the default error handler. `GET /documents/{id}` still answers a `302` redirect to the presigned PDF URL. The app reads `.env` from its working directory.
- The integration test skips unless `POLI_PAGE_API_KEY` is a `pp_sandbox_…` key.

### Added
- `polipagegin.WithoutErrorHandler()` option for `Middleware`.
- Contract tests: `example-app/contract_test.go` runs the integration cases of the Poli Page API contract (`poli-page/api-contract` v1.3.0 in CI) against the example app. CI also vets, lints, tests and builds `example-app/` on every leg of the Gin matrix, and Dependabot proposes each new contract version the next day.

### Removed
- **Breaking:** `polipage-render --template-version` and `--inline-template` (and with them the HTML output of inline mode). `--project` and `--template` are both required.
- Example app: `GET /render/preview` and its dashboard button, since the SDK no longer has `Render.Preview`. `GET /documents/:id/preview` (`Documents.Preview`) stays.

### Fixed
- `PDF` / `PDFStream`: control characters (CR/LF, TAB, DEL, C1) are now stripped from the `Content-Disposition` filename instead of surviving as `_` in the ASCII slot and `%0D%0A`-style escapes in the `filename*` slot; a filename made only of control characters falls back to `document.pdf`.

## [0.1.0] — 2026-06-01

### Added
- `polipagegin.Middleware(client)` — gin.HandlerFunc that attaches a `*polipage.Client` to the gin context. Pair with `polipagegin.ClientFrom(c)` inside handlers.
- `polipagegin.ClientFrom(c *gin.Context) *polipage.Client` — typed accessor for the per-request client; panics with a clear message when the middleware is not installed.
- Response helpers — `polipagegin.PDF(c, bytes, opts)`, `polipagegin.PDFStream(c, reader, opts)`, `polipagegin.Preview(c, html)`, `polipagegin.DocumentRedirect(c, descriptor)`. Each sets `Content-Type`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`, and an RFC 5987–encoded `Content-Disposition`.
- `polipagegin.PDFOptions{Filename, Inline}` — minimal struct for content-disposition shaping; `Inline: true` renders in the browser viewer, the default sends `attachment`.
- `polipagegin.PDFStream` runs a manual 32 KB read/write/flush loop instead of `c.DataFromReader` so bytes leave the process as they arrive, and sets `X-Accel-Buffering: no` so nginx (and convention-compliant proxies) forward the chunked stream instead of buffering it.
- `polipagegin.ErrorMiddleware()` — gin.HandlerFunc that converts a `*polipage.Error` queued in `c.Errors` into a typed JSON response (4xx pass-through, 5xx pass-through, network/timeout → 502 with `code: "NETWORK_ERROR"`). Opt-in.
- `polipagegin.WriteError(c, err)` — per-handler version of `ErrorMiddleware` for code paths that prefer explicit error rendering.
- `polipagegin.Config` + `polipagegin.FromEnv()` — typed configuration loader that reads `POLI_PAGE_API_KEY`, `POLI_PAGE_BASE_URL`, `POLI_PAGE_TIMEOUT`, `POLI_PAGE_MAX_RETRIES`, `POLI_PAGE_RETRY_DELAY` and validates them before constructing the SDK client.
- `cmd/polipage-render/` — smoke-test binary mirroring the Symfony bundle's `bin/console poli-page:render`. Flags use the `--template-version` form to avoid Cobra's reserved `--version`.
- Example Gin application at `example-app/` covering every SDK demo step, with the interactive dashboard at `GET /` matching the symfony-bundle aesthetic.
- CI matrix: Go `1.25` × Gin `v1.10`, plus a forward cell on `tip` (allowed to fail).

### Notes
- `github.com/poli-page/sdk-go` is consumed via `go get` once published. Until then, a `replace` directive in `go.mod` resolves it from `../sdk-go/`; see `docs/spec/gin-integration-specification.md` §12.
- The integration is server-side only and ships no Edge/wasm build.
- `gin.Recovery()` is a hard prerequisite: `polipagegin.ClientFrom` panics on misuse, and that panic must be caught by Gin's recovery middleware. `gin.Default()` includes it; `gin.New()` does not (you wire `r.Use(gin.Recovery())` yourself before `polipagegin.Middleware`).
- The example app sets `gin.SetMode(gin.ReleaseMode)`, registers `gin.Recovery` explicitly, calls `r.SetTrustedProxies(nil)`, and drains in-flight renders via `http.Server.Shutdown` on `SIGTERM`. Production-shape on purpose — consumers copy this `main.go`.

[Unreleased]: https://github.com/poli-page/gin/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/poli-page/gin/releases/tag/v0.1.0
