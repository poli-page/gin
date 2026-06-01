// Package main implements the polipage-render CLI binary. The package
// is intentionally split into three concerns:
//
//   - render.go: pure render() and exitCodeFor(), no Cobra, no os.Exit,
//     no os.Stdout/Stderr. Drives the SDK and returns an exit code.
//   - root.go:   Cobra adapter — flag definitions, buildRequest, and the
//     small exitErr type that carries the exit code up to main().
//   - main.go:   entry point. os.Exit lives here and only here.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
)

// renderRequest is the pure input to render(). Cfg is the resolved
// polipagegin.Config (env + flag overrides); Input is the SDK render
// input (polipage.ProjectModeInput or polipage.InlineModeInput); Out
// is the writer the rendered bytes are streamed into (the -o file or
// stdout, opened by the Cobra adapter in root.go).
type renderRequest struct {
	Cfg   polipagegin.Config
	Input polipage.RenderInput
	Out   io.Writer
}

// render performs the render and writes the bytes to req.Out. Returns
// the process exit code per spec §9.3 and an error for diagnostic
// logging (the caller writes it to stderr).
//
// Project mode renders a PDF via Render.PDFStream and streams the body
// straight into req.Out — no buffering. Inline mode renders an HTML
// preview via Render.Preview and writes the HTML bytes; inline + PDF
// is not a thing the SDK supports.
func render(ctx context.Context, req renderRequest) (int, error) {
	client := polipage.NewClient(req.Cfg.Options()...)

	switch in := req.Input.(type) {
	case polipage.ProjectModeInput:
		body, err := client.Render.PDFStream(ctx, in)
		if err != nil {
			return exitCodeFor(err), err
		}
		defer func() { _ = body.Close() }()
		if _, err := io.Copy(req.Out, body); err != nil {
			return 4, fmt.Errorf("failed to write PDF output: %w", err)
		}
		return 0, nil

	case polipage.InlineModeInput:
		result, err := client.Render.Preview(ctx, in)
		if err != nil {
			return exitCodeFor(err), err
		}
		if _, err := io.WriteString(req.Out, result.HTML); err != nil {
			return 4, fmt.Errorf("failed to write HTML output: %w", err)
		}
		return 0, nil
	}

	return exitMisuse, fmt.Errorf("unknown render input type %T", req.Input)
}

// exitCodeFor maps an SDK error to the spec §9.3 exit code:
//
//   - 1 — auth error  (IsAuthError: 401/403)
//   - 2 — validation  (IsValidationError: 400)
//   - 3 — network     (IsNetworkError: SDK network_error or timeout)
//   - 4 — anything else (rate limit, 5xx, SDK-internal, unknown)
//
// The function is pure: no I/O, no globals. Tests drive it with
// synthetic *polipage.Error values (no RoundTripper plumbing needed).
func exitCodeFor(err error) int {
	var pe *polipage.Error
	if !errors.As(err, &pe) {
		return 4
	}
	switch {
	case pe.IsAuthError():
		return 1
	case pe.IsValidationError():
		return 2
	case pe.IsNetworkError():
		return 3
	default:
		return 4
	}
}
