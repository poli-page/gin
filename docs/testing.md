# Testing

> Define a narrow domain interface for the SDK methods your handlers actually call, fake it in handler tests, and stay off the network — the SDK already tests transport, retries, and error mapping in its own suite.

## Why

`*polipage.Client` is a concrete struct, not an interface. That's the right call for the SDK (interfaces hide the option pattern and break compile-time checks on namespaces), but it does mean handler tests can't replace the client with a `&fakeClient{}` straight away. The idiomatic Go pattern — and the one the SDK's own author recommends — is for *consumers* to define narrow interfaces that capture only the methods their code calls, then fake those interfaces in tests. The integration documents the pattern, ships no Big Generic Interface, and never lets a handler test hit `api.poli.page`.

## How

### Define a narrow domain interface in your app

```go
// internal/render/render.go
package render

import (
	"context"

	"github.com/poli-page/sdk-go"
)

// InvoiceRenderer is what InvoiceController needs from the SDK — nothing more.
type InvoiceRenderer interface {
	PDF(ctx context.Context, in polipage.ProjectModeInput, opts ...any) ([]byte, error)
}
```

`*polipage.Client.Render` satisfies that interface in production via duck typing — Go interfaces are structural. In tests you implement it with a struct.

### Wire it into the handler

```go
// internal/http/invoice_handler.go
package http

import (
	"github.com/gin-gonic/gin"
	"github.com/poli-page/sdk-go"
	polipagegin "github.com/poli-page/gin"

	"myapp/internal/render"
)

type InvoiceHandler struct {
	Renderer render.InvoiceRenderer
}

func (h *InvoiceHandler) Show(c *gin.Context) {
	pdf, err := h.Renderer.PDF(c.Request.Context(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"invoiceId": c.Param("id")},
	})
	if err != nil {
		polipagegin.WriteError(c, err)
		return
	}
	polipagegin.PDF(c, pdf, polipagegin.PDFOptions{Filename: "invoice.pdf"})
}
```

Production wires `h.Renderer = client.Render`; tests wire a fake.

### Pin `gin.TestMode` once for the whole suite

Put a `TestMain` in a package-level `main_test.go` so individual tests don't have to remember:

```go
// internal/http/main_test.go
package http_test

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
```

Without `TestMode`, Gin spams stdout with route registration noise and the recovery middleware logs panics differently — both make CI output harder to read and risky test runners flag them.

### Fake the interface in a test

```go
// internal/http/invoice_handler_test.go
package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apphttp "myapp/internal/http"
)

type fakeRenderer struct {
	called  bool
	gotIn   polipage.ProjectModeInput
	wantErr error
	wantPDF []byte
}

func (f *fakeRenderer) PDF(_ context.Context, in polipage.ProjectModeInput, _ ...any) ([]byte, error) {
	f.called = true
	f.gotIn = in
	return f.wantPDF, f.wantErr
}

func TestInvoiceHandler_Show(t *testing.T) {
	r := gin.New() // TestMain already pinned gin.TestMode.
	fake := &fakeRenderer{wantPDF: []byte("%PDF-1.4")}
	h := &apphttp.InvoiceHandler{Renderer: fake}
	r.GET("/invoices/:id", h.Show)

	req := httptest.NewRequest(http.MethodGet, "/invoices/42", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	assert.True(t, fake.called)
	assert.Equal(t, "42", fake.gotIn.Data["invoiceId"])
}
```

No network. No `httptest.Server` faking the API. No `polipagegin.Middleware`. The handler under test is identical in production and in the test — only `Renderer` differs.

### When the handler reaches the client directly via `ClientFrom`

If you choose the `polipagegin.ClientFrom(c)` style (handler pulls the client off the context), you can still test by installing the middleware with a stub client. The cleanest way is a tiny helper:

```go
// internal/http/testutil/middleware.go
func StubClient(t *testing.T, stub *polipage.Client) gin.HandlerFunc {
	t.Helper()
	return func(c *gin.Context) {
		c.Set("polipagegin.client", stub)
		c.Next()
	}
}
```

But "build a `*polipage.Client` for tests" usually means "make an HTTP call to the real API" — at which point you might as well construct one with a `pp_test_*` key and run it as an integration test gated on `POLI_PAGE_API_KEY`. The narrow-interface pattern above is the better default for unit-level handler tests.

### Integration tests against the develop API

One smoke test per repo. Lives in `tests/integration/` behind a build tag, skips when the env var is unset.

```go
// tests/integration/render_against_develop_test.go
//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func TestRenderAgainstDevelopAPI(t *testing.T) {
	key := os.Getenv("POLI_PAGE_API_KEY")
	if key == "" {
		t.Skip("POLI_PAGE_API_KEY not set")
	}

	client := polipage.NewClient(
		option.WithAPIKey(key),
		option.WithBaseURL("https://api-develop.poli.page"),
		option.WithTimeout(30*time.Second),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pdf, err := client.Render.PDF(ctx, polipage.ProjectModeInput{
		Project:  "getting-started",
		Template: "welcome",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"name": "ci"},
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(pdf) < 100 {
		t.Fatalf("pdf too small: %d bytes", len(pdf))
	}
}
```

Run with `go test -tags=integration ./tests/integration/...`. The test proves DI/middleware wiring + response shape + a real round-trip — nothing the SDK already tests.

## Gotchas

- **Don't spin up an `httptest.Server` that fakes the Poli Page API.** You're rebuilding the SDK's contract tests. Stub at the interface level (`InvoiceRenderer` above) and trust `sdk-go`'s suite.

- **`gin.SetMode(gin.TestMode)` matters.** Pin it once in `TestMain` (shown above); individual tests don't need to call it. Without it Gin spams logs and the recovery middleware logs panics differently.

- **`c.Request.Context()` in tests has no deadline.** `httptest.NewRequest` returns a request with `context.Background()`. If your handler relies on cancellation, build the request explicitly: `req = req.WithContext(ctx)` where `ctx` is a `context.WithTimeout` you control.

- **`*polipage.Error` is a real struct.** When asserting filter behaviour, construct one directly: `&polipage.Error{Code: "RATE_LIMIT", StatusCode: 429}`. `errors.As` checks inside `polipagegin.WriteError` and your own catch blocks both pass.

- **The narrow interface goes in your app, not in this package.** Don't open a PR adding `Renderer interface{ PDF(...); PDFStream(...); ... }` here — that would be the Big Generic Interface this package explicitly does not ship (CLAUDE.md §2). Every consumer's domain interface is different.

- **Integration tests need a `pp_test_*` key, not `pp_live_*`.** The smoke test renders against `api-develop.poli.page`; a live key talks to production and bills your account. CI passes the test key via repository secrets.

## Related

- [README → Example app](../README.md#example-app) — the demo app is the manual smoke test; this doc covers the automated layer below it.
- [docs/middleware.md](middleware.md) — the production wiring the tests bypass.
- [docs/spec/gin-integration-specification.md](spec/gin-integration-specification.md) §13 — the full testing strategy.
- Gin docs on [testing](https://github.com/gin-gonic/gin#testing) — `httptest.NewRecorder` + `gin.New()` is the primitive.
