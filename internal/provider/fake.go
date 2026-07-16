package provider

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeChatModel is a scripted model.ToolCallingChatModel for deterministic tests.
// It returns a preset sequence of assistant messages (typically one or more
// tool-call turns followed by a final answer), advancing one step per Generate
// call. This lets the whole governed pipeline be exercised end-to-end with no
// network and no real LLM.
type FakeChatModel struct {
	// Script is the ordered set of assistant replies to return, one per turn.
	Script []*schema.Message
	turn   int
}

// NewFake builds a FakeChatModel from a script of assistant messages.
func NewFake(script ...*schema.Message) *FakeChatModel {
	return &FakeChatModel{Script: script}
}

// Generate returns the next scripted message, or a terminal answer once the
// script is exhausted.
func (f *FakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if f.turn >= len(f.Script) {
		return schema.AssistantMessage("done", nil), nil
	}
	msg := f.Script[f.turn]
	f.turn++
	return msg, nil
}

// Stream wraps Generate's result in a single-chunk stream.
func (f *FakeChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := f.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// WithTools satisfies ToolCallingChatModel; the fake ignores tool schemas.
func (f *FakeChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}
