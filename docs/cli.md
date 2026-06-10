# `polipage-render` CLI

> The `cmd/polipage-render` binary renders a template end-to-end from the shell, mirroring the Symfony bundle's `bin/console poli-page:render`. Use it as a smoke test in CI and a one-shot render tool in scripts.

## Why

A Gin app's smoke test is "boot the server, curl a route". That works for a developer at a workstation; it's annoying for CI and useless for a teammate who just wants to verify their API key resolves to a usable account. A single Go binary that calls the SDK with one flag per render input is faster, scriptable, and exits with a status code your shell can branch on. The shape copies the Symfony bundle's `poli-page:render` so devs moving between stacks find the same flags.

## How

### Install

```bash
go install github.com/poli-page/gin/cmd/polipage-render@latest
```

Or run without installing:

```bash
go run github.com/poli-page/gin/cmd/polipage-render --help
```

### Render to a file

```bash
polipage-render \
    --project=getting-started \
    --template=welcome \
    --template-version=1.0.0 \
    --data='{"name":"World"}' \
    -o welcome.pdf
```

### Render to stdout (pipe-friendly)

```bash
polipage-render --project=getting-started --template=welcome \
    --template-version=1.0.0 --data='{"name":"World"}' \
    > welcome.pdf
```

When `-o` is omitted the binary writes the PDF bytes to stdout. Diagnostics still go to stderr.

### Render inline HTML (no project required)

```bash
polipage-render \
    --inline-template='<h1>Hello {{ name }}</h1>' \
    --data='{"name":"World"}' \
    -o hello.html
```

Inline mode renders via `Render.Preview` and writes the HTML output; the SDK does not support PDF rendering from a raw `--inline-template` (only project-mode inputs can produce PDFs). Use `--project` + `--template` for a PDF.

## Flags

| Flag | Required | Description |
|---|---|---|
| `--project` | one of `--project` or `--inline-template` | Project slug. |
| `--template` | with `--project` | Template slug. |
| `--template-version` | with `--project` | Template version (semver). **Not** `--version` — see [Gotchas](#gotchas). |
| `--inline-template` | one of `--project` or `--inline-template` | Raw HTML template body. |
| `--data` | _required_ | JSON object passed to the template. |
| `--format` | _optional_ | Page format: `A4`, `Letter`, `Legal`, etc. SDK default applies otherwise. |
| `--orientation` | _optional_ | `portrait` or `landscape`. |
| `--locale` | _optional_ | BCP 47 locale string. |
| `-o`, `--out` | _optional_ | Output file path. Stdout when omitted. |
| `--timeout` | _optional_ | Per-request timeout (`time.ParseDuration` syntax: `30s`, `2m`). |

Environment variables `POLI_PAGE_API_KEY`, `POLI_PAGE_BASE_URL`, `POLI_PAGE_TIMEOUT`, `POLI_PAGE_MAX_RETRIES`, `POLI_PAGE_RETRY_DELAY` are honoured; flags override them when both are set.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. PDF written to `-o` or stdout. |
| `1` | Authentication or authorization failure (`*polipage.Error.IsAuthError()`). |
| `2` | Validation rejected the render (`IsValidationError()`, includes missing data, bad version format, missing template). |
| `3` | Network/timeout failure (`IsNetworkError()`). |
| `4` | Anything else — rate limit, 5xx, or an SDK-internal error. The stderr message names the code. |
| `64` | Misuse — bad flag, missing required flag, invalid JSON in `--data`. |

A simple bash check:

```bash
if ! polipage-render --project=... --template=... --template-version=... --data='{...}' -o out.pdf; then
    case $? in
        1) echo "API key issue" >&2 ;;
        2) echo "Template or data rejected" >&2 ;;
        3) echo "Network down" >&2 ;;
        *) echo "Render failed" >&2 ;;
    esac
    exit 1
fi
```

## CI recipe

GitHub Actions step that smoke-tests every push:

```yaml
# .github/workflows/ci.yml
- name: Render smoke test
  if: env.POLI_PAGE_API_KEY != ''
  env:
    POLI_PAGE_API_KEY: ${{ secrets.POLI_PAGE_API_KEY }}
  run: |
    go run ./cmd/polipage-render \
        --project=getting-started \
        --template=welcome \
        --template-version=1.0.0 \
        --data='{"name":"ci"}' \
        -o /tmp/welcome.pdf
    test -s /tmp/welcome.pdf
```

The `if` guard means the step is silently skipped when forks open PRs without secrets — no spurious red builds.

## Gotchas

- **Use `--template-version`, not `--version`.** Cobra reserves `--version` as a global flag for binaries that opt into it; collisions silently shadow your value with the global one. Same hazard hit the Symfony bundle (`bin/console poli-page:render` was renamed `--template-version` after the original command silently produced no output). See `INTEGRATIONS_PLAN.md` §"Cross-cutting DX patterns" §3 for the cross-framework list.

- **`--data` is a single JSON string, not multiple `--data key=value` flags.** Quote it carefully in your shell. Bash heredoc helps for complex payloads:

  ```bash
  polipage-render --project=... --template=... --template-version=... -o out.pdf --data="$(cat <<'JSON'
  {"items": [{"sku": "A-1", "qty": 3}, {"sku": "B-7", "qty": 1}]}
  JSON
  )"
  ```

- **Stdout-only mode never prints anything but PDF bytes.** Don't redirect both stdout *and* stderr to a file (`> out.pdf 2>&1`) — that corrupts the PDF with the diagnostic lines.

- **`POLI_PAGE_API_KEY` is read from the process environment.** The binary does NOT load `.env` files — keep the load step out of the binary so it can be embedded in any orchestration (`docker run -e ...`, `kubectl exec`, `cron`).

## Related

- [README → Install](../README.md#install) — the one-liner that invokes this binary.
- [docs/spec/gin-integration-specification.md](spec/gin-integration-specification.md) §9 — the full CLI design (exit code table, flag rationale, why Cobra over `flag`).
- Symfony bundle's `docs/cli.md` at `/Users/mickael/Projects/symfony-bundle/docs/cli.md` — the cross-stack reference. Flags and exit codes match by design.
