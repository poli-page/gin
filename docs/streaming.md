# Streaming large PDFs

> Use `client.Render.PDFStream` + `polipagegin.PDFStream` to write chunks to the response as the SDK reads them from the API, instead of buffering the whole document in process memory.

## Why

`Render.PDF` returns `[]byte` — fine for a 200 KB invoice, wasteful for a 20 MB book. Each goroutine handling a render sits on the full payload until the response finishes flushing. `Render.PDFStream` returns an `io.ReadCloser` that yields chunks as the SDK receives them from the upstream API; `polipagegin.PDFStream` writes them straight to `c.Writer` with a 32 KB read buffer and calls `http.Flusher.Flush()` after every chunk so bytes leave the process as they arrive — chunked transfer encoding, no per-render buffer growth, and no waiting for a reverse-proxy buffer to fill.

## How

Pull the stream from the SDK, defer the close, hand it to the helper.

```go
// big_book_handler.go
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/poli-page/sdk-go"
	polipagegin "github.com/poli-page/gin"
)

func bigBookHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)

	body, err := client.Render.PDFStream(c.Request.Context(), polipage.RenderInput{
		Project:  "books",
		Template: "manuscript",
		Data:     map[string]any{"slug": c.Param("slug")},
	})
	if err != nil {
		polipagegin.WriteError(c, err)
		return
	}
	defer body.Close()

	polipagegin.PDFStream(c, body, polipagegin.PDFOptions{
		Filename: c.Param("slug") + ".pdf",
		Inline:   true,
	})
}
```

`PDFStream` writes status 200 with `Content-Type: application/pdf`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`, the same RFC 5987 `Content-Disposition` as buffered `PDF`, plus `X-Accel-Buffering: no` so nginx (and proxies that honour the convention) forwards bytes as they arrive instead of buffering the full response. The body is chunked transfer-encoded — no `Content-Length` — and the helper calls `Flush()` after every 32 KB chunk so the bytes actually reach the wire.

### Inline preview vs download

`Inline: true` renders inside the browser's PDF viewer; the default (`Inline: false`) sends `Content-Disposition: attachment` and the browser saves the file. The filename encoding is identical to the buffered case — see [docs/responses.md](responses.md#rfc-5987-filename-encoding).

```go
// preview_handler.go
polipagegin.PDFStream(c, body, polipagegin.PDFOptions{
	Filename: "facture-élève.pdf",
})
// Content-Disposition: attachment;
//   filename="facture-_l_ve.pdf";
//   filename*=UTF-8''facture-%C3%A9l%C3%A8ve.pdf
```

### Render-and-store-then-stream

When the document is also going to disk or to an audit trail, the SDK's `Render.Document` returns a `*DocumentDescriptor` with a presigned URL and metadata. Save the descriptor's `DocumentID`, redirect the client to the presigned URL, and skip the stream entirely:

```go
// invoice_handler.go
descriptor, err := client.Render.Document(c.Request.Context(), input)
if err != nil {
	polipagegin.WriteError(c, err)
	return
}
go audit.Save(descriptor.DocumentID)
polipagegin.DocumentRedirect(c, descriptor)
```

This is cheaper than streaming when the document was already produced server-side — the browser fetches bytes straight from object storage.

## Gotchas

- **The handler owns the reader's lifetime.** `polipagegin.PDFStream` takes `io.Reader`, not `io.ReadCloser` — `defer body.Close()` lives in the handler right after the SDK call. The symfony-bundle and NestJS packages made the same call (caller owns the stream); replicating it here keeps mistakes obvious.

- **The stream is consumed once.** `Render.PDFStream` returns a single-reader `io.ReadCloser`; you cannot retry the response after partial bytes have been written. A mid-stream read or write error is queued on `c.Errors` and the connection is closed without ending the chunked body, so the client sees a truncated transfer (`unexpected EOF`) rather than a complete-looking PDF. The SDK throws **before** returning the reader when the upstream rejects the request, so a successful `PDFStream` call already means "API said yes".

- **No `Content-Length`.** Chunked transfer encoding is the cost of streaming. Clients that need the size up-front (some legacy PDF viewers, range requests) should use buffered `Render.PDF` + `polipagegin.PDF`.

- **Reverse proxies that buffer kill the benefit.** `PDFStream` sets `X-Accel-Buffering: no` which nginx honours per-response; Cloudflare's "auto-buffering" on Pro+ plans ignores it and buffers anyway (disable in the dashboard for the route, or use the Cloudflare API). Behind any other proxy, check its docs — if you can't get per-response opt-out, set `proxy_buffering off` globally on the streaming-route location block. If you can't disable buffering anywhere in the chain, buffered `Render.PDF` + `polipagegin.PDF` is the saner default; "streaming through a buffering proxy" is the worst of both worlds.

- **`http.Flusher` is not always usable.** Gin's `gin.ResponseWriter` interface always embeds `http.Flusher`, so the type assertion never fails — but its `Flush()` delegates to the underlying writer, which on rare custom-middleware setups may not be a Flusher itself. In every realistic deployment (Go's `net/http` writer, `httptest.NewRecorder`, Gin's default test recorder) the flush works. If you've installed a writer-wrapping middleware and streaming feels laggy, that's the first place to look.

- **Cancellation propagates correctly only if you pass `c.Request.Context()`.** When the browser closes the connection, Gin cancels the request context; the SDK aborts the upstream request, its next read fails with `aborted`, and `PDFStream` queues that error and closes the connection. Passing `context.Background()` instead leaves the goroutine reading from the upstream until the SDK timeout fires.

- **A mid-stream error cannot be surfaced as a clean JSON error.** Once the helper has written status 200 and the first chunk, you cannot upgrade to a 500 — the headers are gone. The helper records the error via `c.Error(err)`, then cuts the response: it hijacks and closes the HTTP/1.x connection, so the client's read fails instead of ending cleanly. It looks through gin's writer and any wrapper that implements `Unwrap`; a wrapper without `Unwrap` (gin-contrib/gzip, for example) is hijacked through its own `Hijack`. When the connection cannot be hijacked (HTTP/2, a `httptest.ResponseRecorder`, or such a wrapper whose `Hijack` fails or panics) it panics with `http.ErrAbortHandler`, which makes `net/http` reset the stream. `gin.Recovery()` catches that panic and finishes the response normally, so behind Recovery an HTTP/2 stream cut mid-way ends as a short `200`, and Gin v1.9/v1.10 Recovery logs a stack trace for it. On that panic path, middleware code after `c.Next()` (such as `gin.Logger` or your own logging of `c.Errors`) runs only if a recovery middleware sits inside it. In your own tests, a mid-stream failure through `httptest.NewRecorder` or `gin.CreateTestContext` panics with `http.ErrAbortHandler`: recover it or assert the panic. If that matters, compare the downloaded size with the document's `sizeBytes`. Plan for this when designing alerts. A failure on the very first read is different: `PDFStream` reads the first chunk before it sets any header, so that error (for example `DOWNLOAD_FAILED`) is queued with nothing written, and the middleware's error handler answers it as JSON with no PDF headers.

## Related

- [README → API at a glance](../README.md#api-at-a-glance) — the parent section this deep-dive expands.
- [docs/responses.md](responses.md) — buffered `PDF`, `Preview`, and `DocumentRedirect` for the cases streaming is overkill.
- [docs/middleware.md](middleware.md) — how the client gets to the handler in the first place.
- Gin docs on [`Context.Writer`](https://pkg.go.dev/github.com/gin-gonic/gin#ResponseWriter) — the `http.Flusher`-aware writer `PDFStream` writes through. (`Context.DataFromReader` would be the idiomatic shortcut but it does not flush between chunks; `PDFStream` runs its own 32 KB Read/Write/Flush loop instead.)
