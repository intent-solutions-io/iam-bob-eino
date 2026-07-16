// Package provider is Bob's provider-neutral model boundary. Every model comes
// through here via an OpenAI-compatible gateway, so Bob is BYOK (bring your own
// key) for any provider and has zero Google dependency by default — Google is
// not in the registry and selecting it is an explicit error in this slice.
//
// Model portability is a gateway concern, not an agent concern: the rest of Bob
// only ever sees model.ToolCallingChatModel.
package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// registryEntry describes an OpenAI-compatible provider endpoint.
type registryEntry struct {
	baseURL   string // OpenAI-compatible base URL ("" = api.openai.com)
	keyEnv    string // env var holding the API key ("" = no key, e.g. local)
	model     string // default model id for this provider
	needsAuth bool
}

// Registry is the set of supported providers. It deliberately contains NO Google
// entry: Bob is zero-Google by default (operating rule P2). Adding Google would
// require an explicit, separate decision.
var Registry = map[string]registryEntry{
	"deepseek": {baseURL: "https://api.deepseek.com/v1", keyEnv: "DEEPSEEK_API_KEY", model: "deepseek-chat", needsAuth: true},
	"openai":   {baseURL: "", keyEnv: "OPENAI_API_KEY", model: "gpt-4o", needsAuth: true},
	"groq":     {baseURL: "https://api.groq.com/openai/v1", keyEnv: "GROQ_API_KEY", model: "llama-3.3-70b-versatile", needsAuth: true},
	"zhipu":    {baseURL: "https://open.bigmodel.cn/api/paas/v4", keyEnv: "ZHIPU_API_KEY", model: "glm-4", needsAuth: true},
	"ollama":   {baseURL: "http://localhost:11434/v1", keyEnv: "", model: "llama3.1", needsAuth: false},
}

// DefaultModel is the zero-Google default selection (provider/model form).
const DefaultModel = "deepseek/deepseek-chat"

// Config is a fully resolved model selection.
type Config struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// Model-selection environment variables. INTENT_BOB_EINO_MODEL is canonical;
// BOB_MODEL is the legacy name kept as a warned compatibility alias — it
// collides with iam-bob-pydantic's BOB_MODEL, where it means something else
// (see 000-docs/005-DR-STND-bob-eino-identity-contract.md).
const (
	ModelEnv       = "INTENT_BOB_EINO_MODEL"
	LegacyModelEnv = "BOB_MODEL"
)

// legacyWarn emits the one-per-process legacy-env deprecation warning. The
// writer is a variable only so tests can capture it; the warning never
// includes the variable's value.
var (
	legacyWarnOnce sync.Once
	legacyWarnOut  io.Writer = os.Stderr
)

func warnLegacyModelEnv() {
	legacyWarnOnce.Do(func() {
		fmt.Fprintf(legacyWarnOut, "warning: %s is deprecated; use %s\n", LegacyModelEnv, ModelEnv)
	})
}

// Resolve turns a "provider/model" selector plus the environment into a Config.
// Precedence: explicit selector (CLI) → INTENT_BOB_EINO_MODEL → BOB_MODEL
// (legacy, warned once per process) → DefaultModel. Values are never printed.
func Resolve(selector string) (Config, error) {
	if selector == "" {
		selector = os.Getenv(ModelEnv)
	}
	if selector == "" {
		if legacy := os.Getenv(LegacyModelEnv); legacy != "" {
			warnLegacyModelEnv()
			selector = legacy
		}
	}
	if selector == "" {
		selector = DefaultModel
	}
	providerName, modelID, ok := strings.Cut(selector, "/")
	if !ok {
		return Config{}, fmt.Errorf("model selector %q must be in provider/model form", selector)
	}
	providerName = strings.ToLower(providerName)
	if providerName == "google" || providerName == "gemini" || providerName == "vertex" {
		return Config{}, fmt.Errorf("provider %q is not supported: Bob is zero-Google by default", providerName)
	}
	entry, known := Registry[providerName]
	if !known {
		return Config{}, fmt.Errorf("unknown provider %q (known: deepseek, openai, groq, zhipu, ollama)", providerName)
	}
	if modelID == "" {
		modelID = entry.model
	}
	var apiKey string
	if entry.needsAuth {
		apiKey = os.Getenv(entry.keyEnv)
		if apiKey == "" {
			return Config{}, fmt.Errorf("provider %q requires %s to be set (BYOK)", providerName, entry.keyEnv)
		}
	}
	return Config{Provider: providerName, Model: modelID, APIKey: apiKey, BaseURL: entry.baseURL}, nil
}

// New constructs an OpenAI-compatible chat model from a resolved Config.
func New(ctx context.Context, cfg Config) (model.ToolCallingChatModel, error) {
	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("build %s model: %w", cfg.Provider, err)
	}
	return cm, nil
}
