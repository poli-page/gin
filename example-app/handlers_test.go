package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// demoTestKey is a well-formed sandbox key for the demo's SDK client.
const demoTestKey = "pp_sandbox_0123456789abcdef0123456789abcdef"

// newDemoServer builds the demo router the way main does, with the SDK
// client pointed at a fake Poli Page API.
func newDemoServer(t *testing.T, api http.Handler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := httptest.NewServer(api)
	t.Cleanup(fake.Close)

	r, err := newRouter(polipagegin.Config{APIKey: demoTestKey, BaseURL: fake.URL})
	require.NoError(t, err)
	return r
}

func TestNotFoundDemo_AnswersTemplateNotFoundThroughTheErrorHandler(t *testing.T) {
	var gotBody map[string]any
	api := http.NewServeMux()
	api.HandleFunc("POST /v1/render", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_demo_1")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"TEMPLATE_NOT_FOUND","detail":"Template does-not-exist not found"}`))
	})
	r := newDemoServer(t, api)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/errors/not-found", http.NoBody))

	assert.Equal(t, map[string]any{
		"project":  "getting-started",
		"template": "does-not-exist",
		"data":     map[string]any{},
	}, gotBody)
	require.Equal(t, http.StatusNotFound, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, map[string]any{
		"code":      "TEMPLATE_NOT_FOUND",
		"status":    float64(404),
		"message":   "Template does-not-exist not found",
		"requestId": "req_demo_1",
	}, body)
}

func TestRenderDemos_SendNoVersion(t *testing.T) {
	var gotBody map[string]any
	api := http.NewServeMux()
	api.HandleFunc("POST /v1/render", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"documentId":"doc_1","expiresAt":"2026-10-27T00:00:00Z"}`))
	})
	r := newDemoServer(t, api)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/documents", http.NoBody))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, map[string]any{
		"project":  "getting-started",
		"template": "welcome",
		"data":     map[string]any{"name": "SDK Demo"},
	}, gotBody)
}

// The fake API fails the test when called, so only gin's own unmatched-route
// 404 passes, never an upstream 404 relayed by the error handler.
func TestRemovedDemoRoutes_AreGone(t *testing.T) {
	removed := []string{"/render/preview", "/render/error", "/api/render/preview", "/api/render/error"}
	r := newDemoServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("fake API called for %s %s", req.Method, req.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))

	for _, route := range r.Routes() {
		assert.NotContains(t, removed, route.Path, "%s %s is registered", route.Method, route.Path)
	}
	for _, path := range removed {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		assert.Equal(t, http.StatusNotFound, w.Code, path)
		assert.Equal(t, "404 page not found", w.Body.String(), path)
	}
}

func TestDocumentPreviewDemo_ServesTheStoredHTML(t *testing.T) {
	api := http.NewServeMux()
	api.HandleFunc("GET /v1/documents/doc_1/preview", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Document-Page-Count", "1")
		_, _ = w.Write([]byte("<p>welcome</p>"))
	})
	r := newDemoServer(t, api)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents/doc_1/preview", http.NoBody))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "<p>welcome</p>", w.Body.String())
}

// The stream route makes the SDK call (render, then the start of the
// download) before it writes a status or a header, so an SDK error is
// answered by the error handler as JSON, never as a committed 200 PDF.
func TestRenderStreamDemo_SDKErrorsAreAnsweredAsJSON(t *testing.T) {
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_stream_404")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"TEMPLATE_NOT_FOUND","detail":"Template 'welcome' not found in project 'getting-started'.","requestId":"req_stream_404"}`))
	}
	drop := func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if assert.NoError(t, err) {
			_ = conn.Close()
		}
	}
	cases := []struct {
		name       string
		render     http.HandlerFunc
		storage    http.HandlerFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "render 404",
			render:     notFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"TEMPLATE_NOT_FOUND","message":"Template 'welcome' not found in project 'getting-started'.","status":404,"requestId":"req_stream_404"}`,
		},
		{
			name:       "render network error",
			render:     drop,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"code":"network_error","message":"Network request failed","status":503,"requestId":null}`,
		},
		{
			name: "download cut before its first byte",
			storage: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Header().Set("Content-Length", "1000")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if assert.NoError(t, err) {
					_ = conn.Close()
				}
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"code":"DOWNLOAD_FAILED","message":"Failed to download PDF: network error","status":null,"requestId":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var origin string
			api := http.NewServeMux()
			api.HandleFunc("POST /v1/render", func(w http.ResponseWriter, r *http.Request) {
				if tc.render != nil {
					tc.render(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"documentId":"doc_1","presignedPdfUrl":"` + origin + `/storage/doc_1.pdf"}`))
			})
			api.HandleFunc("GET /storage/doc_1.pdf", func(w http.ResponseWriter, r *http.Request) {
				if tc.storage == nil {
					t.Errorf("download requested after a failed render")
					return
				}
				tc.storage(w, r)
			})
			fake := httptest.NewUnstartedServer(api)
			fake.Config.SetKeepAlivesEnabled(false)
			fake.Start()
			t.Cleanup(fake.Close)
			origin = fake.URL
			gin.SetMode(gin.TestMode)
			r, err := newRouter(polipagegin.Config{APIKey: demoTestKey, BaseURL: fake.URL})
			require.NoError(t, err)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/render/stream", http.NoBody))

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Empty(t, w.Header().Get("Content-Disposition"))
			assert.Empty(t, w.Header().Get("X-Accel-Buffering"))
			assert.JSONEq(t, tc.wantBody, w.Body.String())
		})
	}
}
