#!/usr/bin/env bash
# Renders a fresh server.json for the MCP Registry from a set of already-
# built release zip assets. Used by the release pipeline (and callable by
# hand) so server.json's version/identifiers/hashes always match exactly
# what's attached to the GitHub release being published.
#
# Usage: render-server-json.sh <version> <tag> <asset-dir>
#   version    e.g. 0.1.0  (no leading v)
#   tag        e.g. v0.1.0 (matches the GitHub release tag)
#   asset-dir  directory containing the
#              aws-mcp-connector-plugin-<tag>-<goos>-<goarch>.zip files
#
# Prints the rendered server.json to stdout.
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <version> <tag> <asset-dir>" >&2
  exit 1
fi

VERSION="$1"
TAG="$2"
ASSET_DIR="$3"
REPO="FerhatDundar/aws-mcp-connector"
PLATFORMS=(darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64 windows-arm64)

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
command -v shasum >/dev/null && HASH_CMD=(shasum -a 256) || HASH_CMD=(sha256sum)

env_vars='[
  {"name":"AWS_PROFILE","description":"Named profile from ~/.aws/config to use. Leave unset to use the default profile.","isRequired":false,"isSecret":false,"format":"string"},
  {"name":"AWS_REGION","description":"Default AWS region if not set elsewhere.","isRequired":false,"isSecret":false,"format":"string"},
  {"name":"AWS_ACCESS_KEY_ID","description":"Static AWS access key. Only needed if not using a profile/SSO/role.","isRequired":false,"isSecret":true,"format":"string"},
  {"name":"AWS_SECRET_ACCESS_KEY","description":"Static AWS secret key. Only needed if not using a profile/SSO/role.","isRequired":false,"isSecret":true,"format":"string"},
  {"name":"AWS_SESSION_TOKEN","description":"Temporary session token, if using short-lived credentials.","isRequired":false,"isSecret":true,"format":"string"},
  {"name":"AWS_MCP_ALLOW_WRITE","description":"Set to \"true\" to permit mutating AWS CLI commands. Defaults to read-only.","isRequired":false,"isSecret":false,"format":"string"},
  {"name":"AWS_MCP_ALLOWED_SERVICES","description":"Optional comma-separated allowlist of AWS CLI service names (e.g. \"s3,ec2\"). Unset allows all.","isRequired":false,"isSecret":false,"format":"string"},
  {"name":"AWS_MCP_CLI_PATH","description":"Optional path to the aws binary. Defaults to \"aws\" resolved via PATH.","isRequired":false,"isSecret":false,"format":"string"}
]'

packages="[]"
for plat in "${PLATFORMS[@]}"; do
  file="$ASSET_DIR/aws-mcp-connector-plugin-${TAG}-${plat}.zip"
  if [ ! -f "$file" ]; then
    echo "missing release asset: $file" >&2
    exit 1
  fi
  sha=$("${HASH_CMD[@]}" "$file" | awk '{print $1}')
  url="https://github.com/${REPO}/releases/download/${TAG}/aws-mcp-connector-plugin-${TAG}-${plat}.zip"

  pkg=$(jq -n \
    --arg url "$url" \
    --arg version "$VERSION" \
    --arg sha "$sha" \
    --argjson envVars "$env_vars" \
    '{
      registryType: "mcpb",
      identifier: $url,
      version: $version,
      fileSha256: $sha,
      transport: { type: "stdio" },
      environmentVariables: $envVars
    }')
  packages=$(jq --argjson p "$pkg" '. + [$p]' <<<"$packages")
done

jq -n \
  --arg version "$VERSION" \
  --arg repo "$REPO" \
  --argjson packages "$packages" \
  '{
    "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
    name: "io.github.FerhatDundar/aws-mcp-connector",
    description: "MCP server for the AWS CLI. Read-only by default; single Go binary.",
    repository: { url: ("https://github.com/" + $repo), source: "github" },
    version: $version,
    websiteUrl: ("https://github.com/" + $repo + "#readme"),
    packages: $packages
  }'
