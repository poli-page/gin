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
//	{"code": "...", "message": "...", "requestId": "..."}
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
// One transformation: an error that satisfies (*polipage.Error).IsNetworkError
// (SDK-level network/timeout, with StatusCode 0) collapses to HTTP 502
// with code "NETWORK_ERROR". 4xx and 5xx responses from the API pass
// through verbatim.
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
	status, code := mapError(pe)
	c.AbortWithStatusJSON(status, gin.H{
		"code":      code,
		"message":   pe.Message,
		"requestId": pe.RequestID,
	})
}

// mapError applies the one transformation the integration layers on top
// of the SDK's *polipage.Error: SDK-level network errors and timeouts
// collapse to 502 "NETWORK_ERROR". Everything else passes through
// (auth, rate limit, validation, generic 4xx/5xx).
func mapError(pe *polipage.Error) (status int, code string) {
	if pe.IsNetworkError() {
		return http.StatusBadGateway, networkErrorCode
	}
	return pe.StatusCode, pe.Code
}

// networkErrorCode is the uppercase response code returned when the SDK
// surfaces a network or timeout failure. Distinct from the lowercase
// polipage.ErrCodeNetworkError ("network_error") which is the SDK's
// internal code on the *polipage.Error — the wire code consumers see is
// uppercase by convention with the API's 4xx/5xx codes.
const networkErrorCode = "NETWORK_ERROR"
