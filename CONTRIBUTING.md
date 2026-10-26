# Contributing to poli-page/gin

Thanks for your interest in improving this package. This guide covers setup, tests, and conventions.

## Setup

```bash
go mod download
```

Set `POLI_PAGE_API_KEY` to a `pp_sandbox_…` key before running integration tests.

## Tests

Unit tests:

```bash
go test ./...
```

Race detector:

```bash
go test -race ./...
```

Integration tests:

```bash
go test -tags=integration ./tests/integration/...
```

Integration tests are skipped unless `POLI_PAGE_API_KEY` starts with `pp_sandbox_`, so a key of a counted environment never renders from a test.

## Lint, vet, build

```bash
gofmt -l .          # must produce no output
go vet ./...
golangci-lint run   # config pinned in .golangci.yml
go build ./...
```

## Pull requests

- Branch off `main`; open the PR against `main`.
- Keep PRs focused — one feature or fix per PR.
- Add an entry under `## Unreleased` in `CHANGELOG.md`.
- Match the existing code style; `gofmt` and `golangci-lint` will flag drift.

## Reporting issues

Open an issue in this repo. Include the package version, Gin version, Go version (`go version`), and a minimal reproduction.
