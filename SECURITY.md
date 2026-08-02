# Security Policy

## Supported Versions

Only the latest published release is supported with security fixes.

| Version | Supported |
|---|---|
| latest | ✅ |
| older  | ❌ |

## Reporting a Vulnerability

Please **do not** open a public issue for security vulnerabilities.

Instead, use GitHub's [private vulnerability reporting](https://github.com/FerhatDundar/aws-mcp-connector/security/advisories/new)
for this repository, or email **ffrhtd@gmail.com** directly with:

- A description of the vulnerability and its potential impact
- Steps to reproduce (a minimal repro is very helpful)
- Any suggested fix, if you have one

You should get an acknowledgement within a few days. Once a fix is
available, a patch release will be cut and the reporter credited (unless
you'd prefer to stay anonymous).

## What's in scope

- The Go MCP server itself (`go-server/`)
- The GitHub Actions workflows (supply-chain concerns — e.g. unpinned
  actions, secret handling)

Credential handling issues are taken especially seriously: this connector
reads credentials from environment variables and never logs or persists
them — if you find a path where they leak (into error messages, logs, or
tool output), please report it.

## Automated scanning already in place

This repo runs [CodeQL](.github/workflows/codeql.yml) and
[govulncheck](.github/workflows/ci.yml) on every push and on a weekly
schedule. If you find something these missed, that's exactly what this
policy is for.
