package polipagegin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	polipage "github.com/poli-page/sdk-go"
)

// PDFOptions controls Content-Disposition for the PDF and PDFStream helpers.
type PDFOptions struct {
	// Filename is the filename presented to the User-Agent. Empty defaults
	// to "document.pdf".
	Filename string
	// Inline picks "inline" instead of "attachment". Useful when the
	// response is rendered inside an <iframe> (the demo dashboard does
	// this) so the browser views the PDF in place instead of triggering
	// a download.
	Inline bool
}

// PDF writes a buffered PDF response per spec §8.1: status 200,
// Content-Type application/pdf, Content-Length, Cache-Control
// "no-store, private", X-Content-Type-Options nosniff, and an RFC 5987
// Content-Disposition (attachment by default, inline when
// opts.Inline is true).
//
// Status is set unconditionally to 200; a c.Status(...) call before PDF
// is silently overwritten. Callers that need a non-200 PDF response (rare)
// must roll the headers and body by hand.
func PDF(c *gin.Context, body []byte, opts PDFOptions) {
	writePDFHeaders(c, opts)
	c.Header("Content-Length", strconv.Itoa(len(body)))
	c.Data(http.StatusOK, pdfContentType, body)
}

// Preview writes an HTML preview response per spec §8.2: status 200,
// Content-Type "text/html; charset=utf-8", Cache-Control "no-store, private",
// X-Content-Type-Options nosniff. No Content-Disposition — the response
// is meant to be rendered, not downloaded.
func Preview(c *gin.Context, html string) {
	c.Header("Cache-Control", noStorePrivate)
	c.Header("X-Content-Type-Options", noSniff)
	c.Data(http.StatusOK, htmlContentType, []byte(html))
}

// DocumentRedirect issues a 302 redirect to descriptor.PresignedPDFURL,
// with Cache-Control "no-store, private". Useful when the caller wants
// the browser to fetch the PDF directly from object storage, saving a
// hop through the application.
//
// The presigned URL has a ~15-minute TTL; once expired, re-fetch the
// descriptor via Documents.Get to refresh.
//
// No body is written: a 302 + Location is sufficient, and a body would
// just add latency to a response the browser never displays.
func DocumentRedirect(c *gin.Context, descriptor *polipage.DocumentDescriptor) {
	c.Header("Cache-Control", noStorePrivate)
	c.Header("Location", descriptor.PresignedPDFURL)
	c.Status(http.StatusFound)
	c.Writer.WriteHeaderNow()
}

// PDFStream writes a chunked PDF response per spec §8.1 + §8.3 — same
// headers as PDF plus X-Accel-Buffering: no (opt nginx-style proxies out
// of response buffering), and no Content-Length (the response is chunked).
// Reads from body 32 KB at a time, writing and flushing each chunk so
// bytes leave the process as they arrive rather than sitting in a
// buffer until EOF.
//
// PDFStream does NOT call body.Close() — the handler that obtained the
// io.ReadCloser from client.Render.PDFStream is responsible for
// `defer body.Close()` before invoking this helper (spec §8.3). The
// signature takes io.Reader (not io.ReadCloser) to advertise that policy.
//
// Passing a nil reader panics: a nil reader is a programmer error, not
// a runtime fallback. The panic message says so; CLAUDE.md §10.1's
// gin.Recovery prerequisite turns it into a 500 in the unlikely event
// it slips into production.
//
// Mid-stream failures (Read returns a non-EOF error, or Write fails) are
// attached to c.Errors via c.Error(...) so downstream logging middleware
// observes them. The response is partial at that point — bytes
// successfully written already reached the wire — and the helper returns
// without writing anything further.
func PDFStream(c *gin.Context, body io.Reader, opts PDFOptions) {
	if body == nil {
		panic("polipagegin: PDFStream called with nil reader")
	}
	writePDFHeaders(c, opts)
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// gin.ResponseWriter unconditionally implements http.Flusher (its
	// Flush() delegates to the underlying writer). The nil-check is a
	// defensive guard for the spec §8.3 "exotic wrapper hides it" case.
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

// Header values shared across helpers. Centralised so the values can be
// audited in one place and the spec §8 contract stays consistent.
const (
	pdfContentType  = "application/pdf"
	htmlContentType = "text/html; charset=utf-8"
	noStorePrivate  = "no-store, private"
	noSniff         = "nosniff"

	// pdfStreamChunk is the Read/Write/Flush chunk size for PDFStream.
	// Matches io.Copy's default and amortises the per-flush syscall cost
	// across a sensible byte count (spec §8.3).
	pdfStreamChunk = 32 * 1024
)

// writePDFHeaders sets the headers shared by PDF and PDFStream per spec
// §8.1 (excluding Content-Length, which PDF sets explicitly and PDFStream
// omits because the response is chunked; and X-Accel-Buffering, which is
// streaming-only).
func writePDFHeaders(c *gin.Context, opts PDFOptions) {
	c.Header("Content-Type", pdfContentType)
	c.Header("Cache-Control", noStorePrivate)
	c.Header("X-Content-Type-Options", noSniff)
	c.Header("Content-Disposition", contentDisposition(opts.Filename, opts.Inline))
}
