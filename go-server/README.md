# go-server

MCP server source for the AWS CLI connector. Single-file (`main.go`),
built with [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go). No AWS
SDK dependency — it shells out to the `aws` binary itself rather than
reimplementing it.

## Build

```bash
go mod tidy                          # fetches deps, writes go.sum
go build -o aws-connector-server .
cp aws-connector-server ../plugin/servers/go/
```

Requires Go 1.25+ and the `aws` CLI v2 installed and configured
(`aws configure` / `aws sso login`) — see `../SETUP.md`.

## Manual test against real AWS

There's no official local sandbox for the AWS CLI itself (unlike LocalStack
for individual services), so this connector was end-to-end verified
against real AWS: `aws_whoami` against the real caller identity,
`aws_list_profiles` against a real `~/.aws/config`, `aws_help` for syntax
lookup, `aws_exec` for read-only calls (`s3 ls`, `ec2 describe-instances`),
confirmed mutating commands are refused without `AWS_MCP_ALLOW_WRITE=true`
+ `confirm=true`, confirmed the `AWS_MCP_ALLOWED_SERVICES` allowlist blocks
out-of-scope services, and confirmed that once both write gates are open
the command genuinely reaches AWS (verified with a `--dry-run` EC2 call,
which validates permissions/syntax without mutating anything).

Minimal manual-test pattern — run the server directly and drive it over
stdio with a JSON-RPC test script (or any MCP client):

```bash
./aws-connector-server
```

Then send `initialize`, `notifications/initialized`, and `tools/call`
JSON-RPC messages over stdin (one per line), reading one JSON reply per
line from stdout.

## Code layout

- **Mutating-command classification** — `isMutatingCommand` /
  `mutatingVerbPrefixes` / `mutatingExactVerbs` — the read-only safety
  gate's core logic, kept separate and unit-testable.
- **Server config** — `loadServerConfig` resolves `AWS_MCP_ALLOW_WRITE`,
  `AWS_MCP_ALLOWED_SERVICES`, `AWS_MCP_CLI_PATH` from the environment once
  at startup.
- **Core operations** — `execAWSCommand`, `runAWSHelp`, `runWhoami`,
  `runListProfiles`, each returning a `map[string]any` for uniform
  JSON/Markdown rendering.
- **Markdown rendering** — one `*MD` function per operation.
- **MCP wiring** — `main()` registers all 4 tools on an `mcp-go` server and
  serves over stdio.
