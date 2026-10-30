package polipagegin

import (
	"fmt"

	"github.com/gin-gonic/gin"
	polipage "github.com/poli-page/sdk-go"
)

// ContextKey is the gin.Context key under which Middleware stores the
// per-request *polipage.Client. Exported so callers writing custom
// middleware (e.g. swapping the client per tenant) can read or write the
// same slot ClientFrom looks at.
const ContextKey = "polipagegin.client"

// Middleware returns a gin.HandlerFunc that attaches client to every
// request's *gin.Context. Pull it back out inside a handler with
// ClientFrom.
//
// The middleware also ships the error handler, on by default: when a
// handler queues a *polipage.Error with c.Error(err) and writes no
// response, the middleware answers with the error's ToPayload() body
// ({code, message, status, requestId}) and its status (500 when the
// payload has none). See ErrorMiddleware for the details. Pass
// WithoutErrorHandler() to turn it off.
//
// Passing a nil client panics at install time — the failure mode we want
// is "the app fails to boot" not "the first request crashes deep inside
// the SDK". Reuse one *polipage.Client per process (it is safe for
// concurrent use); do not construct a fresh one per request.
//
// The application must install gin.Recovery() so a ClientFrom panic from
// a missing middleware surfaces as HTTP 500 instead of crashing the
// process. gin.Default() includes Recovery; gin.New() does not.
func Middleware(client *polipage.Client, opts ...MiddlewareOption) gin.HandlerFunc {
	if client == nil {
		panic("polipagegin: Middleware called with nil client")
	}
	cfg := middlewareConfig{errorHandler: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(c *gin.Context) {
		c.Set(ContextKey, client)
		c.Next()
		if cfg.errorHandler {
			respondQueuedError(c)
		}
	}
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	errorHandler bool
}

// WithoutErrorHandler turns off the error handler Middleware installs by
// default: a *polipage.Error queued on c.Errors is then left for the
// application's own error handling (or ErrorMiddleware, installed
// elsewhere).
func WithoutErrorHandler() MiddlewareOption {
	return func(cfg *middlewareConfig) { cfg.errorHandler = false }
}

// ClientFrom returns the *polipage.Client stored on c by Middleware.
//
// Panics with a clear message when the middleware was not installed on
// the route the request travelled through, or when something else has
// overwritten ContextKey with a value of the wrong type. Both cases are
// programmer errors, never expected at runtime — fail loud so the bug
// shows up in the first request a developer sends.
func ClientFrom(c *gin.Context) *polipage.Client {
	v, ok := c.Get(ContextKey)
	if !ok {
		panic("polipagegin: Middleware(client) not installed; add r.Use(polipagegin.Middleware(client))")
	}
	client, ok := v.(*polipage.Client)
	if !ok {
		panic(fmt.Sprintf("polipagegin: context key %q holds non-*polipage.Client value (got %T)", ContextKey, v))
	}
	return client
}
