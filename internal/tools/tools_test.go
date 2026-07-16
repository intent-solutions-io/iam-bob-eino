package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func newGov(t *testing.T, root string, allowWrites bool, appr approval.Approver) (*governor.Governor, *evidence.MemorySink) {
	t.Helper()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	pol := policy.Default()
	pol.AllowWrites = allowWrites
	sink := &evidence.MemorySink{}
	return governor.New(ws, pol, appr, sink), sink
}

// invoke builds one tool and runs it with the given JSON args.
func invoke(t *testing.T, build func(*governor.Governor) (tool.InvokableTool, error), g *governor.Governor, args string) string {
	t.Helper()
	tl, err := build(g)
	if err != nil {
		t.Fatalf("build tool: %v", err)
	}
	out, err := tl.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	return out
}

func TestReadFileGoverned(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world"), 0o644)
	g, sink := newGov(t, root, false, approval.DenyAll{})

	out := invoke(t, newReadFile, g, `{"path":"hello.txt"}`)
	if !strings.Contains(out, "hello world") {
		t.Fatalf("read_file output = %q, want file contents", out)
	}
	rec := lastRecord(t, sink)
	if rec.Tool.Name != "read_file" || rec.Authorization != "allowed" || rec.Execution != "ok" {
		t.Fatalf("evidence = %+v, want read_file allowed/ok", rec)
	}
}

func TestReadFileEscapeDenied(t *testing.T) {
	g, sink := newGov(t, t.TempDir(), false, approval.DenyAll{})
	out := invoke(t, newReadFile, g, `{"path":"../../etc/passwd"}`)
	if !strings.Contains(out, "DENIED") {
		t.Fatalf("escape read = %q, want DENIED", out)
	}
	if rec := lastRecord(t, sink); rec.Execution != "denied" {
		t.Fatalf("evidence = %+v, want denied", rec)
	}
}

func TestWriteDeniedWhenWritesDisabled(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, false, approval.AutoApprove{})
	out := invoke(t, newWriteFile, g, `{"path":"new.txt","content":"data"}`)
	if !strings.Contains(out, "DENIED") {
		t.Fatalf("write with writes disabled = %q, want DENIED", out)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("file must not be created when writes are denied")
	}
	if rec := lastRecord(t, sink); rec.Execution != "denied" {
		t.Fatalf("evidence = %+v, want denied", rec)
	}
}

func TestWriteNeedsApproval(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, true, approval.DenyAll{}) // writes enabled but no approval
	out := invoke(t, newWriteFile, g, `{"path":"new.txt","content":"data"}`)
	if !strings.Contains(out, "DENIED") {
		t.Fatalf("write without approval = %q, want DENIED", out)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("file must not be created without approval")
	}
	if rec := lastRecord(t, sink); rec.Authorization != "denied" {
		t.Fatalf("evidence = %+v, want authorization denied", rec)
	}
}

func TestWriteAllowedAndVerified(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, true, approval.AutoApprove{})
	out := invoke(t, newWriteFile, g, `{"path":"sub/new.txt","content":"hello"}`)
	if !strings.Contains(out, "verified") {
		t.Fatalf("write = %q, want verified", out)
	}
	got, err := os.ReadFile(filepath.Join(root, "sub", "new.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file content = %q err=%v, want hello", got, err)
	}
	rec := lastRecord(t, sink)
	if rec.Execution != "ok" || rec.Verified != "verified" || rec.ApprovalID == "" {
		t.Fatalf("evidence = %+v, want ok/verified with approval", rec)
	}
}

func TestRunCommandDeniedNotAllowlisted(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, false, approval.AutoApprove{})
	out := invoke(t, newRunCommand, g, `{"command":"rm -rf /"}`)
	if !strings.Contains(out, "DENIED") {
		t.Fatalf("non-allowlisted command = %q, want DENIED", out)
	}
	if rec := lastRecord(t, sink); rec.Execution != "denied" {
		t.Fatalf("evidence = %+v, want denied", rec)
	}
}

func TestRunCommandAllowed(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, false, approval.AutoApprove{})
	out := invoke(t, newRunCommand, g, `{"command":"git --version"}`)
	if !strings.Contains(out, "exit_code=0") {
		t.Fatalf("git --version = %q, want exit_code=0", out)
	}
	if rec := lastRecord(t, sink); rec.Execution != "ok" || rec.Verified != "verified" {
		t.Fatalf("evidence = %+v, want ok/verified", rec)
	}
}

func TestRunCommandRejectsShellInjection(t *testing.T) {
	root := t.TempDir()
	// A canary file the injected command would delete if a shell interpreted it.
	canary := filepath.Join(root, "canary")
	os.WriteFile(canary, []byte("x"), 0o644)
	g, sink := newGov(t, root, false, approval.AutoApprove{})

	injections := []string{
		`{"command":"git --version; rm -rf ."}`,
		`{"command":"git --version && rm canary"}`,
		"{\"command\":\"git --version | rm canary\"}",
		"{\"command\":\"git $(rm canary)\"}",
		"{\"command\":\"git `rm canary`\"}",
	}
	for _, args := range injections {
		out := invoke(t, newRunCommand, g, args)
		if !strings.Contains(out, "DENIED") {
			t.Fatalf("injection %s = %q, want DENIED", args, out)
		}
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatal("canary file was deleted — shell injection succeeded")
	}
	for _, rec := range sink.Records {
		if rec.Execution != "denied" {
			t.Fatalf("injection produced non-denied evidence: %+v", rec)
		}
	}
}

func TestSearchCode(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n// find_this_needle here\n"), 0o644)
	g, sink := newGov(t, root, false, approval.DenyAll{})
	out := invoke(t, newSearchCode, g, `{"pattern":"find_this_needle"}`)
	if !strings.Contains(out, "a.go:") {
		t.Fatalf("search = %q, want a.go match", out)
	}
	if rec := lastRecord(t, sink); rec.Execution != "ok" || rec.RiskClass != "R1" {
		t.Fatalf("evidence = %+v, want ok/R1", rec)
	}
}

func TestReadSecretFileDenied(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".env"), []byte("OPENAI_API_KEY=sk-verysecretvalue000000"), 0o600)
	g, sink := newGov(t, root, false, approval.DenyAll{})
	out := invoke(t, newReadFile, g, `{"path":".env"}`)
	if !strings.Contains(out, "DENIED") || strings.Contains(out, "sk-verysecret") {
		t.Fatalf("read .env = %q, want DENIED with no secret", out)
	}
	if rec := lastRecord(t, sink); rec.Execution != "denied" {
		t.Fatalf("evidence = %+v, want denied", rec)
	}
}

func TestRunCommandEvidenceIsFaithful(t *testing.T) {
	root := t.TempDir()
	g, sink := newGov(t, root, false, approval.AutoApprove{})
	invoke(t, newRunCommand, g, `{"command":"git --version"}`)
	rec := lastRecord(t, sink)
	// The evidence asset must record the full command, not just "git", so an
	// auditor can see what actually ran (security review C2).
	if !strings.Contains(rec.Asset, "git --version") {
		t.Fatalf("evidence asset = %q, want the full command", rec.Asset)
	}
}

func lastRecord(t *testing.T, sink *evidence.MemorySink) evidence.Record {
	t.Helper()
	if len(sink.Records) == 0 {
		t.Fatal("no evidence recorded")
	}
	return sink.Records[len(sink.Records)-1]
}
