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

Releases are manual, as in [`poli-page/sdk-go`](https://github.com/poli-page/sdk-go). The Go module system is the registry: pushing a `vX.Y.Z` tag is the whole release. `proxy.golang.org` serves the tag within minutes of the first request for it, and `sum.golang.org` records its checksum. The module has no version constant; the tag is its version. Nothing is set up on a registry: no account, token, secret or GitHub environment.

Module path: `github.com/poli-page/gin`. The path belongs to this repository, so no name has to be reserved. The first tagged version is `v0.11.0`; before it the proxy lists only pseudo-versions of `main`.

What the module ships: the tracked files of the repository root and its packages (`cmd/polipage-render`, `tests/integration`), test files included as in every Go module. `example-app/` has its own `go.mod`, so it is a separate module and is left out of the `github.com/poli-page/gin` module zip; its `replace github.com/poli-page/gin => ..` applies only when `example-app` is the main module and never reaches a consumer.

### Versions

`github.com/poli-page/gin` shares its major.minor with the Poli Page API: module `0.13.x` talks to API `0.13`, passes the [API contract](https://github.com/poli-page/api-contract) `v0.13.0`, and requires `github.com/poli-page/sdk-go` of the same minor (`v0.13.0` in both `go.mod` files). A `go.mod` requirement is a minimum and cannot cap the version, so the README asks users to keep the SDK on `0.13.x`. Patch numbers are free: a fix to the module alone is a patch, whatever the API's or the SDK's patch. A change of the API contract, a new feature, and a raise of the minimum Go or Gin version are each a minor. The module may skip minors when it has nothing to ship for one; there was no `0.12`.

`scripts/check-release-versions.sh vX.Y.Z` refuses a version whose major.minor differs from the contract version pinned in `.github/workflows/ci.yml` (a stable contract tag on the pinned commit, never the comment next to it or a commented-out line; every workflow that pins the contract must pin the same commit) or from the `github.com/poli-page/sdk-go` requirement in `go.mod` and `example-app/go.mod`, and names both versions. It also refuses a version with no `## [X.Y.Z] - ` section in `CHANGELOG.md`. A pushed tag is the release, so run it before tagging (step 4 below); the Release workflow (`.github/workflows/release.yml`) re-runs it on the pushed tag, so a mismatch shows up red at once.

### Each release

1. Move `[Unreleased]` to `[X.Y.Z] - YYYY-MM-DD` in `CHANGELOG.md` and update the compare links at the bottom.
2. For a new minor, require `github.com/poli-page/sdk-go` of that minor in both `go.mod` files (`go get github.com/poli-page/sdk-go@vX.Y.Z && go mod tidy`, then the same in `example-app/`), pin the API contract of that minor in `.github/workflows/ci.yml` and `.github/workflows/ci-tip.yml`, and update the README requirements and install line.
3. Commit `chore: release X.Y.Z` through a PR on `main` and wait for CI to be green.
4. Update `main`, then run the pre-flight checks on it, with the contract version CI pins in `.github/workflows/ci.yml`. They are the gate: a pushed tag cannot be taken back, so **if any command fails, stop: do not tag.** The subshell stops at the first failure, and its last line prints only when every check passed; only then is `checked` set to the commit, and step 5 refuses to tag while it is empty. Run it as is: `set -e` has no effect in a subshell followed by `&&` or `||`, so the status is tested on its own line.
   ```bash
   git switch main && git pull --ff-only
   checked=
   sha="$(git rev-parse HEAD)"
   (
     set -euo pipefail
     ./scripts/check-release-versions.sh vX.Y.Z
     test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
     go vet ./...
     golangci-lint run
     CI=true POLI_CONTRACT=<path to contract.json> go test -race -count=1 ./...
     cd example-app
     go vet ./...
     golangci-lint run
     CI=true POLI_CONTRACT=<path to contract.json> go test -race -count=1 ./...
     echo "pre-flight passed on $sha"
   )
   [ $? -eq 0 ] && checked="$sha"
   ```
5. Tag exactly the commit you checked, and only while it is still the tip of `origin/main`; do not pull again. If the pre-flight failed (`checked` is empty) or `origin/main` moved, nothing is tagged: fix the failure, or go back to step 4 on the new `main`.
   ```bash
   test -n "$checked" && git fetch origin \
     && test "$(git rev-parse HEAD)" = "$checked" && test "$(git rev-parse origin/main)" = "$checked" \
     && git tag vX.Y.Z "$checked" && git push origin vX.Y.Z
   ```
   A pushed tag is final: the checksum database keeps it, so a fix goes into a new version (`retract` in `go.mod` to warn users off a bad one).
6. Check that the Release workflow is green on the tag.
7. Request the version once so the proxy fetches it: `GOPROXY=https://proxy.golang.org GO111MODULE=on go list -m github.com/poli-page/gin@vX.Y.Z`. Then open `https://pkg.go.dev/github.com/poli-page/gin@vX.Y.Z` and, if it is not listed yet, click "Request" on that page.
8. Optionally create a GitHub release from the tag with the `CHANGELOG` section: `gh release create vX.Y.Z --notes-file <section>`. There are no binary artifacts; the module is the artifact.

## Reporting issues

Open an issue in this repo. Include the package version, Gin version, Go version (`go version`), and a minimal reproduction.
