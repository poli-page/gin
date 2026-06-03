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
