//go:build integration

// Package integration holds the gated integration test that talks to the
// live Poli Page API. Behind the //go:build integration tag so the full
// test suite (`go test ./...`) never touches the network.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// TestRenderAgainstLiveAPI is the v0.1.0 smoke test: a pp_test_* key
// renders the "getting-started/welcome" template against the live API
// and asserts a non-empty PDF comes back. Proves end-to-end wiring
// without re-testing anything the SDK contract suite already covers.
//
// Skipped (NOT failed) when POLI_PAGE_API_KEY is unset, so PRs from
// forks that lack the secret stay green.
//
// Run locally:
//
//	POLI_PAGE_API_KEY=pp_test_... \
//	go test -tags=integration ./tests/integration/...
//
// Set POLI_PAGE_TEST_BASE_URL to override the SDK's default API host.
func TestRenderAgainstLiveAPI(t *testing.T) {
	key := os.Getenv("POLI_PAGE_API_KEY")
	if key == "" {
		t.Skip("POLI_PAGE_API_KEY not set; skipping integration test")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(key),
		option.WithTimeout(30 * time.Second),
	}
	if v := os.Getenv("POLI_PAGE_TEST_BASE_URL"); v != "" {
		opts = append(opts, option.WithBaseURL(v))
	}
	client := polipage.NewClient(opts...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pdf, err := client.Render.PDF(ctx, polipage.ProjectModeInput{
		Project:  "getting-started",
		Template: "welcome",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"name": "ci"},
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(pdf) < 100 {
		t.Fatalf("rendered pdf is suspiciously small: %d bytes (expected at least a few KB)", len(pdf))
	}
}
