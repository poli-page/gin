// example-app is the runnable demo for the polipagegin integration. It
// wires the middleware, response helpers, and ErrorMiddleware exactly
// the way a production Gin service would — copy verbatim and adjust to
// taste.
//
// Run:
//
//	cd example-app
//	go run .
//	# → http://127.0.0.1:8080
//
// POLI_PAGE_API_KEY is required (a pp_test_* key). Other POLI_PAGE_*
// variables are honored per spec §7.2; see the parent docs/ for the
// full list.
package main

import (
	"context"
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

// workspaceDotEnv is the dev-time convenience path the example app reads
// at boot. A missing file is not an error — real shell exports always
// win, and Docker / k8s / systemd-driven deploys don't go through this
// loader at all.
const workspaceDotEnv = "/Users/mickael/Projects/.env"

func main() {
	if err := loadDotEnv(workspaceDotEnv); err != nil {
		log.Printf("warning: could not read %s: %v", workspaceDotEnv, err)
	}

	gin.SetMode(gin.ReleaseMode)

	cfg, err := polipagegin.FromEnv()
	if err != nil {
		log.Fatalf("polipagegin.FromEnv: %v", err)
	}
	client := polipage.NewClient(cfg.Options()...)

	r := gin.New()
	r.Use(
		gin.Logger(),
		gin.Recovery(),
		polipagegin.Middleware(client),
		polipagegin.ErrorMiddleware(),
	)
	// Explicit no-trust default: c.ClientIP() returns the direct peer.
	// Set to your front proxy's CIDR list if running behind one.
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("SetTrustedProxies: %v", err)
	}

	registerRoutes(r)

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
