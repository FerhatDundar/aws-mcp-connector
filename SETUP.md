# Setup guide — AWS CLI connector

This connector shells out to the `aws` binary already installed and
configured on the host. It does not manage credentials itself — it uses
whatever the AWS CLI's own credential chain resolves (profile, SSO, IAM
role, or static keys). If `aws sts get-caller-identity` works in your
terminal, this connector will work too.

---

## 1. Prerequisites

1. **AWS CLI v2 installed** — `aws --version` to check; install from
   <https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html>
   if not.
2. **Credentials configured** — one of:
   - `aws configure` (static access key/secret, simplest)
   - `aws sso login` (IAM Identity Center / SSO)
   - An IAM role attached to the host (EC2/ECS/CloudShell — nothing to do)

   Verify with:

   ```bash
   aws sts get-caller-identity
   ```

   If that prints your account/ARN, you're set. If it errors, fix that
   first — this connector's startup check runs the exact same command and
   will refuse to start otherwise.

## 2. Get the server binary

**Option A — download a release (no Go needed):** grab
`aws-mcp-connector-plugin-<version>-<os>-<arch>.zip` from the
[latest release](https://github.com/FerhatDundar/aws-mcp-connector/releases/latest)
and unzip it — the `plugin/` folder inside is ready to install, skip to
step 4.

**Option B — build from source:** you need Go 1.25+ installed
(`go version` to check; get it from <https://go.dev/dl/> if not).

```bash
cd subprojects/aws-mcp-connector/go-server
go mod tidy                          # fetches deps, writes go.sum
go build -o aws-connector-server .
cp aws-connector-server ../plugin/servers/go/
```

## 3. Configure the plugin

Edit `plugin/.mcp.json`:

```json
{
  "mcpServers": {
    "aws": {
      "command": "${CLAUDE_PLUGIN_ROOT}/servers/go/aws-connector-server",
      "env": {
        "AWS_PROFILE": "",
        "AWS_REGION": "",
        "AWS_MCP_ALLOW_WRITE": "false",
        "AWS_MCP_ALLOWED_SERVICES": ""
      }
    }
  }
}
```

- Leave `AWS_PROFILE` empty to use your default profile, or set it to a
  named profile from `~/.aws/config`.
- Leave `AWS_REGION` empty to use the CLI's own default (from the profile
  or `AWS_DEFAULT_REGION`), or pin one explicitly.
- **`AWS_MCP_ALLOW_WRITE` defaults to `"false"` — read-only.** Mutating
  commands (`create-*`, `delete-*`, `put-*`, `update-*`, `terminate-*`,
  s3's `rm`/`mv`/`sync`/`cp`/`mb`/`rb`, etc.) are refused by the server
  before they ever reach AWS. Set it to `"true"` only once you're
  comfortable letting the agent make real changes — and even then, every
  individual mutating call still needs `confirm=true` passed explicitly.
- `AWS_MCP_ALLOWED_SERVICES` is optional extra scoping — a comma-separated
  allowlist like `"s3,ec2"` if you want to restrict which AWS services are
  reachable at all, regardless of read/write.
- If you're not using a profile/SSO/role, you can instead set
  `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`
  directly in this `env` block — but a profile is usually easier to
  rotate/manage.

Keep this file out of any shared/committed location if you put static
keys in it. (The subproject's `.gitignore` already blocks `.env`/`*.token`
files; the `.mcp.json` inside the *installed* plugin lives in Cowork's
plugin directory, not the repo.)

## 4. Install the plugin and verify

1. In the Claude desktop app: **Settings → Capabilities** → add a plugin
   from a local folder, and point it at:
   `subprojects/aws-mcp-connector/plugin/`
   (A plugin can't be registered from inside a chat session — it has to be
   added here.)
2. Restart / reload so the new MCP server is picked up.
3. Back in a chat, verify:
   - Ask: **"Run aws_whoami"** → should return your AWS account/ARN.
   - Ask: **"List my S3 buckets"** (`aws_exec` with `args: ["s3","ls"]`) →
     should return your buckets, or an empty list (not an error).
   - Ask it to do something mutating, e.g. **"tag this EC2 instance"** →
     should be refused with a clear read-only-mode message unless you've
     set `AWS_MCP_ALLOW_WRITE=true` and it passes `confirm=true`.

Done. From here you can ask things like *"what EC2 instances are running
in us-west-2?"*, *"show me the IAM policy attached to this role,"* or
*"what's the help text for `aws s3api put-bucket-policy`?"*

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---------|--------------------|
| Server refuses to start, `no usable AWS credentials found` | `aws sts get-caller-identity` fails in your terminal too — fix credentials first (`aws configure` / `aws sso login`). |
| `aws CLI not found` | The `aws` binary isn't on `PATH` for the process running this connector — install it, or set `AWS_MCP_CLI_PATH` to its full path. |
| Every mutating command is refused | Expected by default. Set `AWS_MCP_ALLOW_WRITE=true` in `.mcp.json` and pass `confirm=true` on the specific tool call. |
| `service "X" is not in the allowed list` | `AWS_MCP_ALLOWED_SERVICES` is set and doesn't include that service — add it or clear the variable. |
| `could not determine the AWS CLI service from args` | The first element of `args` was a flag (e.g. `--region`) instead of a service name. Put the service/subcommand first; pass global flags after, or set `AWS_REGION`/`AWS_PROFILE` on the server instead. |
| Output looks cut off | `max_bytes` was hit — raise it on the call (hard cap 5,000,000), or narrow the query (e.g. add `--max-items`, a filter, a narrower `--query`). |
| Tools don't appear at all | Binary not built/copied (step 2), or plugin not installed/reloaded (step 4). |
