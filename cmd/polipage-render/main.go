package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func main() {
	cmd := newRootCmd()
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		return
	}

	fmt.Fprintln(os.Stderr, "polipage-render:", err)

	var ex *exitErr
	if errors.As(err, &ex) {
		os.Exit(ex.code)
	}
	// Cobra surfaced a flag-parsing error (bad flag, etc.) — sysexits
	// EX_USAGE is the documented mapping.
	os.Exit(exitMisuse)
}
