package polipagegin_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newErrorTestContext builds a *gin.Context backed by a recorder for the
// WriteError tests (which call the helper directly, no full router).
func newErrorTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	return c, w
}

// runErrorMiddleware drives a fresh router with ErrorMiddleware installed,
// then calls handler inside the GET / route, returning the recorder and
// the captured context (so tests can inspect c.Errors after).
func runErrorMiddleware(handler gin.HandlerFunc) (*httptest.ResponseRecorder, *gin.Context) {
	var captured *gin.Context
	r := gin.New()
	r.Use(polipagegin.ErrorMiddleware())
	r.GET("/", func(c *gin.Context) {
		captured = c
		handler(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	return w, captured
}

func TestErrorMiddleware_ValidationError400PassThrough(t *testing.T) {
	pe := &polipage.Error{
		Code:       polipage.ErrCodeValidationError,
		StatusCode: http.StatusBadRequest,
		Message:    "data.email is required",
		RequestID:  "req_abc123",
	}
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(pe)
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "VALIDATION_ERROR", body["code"])
	assert.Equal(t, "data.email is required", body["message"])
	assert.Equal(t, "req_abc123", body["requestId"])
}

func TestErrorMiddleware_AuthError401PassThrough(t *testing.T) {
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(&polipage.Error{
			Code:       polipage.ErrCodeInvalidAPIKey,
			StatusCode: http.StatusUnauthorized,
			Message:    "API key is invalid",
			RequestID:  "req_auth",
		})
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "INVALID_API_KEY", body["code"])
}

func TestErrorMiddleware_RateLimit429PassThrough(t *testing.T) {
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(&polipage.Error{
			Code:       polipage.ErrCodeQuotaExceeded,
			StatusCode: http.StatusTooManyRequests,
			Message:    "quota exceeded",
			RequestID:  "req_rate",
		})
	})

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "QUOTA_EXCEEDED", body["code"])
}

func TestErrorMiddleware_NetworkErrorMapsTo503(t *testing.T) {
	// SDK's network_error has StatusCode 0 (request never reached the API);
	// ToPayload() surfaces 503 (Service Unavailable). Code passes through
	// verbatim — no "NETWORK_ERROR" rewrite.
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(&polipage.Error{
			Code:      polipage.ErrCodeNetworkError,
			Message:   "connection refused",
			RequestID: "req_net",
		})
	})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"network errors map to 503 via the SDK payload")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, polipage.ErrCodeNetworkError, body["code"],
		"code is the SDK's code verbatim — no NETWORK_ERROR rewrite")
	assert.Equal(t, "connection refused", body["message"])
	assert.Equal(t, float64(503), body["status"])
	assert.Equal(t, "req_net", body["requestId"])
}

func TestErrorMiddleware_TimeoutMapsTo504(t *testing.T) {
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(&polipage.Error{
			Code:    polipage.ErrCodeTimeout,
			Message: "request deadline exceeded",
		})
	})

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, polipage.ErrCodeTimeout, body["code"])
	assert.Equal(t, float64(504), body["status"])
}

func TestErrorMiddleware_IgnoresNonPolipageErrors(t *testing.T) {
	// Narrow on purpose: a non-polipage error is left for downstream
	// logging/tracing middleware (or Gin's default) to handle. The
	// middleware writes no body and no JSON Content-Type.
	w, captured := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(errors.New("some other error"))
	})

	assert.Empty(t, w.Body.Bytes(),
		"middleware must not write a body for non-polipage errors")
	assert.NotEqual(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"),
		"middleware must not set the JSON Content-Type for non-polipage errors")
	require.NotNil(t, captured)
	require.Len(t, captured.Errors, 1,
		"the generic error remains on c.Errors for downstream middleware to see")
}

func TestErrorMiddleware_UsesLastErrorWhenMultiple(t *testing.T) {
	// c.Errors is a slice; the middleware reads .Last() so the response
	// matches the most recent error (same rule logging/tracing follow).
	w, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(errors.New("first generic"))
		_ = c.Error(&polipage.Error{
			Code:       polipage.ErrCodeValidationError,
			StatusCode: http.StatusBadRequest,
			Message:    "second (last) is the polipage one",
			RequestID:  "req_last",
		})
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "req_last", body["requestId"])
}

// TestErrorMiddleware_GinErrorUnwrapChainReachesPolipageError documents
// the invariant the middleware relies on: gin v1.9+ implements
// *Error.Unwrap → .Err, so errors.As against either form (the wrapped
// *gin.Error directly OR the explicit .Err access) extracts the wrapped
// *polipage.Error. The middleware uses .Err for explicitness; this test
// asserts the relationship — if a future gin version drops Unwrap, the
// direct-form assertion fails and we know to either keep .Err (still
// correct) or update the codebase.
func TestErrorMiddleware_GinErrorUnwrapChainReachesPolipageError(t *testing.T) {
	pe := &polipage.Error{
		Code:       polipage.ErrCodeValidationError,
		StatusCode: http.StatusBadRequest,
		Message:    "x",
		RequestID:  "y",
	}
	ge := &gin.Error{Err: pe, Type: gin.ErrorTypePrivate}

	var found *polipage.Error
	assert.True(t, errors.As(ge.Err, &found),
		"the explicit .Err access must extract the wrapped *polipage.Error")
	found = nil
	assert.True(t, errors.As(ge, &found),
		"the unwrap chain on *gin.Error must also reach *polipage.Error "+
			"(gin v1.9+ implements Error.Unwrap returning .Err)")
}

func TestWriteError_PolipageErrorProducesSameJSONAsMiddleware(t *testing.T) {
	pe := &polipage.Error{
		Code:       polipage.ErrCodeValidationError,
		StatusCode: http.StatusBadRequest,
		Message:    "data.email is required",
		RequestID:  "req_write",
	}

	c1, w1 := newErrorTestContext()
	polipagegin.WriteError(c1, pe)

	w2, _ := runErrorMiddleware(func(c *gin.Context) {
		_ = c.Error(pe)
	})

	assert.Equal(t, w2.Code, w1.Code, "WriteError must match middleware status")
	assert.JSONEq(t, w2.Body.String(), w1.Body.String(),
		"WriteError must produce the same JSON body as the middleware")
}

func TestWriteError_NonPolipageErrorProduces500(t *testing.T) {
	c, w := newErrorTestContext()
	polipagegin.WriteError(c, errors.New("boom"))

	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"a non-polipage error is opaque to WriteError; surface as 500 to make the bug visible")
	assert.Empty(t, w.Body.Bytes(),
		"no JSON body — the caller's logging middleware owns the message")
}

// runMiddleware drives a fresh router with Middleware(client, opts...)
// installed and handler on GET /: the error handler under test is the
// one Middleware ships, not ErrorMiddleware.
func runMiddleware(t *testing.T, handler gin.HandlerFunc, opts ...polipagegin.MiddlewareOption) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(polipagegin.Middleware(newTestClient(t), opts...))
	r.GET("/", handler)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	return w
}

func queue(err error) gin.HandlerFunc {
	return func(c *gin.Context) { _ = c.Error(err) }
}

func TestMiddleware_ErrorHandlerOnByDefault(t *testing.T) {
	w := runMiddleware(t, queue(&polipage.Error{
		Code:       "TEMPLATE_NOT_FOUND",
		StatusCode: http.StatusNotFound,
		Message:    "Template 'does-not-exist' not found in project 'getting-started'.",
		RequestID:  "req_404",
	}))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{
		"code": "TEMPLATE_NOT_FOUND",
		"message": "Template 'does-not-exist' not found in project 'getting-started'.",
		"status": 404,
		"requestId": "req_404"
	}`, w.Body.String())
}

func TestMiddleware_WithoutErrorHandlerLeavesTheErrorAlone(t *testing.T) {
	w := runMiddleware(t, queue(&polipage.Error{
		Code:       "TEMPLATE_NOT_FOUND",
		StatusCode: http.StatusNotFound,
		Message:    "not found",
	}), polipagegin.WithoutErrorHandler())

	// gin's default for a handler that wrote nothing.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestMiddleware_ErrorHandlerBodyIsThePayload(t *testing.T) {
	cases := []struct {
		name       string
		err        *polipage.Error
		wantStatus int
		wantBody   string
	}{
		{
			name: "API error",
			err: &polipage.Error{
				Code: "INTERNAL_ERROR", StatusCode: http.StatusInternalServerError,
				Message: "Unexpected server error.", RequestID: "req_5xx",
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"code":"INTERNAL_ERROR","message":"Unexpected server error.","status":500,"requestId":"req_5xx"}`,
		},
		{
			name:       "network_error answers 503",
			err:        &polipage.Error{Code: polipage.ErrCodeNetworkError, Message: "Network request failed"},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"code":"network_error","message":"Network request failed","status":503,"requestId":null}`,
		},
		{
			name:       "timeout answers 504",
			err:        &polipage.Error{Code: polipage.ErrCodeTimeout, Message: "Request timed out after 60000ms"},
			wantStatus: http.StatusGatewayTimeout,
			wantBody:   `{"code":"timeout","message":"Request timed out after 60000ms","status":504,"requestId":null}`,
		},
		{
			name:       "SDK error with no status answers 500 with a null status",
			err:        &polipage.Error{Code: polipage.ErrCodeAborted, Message: "Request was aborted"},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"code":"aborted","message":"Request was aborted","status":null,"requestId":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := runMiddleware(t, queue(tc.err))
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
			assert.JSONEq(t, tc.wantBody, w.Body.String())

			payload, err := json.Marshal(tc.err.ToPayload())
			require.NoError(t, err)
			assert.JSONEq(t, string(payload), w.Body.String(), "the body is ToPayload(), unchanged")
		})
	}
}

func TestMiddleware_ErrorHandlerKeepsAWrittenResponse(t *testing.T) {
	w := runMiddleware(t, func(c *gin.Context) {
		c.String(http.StatusAccepted, "already answered")
		_ = c.Error(&polipage.Error{Code: polipage.ErrCodeTimeout, Message: "late"})
	})

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "already answered", w.Body.String())
}

func TestMiddleware_WithErrorMiddlewareAnswersOnce(t *testing.T) {
	r := gin.New()
	r.Use(polipagegin.Middleware(newTestClient(t)), polipagegin.ErrorMiddleware())
	r.GET("/", queue(&polipage.Error{Code: polipage.ErrCodeNetworkError, Message: "Network request failed"}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"code":"network_error","message":"Network request failed","status":503,"requestId":null}`, w.Body.String())
}

func TestMiddleware_ErrorHandlerDropsHeadersSetForAnotherAnswer(t *testing.T) {
	w := runMiddleware(t, func(c *gin.Context) {
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", `attachment; filename="a.pdf"`)
		c.Header("X-Accel-Buffering", "no")
		c.Header("Content-Length", "1234")
		_ = c.Error(&polipage.Error{Code: polipage.ErrCodeTimeout, Message: "Request timed out after 60000ms"})
	})

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Header().Get("Content-Disposition"))
	assert.Empty(t, w.Header().Get("X-Accel-Buffering"))
	assert.NotEqual(t, "1234", w.Header().Get("Content-Length"))
	assert.JSONEq(t, `{"code":"timeout","message":"Request timed out after 60000ms","status":504,"requestId":null}`, w.Body.String())
}
