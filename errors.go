package polipagegin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	polipage "github.com/poli-page/sdk-go"
)

// ErrorMiddleware returns a gin.HandlerFunc that converts any
// *polipage.Error queued on c.Errors into a JSON response with a stable
// shape:
//
//	{"code": "...", "message": "...", "status": 401, "requestId": "..."}
//
// The middleware runs after the handler chain (it issues c.Next() and
// inspects c.Errors when control returns) and reads c.Errors.Last() —
// matching the convention downstream logging/tracing middleware
// typically follows. Multiple errors are tolerated; the most recent one
// drives the response.
//
// Narrow on purpose: only *polipage.Error is intercepted. Any other
// error type is left untouched for the caller's existing error-handling
// middleware (or Gin's default) to deal with. Widening to all `error`
// values would swallow non-Poli-Page failures and destroy observability,
// violating the thin-wrapper stance — same decision the NestJS package
// made with @Catch(PoliPageError).
//
// Status and code come from the SDK's canonical Payload: 503 for network
// failures, 504 for timeouts, the upstream HTTP status otherwise. Code
// is the API's verbatim code (lowercase for transport, whatever the API
// returns for 4xx/5xx).
func ErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		ge := c.Errors.Last()
		if ge == nil {
			return
		}
		// gin v1.9+ implements *Error.Unwrap → .Err, so errors.As works
		// against either form. We use ge.Err to keep the wrap chain
		// explicit at the call site (CLAUDE.md §10.4 history).
		var pe *polipage.Error
		if !errors.As(ge.Err, &pe) {
			return
		}
		respondPolipageError(c, pe)
	}
}

// WriteError is the per-handler version of ErrorMiddleware: same JSON
// shape and same one transformation. Use it inside a handler when the
// error flow is explicit rather than queued on c.Errors.
//
// A non-*polipage.Error argument produces an HTTP 500 with no body —
// the caller's logging middleware owns the message. WriteError is
// intentionally opaque to anything it does not recognise; widening it
// would re-introduce the destroyed-observability hazard ErrorMiddleware
// already avoids.
func WriteError(c *gin.Context, err error) {
	var pe *polipage.Error
	if !errors.As(err, &pe) {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	respondPolipageError(c, pe)
}

// respondPolipageError is the shared response writer for ErrorMiddleware
// and WriteError. Kept private so the JSON shape and the
// AbortWithStatusJSON call live in one place.
func respondPolipageError(c *gin.Context, pe *polipage.Error) {
	payload := pe.ToPayload()
	status := payload.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	c.AbortWithStatusJSON(status, gin.H{
		"code":      payload.Code,
		"message":   payload.Message,
		"status":    status,
		"requestId": payload.RequestID,
	})
}
