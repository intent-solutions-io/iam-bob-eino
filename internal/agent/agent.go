// Package agent wires Bob's specialization (persona + governed tools) onto
// Eino's ADK agent machinery. Eino owns the ReAct loop, tool dispatch, and
// streaming; Bob owns the persona, the governed tools, and how the run is
// driven. Bob is not a new agent framework — it is a specialization of Eino's.
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

// Config parameterizes agent construction.
type Config struct {
	Model         model.BaseModel[*schema.Message]
	Tools         []tool.BaseTool
	MaxIterations int
}

// New builds Bob as an Eino ChatModelAgent with the coding persona and the
// governed tool set.
func New(ctx context.Context, cfg Config) (*adk.ChatModelAgent, error) {
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 16
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		// The agent's machine name is the canonical component id, never the
		// bare persona — "Bob" stays human-facing prose in persona.go.
		Name:        version.Component,
		Description: "A governed local software-engineering agent (Intent Agent Model).",
		Instruction: Persona,
		Model:       cfg.Model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: cfg.Tools},
		},
		MaxIterations: cfg.MaxIterations,
	})
}

// Run executes a single task through the agent, streaming a human-readable
// trace of tool calls and answers to trace, and returns Bob's final answer.
func Run(ctx context.Context, ag *adk.ChatModelAgent, task string, trace io.Writer) (string, error) {
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: ag})
	iter := runner.Query(ctx, task)

	var final strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return final.String(), fmt.Errorf("agent run: %w", event.Err)
		}
		out := event.Output
		if out == nil || out.MessageOutput == nil {
			continue
		}
		// GetMessage concatenates the stream when streaming is enabled, so this
		// stays correct if EnableStreaming is turned on later.
		msg, gerr := out.MessageOutput.GetMessage()
		if gerr != nil {
			return final.String(), fmt.Errorf("agent event: %w", gerr)
		}
		if msg == nil {
			continue
		}
		switch {
		case len(msg.ToolCalls) > 0:
			for _, tc := range msg.ToolCalls {
				// Redact model-supplied tool arguments (e.g. write_file content)
				// before they reach the terminal trace.
				fmt.Fprintf(trace, "→ tool: %s %s\n", tc.Function.Name, oneLine(tc.Function.Arguments))
			}
		case msg.Role == schema.Tool:
			fmt.Fprintf(trace, "← result: %s\n", oneLine(msg.Content))
		case msg.Content != "":
			fmt.Fprintf(trace, "· bob: %s\n", oneLine(msg.Content))
			final.Reset()
			final.WriteString(msg.Content)
		}
	}
	return final.String(), nil
}

// oneLine collapses a multi-line string to a single trimmed, redacted line for
// the trace so secrets never reach the terminal.
func oneLine(s string) string {
	s = evidence.Redact(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
