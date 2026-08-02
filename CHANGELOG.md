# Changelog

## [0.1.1](https://github.com/FerhatDundar/aws-mcp-connector/compare/v0.1.0...v0.1.1) (2026-08-02)


### Bug Fixes

* **scripts:** interpolate REPO via jq --arg instead of literal ${REPO} in server.json render ([ea148ea](https://github.com/FerhatDundar/aws-mcp-connector/commit/ea148eac311dd7192bf052558959d6ac5023544b))

## 0.1.0 (2026-08-02)

Initial release.

### ✨ Features

- MCP server exposing 4 tools for running the AWS CLI directly:
  `aws_exec`, `aws_help`, `aws_whoami`, `aws_list_profiles`
- Shells out to the `aws` binary rather than reimplementing the AWS SDK,
  so it covers every service the CLI supports, not a hand-curated subset
- Read-only by default: mutating commands (`create-*`, `delete-*`,
  `put-*`, `update-*`, `terminate-*`, s3's `rm`/`mv`/`sync`/`cp`/`mb`/`rb`,
  etc.) are refused unless `AWS_MCP_ALLOW_WRITE=true` is set on the server
  *and* `confirm=true` is passed on the specific call
- Optional `AWS_MCP_ALLOWED_SERVICES` allowlist to further scope which AWS
  services are reachable at all
- Auth via the AWS CLI's own credential chain (profile, SSO, IAM role, or
  static keys) — no credential handling in this connector itself
- `markdown`/`json` response formats on every tool
- `--version` flag; ldflag-injected build version

### 🧪 Quality

- Unit tests (including the mutating-command classifier and every safety
  gate), `go vet`, `gofmt`, `golangci-lint` (including gosec), `govulncheck`,
  and CodeQL all wired into CI
- End-to-end verified against real AWS during development: `aws_whoami`
  and `aws_list_profiles` against real account/config, `aws_exec` reads
  (`s3 ls`, `ec2 describe-instances`), confirmed the read-only and
  allowlist gates block correctly, and confirmed the write-gate genuinely
  opens the pipe to AWS once both `AWS_MCP_ALLOW_WRITE=true` and
  `confirm=true` are set — verified safely via a `--dry-run` EC2 call that
  validates permissions without mutating anything

### 🤝 Project infrastructure

- Contribution guide, Code of Conduct, security policy, issue/PR templates
- Branch protection: all changes (including the maintainer's) land via
  reviewed, CI-green pull requests
- Automated semver releases via [release-please](https://github.com/googleapis/release-please),
  starting from this baseline
- Cross-platform (linux/darwin/windows × amd64/arm64) zipped plugin
  bundles attached to every release

---

*From here on, this file is maintained automatically by release-please
based on [Conventional Commits](https://www.conventionalcommits.org/).*
