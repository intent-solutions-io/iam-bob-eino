package agent_test

import (
	"context"
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
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// TestAgentRunsGovernedToolThroughEino drives the full stack with no network:
// a scripted fake model calls read_file, Eino's ADK runs the governed tool, and
// the final answer plus an evidence record prove the vertical slice works.
func TestAgentRunsGovernedToolThroughEino(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello from bob"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sink := &evidence.MemorySink{}
	gov := governor.New(ws, policy.Default(), approval.DenyAll{}, sink)
	toolset, err := tools.All(gov)
	if err != nil {
		t.Fatal(err)
	}

	// Script: first turn calls read_file, second turn gives the final answer.
	toolCall := schema.ToolCall{
		ID:       "call_1",
		Type:     "function",
		Function: schema.FunctionCall{Name: "read_file", Arguments: `{"path":"hello.txt"}`},
	}
	fake := provider.NewFake(
		schema.AssistantMessage("", []schema.ToolCall{toolCall}),
		schema.AssistantMessage("I read hello.txt for you.", nil),
	)

	ag, err := agent.New(context.Background(), agent.Config{Model: fake, Tools: toolset})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	answer, err := agent.Run(context.Background(), ag, "read hello.txt", io.Discard)
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	if !strings.Contains(answer, "read hello.txt") {
		t.Fatalf("answer = %q, want final scripted answer", answer)
	}

	// The governed read_file must have executed and produced an evidence record.
	var found bool
	for _, rec := range sink.Records {
		if rec.Tool.Name == "read_file" && rec.Execution == "ok" && rec.Authorization == "allowed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no read_file evidence recorded; got %d records", len(sink.Records))
	}
}
