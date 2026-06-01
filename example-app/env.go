package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// loadDotEnv reads KEY=VALUE pairs from path and pushes them into the
// process environment ONLY when the key is not already set there.
// Real shell exports always win — same rule the Symfony and NestJS
// example apps follow (INTEGRATIONS_PLAN.md §"Cross-cutting DX patterns" §2).
//
// Hand-rolled (no joho/godotenv dependency) — the parser is intentionally
// minimal: blank lines and comments (lines starting with '#') are
// skipped, single layer of surrounding quotes is stripped, no
// interpolation, no multi-line values.
//
// A missing file is not an error: the example app falls back to the
// process env it was launched with (Docker / k8s / systemd / `direnv`
// already inject vars; this loader is the dev-time convenience).
func loadDotEnv(path string) error {
	f, err := os.Open(path) //nolint:gosec // G304: example-app loads a developer-supplied path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue // shell export wins
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("setenv %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
