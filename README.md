<div align="center">

# 🔌 aws-mcp-connector

**Talk to AWS CLI from an MCP-speaking agent.**

[![CI](https://github.com/FerhatDundar/aws-mcp-connector/actions/workflows/ci.yml/badge.svg)](https://github.com/FerhatDundar/aws-mcp-connector/actions/workflows/ci.yml)
[![CodeQL](https://github.com/FerhatDundar/aws-mcp-connector/actions/workflows/codeql.yml/badge.svg)](https://github.com/FerhatDundar/aws-mcp-connector/actions/workflows/codeql.yml)
[![Latest release](https://img.shields.io/github/v/release/FerhatDundar/aws-mcp-connector?color=blueviolet&label=release)](https://github.com/FerhatDundar/aws-mcp-connector/releases/latest)
[![MCP Registry](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fregistry.modelcontextprotocol.io%2Fv0%2Fservers%3Fsearch%3Daws-mcp-connector&query=%24.servers%5B-1%3A%5D.server.version&label=MCP%20Registry&prefix=v&color=6A2FEE&logo=modelcontextprotocol)](https://registry.modelcontextprotocol.io/?q=aws-mcp-connector)
[![Go Reference](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/protocol-MCP-orange)](https://modelcontextprotocol.io/)
[![Conventional Commits](https://img.shields.io/badge/commits-conventional-ff69b4)](https://www.conventionalcommits.org/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

---

A single static Go binary that speaks the [Model Context Protocol](https://modelcontextprotocol.io/)
and lets an agent run AWS CLI commands: any `aws <service> <operation>`
invocation, across every service the CLI supports, with a read-only-by-default
safety gate on anything that mutates state.

No Python, no `uv`, no runtime dependency to install — just a binary and
an `.mcp.json`. It shells out to the `aws` binary already installed and
configured on the host (profile, SSO, IAM role, or static keys — whatever
the AWS CLI's own credential chain resolves) instead of reimplementing the
AWS SDK, so it gets the full breadth of the CLI for free rather than a
hand-curated subset of services.

## ✨ Why this exists

> An agent that only has a narrow, hand-picked set of AWS tools hits a wall
> the moment you need something outside that set. This connector instead
> wraps the AWS CLI itself, so an agent can run `aws s3 ls`, `aws ec2
> describe-instances`, `aws iam list-users` — anything the CLI can do —
> without waiting on a new tool to be written for it. Mutating commands are
> blocked by default and require both a server-level opt-in and a per-call
> `confirm=true`, so exploring/debugging is safe out of the box.

## 🧰 Tools

| Tool | What it does | Write? |
|---|---|:---:|
| `aws_exec` | Run any `aws <service> <operation> ...` command. Read-only by default — mutating commands need `AWS_MCP_ALLOW_WRITE=true` on the server *and* `confirm=true` on the call. | ✅ (gated) |
| `aws_help` | Show `aws <service> [subcommand] help` text — always safe, use it to check exact syntax before calling `aws_exec`. | |
| `aws_whoami` | Show the AWS identity (account, ARN, user/role) the configured credentials resolve to. | |
| `aws_list_profiles` | List named profiles configured in `~/.aws/config` on the host. | |

Every tool accepts an optional `response_format`: `markdown` (default,
pretty tables for a chat UI) or `json` (for programmatic use).

## 🚀 Quickstart

**Fastest path:** grab a prebuilt bundle from the [latest release](https://github.com/FerhatDundar/aws-mcp-connector/releases/latest) —
download `aws-mcp-connector-plugin-<version>-<os>-<arch>.zip`, unzip it,
and point Cowork/Claude at the `plugin/` folder inside (see step 4 of
[SETUP.md](SETUP.md)). No Go toolchain required.

**From source:**

```bash
# 1. Build
cd go-server
go mod tidy
go build -o aws-connector-server .
cp aws-connector-server ../plugin/servers/go/

# 2. Set up auth — needs the aws CLI itself installed and configured
#    (aws configure / aws sso login) — see SETUP.md
export AWS_PROFILE=default   # optional, only if not using "default"

# 3. Run
./go-server/aws-connector-server   # serves MCP over stdio
```

Or `make build` — see the [Makefile](Makefile) for every shortcut
(`test`, `vet`, `fmt`, `lint`, `tidy`).

Full walkthrough — including wiring this up as a Claude/Cowork plugin — is
in **[SETUP.md](SETUP.md)**.

## 🔐 Configuration

Everything is environment variables, passed through by the plugin's
`.mcp.json`:

| Variable | Purpose | Default |
|---|---|---|
| `AWS_PROFILE` | Named profile from `~/.aws/config` to use. | unset (default profile) |
| `AWS_REGION` | Default region if not set elsewhere. | AWS CLI's own default |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` | Static credentials — only needed if not using a profile/SSO/role. | unset |
| `AWS_MCP_ALLOW_WRITE` | `"true"` to permit mutating commands at all (still needs `confirm=true` per call). | `false` (read-only) |
| `AWS_MCP_ALLOWED_SERVICES` | Comma-separated allowlist of AWS CLI service names, e.g. `"s3,ec2"`. | unset (unrestricted) |
| `AWS_MCP_CLI_PATH` | Path to the `aws` binary. | `aws` resolved via `PATH` |

## 🧪 Quality bar

This isn't a toy script — it's got the same checks you'd expect from a
production Go service:

- ✅ **Unit tests** for every input-validation path (`go test ./...`)
- ✅ **`go vet`** + **`gofmt`** clean
- ✅ **[golangci-lint](https://golangci-lint.run/)** (govet, staticcheck, errcheck, gosec, and more)
- ✅ **[govulncheck](https://go.dev/blog/vuln)** — no known vulnerabilities in the dependency graph
- ✅ **[CodeQL](https://codeql.github.com/)** static security analysis on every push
- ✅ **End-to-end verified** against a real (or sandboxed) AWS CLI backend — not mocks
- ✅ **[Dependabot](.github/dependabot.yml)** keeps Go modules and Actions current

All of it runs in [CI](.github/workflows/ci.yml) on every push and PR.

## 🏷️ Releases & versioning

Versions follow [semver](https://semver.org/) and are cut automatically by
[release-please](https://github.com/googleapis/release-please) from
[Conventional Commits](https://www.conventionalcommits.org/) on `main`:

- `fix: ...` → patch (`v0.1.0` → `v0.1.1`)
- `feat: ...` → minor (`v0.1.1` → `v0.2.0`)
- `feat!: ...` / `BREAKING CHANGE:` footer → major (`v0.2.0` → `v1.0.0`)

Every merged PR updates a standing **"chore(main): release vX.Y.Z"** PR
with an auto-generated [CHANGELOG.md](CHANGELOG.md). Merging that PR:

1. tags the release and publishes it on GitHub
2. builds and attaches zipped, ready-to-install plugin bundles for
   linux/darwin/windows × amd64/arm64
3. regenerates `server.json` from those exact assets (fresh version +
   SHA-256 hashes) and publishes it to the
   [official MCP Registry](https://registry.modelcontextprotocol.io/) via
   `mcp-publisher`, authenticated with GitHub OIDC — no stored secrets

See [.github/workflows/release-please.yml](.github/workflows/release-please.yml)
and [.github/workflows/publish-mcp-registry.yml](.github/workflows/publish-mcp-registry.yml)
(also runnable by hand for an existing tag via `workflow_dispatch`).

## 📁 Layout

```
aws-mcp-connector/
├── README.md                  ← you are here
├── SETUP.md                   ← step-by-step setup guide
├── CONTRIBUTING.md             ← how to contribute
├── CODE_OF_CONDUCT.md
├── SECURITY.md                 ← vulnerability reporting
├── CODEOWNERS
├── LICENSE                     ← MIT
├── Makefile                    ← build / test / lint shortcuts
├── .golangci.yml                ← lint rules
├── release-please-config.json  ← semver/changelog automation config
├── .release-please-manifest.json
├── server.json                  ← MCP Registry manifest (regenerated fresh per release by CI)
├── scripts/
│   └── render-server-json.sh    ← rebuilds server.json from a release's zip assets
├── .github/
│   ├── workflows/
│   │   ├── ci.yml                     ← build, vet, test, lint, govulncheck
│   │   ├── codeql.yml                 ← security scanning
│   │   ├── pr-title.yml               ← Conventional Commits PR title check
│   │   ├── release-please.yml         ← version PRs, tagging, GitHub releases
│   │   ├── publish-mcp-registry.yml   ← publishes server.json to the MCP Registry
│   │   └── rebuild-release-assets.yml ← manual re-attach fallback
│   ├── ISSUE_TEMPLATE/
│   ├── PULL_REQUEST_TEMPLATE.md
│   └── dependabot.yml
├── go-server/                  ← the MCP server source
│   ├── main.go
│   ├── main_test.go
│   ├── go.mod / go.sum
│   └── README.md
└── plugin/                     ← installable Cowork/Claude plugin
    ├── .claude-plugin/plugin.json
    ├── .mcp.json                ← holds credentials locally — never commit real ones
    └── servers/go/              ← compiled binary goes here
```

## 🤝 Contributing

PRs and issues are very welcome — see **[CONTRIBUTING.md](CONTRIBUTING.md)**
for the full guide (setup, coding conventions, how to add a new tool) and
the **[Code of Conduct](CODE_OF_CONDUCT.md)**.

`main` is protected: every change, including the maintainer's, lands via
pull request with CI green. PR titles must follow
[Conventional Commits](https://www.conventionalcommits.org/) — that's what
drives the automatic versioning above.

Found a security issue? Please follow **[SECURITY.md](SECURITY.md)**
instead of opening a public issue.

## 📄 License

[MIT](LICENSE) © FerhatDundar
