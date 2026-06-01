package polipagegin_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
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
