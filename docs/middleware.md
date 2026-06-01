# Middleware

> Install `polipagegin.Middleware(client)` once on your router, then pull the per-request `*polipage.Client` from `*gin.Context` with `polipagegin.ClientFrom(c)` inside every handler.

## Why

A Gin app shares one `*polipage.Client` across every handler (the SDK client is goroutine-safe and pools connections; see `polipage.go`). The two natural ways to hand it to handlers are a package-level singleton (works, but couples every handler to a global and makes tests awkward) or attaching it to `*gin.Context` via middleware. We pick the second: it keeps the wiring visible, makes per-request overrides cheap, and matches the pattern set by `getsentry/sentry-go/gin`, `gin-contrib/sessions`, and most middleware-shaped Gin packages.

## How

### Install once

```go
// main.go
client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))

r := gin.Default() // Default includes gin.Recovery() — required, see below.
r.Use(polipagegin.Middleware(client))
```

The middleware runs first on every request. It calls `c.Set("polipagegin.client", client)` and yields to the next handler — no allocation per request beyond the map entry Gin already maintains.

`polipagegin.Middleware(client)` returns a `gin.HandlerFunc`. Install it via `Use` on either `*gin.Engine` (global) or any `*gin.RouterGroup` (scoped to that group's routes). See [Per-route or per-group clients](#per-route-or-per-group-clients) below for the scoped pattern.

### `gin.Recovery()` is required

`polipagegin.ClientFrom(c)` panics with an explicit message when the middleware was not installed (see [Gotchas](#gotchas)). That panic is caught by Gin's `Recovery()` middleware and converted to a 500 — `gin.Default()` registers `Recovery()` automatically, so the panic surfaces cleanly. If you bootstrap with `gin.New()` instead, **you must add `r.Use(gin.Recovery())` before `polipagegin.Middleware(client)`** or the panic crashes the process. Same rule applies to any panic from a handler: install Recovery once.

```go
// main.go
r := gin.New()
r.Use(gin.Recovery())                    // required
r.Use(polipagegin.Middleware(client))    // safe to panic from here on
```

### Pull the client in a handler

```go
// invoice_handler.go
func invoiceHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)

	pdf, err := client.Render.PDF(c.Request.Context(), polipage.ProjectModeInput{
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

`ClientFrom` returns the typed `*polipage.Client`. It panics if `Middleware` was not installed — see the [Gotchas](#gotchas) section below.

### Per-route or per-group clients

You can install a second middleware on a sub-group with a different client (different API key, different base URL, different `onRetry` callback). The sub-group's `Use` runs after the global one and the second `c.Set` overwrites the first for the duration of the request.

```go
// main.go
sandbox := polipage.NewClient(
	option.WithAPIKey(os.Getenv("POLI_PAGE_SANDBOX_KEY")),
	option.WithBaseURL("https://api-develop.poli.page"),
)

r.Use(polipagegin.Middleware(prodClient))

dev := r.Group("/dev")
dev.Use(polipagegin.Middleware(sandbox))
dev.GET("/preview", previewHandler) // sees sandbox
```

Inside `previewHandler`, `polipagegin.ClientFrom(c)` returns the sandbox client; everywhere else it returns prod.

### Request-scoped context

Every SDK call must take `c.Request.Context()`, not `context.Background()`. Gin cancels the request context when the client disconnects; the SDK aborts in flight with `polipage.ErrAborted` and your handler returns immediately. Passing `context.Background()` defeats this and leaves goroutines running after the connection closes.

```go
// invoice_handler.go
// good
pdf, err := client.Render.PDF(c.Request.Context(), input)
// bad — never cancels
pdf, err := client.Render.PDF(context.Background(), input)
```

## Gotchas

- **`ClientFrom` panics if the middleware was not installed.** The panic message is explicit: `polipagegin: Middleware(client) not installed; add r.Use(polipagegin.Middleware(client))`. Gin's default recovery middleware catches it and returns 500 — louder than the nil-pointer dereference you'd get otherwise. If you want a soft fallback, write your own accessor that calls `c.Get("polipagegin.client")` and checks the boolean.

- **Don't share the SDK client across processes.** The `*polipage.Client` you construct in `main` is process-local; pooled connections do not survive a restart. For a fleet of replicas, each replica constructs its own — that's already what happens by default.

- **Don't construct a new client per request.** `polipage.NewClient` validates options on the first method call (lazy), but it still allocates and warms up a transport. Build once at startup, install once with `Middleware`, reuse across every request. The Gin context key is there so you never feel pressure to "just `polipage.NewClient` again here".

- **Avoid `context.WithTimeout` on top of `c.Request.Context()` unless you mean it.** The SDK already honours `option.WithTimeout` / `option.WithRequestTimeout`. Wrapping the request context with a shorter deadline is fine when you want to fail fast, but layering both timeout *and* SDK timeout is usually a sign you should pick one.

- **Custom context keys clash.** Gin's `c.Set`/`c.Get` is a `map[string]any` on the context. The package uses the key `"polipagegin.client"` — if you reuse that string in your own code, you'll silently overwrite the SDK client. The constant is exported as `polipagegin.ContextKey` so you can detect collisions in static analysis.

## Related

- [README → Quick start](../README.md#quick-start) — the minimal install-and-use example this doc expands.
- [docs/responses.md](responses.md) — the helpers most handlers will call right after `ClientFrom(c)`.
- [docs/testing.md](testing.md) — how to fake the client in handler tests without going through `Middleware`.
- Gin docs on [`Context.Set`/`Context.Get`](https://pkg.go.dev/github.com/gin-gonic/gin#Context.Set) — the primitive `Middleware` builds on.
