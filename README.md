# Poli Page for Gin

> Render Poli Page documents as Gin HTTP responses.

## About

This package wires the [Poli Page Go SDK](https://github.com/poli-page/sdk-go) into [Gin](https://gin-gonic.com/). You install a middleware that attaches a `*polipage.Client` to the gin context, return PDFs and HTML previews through typed response helpers that set the right headers, load configuration from environment variables, and get `*polipage.Error` answered as JSON by the error handler the middleware installs by default.

**When to use this:**

- You want a single `*polipage.Client` shared by every handler, plumbed through `*gin.Context` rather than a package-level singleton.
- You want PDF and HTML preview responses with the correct `Content-Type`, `Cache-Control`, `X-Content-Type-Options`, and RFC 5987 `Content-Disposition` headers without writing them by hand.
- You want `*polipage.Error` mapped to JSON responses by the middleware instead of a `switch` block in every handler.

**When not to:**

- You're not on Gin — install [`github.com/poli-page/sdk-go`](https://github.com/poli-page/sdk-go) directly. The SDK is framework-agnostic and works fine inside `net/http`, Chi, Echo, Fiber, or anything else.
- You need to reimplement transport, retries, or error mapping — that belongs in the SDK, not in a wrapper on top of it.

## Requirements

- Go `1.25+` (matches `github.com/poli-page/sdk-go`)
- The Poli Page Go SDK `github.com/poli-page/sdk-go` `v0.11.0` or later (environment API keys, no template versions)
- Gin `v1.10` (the module requires `v1.10.0`)
- A Poli Page API key from [app.poli.page](https://app.poli.page)

## Install

```bash
go get github.com/poli-page/gin github.com/poli-page/sdk-go@v0.11.0
```

Set your key in the environment:

```
POLI_PAGE_API_KEY=pp_sandbox_your_key_here
```

An API key is `pp_{environment}_{32 lowercase hex}`. It belongs to one environment of your organization, and that environment's published copy of the project is the one rendered:

| Environment                           | Key            | Watermark | Counted |
| ------------------------------------- | -------------- | --------- | ------- |
| `sandbox` (every org)                 | `pp_sandbox_…` | yes       | no      |
| `live` (every org, deletable on Pro+) | `pp_live_…`    | no        | yes     |
| created on Pro+ (e.g. `staging`)      | `pp_staging_…` | no        | yes     |

"Counted" means the render counts toward the organization's monthly PDF quota.

Verify everything is wired end-to-end with the bundled smoke-test binary:

```bash
go run github.com/poli-page/gin/cmd/polipage-render \
    --project=getting-started \
    --template=welcome \
    --data='{"name":"World"}' \
    -o welcome.pdf
```

## Quick start

Install the middleware, then return a PDF from a handler.

```go
// main.go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
	polipagegin "github.com/poli-page/gin"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	r := gin.Default()
	r.Use(polipagegin.Middleware(client))

	r.GET("/invoices/:id", invoiceHandler)

	log.Fatal(r.Run(":8080"))
}

func invoiceHandler(c *gin.Context) {
	id := c.Param("id")
	client := polipagegin.ClientFrom(c)

	pdf, err := client.Render.PDF(c.Request.Context(), polipage.RenderInput{
		Project:  "billing",
		Template: "invoice",
		Data:     map[string]any{"invoiceId": id},
	})
	if err != nil {
		_ = c.Error(err) // answered by the middleware's error handler
		return
	}

	polipagegin.PDF(c, pdf, polipagegin.PDFOptions{
		Filename: "invoice-" + id + ".pdf",
	})
	// → 200 application/pdf, Content-Disposition: attachment; filename="invoice-…"
	_ = http.StatusOK
}
```

## Configuration

You either construct the SDK client yourself with `option.With*` and pass it to `polipagegin.Middleware`, or call `polipagegin.FromEnv()` to load everything from the environment.

| Variable | Default | Description |
|---|---|---|
| `POLI_PAGE_API_KEY` | _required_ | Your API key, `pp_{environment}_…` (see the key table above). A missing or empty value is an error. Any other value is accepted: one that does not match `pp_{environment}_{32 lowercase hex}` logs one `log/slog` warning (it names the expected format and shows no part of the key, not even its `pp_{environment}_` part) and the API decides whether the key is valid (`401 INVALID_API_KEY`). |
| `POLI_PAGE_BASE_URL` | SDK default | Override the API origin. |
| `POLI_PAGE_TIMEOUT` | SDK default | Per-request timeout, parsed by `time.ParseDuration` (`30s`, `2m`). |
| `POLI_PAGE_MAX_RETRIES` | SDK default | Integer in `[0, 10]`. |
| `POLI_PAGE_RETRY_DELAY` | SDK default | Base retry delay, parsed by `time.ParseDuration`. |

```go
// main.go
cfg, err := polipagegin.FromEnv()
if err != nil {
	log.Fatal(err) // validation errors include the offending variable name
}
client := polipage.NewClient(cfg.Options()...)
```

`cfg.Options()` returns the `[]option.RequestOption` slice you pass to `polipage.NewClient` — handy when you want to combine env-loaded options with programmatic ones like `option.WithLogger`.

## API at a glance

| Symbol | Purpose |
|---|---|
| `polipagegin.Middleware(client, opts...)` | gin.HandlerFunc that attaches the SDK client to the gin context and answers a queued `*polipage.Error` (the error handler, on by default). |
| `polipagegin.WithoutErrorHandler()` | `Middleware` option that turns the error handler off. |
| `polipagegin.ClientFrom(c)` | Returns the per-request `*polipage.Client`; panics with a clear message if the middleware was not installed. |
| `polipagegin.PDF(c, bytes, opts)` | Writes a buffered PDF response with the right headers. |
| `polipagegin.PDFStream(c, reader, opts)` | Streams an `io.Reader` (typically from `Render.PDFStream`) to the client. |
| `polipagegin.Preview(c, html)` | Writes an HTML preview response (`text/html; charset=utf-8`, `no-store, private`). |
| `polipagegin.DocumentRedirect(c, descriptor)` | 302 redirect to a `DocumentDescriptor.PresignedPDFURL`. |
| `polipagegin.ErrorMiddleware()` | The error handler alone, for an app that turned it off in `Middleware` and installs it elsewhere. |
| `polipagegin.WriteError(c, err)` | Per-handler version of the error handler for code paths that prefer explicit error rendering. |
| `polipagegin.Config` / `polipagegin.FromEnv()` | Env-driven configuration loader. |
| `cmd/polipage-render` | Smoke-test binary that renders a template end-to-end. |

Full reference: [pkg.go.dev/github.com/poli-page/gin](https://pkg.go.dev/github.com/poli-page/gin).

## Errors

The SDK returns a single `*polipage.Error` type with predicate methods rather than separate error types per category. The taxonomy you typically discriminate on:

- **Auth** — `err.IsAuthError()` (HTTP 401/403, sentinels `polipage.ErrUnauthorized` / `polipage.ErrForbidden`).
- **Rate limit** — `err.IsRateLimitError()` (HTTP 429, sentinel `polipage.ErrRateLimit`).
- **Validation** — `err.IsValidationError()` (HTTP 400, sentinel `polipage.ErrValidation`). Template or data rejected.
- **Network / transport** — `err.IsNetworkError()` (connection, DNS, TLS, or timeout).

Handle them inline:

```go
// invoice_handler.go
import (
	"errors"

	"github.com/poli-page/sdk-go"
)

pdf, err := client.Render.PDF(c.Request.Context(), input)
if err != nil {
	var pe *polipage.Error
	if errors.As(err, &pe) {
		switch {
		case pe.IsAuthError():       c.AbortWithStatus(http.StatusUnauthorized)
		case pe.IsRateLimitError():  c.AbortWithStatus(http.StatusTooManyRequests)
		case pe.IsNetworkError():    c.AbortWithStatus(http.StatusBadGateway)
		default:                     c.AbortWithStatus(http.StatusInternalServerError)
		}
		return
	}
	c.AbortWithStatus(http.StatusInternalServerError)
	return
}
```

Or queue the error with `c.Error(err)` and let the error handler answer.

### Error handler

`polipagegin.Middleware(client)` installs an error handler by default. When a handler queues a `*polipage.Error` with `c.Error(err)` and writes no response, the middleware answers with `Content-Type: application/json` and the SDK's `err.ToPayload()` as the body, unchanged:

```json
{ "code": "TEMPLATE_NOT_FOUND", "message": "Template 'does-not-exist' not found in project 'getting-started'.", "status": 404, "requestId": "req_…" }
```

- The HTTP status is the payload's `status`: the API's status for an API error, `503` for `network_error` and `504` for `timeout`.
- An SDK error with no status (for example `aborted`) answers `500`, and the body keeps `"status": null`. `requestId` is `null` when the API sent none.
- `code` and `message` are the SDK's, verbatim.
- Only `*polipage.Error` is handled (the last error on `c.Errors`); any other error is left for your own error handling, and a response the handler already wrote is never overwritten. Headers set for another answer (`Content-Disposition`, `X-Accel-Buffering`, `Content-Length`, a PDF `Content-Type`) are dropped from the JSON answer.

Turn it off with one option:

```go
r.Use(polipagegin.Middleware(client, polipagegin.WithoutErrorHandler()))
```

`polipagegin.ErrorMiddleware()` is the same handler on its own, for when you turn it off in `Middleware` and want it somewhere else in the chain. `polipagegin.WriteError(c, err)` writes the same answer from inside a handler.

## Example app

A runnable Gin application that exercises every SDK demo step lives in [`example-app/`](example-app/). It serves an interactive dashboard at `http://127.0.0.1:8080/` with one button per SDK feature, inline `<iframe>` previews for PDFs, and pretty-printed JSON for stored-document operations. It reads `POLI_PAGE_API_KEY` (a `pp_sandbox_…` key) and, optionally, `POLI_PAGE_BASE_URL`, and renders the `getting-started` project's `welcome` template. It serves the demo routes at the root: `GET /render/pdf` answers the PDF, `GET /documents/{id}` answers a `302` redirect to the document's presigned PDF URL, `GET /documents/{id}/preview`, `GET /documents/{id}/thumbnails` and `DELETE /documents/{id}` call the matching SDK methods, and `GET /errors/not-found` renders the `does-not-exist` template and answers `404` with code `TEMPLATE_NOT_FOUND` through the default error handler. Use a fresh organization: one that renamed, deleted or removed `getting-started` from `sandbox` gets `404 PROJECT_NOT_FOUND` or `404 PROJECT_NOT_IN_ENVIRONMENT`. The app also reads a `.env` file from its working directory, without overriding variables already set.

```bash
cd example-app
go run .
# http://127.0.0.1:8080
```

## Going further

- [docs/middleware.md](docs/middleware.md) — `Middleware` lifecycle, `ClientFrom` semantics, multiple-client patterns, request-scoped context propagation.
- [docs/responses.md](docs/responses.md) — All four response helpers, their headers, and the RFC 5987 filename encoding.
- [docs/streaming.md](docs/streaming.md) — Stream multi-MB PDFs through `Render.PDFStream` and `polipagegin.PDFStream`.
- [docs/events.md](docs/events.md) — Wire `option.WithOnRetry` / `option.WithOnError` into your logger, metrics, or `gin.Context` for per-request audit logs.
- [docs/cli.md](docs/cli.md) — Every `polipage-render` flag, exit codes, and CI recipes.
- [docs/testing.md](docs/testing.md) — Stub the SDK client behind a domain interface so handler tests never touch the network.

## Compatibility

| Package | Gin | Go |
|---|---|---|
| `v0.11.x` | `v1.10` | `1.25+` |

`go.mod` requires Gin `v1.10.0`, so Go's minimal version selection never picks an older Gin for your build. CI also runs the suite against `v1.9.1` by downgrading inside this repository.

The supported Go versions follow the upstream policy of "two latest stable releases".

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

### Contract tests

`example-app/contract_test.go` runs the integration cases (`impl.*`) of the [Poli Page API contract](https://github.com/poli-page/api-contract) against the example app, with its SDK pointed at a local server. CI runs them against the pinned contract version. Locally, from `example-app/`:

```bash
POLI_CONTRACT=<path to contract.json> go test ./...
```

Without `POLI_CONTRACT` the test is skipped (and fails when `CI` is set). Cases allowed to fail are listed, with the reason, in `example-app/testdata/contract-skip.json`.

## License

Released under the [MIT License](LICENSE).
