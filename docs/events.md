# Lifecycle events

> Wire the SDK's `option.WithOnRetry` / `option.WithOnError` callbacks into your `slog` logger, Prometheus counters, or `*gin.Context` so per-request audit trails and metrics see every retry and terminal failure.

## Why

The SDK exposes two lifecycle hooks on client construction — `option.WithOnRetry(func(RetryEvent))` and `option.WithOnError(func(error))`. They run inside the SDK call site (synchronously, on the same goroutine), which makes them the right place to count retries, attach `x-request-id` to span context, or emit structured log lines. The Gin integration doesn't try to wrap them in a bespoke event bus — Go already has `slog`, `expvar`, OpenTelemetry, and any number of metrics libraries that compose cleanly with plain functions. We just document the patterns we use.

## How

### Structured logging with `slog`

```go
// main.go
import (
	"log/slog"
	"os"

	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

client := polipage.NewClient(
	option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	option.WithLogger(logger), // SDK internals log here at DEBUG/WARN/ERROR
	option.WithOnRetry(func(e polipage.RetryEvent) {
		// e.Attempt int; e.DelayMs float64; e.Reason error (typed *polipage.Error at runtime).
		attrs := []slog.Attr{
			slog.Int("attempt", e.Attempt),
			slog.Float64("delay_ms", e.DelayMs),
		}
		var pe *polipage.Error
		if errors.As(e.Reason, &pe) {
			attrs = append(attrs, slog.String("reason_code", pe.Code))
		} else if e.Reason != nil {
			attrs = append(attrs, slog.String("reason", e.Reason.Error()))
		}
		logger.LogAttrs(context.Background(), slog.LevelWarn, "polipage.retry", attrs...)
	}),
	option.WithOnError(func(err error) {
		var pe *polipage.Error
		if errors.As(err, &pe) {
			logger.Error("polipage.error",
				"code", pe.Code,
				"status", pe.StatusCode,
				"request_id", pe.RequestID,
				"message", pe.Message,
			)
			return
		}
		logger.Error("polipage.error", "err", err)
	}),
)
```

`option.WithLogger` already gives you the SDK's internal log line per attempt. The two callbacks above are the place to add custom shape — anything that should not block the request goes here.

### Per-request audit trail

Forward each event into a request-scoped collector so your logging/tracing middleware can collate everything into one log line per request. **Do not** close over `*gin.Context` and call `c.Set(...)` from inside the callback — `*gin.Context` is not safe to use from a goroutine that may outlive the handler (the SDK's retry path doesn't fork goroutines today, but the callback runs synchronously inside the SDK call, and if you ever fan out you'll race the gin pool's reuse of the context). The right shape is a `*renderTrail` value the handler owns:

```go
// observability.go
type renderTrail struct {
	mu     sync.Mutex
	events []renderEvent
}

type renderEvent struct {
	Attempt int
	DelayMs float64
	Reason  error // *polipage.Error at runtime; nil for terminal-error rows
	Err     error
}

func (t *renderTrail) push(e renderEvent) {
	t.mu.Lock()
	t.events = append(t.events, e)
	t.mu.Unlock()
}

func renderHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	trail := &renderTrail{}

	pdf, err := client.Render.PDF(c.Request.Context(), input,
		option.WithOnRetry(func(e polipage.RetryEvent) {
			trail.push(renderEvent{Attempt: e.Attempt, DelayMs: e.DelayMs, Reason: e.Reason})
		}),
		option.WithOnError(func(err error) {
			trail.push(renderEvent{Err: err})
		}),
	)
	// Stash the trail for the logging middleware that runs after c.Next() returns.
	c.Set("polipage.trail", trail.events)

	if err != nil {
		polipagegin.WriteError(c, err)
		return
	}
	polipagegin.PDF(c, pdf, polipagegin.PDFOptions{Filename: "invoice.pdf"})
}
```

The hooks are passed per-call via `option.WithOnRetry` / `option.WithOnError` (yes, the SDK accepts them as per-call options too — see `sdk-go/option/`), so the global client stays untouched and only the handler that wants the audit trail pays for it. If you actually need to do async work from inside the callback (write to a database, ship to a queue), capture `c.Copy()` instead of `c`:

```go
// observability.go
ctxCopy := c.Copy()
option.WithOnError(func(err error) {
	go shipToAuditQueue(ctxCopy, err)
})
```

`c.Copy()` returns a context safe to pass to a goroutine — the original `c` is recycled into Gin's pool after the handler returns and reusing it from a goroutine corrupts the next request.

### Prometheus counters

```go
// metrics.go
var (
	renderRetries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "polipage_render_retries_total",
		Help: "Total retries emitted by the Poli Page SDK.",
	}, []string{"reason"})

	renderErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "polipage_render_errors_total",
		Help: "Total terminal errors emitted by the Poli Page SDK.",
	}, []string{"code"})
)

client := polipage.NewClient(
	option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	option.WithOnRetry(func(e polipage.RetryEvent) {
		// e.Reason is an error (typed *polipage.Error at runtime). Extract a stable code for the label.
		reason := "unknown"
		var pe *polipage.Error
		if errors.As(e.Reason, &pe) {
			reason = pe.Code
		}
		renderRetries.WithLabelValues(reason).Inc()
	}),
	option.WithOnError(func(err error) {
		code := "unknown"
		var pe *polipage.Error
		if errors.As(err, &pe) {
			code = pe.Code
		}
		renderErrors.WithLabelValues(code).Inc()
	}),
)
```

Both labels are API-level codes from `*polipage.Error` (`VALIDATION_ERROR`, `RATE_LIMIT`, `NETWORK_ERROR`, etc.) — a bounded set, which is what you want for a Prometheus label (unbounded label cardinality kills the time-series database). Don't label by `err.Error()` (free-form), `e.DelayMs` (continuous), or `pe.RequestID` (unique per request).

## Gotchas

- **Callbacks run synchronously inside the SDK call site.** Heavy work in `OnRetry` or `OnError` (writing to a database, awaiting an HTTP call) blocks the render request that triggered it. Emit-and-return. Push slow work onto a worker pool or a buffered channel.

- **`OnError` fires for every terminal failure, including ones the caller will surface as 4xx.** Filter on `pe.IsRetryable()` if you only care about server-side issues; filter on `pe.IsAuthError()` if you only care about misconfigurations.

- **`e.Attempt` is the attempt that is about to run.** The SDK fires `OnRetry` before each retry sleep with `Attempt=2` for the first retry, `Attempt=3` for the second, etc. The initial (`Attempt=1`) HTTP call does not fire the hook. If you want a "total HTTP calls" metric, count `OnRetry` fires + 1 per render.

- **`e.Reason` is `error`, not `string`.** At runtime it's a `*polipage.Error`; extract `.Code` via `errors.As` for stable labels and structured-log fields. Calling `e.Reason.(string)` will panic.

- **The SDK does not retry on `context.Canceled`.** If you cancel `c.Request.Context()`, you get `polipage.ErrAborted` with zero `OnRetry` fires — that's by design (no point retrying a request nobody is waiting for).

- **Panics in callbacks are recovered by the SDK.** The SDK wraps each callback in a `recover()` so a buggy `OnRetry` cannot crash the calling goroutine. You'll see a one-line log if `option.WithLogger` is set; otherwise the panic disappears. Don't rely on panicking as a "stop retrying" signal — use `option.WithMaxRetries(0)` to disable retries entirely.

- **Never close over `*gin.Context` in a callback that may fan out to a goroutine.** Gin pools `*gin.Context` values and recycles them after the handler returns; a goroutine that touches `c` after the response went out is reading the next request's state. Use `c.Copy()` if you genuinely need the request context in async work, or — better — push your callback state onto a local struct the handler owns and copy the values out before returning.

## Related

- [README → Configuration](../README.md#configuration) — where `option.With*` lives.
- [docs/middleware.md](middleware.md) — where the client is built, which is where the callbacks are attached.
- `sdk-go/option/` — the canonical list of construction-time and per-call options.
- Go `log/slog` [docs](https://pkg.go.dev/log/slog) — the structured-logging primitive the first example uses.
