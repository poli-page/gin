# Changelog

All notable changes to `poli-page/gin` are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.11.0] - 2026-10-30

First release. The version matches the `github.com/poli-page/sdk-go` `v0.11.0` release it requires. Entries marked **Breaking** change behaviour of the pre-release code on `main`; no `0.1.0` was ever tagged.

### Added
- `polipagegin.Middleware(client)` — gin.HandlerFunc that attaches a `*polipage.Client` to the gin context. Pair with `polipagegin.ClientFrom(c)` inside handlers.
- `polipagegin.ClientFrom(c *gin.Context) *polipage.Client` — typed accessor for the per-request client; panics with a clear message when the middleware is not installed.
- Response helpers — `polipagegin.PDF(c, bytes, opts)`, `polipagegin.PDFStream(c, reader, opts)`, `polipagegin.Preview(c, html)`, `polipagegin.DocumentRedirect(c, descriptor)`. Each sets `Content-Type`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`, and an RFC 5987–encoded `Content-Disposition`.
- `polipagegin.PDFOptions{Filename, Inline}` — minimal struct for content-disposition shaping; `Inline: true` renders in the browser viewer, the default sends `attachment`.
- `polipagegin.PDFStream` runs a manual 32 KB read/write/flush loop instead of `c.DataFromReader` so bytes leave the process as they arrive, and sets `X-Accel-Buffering: no` so nginx (and convention-compliant proxies) forward the chunked stream instead of buffering it.
- `polipagegin.ErrorMiddleware()` — gin.HandlerFunc that converts a `*polipage.Error` queued in `c.Errors` into a JSON response. `Middleware(client)` installs the same error handler by default (see Changed); `ErrorMiddleware` places it elsewhere in the chain when it was turned off with `WithoutErrorHandler()`.
- `polipagegin.WriteError(c, err)` — per-handler version of `ErrorMiddleware` for code paths that prefer explicit error rendering.
- `polipagegin.Config` + `polipagegin.FromEnv()` — typed configuration loader that reads `POLI_PAGE_API_KEY`, `POLI_PAGE_BASE_URL`, `POLI_PAGE_TIMEOUT`, `POLI_PAGE_MAX_RETRIES`, `POLI_PAGE_RETRY_DELAY` and validates them before constructing the SDK client.
- `cmd/polipage-render/` — smoke-test binary mirroring the Symfony bundle's `bin/console poli-page:render`.
- Example Gin application at `example-app/` covering every SDK demo step, with the interactive dashboard at `GET /` matching the symfony-bundle aesthetic.
- CI matrix: Go `1.25` × Gin `v1.9.1` and `v1.10.0`, plus a nightly run on Go `tip`.
- `polipagegin.WithoutErrorHandler()` option for `Middleware`.
- Contract tests: `example-app/contract_test.go` runs the integration cases of the Poli Page API contract (`poli-page/api-contract` v1.3.0 in CI) against the example app. CI also vets, lints, tests and builds `example-app/` on every leg of the Gin matrix, and Dependabot proposes each new contract version the next day.

### Changed
- CI: the live API job runs on `main` (and on demand) against the production API with the sandbox key from the `POLI_PAGE_API_KEY` secret, and skips with a notice when the key is absent or not a `pp_sandbox_` key.
- **Breaking:** API keys are `pp_{environment}_{32 lowercase hex}` for any environment (`pp_sandbox_…`, `pp_live_…`, `pp_staging_…`). `FromEnv` no longer rejects a key by its `pp_test_` / `pp_live_` prefix: a missing or empty `POLI_PAGE_API_KEY` is still an error, and any other value that does not match the key format logs one `log/slog` warning (it names the expected format and shows no part of the key, not even its `pp_{environment}_` part) and loads, so the API answers with its own error.
- **Breaking:** requires `github.com/poli-page/sdk-go` `v0.11.0`, the SDK release with environments: renders take a `polipage.RenderInput` with no `Version`, and the key selects the environment whose published copy is rendered. Both `go.mod` files require the published module instead of a `replace` to a sibling `../sdk-go` checkout.
- **Breaking:** `Middleware(client)` installs the error handler by default. A `*polipage.Error` queued with `c.Error(err)` is answered with `Content-Type: application/json`, the error's `ToPayload()` as the body (`{code, message, status, requestId}`, `null` when unknown) and its `status` as the HTTP status (`503` for `network_error`, `504` for `timeout`, `500` when the payload has no status). Turn it off with `Middleware(client, polipagegin.WithoutErrorHandler())`. A response the handler already wrote is never overwritten.
- **Breaking:** `ErrorMiddleware` and `WriteError` write the same body: an error with no status now answers `500` with `"status": null` in the body instead of `"status": 500`, and `requestId` is `null` instead of `""` when the API sent none. The JSON answer drops headers set for another answer (`Content-Disposition`, `X-Accel-Buffering`, `Content-Length`) and always has `Content-Type: application/json`.
- **Breaking:** example app: the demo routes move from `/api/...` to the root (`GET /render/pdf`, `GET /documents/{id}`, `GET /errors/not-found`, ...). `GET /errors/not-found` replaces `GET /api/render/error`: it renders the `does-not-exist` template of `getting-started` with `{}` and answers `404` with code `TEMPLATE_NOT_FOUND` through the default error handler. `GET /documents/{id}` still answers a `302` redirect to the presigned PDF URL. The app reads `.env` from its working directory.
- The integration test skips unless `POLI_PAGE_API_KEY` is a `pp_sandbox_…` key.

### Removed
- **Breaking:** `polipage-render --template-version` and `--inline-template` (and with them the HTML output of inline mode). `--project` and `--template` are both required.
- Example app: `GET /render/preview` and its dashboard button, since the SDK no longer has `Render.Preview`. `GET /documents/:id/preview` (`Documents.Preview`) stays.

### Fixed
- `PDFStream`: a read or write failure after the first chunk no longer ends the chunked body cleanly, which made a truncated PDF look like a complete download. The helper queues the error and closes the HTTP/1.x connection, so the client sees an incomplete transfer; on a writer that cannot be hijacked (HTTP/2, a `httptest.ResponseRecorder`, or a writer wrapper without `Unwrap` whose `Hijack` fails or panics) it panics with `http.ErrAbortHandler`, which `gin.Recovery()` catches. Tests that drive a mid-stream failure through `httptest.NewRecorder` or `gin.CreateTestContext` now see that panic unless a recovery middleware catches it: recover it or assert it. Any other panic from a wrapper's `Hijack` is passed on unchanged.
- `PDFStream` reads the first chunk before it sets any header. A failure on that first read (for example the SDK's `DOWNLOAD_FAILED`) is queued with nothing set, so the error handler answers it as JSON instead of JSON under `Content-Type: application/pdf` and `Content-Disposition: attachment`.
- `PDF` / `PDFStream`: control characters (CR/LF, TAB, DEL, C1) are now stripped from the `Content-Disposition` filename instead of surviving as `_` in the ASCII slot and `%0D%0A`-style escapes in the `filename*` slot; a filename made only of control characters falls back to `document.pdf`.

### Notes
- The integration is server-side only and ships no Edge/wasm build.
- `gin.Recovery()` is a hard prerequisite: `polipagegin.ClientFrom` panics on misuse, and that panic must be caught by Gin's recovery middleware. `gin.Default()` includes it; `gin.New()` does not (you wire `r.Use(gin.Recovery())` yourself before `polipagegin.Middleware`).
- The example app sets `gin.SetMode(gin.ReleaseMode)`, registers `gin.Recovery` explicitly, calls `r.SetTrustedProxies(nil)`, and drains in-flight renders via `http.Server.Shutdown` on `SIGTERM`. Production-shape on purpose — consumers copy this `main.go`.

[Unreleased]: https://github.com/poli-page/gin/compare/v0.11.0...HEAD
[0.11.0]: https://github.com/poli-page/gin/releases/tag/v0.11.0
