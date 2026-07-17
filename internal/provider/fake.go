package provider

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeChatModel is a scripted model.ToolCallingChatModel for deterministic
// tests — the offline model stub behind the lifecycle e2e suite. It returns a
// preset sequence of assistant messages (typically one or more tool-call
// turns followed by a final answer), advancing one step per Generate call,
// and can inject per-turn errors or a context-blocking turn so provider
// failures and timeouts are exercised with no network and no real LLM.
type FakeChatModel struct {
	// Script is the ordered set of assistant replies to return, one per turn.
	Script []*schema.Message
	// Errors maps a 0-based turn index to an error returned INSTEAD of that
	// turn's message — the provider-failure fixture (e.g. a rate-limit-shaped
	// error on turn 1).
	Errors map[int]error
	// BlockTurn, when > 0, makes the BlockTurn-th turn (1-based) block until
	// the context is done and then return ctx.Err() — the timeout fixture.
	BlockTurn int
	// TurnUsage, when set, is stamped as each returned message's
	// ResponseMeta.Usage — the provider-usage-accounting fixture.
	TurnUsage *schema.TokenUsage

	turn int
}

// NewFake builds a FakeChatModel from a script of assistant messages.
func NewFake(script ...*schema.Message) *FakeChatModel {
	return &FakeChatModel{Script: script}
}

// Generate returns the next scripted step: a blocking wait, an injected
// error, the scripted message, or a terminal answer once the script is
// exhausted.
func (f *FakeChatModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	turn := f.turn
	f.turn++
	if f.BlockTurn > 0 && turn+1 == f.BlockTurn {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err, ok := f.Errors[turn]; ok {
		return nil, err
	}
	var msg *schema.Message
	if turn >= len(f.Script) {
		msg = schema.AssistantMessage("done", nil)
	} else {
		msg = f.Script[turn]
	}
	if f.TurnUsage != nil && msg.ResponseMeta == nil {
		msg.ResponseMeta = &schema.ResponseMeta{Usage: f.TurnUsage}
	}
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

// WithTools satisfies ToolCallingChatModel; the fixture ignores tool schemas.
func (f *FakeChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}
