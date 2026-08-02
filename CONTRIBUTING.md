# Contributing to aws-mcp-connector

Thanks for considering a contribution — patches, bug reports, and docs
fixes are all welcome.

## Ground rules

- Every change lands via **pull request**. `main` is protected — even the
  maintainer merges through PRs (direct pushes are reserved for trivial
  fixes).
- CI must be green before merge: build, tests, `gofmt`, `go vet`,
  `golangci-lint`, and `govulncheck`.
- Please read the [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting set up

You'll need Go 1.25+ <!-- and whatever local test double this connector uses, e.g. Docker for a sandbox service -->.

```bash
git clone https://github.com/FerhatDundar/aws-mcp-connector.git
cd aws-mcp-connector
make build        # builds go-server/aws-connector-server
make sandbox-up    # starts the local test double, if this connector has one
make test          # go test ./...
make vet           # go vet ./...
make fmt           # gofmt check
make lint          # golangci-lint (install it first: https://golangci-lint.run/welcome/install/)
```

See [SETUP.md](SETUP.md) for the full setup walkthrough (including how to
get test credentials) and [go-server/README.md](go-server/README.md) for
how the code is laid out.

## Making a change

1. **Fork** the repo and create a branch off `main`:
   `git checkout -b feat/short-description`
2. Make your change. Keep the diff focused — one logical change per PR is
   much easier to review than a grab-bag.
3. Add or update tests in `go-server/main_test.go` for anything
   behavioral. New tools need at least an input-validation test; if you
   can exercise it against a real (or sandboxed) backend, even better.
4. Run `make fmt vet test lint` locally — matches what CI checks.
5. Commit using [Conventional Commits](https://www.conventionalcommits.org/):
   - `feat: add aws_copy_item tool`
   - `fix: handle empty prefix in aws_list_items`
   - `docs: clarify auth setup`
   - `feat!: rename max_bytes to byte_limit` (`!` = breaking change)

   This matters beyond style: releases are automated from these prefixes
   (`fix:` → patch bump, `feat:` → minor, `!` → major). A PR with an
   unconventional title will fail the **PR title check**.
6. Push and open a PR against `main`. Fill in the PR template — it's
   short on purpose.

## Adding a new tool

Look at any existing tool in `go-server/main.go` for the pattern: a core
function returning `map[string]any`, a `*MD` markdown renderer, and an
`s.AddTool(...)` registration block near the bottom of `main()`. Keep new
tools consistent with the existing ones:

- Validate required arguments before touching the API client.
- Route real API errors through the friendly-error wrapper so failures
  stay actionable.
- Support `response_format` (`markdown`/`json`) like every other tool.
- Mark destructive operations with `mcp.WithDestructiveHintAnnotation(true)`.

## Reporting bugs

Open an issue with the **Bug report** template. The more reproducible,
the faster it gets fixed — a minimal `aws_*` tool call plus what you
expected vs. what happened is ideal. If it's a security issue, see
[SECURITY.md](SECURITY.md) instead of opening a public issue.

## Questions / feature ideas

Open an issue with the **Feature request** template, or start a
[Discussion](https://github.com/FerhatDundar/aws-mcp-connector/discussions)
if one is enabled.
