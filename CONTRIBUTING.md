# Contributing to poli-page/gin

Thanks for your interest in improving this package. This guide covers setup, tests, and conventions.

## Setup

```bash
go mod download
```

Set `POLI_PAGE_API_KEY` to a `pp_sandbox_…` key before running integration tests. Set `POLI_PAGE_BASE_URL` too to target another API host; unset, the SDK default is used. The live API CI job reads the same two names from repository secrets: `POLI_PAGE_API_KEY` (a `pp_sandbox_` key) and the optional `POLI_PAGE_BASE_URL`.

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

Example app tests (a separate Go module):

```bash
cd example-app
go test -race ./...
```

### Contract tests

`example-app/contract_test.go` runs the integration cases (`impl.*`) of the [Poli Page API contract](https://github.com/poli-page/api-contract) against the example app. CI runs them against the contract version pinned in `.github/workflows/ci.yml`. Locally, download `contract.json` from that version and run, from `example-app/`:

```bash
POLI_CONTRACT=<path to contract.json> go test ./...
```

Without `POLI_CONTRACT` the test is skipped locally and fails when `CI` is set. A case allowed to fail goes in `example-app/testdata/contract-skip.json` with its reason; a listed case that passes fails the run.

## Lint, vet, build

```bash
gofmt -l .          # must produce no output
go vet ./...
golangci-lint run   # config pinned in .golangci.yml
go build ./...
(cd example-app && go vet ./... && golangci-lint run && go build ./...)
```

## Pull requests

- Branch off `main`; open the PR against `main`.
- Keep PRs focused — one feature or fix per PR.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md`.
- Match the existing code style; `gofmt` and `golangci-lint` will flag drift.

## Releasing

Releases are manual and there is no release workflow, as in [`poli-page/sdk-go`](https://github.com/poli-page/sdk-go). The Go module system is the registry: pushing a `vX.Y.Z` tag is the whole release. `proxy.golang.org` serves the tag within minutes of the first request for it, and `sum.golang.org` records its checksum. The module has no version constant; the tag is its version.

Module path: `github.com/poli-page/gin`. The path belongs to this repository, so no name has to be reserved. The proxy lists no tagged version before `v0.11.0`, only pseudo-versions of `main`.

What the module ships: the tracked files of the repository root and its packages (`cmd/polipage-render`, `tests/integration`), test files included as in every Go module. `example-app/` has its own `go.mod`, so it is a separate module and is left out of the `github.com/poli-page/gin` module zip; its `replace github.com/poli-page/gin => ..` applies only when `example-app` is the main module and never reaches a consumer.

### First release (one time)

Nothing to set up on a registry: no account, token, secret or GitHub environment. After the `v0.11.0` tag is pushed:

1. Request the version once so the proxy fetches it: `GOPROXY=https://proxy.golang.org GO111MODULE=on go list -m github.com/poli-page/gin@v0.11.0`.
2. Open `https://pkg.go.dev/github.com/poli-page/gin@v0.11.0` and, if it is not listed yet, click "Request" on that page. The license (MIT) must be detected for the docs to show.

### Each release

1. Move `[Unreleased]` to `[X.Y.Z] - YYYY-MM-DD` in `CHANGELOG.md` and update the compare links at the bottom.
2. If `github.com/poli-page/sdk-go` moved to a new minor, bump it in both `go.mod` files and the README requirements.
3. Commit `chore: release X.Y.Z` through a PR on `main` and wait for CI to be green.
4. On the merged `main`, run the pre-flight checks, with the contract version CI pins in `.github/workflows/ci.yml`:
   ```bash
   gofmt -l .
   go vet ./...
   golangci-lint run
   POLI_CONTRACT=<path to contract.json> go test -race -count=1 ./...
   (cd example-app && go vet ./... && golangci-lint run && POLI_CONTRACT=<path to contract.json> go test -race -count=1 ./...)
   ```
5. Tag the merge commit and push the tag: `git switch main && git pull --ff-only && git tag vX.Y.Z && git push origin vX.Y.Z`. A pushed tag is final: the checksum database keeps it, so a fix goes into a new version (`retract` in `go.mod` to warn users off a bad one).
6. Optionally create a GitHub release from the tag with the `CHANGELOG` section: `gh release create vX.Y.Z --notes-file <section>`. There are no binary artifacts; the module is the artifact.

## Reporting issues

Open an issue in this repo. Include the package version, Gin version, Go version (`go version`), and a minimal reproduction.
