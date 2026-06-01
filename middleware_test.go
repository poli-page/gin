package polipagegin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) *polipage.Client {
	t.Helper()
	return polipage.NewClient(option.WithAPIKey("pp_test_dummy"))
}

func TestMiddleware_AttachesClientToContext(t *testing.T) {
	client := newTestClient(t)
	var got *polipage.Client

	r := gin.New()
	r.Use(polipagegin.Middleware(client))
	r.GET("/", func(c *gin.Context) {
		got = polipagegin.ClientFrom(c)
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Same(t, client, got, "ClientFrom must return the same pointer the middleware stored")
}

func TestMiddleware_PanicsOnNilClientAtInstall(t *testing.T) {
	assert.PanicsWithValue(t,
		"polipagegin: Middleware called with nil client",
		func() { polipagegin.Middleware(nil) },
		"nil client at install time must panic loudly, not defer to a request-time crash")
}

func TestClientFrom_PanicsWhenMiddlewareMissing(t *testing.T) {
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		polipagegin.ClientFrom(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	assert.PanicsWithValue(t,
		"polipagegin: Middleware(client) not installed; add r.Use(polipagegin.Middleware(client))",
		func() { r.ServeHTTP(w, req) })
}

func TestClientFrom_PanicsOnTamperedContextValue(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(polipagegin.ContextKey, "not a *polipage.Client")
		c.Next()
	})
	r.GET("/", func(c *gin.Context) {
		polipagegin.ClientFrom(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	assert.PanicsWithValue(t,
		`polipagegin: context key "polipagegin.client" holds non-*polipage.Client value (got string)`,
		func() { r.ServeHTTP(w, req) })
}

func TestMiddleware_ScopedToRouterGroup(t *testing.T) {
	client := newTestClient(t)

	r := gin.New()
	api := r.Group("/api")
	api.Use(polipagegin.Middleware(client))
	api.GET("/in", func(c *gin.Context) {
		_ = polipagegin.ClientFrom(c)
		c.Status(http.StatusNoContent)
	})
	r.GET("/out", func(c *gin.Context) {
		polipagegin.ClientFrom(c)
	})

	// Inside the group: middleware applies, ClientFrom resolves.
	wIn := httptest.NewRecorder()
	r.ServeHTTP(wIn, httptest.NewRequest(http.MethodGet, "/api/in", http.NoBody))
	assert.Equal(t, http.StatusNoContent, wIn.Code)

	// Outside the group: no middleware, ClientFrom panics.
	wOut := httptest.NewRecorder()
	assert.Panics(t, func() {
		r.ServeHTTP(wOut, httptest.NewRequest(http.MethodGet, "/out", http.NoBody))
	})
}

// TestClientFrom_RecoverySurfacesAs500 is the load-bearing check for the
// "gin.Recovery() must be installed" prerequisite documented in CLAUDE.md §10.1.
// Removing gin.Recovery() from this test must make it fail loudly.
func TestClientFrom_RecoverySurfacesAs500(t *testing.T) {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/", func(c *gin.Context) {
		polipagegin.ClientFrom(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	require.NotPanics(t, func() {
		r.ServeHTTP(w, req)
	}, "Recovery must convert the ClientFrom panic into a 500 response")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
