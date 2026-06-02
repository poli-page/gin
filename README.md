# Poli Page for Gin

> Render Poli Page documents as Gin HTTP responses.

## About

This package wires the [Poli Page Go SDK](https://github.com/poli-page/sdk-go) into [Gin](https://gin-gonic.com/). You install a middleware that attaches a `*polipage.Client` to the gin context, return PDFs and HTML previews through typed response helpers that set the right headers, load configuration from environment variables, and translate `*polipage.Error` into JSON responses with an opt-in error middleware.

**When to use this:**

- You want a single `*polipage.Client` shared by every handler, plumbed through `*gin.Context` rather than a package-level singleton.
- You want PDF and HTML preview responses with the correct `Content-Type`, `Cache-Control`, `X-Content-Type-Options`, and RFC 5987 `Content-Disposition` headers without writing them by hand.
- You want `*polipage.Error` mapped to JSON responses with one middleware instead of a `switch` block in every handler.

**When not to:**

- You're not on Gin — install [`github.com/poli-page/sdk-go`](https://github.com/poli-page/sdk-go) directly. The SDK is framework-agnostic and works fine inside `net/http`, Chi, Echo, Fiber, or anything else.
- You need to reimplement transport, retries, or error mapping — that belongs in the SDK, not in a wrapper on top of it.

## Requirements

- Go `1.25+` (matches `github.com/poli-page/sdk-go`)
- Gin `v1.10` (also supports `v1.9`)
- A Poli Page API key from [app.poli.page](https://app.poli.page)

## Install

```bash
go get github.com/poli-page/gin
```

Set your key in the environment:

```
POLI_PAGE_API_KEY=pp_test_your_key_here
```

Verify everything is wired end-to-end with the bundled smoke-test binary:

```bash
go run github.com/poli-page/gin/cmd/polipage-render \
    --project=getting-started \
    --template=welcome \
    --template-version=1.0.0 \
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

	pdf, err := client.Render.PDF(c.Request.Context(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"invoiceId": id},
	})
	if err != nil {
		polipagegin.WriteError(c, err)
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
| `POLI_PAGE_API_KEY` | _required_ | Key starting with `pp_test_` or `pp_live_`. |
| `POLI_PAGE_BASE_URL` | SDK default | Override the API origin (e.g. `https://api-develop.poli.page`). |
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
| `polipagegin.Middleware(client)` | gin.HandlerFunc that attaches the SDK client to the gin context. |
| `polipagegin.ClientFrom(c)` | Returns the per-request `*polipage.Client`; panics with a clear message if the middleware was not installed. |
| `polipagegin.PDF(c, bytes, opts)` | Writes a buffered PDF response with the right headers. |
| `polipagegin.PDFStream(c, reader, opts)` | Streams an `io.Reader` (typically from `Render.PDFStream`) to the client. |
| `polipagegin.Preview(c, html)` | Writes an HTML preview response (`text/html; charset=utf-8`, `no-store, private`). |
| `polipagegin.DocumentRedirect(c, descriptor)` | 302 redirect to a `DocumentDescriptor.PresignedPDFURL`. |
| `polipagegin.ErrorMiddleware()` | gin.HandlerFunc that converts any `*polipage.Error` queued in `c.Errors` into a typed JSON response. |
| `polipagegin.WriteError(c, err)` | Per-handler version of `ErrorMiddleware` for code paths that prefer explicit error rendering. |
| `polipagegin.Config` / `polipagegin.FromEnv()` | Env-driven configuration loader. |
| `cmd/polipage-render` | Smoke-test binary that renders a template end-to-end. |

Full reference: [pkg.go.dev/github.com/poli-page/gin](https://pkg.go.dev/github.com/poli-page/gin).

## Errors

The SDK returns a single `*polipage.Error` type with predicate methods rather than separate error types per category. The taxonomy you typically discriminate on:

- **Auth** — `err.IsAuthError()` (HTTP 401/403, sentinels `polipage.ErrUnauthorized` / `polipage.ErrForbidden`).
- **Rate limit** — `err.IsRateLimitError()` (HTTP 429, sentinel `polipage.ErrRateLimit`).
- **Validation** — `err.IsValidationError()` (HTTP 400, sentinel `polipage.ErrValidation`). Template, data, or version rejected.
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

Or install `polipagegin.ErrorMiddleware()` once and skip the boilerplate — 4xx/5xx pass through unchanged and network/timeout becomes a 502 with `code: "NETWORK_ERROR"`. The middleware only transforms `*polipage.Error` queued in `c.Errors`; other failures fall through to whatever you have downstream.

## Example app

A runnable Gin application that exercises every SDK demo step lives in [`example-app/`](example-app/). It serves an interactive dashboard at `http://127.0.0.1:8080/` with one button per SDK feature, inline `<iframe>` previews for PDFs, and pretty-printed JSON for stored-document operations.

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
| `0.1.x` | `v1.9` / `v1.10` | `1.25+` |

The supported Go versions follow the upstream policy of "two latest stable releases".

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Released under the [MIT License](LICENSE).
