// Command aws-connector-server is an MCP server that lets an agent run AWS
// CLI commands. It shells out to the `aws` binary already installed and
// configured on the host (profiles, SSO, IAM roles, or static keys —
// whatever the AWS CLI's own credential chain already resolves) rather than
// reimplementing the AWS SDK, so it inherits the full breadth of the CLI
// (every service, every operation) for free instead of a hand-curated
// subset. Built with mark3labs/mcp-go, mirroring the layout and conventions
// of the sibling {slug}-mcp-connector repos (s3-mcp-connector,
// discord-mcp-connector, github-mcp-connector, sqlite-mcp-connector).
//
// Safety: mutating commands (anything that isn't a clear read — create/
// delete/put/update/terminate/etc., or high-level s3 verbs like rm/mv/sync)
// are blocked unless both AWS_MCP_ALLOW_WRITE=true is set for the server
// process AND the caller passes confirm=true on that specific tool call.
// Read-only by default.
//
// Auth / config (all via environment variables, passed through by the
// plugin's .mcp.json):
//
//	AWS_PROFILE            — optional. Named profile from ~/.aws/config.
//	AWS_REGION              — optional. Default region if not set elsewhere.
//	AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN — optional
//	    static credentials; only needed if no profile/SSO/role is set up.
//	AWS_MCP_ALLOW_WRITE     — "true" to permit mutating commands at all.
//	    Defaults to "false" (read-only mode).
//	AWS_MCP_ALLOWED_SERVICES — optional comma-separated allowlist of AWS CLI
//	    top-level service names (e.g. "s3,ec2,iam"). Defaults to unrestricted.
//	AWS_MCP_CLI_PATH        — optional path to the aws binary. Defaults to
//	    "aws" resolved via PATH.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultTimeoutSeconds = 30
	maxTimeoutSeconds     = 120
	defaultMaxBytes       = 200_000
	hardMaxBytes          = 5_000_000
)

// ---------------------------------------------------------------------
// Mutating-command classification — the read-only safety gate
// ---------------------------------------------------------------------

// mutatingVerbPrefixes catches the vast majority of AWS CLI "low-level"
// (service-api, e.g. `aws ec2api ...` / `aws s3api ...`) operation names,
// which are consistently verb-first and hyphenated.
var mutatingVerbPrefixes = []string{
	"create-", "delete-", "put-", "update-", "remove-", "terminate-",
	"stop-", "start-", "run-", "modify-", "attach-", "detach-",
	"revoke-", "authorize-", "tag-", "untag-", "associate-",
	"disassociate-", "reboot-", "reset-", "purge-", "enable-",
	"disable-", "add-", "replace-", "set-", "register-",
	"deregister-", "apply-", "deploy-", "invoke-", "publish-",
	"send-", "copy-", "move-", "restore-", "reject-", "accept-",
	"cancel-", "abort-", "import-", "export-", "grant-",
	"promote-", "failover-", "reencrypt-", "rotate-",
}

// mutatingExactVerbs catches the AWS CLI's "high-level" commands (mainly
// under `aws s3 ...`), which are short verbs rather than hyphenated names.
var mutatingExactVerbs = map[string]bool{
	"rm": true, "mv": true, "sync": true, "cp": true,
	"mb": true, "rb": true, "website": true,
}

// isMutatingCommand reports whether any positional (non-flag) token in the
// command looks like a mutating operation. This is deliberately
// conservative — it errs toward classifying ambiguous things as mutating
// rather than risk silently allowing a destructive call through.
func isMutatingCommand(args []string) bool {
	for _, a := range args {
		low := strings.ToLower(a)
		if strings.HasPrefix(low, "-") {
			continue
		}
		if mutatingExactVerbs[low] {
			return true
		}
		for _, p := range mutatingVerbPrefixes {
			if strings.HasPrefix(low, p) {
				return true
			}
		}
	}
	return false
}

// topLevelService returns args[0] lowercased, which by convention (and by
// this connector's tool description) must be the AWS CLI service/command
// name (e.g. "s3", "ec2", "iam") — global flags like --region/--profile
// belong after the subcommand, or should come from AWS_REGION/AWS_PROFILE
// env vars instead, precisely so this check can't be bypassed by putting a
// flag first. Returns "" if args is empty or args[0] looks like a flag.
func topLevelService(args []string) string {
	if len(args) == 0 {
		return ""
	}
	first := strings.ToLower(args[0])
	if strings.HasPrefix(first, "-") {
		return ""
	}
	return first
}

// ---------------------------------------------------------------------
// Server config, resolved once at startup from the environment
// ---------------------------------------------------------------------

type serverConfig struct {
	cliPath         string
	allowWrite      bool
	allowedServices map[string]bool // nil/empty means unrestricted
}

// scrubEmptyPassthroughEnv unsets AWS_PROFILE / AWS_REGION if they're
// present but set to an empty string. The AWS CLI treats an explicitly-
// empty AWS_PROFILE as "use the profile literally named ”" and fails with
// "could not be found", instead of falling back to the default profile the
// way a genuinely *unset* AWS_PROFILE would. MCP hosts commonly populate
// .mcp.json's optional env entries as "" when the user hasn't filled them
// in (this connector's own template does exactly that), so the empty-vs-
// unset distinction has to be handled here rather than assumed away.
func scrubEmptyPassthroughEnv() {
	for _, key := range []string{"AWS_PROFILE", "AWS_REGION"} {
		if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) == "" {
			_ = os.Unsetenv(key)
		}
	}
}

func loadServerConfig() serverConfig {
	cfg := serverConfig{cliPath: "aws"}
	if p := strings.TrimSpace(os.Getenv("AWS_MCP_CLI_PATH")); p != "" {
		cfg.cliPath = p
	}
	cfg.allowWrite = isTruthy(os.Getenv("AWS_MCP_ALLOW_WRITE"))
	if raw := strings.TrimSpace(os.Getenv("AWS_MCP_ALLOWED_SERVICES")); raw != "" {
		cfg.allowedServices = map[string]bool{}
		for _, s := range strings.Split(raw, ",") {
			s = strings.ToLower(strings.TrimSpace(s))
			if s != "" {
				cfg.allowedServices[s] = true
			}
		}
	}
	return cfg
}

func isTruthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// checkStartup verifies the aws binary is on PATH and that credentials
// resolve, by making one cheap call (`aws sts get-caller-identity`). This
// is a network call, but for a CLI-subprocess connector it's the only way
// to catch "aws not installed" / "no credentials configured" / "expired
// SSO session" before the first real tool call surfaces a confusing error.
func checkStartup(cfg serverConfig) error {
	if _, err := exec.LookPath(cfg.cliPath); err != nil {
		return fmt.Errorf("aws CLI not found (looked for %q on PATH): %w — install it or set AWS_MCP_CLI_PATH", cfg.cliPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// #nosec G204 -- cfg.cliPath is operator-configured (AWS_MCP_CLI_PATH
	// env var, defaults to "aws" resolved via PATH), never derived from an
	// MCP tool call argument. The fixed arg list here is not tainted.
	cmd := exec.CommandContext(ctx, cfg.cliPath, "sts", "get-caller-identity", "--output", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("aws sts get-caller-identity failed — no usable AWS credentials found: %s (%w). Configure a profile (aws configure), SSO (aws sso login), or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// friendlyAWSError adds actionable hints to common aws-cli failures instead
// of surfacing the raw stderr blob.
func friendlyAWSError(action, stderrText string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(stderrText)
	if msg == "" {
		msg = err.Error()
	}
	switch {
	case strings.Contains(msg, "Unable to locate credentials"), strings.Contains(msg, "ExpiredToken"), strings.Contains(msg, "InvalidClientTokenId"):
		return fmt.Errorf("%s failed: no usable/valid AWS credentials — run `aws sso login` or `aws configure`, or check AWS_PROFILE. (%s)", action, msg)
	case strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "UnauthorizedAccess") || strings.Contains(msg, "is not authorized to perform"):
		return fmt.Errorf("%s failed: access denied — the configured credentials lack permission for this action. (%s)", action, msg)
	case strings.Contains(msg, "could not be found") || strings.Contains(msg, "NotFound") || strings.Contains(msg, "does not exist"):
		return fmt.Errorf("%s failed: resource not found — double check the name/ID/region. (%s)", action, msg)
	case strings.Contains(msg, "Could not connect") || strings.Contains(msg, "Connection refused") || strings.Contains(msg, "EndpointConnectionError"):
		return fmt.Errorf("%s failed: could not reach AWS — check network connectivity and AWS_REGION. (%s)", action, msg)
	case strings.Contains(msg, "Invalid choice") || strings.Contains(msg, "usage:") || strings.Contains(msg, "unrecognized arguments"):
		return fmt.Errorf("%s failed: invalid command/arguments — use aws_help with the same service/subcommand to see valid syntax. (%s)", action, msg)
	case strings.Contains(msg, "command not found"):
		return fmt.Errorf("%s failed: the aws CLI is not installed or not on PATH in the connector's environment. (%s)", action, msg)
	default:
		return fmt.Errorf("%s failed: %s", action, msg)
	}
}

// ---------------------------------------------------------------------
// Core operations — each returns a map[string]any for uniform rendering
// ---------------------------------------------------------------------

func execAWSCommand(ctx context.Context, cfg serverConfig, args []string, confirmed bool, timeoutSeconds, maxBytes int) (map[string]any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf(`args is required, e.g. ["s3", "ls"] or ["ec2", "describe-instances"]`)
	}

	svc := topLevelService(args)
	if svc == "" {
		return nil, fmt.Errorf("could not determine the AWS CLI service from args %v — the first element should be a service name like \"s3\" or \"ec2\"", args)
	}
	if len(cfg.allowedServices) > 0 && !cfg.allowedServices[svc] {
		allowed := make([]string, 0, len(cfg.allowedServices))
		for s := range cfg.allowedServices {
			allowed = append(allowed, s)
		}
		sort.Strings(allowed)
		return nil, fmt.Errorf("service %q is not in the allowed list (%s) — set AWS_MCP_ALLOWED_SERVICES to include it, or leave it unset to allow all services", svc, strings.Join(allowed, ", "))
	}

	mutating := isMutatingCommand(args)
	if mutating {
		if !cfg.allowWrite {
			return nil, fmt.Errorf("this looks like a mutating command (aws %s) but the server is in read-only mode — set AWS_MCP_ALLOW_WRITE=true in the connector's env to permit writes", strings.Join(args, " "))
		}
		if !confirmed {
			return nil, fmt.Errorf("this looks like a mutating command (aws %s) — pass confirm=true to actually run it", strings.Join(args, " "))
		}
	}

	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTimeoutSeconds
	}
	if timeoutSeconds > maxTimeoutSeconds {
		timeoutSeconds = maxTimeoutSeconds
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes > hardMaxBytes {
		maxBytes = hardMaxBytes
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	fullArgs := args
	if !containsFlag(args, "--output") {
		fullArgs = append(append([]string{}, args...), "--output", "json")
	}

	start := time.Now()
	// #nosec G204 -- fullArgs comes from the caller's MCP tool-call
	// arguments by design: this tool's entire purpose is to run arbitrary
	// `aws` CLI commands. It runs the aws binary directly (no shell), so
	// there's no shell-injection risk, only "the AWS CLI does what it's
	// told" — which is why isMutatingCommand + AWS_MCP_ALLOW_WRITE +
	// confirm=true gate anything that isn't a clear read, and
	// AWS_MCP_ALLOWED_SERVICES can restrict which services are reachable
	// at all.
	cmd := exec.CommandContext(runCtx, cfg.cliPath, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return nil, friendlyAWSError(fmt.Sprintf("run `aws %s`", strings.Join(fullArgs, " ")), stderr.String(), runErr)
		}
		exitCode = exitErr.ExitCode()
	}

	outText, outTruncated := capBytes(stdout.String(), maxBytes)
	errText, errTruncated := capBytes(stderr.String(), maxBytes)

	result := map[string]any{
		"command":      "aws " + strings.Join(fullArgs, " "),
		"exit_code":    exitCode,
		"succeeded":    exitCode == 0,
		"stdout":       outText,
		"stderr":       errText,
		"truncated":    outTruncated || errTruncated,
		"duration_ms":  duration.Milliseconds(),
		"was_mutating": mutating,
	}
	if exitCode != 0 {
		result["error_hint"] = friendlyAWSError(fmt.Sprintf("run `aws %s`", strings.Join(fullArgs, " ")), stderr.String(), fmt.Errorf("exit code %d", exitCode)).Error()
	}
	return result, nil
}

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

func capBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	return s[:max], true
}

func runAWSHelp(ctx context.Context, cfg serverConfig, args []string) (map[string]any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf(`args is required, e.g. ["s3"] or ["ec2", "describe-instances"]`)
	}
	fullArgs := append(append([]string{}, args...), "help")
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- fullArgs is caller-supplied, but this always appends
	// "help" and only ever prints documentation; there is no mutating
	// counterpart to `aws ... help`, so it needs none of aws_exec's gating.
	cmd := exec.CommandContext(runCtx, cfg.cliPath, fullArgs...)
	// `aws ... help` pipes through a pager (less) by default; disable it so
	// the whole page comes back on stdout instead of blocking on a TTY.
	cmd.Env = append(os.Environ(), "AWS_PAGER=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, friendlyAWSError(fmt.Sprintf("run `aws %s`", strings.Join(fullArgs, " ")), stderr.String(), err)
	}
	text, truncated := capBytes(stdout.String(), defaultMaxBytes)
	return map[string]any{
		"command":   "aws " + strings.Join(fullArgs, " "),
		"help_text": text,
		"truncated": truncated,
	}, nil
}

func runWhoami(ctx context.Context, cfg serverConfig) (map[string]any, error) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- cfg.cliPath is operator-configured, not tool-call
	// input; the rest of the arg list is fixed. See checkStartup.
	cmd := exec.CommandContext(runCtx, cfg.cliPath, "sts", "get-caller-identity", "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, friendlyAWSError("get caller identity", stderr.String(), err)
	}
	var identity map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &identity); err != nil {
		return nil, fmt.Errorf("could not parse aws sts get-caller-identity output: %w", err)
	}
	result := map[string]any{
		"account": identity["Account"],
		"user_id": identity["UserId"],
		"arn":     identity["Arn"],
	}
	if region := strings.TrimSpace(os.Getenv("AWS_REGION")); region != "" {
		result["region"] = region
	}
	if profile := strings.TrimSpace(os.Getenv("AWS_PROFILE")); profile != "" {
		result["profile"] = profile
	}
	return result, nil
}

func runListProfiles(ctx context.Context, cfg serverConfig) (map[string]any, error) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- cfg.cliPath is operator-configured, not tool-call
	// input; the rest of the arg list is fixed. See checkStartup.
	cmd := exec.CommandContext(runCtx, cfg.cliPath, "configure", "list-profiles")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, friendlyAWSError("list configured profiles", stderr.String(), err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	profiles := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			profiles = append(profiles, l)
		}
	}
	return map[string]any{"count": len(profiles), "profiles": profiles}, nil
}

// ---------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------

func successLabel(v any) string {
	if b, ok := v.(bool); ok && b {
		return "success"
	}
	return "failed"
}

func execMD(d map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# `%v`\n\n", d["command"])
	fmt.Fprintf(&b, "- **Exit code**: %v (%s)\n", d["exit_code"], successLabel(d["succeeded"]))
	fmt.Fprintf(&b, "- **Duration**: %vms\n", d["duration_ms"])
	if h, ok := d["error_hint"].(string); ok && h != "" {
		fmt.Fprintf(&b, "- **Error hint**: %s\n", h)
	}
	b.WriteString("\n**stdout:**\n```\n")
	fmt.Fprintf(&b, "%v", d["stdout"])
	b.WriteString("\n```\n")
	if s, ok := d["stderr"].(string); ok && strings.TrimSpace(s) != "" {
		b.WriteString("\n**stderr:**\n```\n")
		b.WriteString(s)
		b.WriteString("\n```\n")
	}
	if t, _ := d["truncated"].(bool); t {
		b.WriteString("\n_Output truncated by max_bytes — raise it (hard cap 5,000,000) to see more._\n")
	}
	return b.String()
}

func helpMD(d map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Help: `%v`\n\n```\n%v\n```\n", d["command"], d["help_text"])
	if t, _ := d["truncated"].(bool); t {
		b.WriteString("\n_Help text truncated._\n")
	}
	return b.String()
}

func whoamiMD(d map[string]any) string {
	var b strings.Builder
	b.WriteString("# AWS identity\n\n")
	fmt.Fprintf(&b, "- **Account**: %v\n", d["account"])
	fmt.Fprintf(&b, "- **ARN**: %v\n", d["arn"])
	fmt.Fprintf(&b, "- **User ID**: %v\n", d["user_id"])
	if r, ok := d["region"]; ok {
		fmt.Fprintf(&b, "- **Region**: %v\n", r)
	}
	if p, ok := d["profile"]; ok {
		fmt.Fprintf(&b, "- **Profile**: %v\n", p)
	}
	return b.String()
}

func profilesMD(d map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Configured AWS profiles\n\n%v profile(s).\n\n", d["count"])
	profiles, _ := d["profiles"].([]string)
	if len(profiles) == 0 {
		b.WriteString("_No named profiles found in ~/.aws/config._\n")
		return b.String()
	}
	for _, p := range profiles {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	return b.String()
}

// ---------------------------------------------------------------------
// MCP wiring
// ---------------------------------------------------------------------

func getFormat(req mcp.CallToolRequest) string {
	f := req.GetString("response_format", "markdown")
	if f != "json" {
		f = "markdown"
	}
	return f
}

func resultOrError(data map[string]any, err error, format string, mdFn func(map[string]any) string) *mcp.CallToolResult {
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err))
	}
	if format == "json" {
		return mcp.NewToolResultText(renderJSON(data))
	}
	return mcp.NewToolResultText(mdFn(data))
}

func renderJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// version is set at build time via -ldflags "-X main.version=vX.Y.Z" by the
// release workflow. Local `go build` leaves it at "dev".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("aws-connector-server " + version)
		return
	}

	scrubEmptyPassthroughEnv()
	cfg := loadServerConfig()
	if err := checkStartup(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "aws-connector-server: %v\n", err)
		os.Exit(1)
	}

	s := server.NewMCPServer("aws-mcp-connector", "0.1.0", server.WithToolCapabilities(false))

	s.AddTool(mcp.NewTool("aws_exec",
		mcp.WithDescription(`Run an AWS CLI command, e.g. args=["s3","ls"] or args=["ec2","describe-instances","--region","us-west-2"]. args[0] must be the AWS CLI service/command (e.g. "s3", "ec2", "iam") — don't put global flags like --region/--profile first; either put them after the subcommand, or set AWS_REGION/AWS_PROFILE on the server instead. Output defaults to --output json unless args already include --output. Read-only by default: mutating commands (create-/delete-/put-/update-/terminate-/etc., or s3's rm/mv/sync/cp/mb/rb) are blocked unless the server has AWS_MCP_ALLOW_WRITE=true set AND confirm=true is passed on this call. Use aws_help first to check exact subcommand/flag syntax if unsure.`),
		mcp.WithArray("args", mcp.Required(), mcp.Items(map[string]any{"type": "string"}), mcp.Description(`The CLI arguments after "aws", as separate array elements, with the service/command name first — e.g. ["s3","ls","my-bucket"] or ["ec2","describe-instances","--region","us-west-2"]. Do not include the leading "aws" itself.`)),
		mcp.WithBoolean("confirm", mcp.DefaultBool(false), mcp.Description("Must be true to execute a mutating command. Ignored for read-only commands.")),
		mcp.WithNumber("timeout_seconds", mcp.DefaultNumber(defaultTimeoutSeconds), mcp.Description("Max seconds to let the command run before killing it (hard cap 120).")),
		mcp.WithNumber("max_bytes", mcp.DefaultNumber(defaultMaxBytes), mcp.Description("Max bytes of stdout/stderr to return (hard cap 5,000,000).")),
		mcp.WithString("response_format", mcp.Enum("markdown", "json"), mcp.DefaultString("markdown"), mcp.Description("Output format: 'markdown' or 'json'")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := req.RequireStringSlice("args")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		confirm := req.GetBool("confirm", false)
		timeoutSeconds := req.GetInt("timeout_seconds", defaultTimeoutSeconds)
		maxBytes := req.GetInt("max_bytes", defaultMaxBytes)
		data, err := execAWSCommand(ctx, cfg, args, confirm, timeoutSeconds, maxBytes)
		return resultOrError(data, err, getFormat(req), execMD), nil
	})

	s.AddTool(mcp.NewTool("aws_help",
		mcp.WithDescription(`Show AWS CLI help text for a service and/or subcommand, e.g. args=["ec2"] or args=["ec2","describe-instances"]. Always safe to call — use this to check exact syntax before calling aws_exec.`),
		mcp.WithArray("args", mcp.Required(), mcp.Items(map[string]any{"type": "string"}), mcp.Description(`Service and optional subcommand to get help for, e.g. ["s3api","put-object"].`)),
		mcp.WithString("response_format", mcp.Enum("markdown", "json"), mcp.DefaultString("markdown"), mcp.Description("Output format: 'markdown' or 'json'")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := req.RequireStringSlice("args")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := runAWSHelp(ctx, cfg, args)
		return resultOrError(data, err, getFormat(req), helpMD), nil
	})

	s.AddTool(mcp.NewTool("aws_whoami",
		mcp.WithDescription("Show the AWS identity (account, ARN, user/role) the connector's credentials resolve to. Good first call to confirm auth is working."),
		mcp.WithString("response_format", mcp.Enum("markdown", "json"), mcp.DefaultString("markdown"), mcp.Description("Output format: 'markdown' or 'json'")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := runWhoami(ctx, cfg)
		return resultOrError(data, err, getFormat(req), whoamiMD), nil
	})

	s.AddTool(mcp.NewTool("aws_list_profiles",
		mcp.WithDescription("List named AWS CLI profiles configured in ~/.aws/config on the host running this connector."),
		mcp.WithString("response_format", mcp.Enum("markdown", "json"), mcp.DefaultString("markdown"), mcp.Description("Output format: 'markdown' or 'json'")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := runListProfiles(ctx, cfg)
		return resultOrError(data, err, getFormat(req), profilesMD), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "aws-connector-server error: %v\n", err)
		os.Exit(1)
	}
}
