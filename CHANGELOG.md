# Changelog

All notable changes to `poli-page/gin` are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — TBD

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
