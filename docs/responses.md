# Responses

> `polipagegin.PDF`, `PDFStream`, `Preview`, and `DocumentRedirect` write Gin responses with the headers Poli Page expects — `Content-Type`, `Cache-Control: no-store, private`, `X-Content-Type-Options: nosniff`, and an RFC 5987–encoded `Content-Disposition`.

## Why

Returning a PDF from a Gin handler is two lines if you only care about the body and four-to-ten lines once you remember the headers: `Content-Type: application/pdf`, a Content-Disposition with both `filename="…"` (ASCII fallback) and `filename*=UTF-8''…` (the percent-encoded UTF-8 name browsers actually display), `Cache-Control` to keep proxies from caching tenant-scoped documents, and `X-Content-Type-Options: nosniff` to stop browsers from second-guessing the type. The response helpers encode all of that once, the same way the symfony-bundle's `PoliPageResponseFactory` does and the NestJS package's `PoliPagePdfFile` does, so handlers stay focused on the render call.

## How

### `PDF(c, bytes, opts)`

Send a buffered PDF — what you have after `client.Render.PDF(...)`.

```go
// invoice_handler.go
pdf, err := client.Render.PDF(c.Request.Context(), input)
if err != nil {
	polipagegin.WriteError(c, err)
	return
}
polipagegin.PDF(c, pdf, polipagegin.PDFOptions{
	Filename: "invoice-" + id + ".pdf",
})
// → 200 OK
// → Content-Type: application/pdf
// → Content-Disposition: attachment; filename="invoice-42.pdf"
// → Cache-Control: no-store, private
// → X-Content-Type-Options: nosniff
```

Set `Inline: true` to display the document in the browser's built-in viewer instead of triggering a download.

```go
// preview_handler.go
polipagegin.PDF(c, pdf, polipagegin.PDFOptions{
	Filename: "preview.pdf",
	Inline:   true,
})
// → Content-Disposition: inline; filename="preview.pdf"
```

### `PDFStream(c, reader, opts)`

Stream straight from `Render.PDFStream` without buffering the document into memory.

```go
// big_book_handler.go
body, err := client.Render.PDFStream(c.Request.Context(), input)
if err != nil {
	polipagegin.WriteError(c, err)
	return
}
defer body.Close()

polipagegin.PDFStream(c, body, polipagegin.PDFOptions{
	Filename: "book.pdf",
	Inline:   true,
})
```

`PDFStream` accepts any `io.Reader`. The caller owns the lifecycle of an `io.ReadCloser` — `defer body.Close()` lives in the handler, not in the helper. See [docs/streaming.md](streaming.md) for the full discussion.

### `Preview(c, html)`

Render the HTML preview from `Render.Preview(...).HTML` (or `Documents.Preview(...).HTML`) with the right headers.

```go
// preview_handler.go
result, err := client.Render.Preview(c.Request.Context(), input)
if err != nil {
	polipagegin.WriteError(c, err)
	return
}
polipagegin.Preview(c, result.HTML)
// → 200 OK
// → Content-Type: text/html; charset=utf-8
// → Cache-Control: no-store, private
// → X-Content-Type-Options: nosniff
```

### `DocumentRedirect(c, descriptor)`

302 the client to the descriptor's `PresignedPDFURL` (presigned URLs have a ~15 minute TTL — long enough for the browser to follow once, short enough to avoid sharing).

```go
// document_handler.go
descriptor, err := client.Documents.Get(c.Request.Context(), id)
if err != nil {
	polipagegin.WriteError(c, err)
	return
}
polipagegin.DocumentRedirect(c, descriptor)
// → 302 Found
// → Location: https://storage.poli.page/...?X-Amz-Signature=…
// → Cache-Control: no-store, private
```

The handler does not buffer the PDF — the browser fetches it straight from object storage.

### RFC 5987 filename encoding

`Filename` is encoded in two slots inside `Content-Disposition`:

```go
// pdf_options.go
polipagegin.PDFOptions{Filename: "facture-élève.pdf"}
// → Content-Disposition: attachment;
//      filename="facture-_l_ve.pdf";
//      filename*=UTF-8''facture-%C3%A9l%C3%A8ve.pdf
```

- `filename="…"` — ASCII fallback for clients that don't speak RFC 5987 (rare in 2026, kept for parity with the symfony-bundle).
- `filename*=UTF-8''…` — the actual UTF-8 name browsers display. Encoded with `url.PathEscape` on a per-character basis, matching the encoding the Symfony helper produces.

Empty `Filename` defaults to `document.pdf`.

## Gotchas

- **`PDFStream` does not close the reader.** Helper takes `io.Reader` on purpose so it does not invade `io.ReadCloser` semantics — `defer body.Close()` belongs in the handler immediately after the SDK call. See spec §8.3 and the symfony-bundle's matching decision.

- **`Cache-Control: no-store, private` is non-negotiable.** Poli Page documents are tenant-scoped; a shared proxy must never cache them. Override by writing your own headers *before* calling the helper (the helper uses `c.Writer.Header().Set` which does not overwrite a header that downstream middleware has already set unless… don't, the default is the safe one).

- **`Content-Length` is set for buffered responses, not for streams.** `PDF` knows the byte length up front; `PDFStream` uses chunked transfer encoding. If a client needs `Content-Length` (some old PDF viewers, range requests), use `PDF` instead.

- **Don't combine `polipagegin.PDF` with `c.JSON` in the same handler.** Both write response bodies and headers; whichever runs second corrupts the response. Pick one path per handler and `return` immediately after.

- **The helpers set status 200 unconditionally.** Setting `c.Status(http.StatusCreated)` *before* `polipagegin.PDF` is silently overwritten. If you genuinely need a non-200 PDF response (very rare — `Render.PDF` either returns the document or an error), write the headers yourself with `c.Header(...)` + `c.Data(status, "application/pdf", bytes)`.

- **Don't put PDF routes behind `gin-contrib/gzip`.** PDFs are already deflate-compressed inside the container; running them through HTTP gzip costs CPU for ~0 bytes saved and can confuse PDF viewers that read `Content-Encoding`. If you install gzip globally, exclude PDF routes via the middleware's `gzip.WithExcludedExtensions([]string{".pdf"})` or `gzip.WithExcludedPaths([]string{"/invoices"})` filter. The same rule applies to `Content-Encoding`-rewriting reverse proxies (Cloudflare's auto-minify, nginx `gzip on` on PDF MIME types).

## Related

- [README → API at a glance](../README.md#api-at-a-glance) — the parent listing.
- [docs/streaming.md](streaming.md) — when to pick `PDFStream` over `PDF`.
- [docs/middleware.md](middleware.md) — how the client gets to the handler in the first place.
- [docs/spec/gin-integration-specification.md](spec/gin-integration-specification.md) §8 — the exhaustive header contract these helpers implement.
