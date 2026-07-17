package agent_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/agent"
	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// TestOfflineRunEmitsIdentityInOnDiskEvidence drives one offline
// deterministic-Eino-model-fixture agent run against a real JSONL sink, then
// re-reads the log from disk and proves: every record carries the structured
// agent_identity, the chain verifies, and a hand-tampered identity fails
// verification. This is the end-to-end proof behind docs 004/005.
func TestOfflineRunEmitsIdentityInOnDiskEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello from bob"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}

	evPath := filepath.Join(t.TempDir(), "evidence.jsonl")
	sink, err := evidence.NewJSONLSink(evPath)
	if err != nil {
		t.Fatal(err)
	}
	gov := governor.New(ws, policy.Default(), approval.DenyAll{}, sink)
	toolset, err := tools.All(gov)
	if err != nil {
		t.Fatal(err)
	}

	toolCall := schema.ToolCall{
		ID:       "call_1",
		Type:     "function",
		Function: schema.FunctionCall{Name: "read_file", Arguments: `{"path":"hello.txt"}`},
	}
	fixture := provider.NewFake(
		schema.AssistantMessage("", []schema.ToolCall{toolCall}),
		schema.AssistantMessage("done reading", nil),
	)
	ag, err := agent.New(context.Background(), agent.Config{Model: fixture, Tools: toolset})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := agent.Run(context.Background(), ag, "read hello.txt", io.Discard); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	sink.Close()

	// Re-read the log from disk — the projection surface downstream tools see.
	raw, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatal(err)
	}
	var records []evidence.Record
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var rec evidence.Record
		if err := json.Unmarshal([]byte(sc.Text()), &rec); err != nil {
			t.Fatalf("parse evidence line: %v", err)
		}
		records = append(records, rec)
	}
	if len(records) == 0 {
		t.Fatal("no evidence records on disk")
	}

	for i, rec := range records {
		if rec.AgentIdentity == nil {
			t.Fatalf("record %d missing agent_identity", i)
		}
		if err := rec.AgentIdentity.Validate(); err != nil {
			t.Fatalf("record %d identity invalid: %v", i, err)
		}
		if rec.AgentIdentity.ComponentID != identity.ComponentID {
			t.Errorf("record %d component = %q", i, rec.AgentIdentity.ComponentID)
		}
	}
	if i := evidence.VerifyChain(records); i != -1 {
		t.Fatalf("on-disk chain broke at %d", i)
	}

	// Hand-tamper the identity of the first record: verification must fail.
	forged := *records[0].AgentIdentity
	forged.RoleID = "exfiltration"
	records[0].AgentIdentity = &forged
	if i := evidence.VerifyChain(records); i != 0 {
		t.Fatalf("tampered identity must break the chain at 0, got %d", i)
	}
}
