package polipagegin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	polipage "github.com/poli-page/sdk-go"
)

// ErrorMiddleware returns a gin.HandlerFunc that converts the last
// *polipage.Error queued on c.Errors into a JSON response:
//
//	{"code": "...", "message": "...", "status": 401, "requestId": "..."}
//
// Middleware(client) already does this by default, so most applications
// never install ErrorMiddleware. Use it when the error handler was turned
// off with WithoutErrorHandler and should run somewhere else in the chain
// (for example on a different router group).
//
// The middleware runs after the handler chain (it issues c.Next() and
// inspects c.Errors when control returns) and reads c.Errors.Last(),
// the convention downstream logging/tracing middleware typically follows.
// Multiple errors are tolerated; the most recent one drives the response.
//
// Narrow on purpose: only *polipage.Error is intercepted. Any other
// error type is left untouched for the caller's existing error-handling
// middleware (or Gin's default) to deal with. Widening to all `error`
// values would swallow non-Poli-Page failures and destroy observability.
//
// The body is the SDK's (*polipage.Error).ToPayload(), unchanged. Its
// status is the API's HTTP status, 503 for network_error and 504 for
// timeout; the response uses the same status, or 500 when the payload has
// none (the body then carries "status": null). Nothing is written when
// the handler already wrote a response.
func ErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		respondQueuedError(c)
	}
}

// WriteError is the per-handler version of the error handler: same JSON
// body and status. Use it inside a handler when the error flow is
// explicit rather than queued on c.Errors.
//
// A non-*polipage.Error argument produces an HTTP 500 with no body:
// the caller's logging middleware owns the message. WriteError is
// intentionally opaque to anything it does not recognise; widening it
// would re-introduce the destroyed-observability hazard the error
// handler already avoids.
func WriteError(c *gin.Context, err error) {
	var pe *polipage.Error
	if !errors.As(err, &pe) {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	respondPolipageError(c, pe)
}

// respondQueuedError answers the last error on c.Errors when it is a
// *polipage.Error and no response has been written yet.
func respondQueuedError(c *gin.Context) {
	ge := c.Errors.Last()
	if ge == nil {
		return
	}
	// gin v1.9+ implements *Error.Unwrap → .Err, so errors.As works
	// against either form. ge.Err keeps the wrap chain explicit.
	var pe *polipage.Error
	if !errors.As(ge.Err, &pe) {
		return
	}
	if c.Writer.Written() {
		// The handler (or an inner error handler) already answered:
		// writing again would append a second body to the response.
		return
	}
	respondPolipageError(c, pe)
}

// respondPolipageError is the shared response writer: the body is the
// SDK's payload, the status its status or 500 when it has none.
func respondPolipageError(c *gin.Context, pe *polipage.Error) {
	payload := pe.ToPayload()
	status := payload.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	c.AbortWithStatusJSON(status, payload)
}
