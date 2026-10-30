// example-app is the runnable demo for the polipagegin integration. It
// wires the middleware (with its default error handler) and the response
// helpers exactly the way a production Gin service would — copy verbatim
// and adjust to taste.
//
// Run:
//
//	cd example-app
//	go run .
//	# → http://127.0.0.1:8080
//
// POLI_PAGE_API_KEY is required; any non-empty key runs the app. Use a
// pp_sandbox_* key so demo renders are watermarked and not counted. Other
// POLI_PAGE_* variables are honored too; see the parent README for the
// full list.
package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
)

// newRouter builds the demo's Gin engine from the integration's
// configuration: one SDK client, the polipagegin middleware with its
// default error handler, and the demo routes. main serves it; the tests
// call it with a Config pointed at a local server.
func newRouter(cfg polipagegin.Config) (*gin.Engine, error) {
	client := polipage.NewClient(cfg.Options()...)

	r := gin.New()
	r.Use(
		gin.Logger(),
		gin.Recovery(),
		polipagegin.Middleware(client),
	)
	// Explicit no-trust default: c.ClientIP() returns the direct peer.
	// Set to your front proxy's CIDR list if running behind one.
	if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	registerRoutes(r, assets)
	return r, nil
}

// assets bundles the demo dashboard's static files into the binary so a
// single `go run .` (or built binary) serves the UI without an external
// asset directory.
//
// Note: //go:embed fails at compile time if any named directory is
// empty; both templates/ and static/ must contain at least one file
// before this compiles.
//
//go:embed templates static
var assets embed.FS

// dotEnvFile is the dev-time convenience file the example app reads at
// boot, relative to the working directory. A missing file is not an
// error — real shell exports always win, and Docker / k8s / systemd-driven
// deploys don't go through this loader at all.
const dotEnvFile = ".env"

func main() {
	if err := loadDotEnv(dotEnvFile); err != nil {
		log.Printf("warning: could not read %s: %v", dotEnvFile, err)
	}

	gin.SetMode(gin.ReleaseMode)

	cfg, err := polipagegin.FromEnv()
	if err != nil {
		log.Fatalf("polipagegin.FromEnv: %v", err)
	}
	r, err := newRouter(cfg)
	if err != nil {
		log.Fatalf("newRouter: %v", err)
	}

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("polipagegin demo listening on http://127.0.0.1%s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down — waiting up to 30s for active connections")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
