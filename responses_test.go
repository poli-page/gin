package polipagegin_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newResponseTestContext builds a *gin.Context backed by a recorder, with
// a non-nil Request so helpers that read c.Request (e.g. c.Redirect) do
// not panic.
func newResponseTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	return c, w
}

func TestPDF_SetsAllHeadersAndBody(t *testing.T) {
	body := []byte("%PDF-1.7\n...fake pdf bytes...")
	c, w := newResponseTestContext()

	polipagegin.PDF(c, body, polipagegin.PDFOptions{Filename: "invoice-42.pdf"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, strconv.Itoa(len(body)), w.Header().Get("Content-Length"))
	assert.Equal(t,
		`attachment; filename="invoice-42.pdf"; filename*=UTF-8''invoice-42.pdf`,
		w.Header().Get("Content-Disposition"))
	assert.Equal(t, body, w.Body.Bytes())
}

func TestPDF_InlineDispositionWhenRequested(t *testing.T) {
	c, w := newResponseTestContext()

	polipagegin.PDF(c, []byte("data"), polipagegin.PDFOptions{
		Filename: "invoice-42.pdf",
		Inline:   true,
	})

	assert.True(t,
		strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline; "),
		"Inline: true must produce a Content-Disposition starting with 'inline; ', got %q",
		w.Header().Get("Content-Disposition"))
}

func TestPDF_EmptyFilenameDefaultsToDocumentPDF(t *testing.T) {
	c, w := newResponseTestContext()

	polipagegin.PDF(c, []byte("data"), polipagegin.PDFOptions{})

	assert.Equal(t,
		`attachment; filename="document.pdf"; filename*=UTF-8''document.pdf`,
		w.Header().Get("Content-Disposition"))
}

func TestPreview_SetsAllHeadersAndBody(t *testing.T) {
	html := "<h1>Hello, world</h1>"
	c, w := newResponseTestContext()

	polipagegin.Preview(c, html)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, html, w.Body.String())
	assert.Empty(t, w.Header().Get("Content-Disposition"),
		"Preview must NOT set Content-Disposition (spec §8.2)")
}

func TestDocumentRedirect_302WithLocationAndCacheControl(t *testing.T) {
	desc := &polipage.DocumentDescriptor{
		PresignedPDFURL: "https://s3.example.com/doc-42.pdf?sig=abc",
	}
	c, w := newResponseTestContext()

	polipagegin.DocumentRedirect(c, desc)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, desc.PresignedPDFURL, w.Header().Get("Location"))
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Body.String(),
		"DocumentRedirect must not write a body; 302 + Location is enough")
}

// flushCounter wraps an httptest.ResponseRecorder and counts calls to its
// Flush method. Gin's responseWriter.Flush delegates to the underlying
// writer's Flush(), so this lets us observe the cadence of the periodic
// flush in PDFStream's 32 KB loop.
type flushCounter struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushCounter) Flush() {
	f.flushes++
}

func TestPDFStream_SetsAllHeadersAndBody(t *testing.T) {
	data := []byte("%PDF-1.7\n...fake pdf bytes for streaming...")
	c, w := newResponseTestContext()

	polipagegin.PDFStream(c, bytes.NewReader(data), polipagegin.PDFOptions{Filename: "stream.pdf"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"),
		"PDFStream must opt nginx-style proxies out of response buffering")
	assert.Equal(t,
		`attachment; filename="stream.pdf"; filename*=UTF-8''stream.pdf`,
		w.Header().Get("Content-Disposition"))
	assert.Empty(t, w.Header().Get("Content-Length"),
		"PDFStream uses chunked encoding — Content-Length must not be set")
	assert.Equal(t, data, w.Body.Bytes(),
		"streamed body must equal the input reader's contents byte-for-byte")
}

func TestPDFStream_FlushesEveryChunk(t *testing.T) {
	// 100 KB through a 32 KB chunk loop → 4 Read/Write/Flush cycles
	// (32 + 32 + 32 + 4 KB). The plan asserts "at least 3"; we get 4.
	data := make([]byte, 100*1024)
	for i := range data {
		data[i] = byte(i)
	}
	w := &flushCounter{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	polipagegin.PDFStream(c, bytes.NewReader(data), polipagegin.PDFOptions{Filename: "big.pdf"})

	assert.GreaterOrEqual(t, w.flushes, 3,
		"100 KB at 32 KB chunks must produce at least 3 flush() calls; got %d", w.flushes)
	assert.Equal(t, data, w.Body.Bytes(),
		"streamed body must round-trip 100 KB without truncation or corruption")
}

// TestPDFStream_BodyIntactThroughDefaultRecorder is the spec §8.3 "the
// helper falls through silently when an exotic wrapper hides the Flusher"
// stand-in. In modern Go, httptest.NewRecorder DOES implement http.Flusher
// — so a strict "non-flusher" probe is not directly testable through the
// standard recorder. This test asserts the next best thing: PDFStream
// completes cleanly (no panic, full body) through the framework-default
// recorder. The defensive `if flusher != nil` branch in the implementation
// remains as a guard for the theoretical case the spec describes.
func TestPDFStream_BodyIntactThroughDefaultRecorder(t *testing.T) {
	data := make([]byte, 64*1024) // exactly two 32 KB chunks
	for i := range data {
		data[i] = byte(i)
	}
	c, w := newResponseTestContext()

	require.NotPanics(t, func() {
		polipagegin.PDFStream(c, bytes.NewReader(data), polipagegin.PDFOptions{Filename: "x.pdf"})
	})
	assert.Equal(t, data, w.Body.Bytes())
}

// erroringReader returns data on the first Read, then returns the error
// on the second Read — simulating a mid-stream failure after some bytes
// have already been delivered to the caller.
type erroringReader struct {
	data     []byte
	err      error
	consumed bool
}

func (r *erroringReader) Read(p []byte) (int, error) {
	if r.consumed {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.consumed = true
	return n, nil
}

func TestPDFStream_MidStreamErrorAttachedToGinErrors(t *testing.T) {
	partial := []byte("first chunk before failure")
	fail := errors.New("simulated mid-stream read failure")
	reader := &erroringReader{data: partial, err: fail}

	c, w := newResponseTestContext()
	polipagegin.PDFStream(c, reader, polipagegin.PDFOptions{Filename: "x.pdf"})

	require.Len(t, c.Errors, 1, "exactly one error queued on c.Errors")
	last := c.Errors.Last()
	require.NotNil(t, last)

	// CLAUDE.md §10.4: the wrapped error lives on .Err — direct errors.Is
	// against c.Errors.Last() (no .Err) silently fails.
	assert.ErrorIs(t, last.Err, fail,
		"the mid-stream error must reach c.Errors via .Err so logging middleware sees it")
	assert.Contains(t, last.Err.Error(), "stream read failed",
		"the wrapped error message must identify which side failed")

	assert.Equal(t, partial, w.Body.Bytes(),
		"bytes successfully read before the failure must still reach the wire")
}

func TestPDFStream_PanicsOnNilReader(t *testing.T) {
	c, _ := newResponseTestContext()

	assert.PanicsWithValue(t,
		"polipagegin: PDFStream called with nil reader",
		func() {
			polipagegin.PDFStream(c, nil, polipagegin.PDFOptions{Filename: "x.pdf"})
		},
		"nil reader is a programmer error and must panic loudly, not produce an empty response")
}
