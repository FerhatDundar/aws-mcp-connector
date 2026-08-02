package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsTruthy(t *testing.T) {
	cases := map[string]bool{
		"true": true, "True": true, "1": true, "yes": true, "on": true,
		"": false, "false": false, "0": false, "nope": false, "  ": false,
		" TRUE ": true,
	}
	for in, want := range cases {
		if got := isTruthy(in); got != want {
			t.Errorf("isTruthy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsMutatingCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"s3", "ls"}, false},
		{[]string{"ec2", "describe-instances"}, false},
		{[]string{"sts", "get-caller-identity"}, false},
		{[]string{"iam", "list-users"}, false},
		{[]string{"ec2", "terminate-instances", "--instance-ids", "i-123"}, true},
		{[]string{"ec2", "create-tags", "--resources", "i-123"}, true},
		{[]string{"s3", "rm", "s3://bucket/key"}, true},
		{[]string{"s3", "sync", "./dir", "s3://bucket/"}, true},
		{[]string{"s3", "cp", "file.txt", "s3://bucket/"}, true},
		{[]string{"s3api", "put-object", "--bucket", "x"}, true},
		{[]string{"iam", "delete-user", "--user-name", "x"}, true},
		{[]string{"--region", "us-east-1", "s3", "ls"}, false},
	}
	for _, c := range cases {
		if got := isMutatingCommand(c.args); got != c.want {
			t.Errorf("isMutatingCommand(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestTopLevelService(t *testing.T) {
	cases := map[string]string{
		"":      "",
		"-":     "",
		"s3 ls": "s3",
		"--region us-east-1 ec2 describe-instances": "",
		"ec2 describe-instances --region us-west-2": "ec2",
	}
	for in, want := range cases {
		args := strings.Fields(in)
		if got := topLevelService(args); got != want {
			t.Errorf("topLevelService(%v) = %q, want %q", args, got, want)
		}
	}
}

func TestFriendlyAWSErrorNil(t *testing.T) {
	if err := friendlyAWSError("do a thing", "", nil); err != nil {
		t.Errorf("expected nil error to stay nil, got %v", err)
	}
}

func TestFriendlyAWSErrorKnownPatterns(t *testing.T) {
	cases := []struct {
		stderr string
		want   string
	}{
		{"Unable to locate credentials", "no usable/valid AWS credentials"},
		{"An error occurred (AccessDenied) when calling", "access denied"},
		{"could not be found", "resource not found"},
		{"Could not connect to the endpoint URL", "could not reach AWS"},
	}
	for _, c := range cases {
		err := friendlyAWSError("run a thing", c.stderr, errTest("boom"))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("friendlyAWSError(stderr=%q) = %v, want to contain %q", c.stderr, err, c.want)
		}
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// The following tests exercise the input-validation and safety-gate guards
// on the core operations. All of them return before touching os/exec, so
// they run without a network connection or the aws CLI binary installed.

func TestExecAWSCommandRequiresArgs(t *testing.T) {
	cfg := serverConfig{cliPath: "aws"}
	if _, err := execAWSCommand(context.Background(), cfg, nil, false, 0, 0); err == nil {
		t.Error("expected error for empty args")
	}
}

func TestExecAWSCommandRequiresResolvableService(t *testing.T) {
	cfg := serverConfig{cliPath: "aws"}
	if _, err := execAWSCommand(context.Background(), cfg, []string{"--region", "us-east-1"}, false, 0, 0); err == nil {
		t.Error("expected error when no positional service token is present")
	}
}

func TestExecAWSCommandEnforcesAllowedServices(t *testing.T) {
	cfg := serverConfig{cliPath: "aws", allowedServices: map[string]bool{"s3": true}}
	if _, err := execAWSCommand(context.Background(), cfg, []string{"ec2", "describe-instances"}, false, 0, 0); err == nil {
		t.Error("expected error for a service outside AWS_MCP_ALLOWED_SERVICES")
	}
}

func TestExecAWSCommandBlocksMutatingInReadOnlyMode(t *testing.T) {
	cfg := serverConfig{cliPath: "aws", allowWrite: false}
	_, err := execAWSCommand(context.Background(), cfg, []string{"ec2", "terminate-instances", "--instance-ids", "i-1"}, true, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		t.Errorf("expected read-only-mode error, got %v", err)
	}
}

func TestExecAWSCommandRequiresConfirmForMutating(t *testing.T) {
	cfg := serverConfig{cliPath: "aws", allowWrite: true}
	_, err := execAWSCommand(context.Background(), cfg, []string{"s3", "rm", "s3://bucket/key"}, false, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "confirm=true") {
		t.Errorf("expected confirm=true error, got %v", err)
	}
}

func TestRunAWSHelpRequiresArgs(t *testing.T) {
	cfg := serverConfig{cliPath: "aws"}
	if _, err := runAWSHelp(context.Background(), cfg, nil); err == nil {
		t.Error("expected error for empty args")
	}
}

func TestContainsFlag(t *testing.T) {
	if !containsFlag([]string{"s3", "ls", "--output", "text"}, "--output") {
		t.Error("expected to find --output flag")
	}
	if !containsFlag([]string{"s3", "ls", "--output=text"}, "--output") {
		t.Error("expected to find --output=text flag")
	}
	if containsFlag([]string{"s3", "ls"}, "--output") {
		t.Error("did not expect to find --output flag")
	}
}

func TestCapBytes(t *testing.T) {
	text, truncated := capBytes("hello world", 5)
	if text != "hello" || !truncated {
		t.Errorf("capBytes short cap = (%q, %v), want (\"hello\", true)", text, truncated)
	}
	text, truncated = capBytes("hi", 5)
	if text != "hi" || truncated {
		t.Errorf("capBytes under limit = (%q, %v), want (\"hi\", false)", text, truncated)
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	data := map[string]any{"a": 1, "b": "two"}
	out := renderJSON(data)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("renderJSON output did not parse as JSON: %v", err)
	}
	if parsed["b"] != "two" {
		t.Errorf("round-tripped value mismatch: %v", parsed)
	}
}
