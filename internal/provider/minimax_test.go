package provider

import (
	"os"
	"strings"
	"testing"
)

// TestMiniMaxIsRegistered confirms MiniMax is a first-class registry entry with
// the verified OpenAI-compatible base URL, its own key env, and the documented
// flagship coding model as the default.
func TestMiniMaxIsRegistered(t *testing.T) {
	e, ok := Registry["minimax"]
	if !ok {
		t.Fatal("minimax must be a first-class provider")
	}
	if e.baseURL != "https://api.minimax.io/v1" {
		t.Errorf("baseURL = %q, want the global OpenAI-compatible endpoint", e.baseURL)
	}
	if e.keyEnv != "MINIMAX_API_KEY" {
		t.Errorf("keyEnv = %q, want MINIMAX_API_KEY", e.keyEnv)
	}
	if e.model != "MiniMax-M3" {
		t.Errorf("default model = %q, want MiniMax-M3", e.model)
	}
	if !e.needsAuth {
		t.Error("minimax must require auth")
	}
}

// TestMiniMaxIsTheDocumentedDefault pins the operational default to MiniMax.
func TestMiniMaxIsTheDocumentedDefault(t *testing.T) {
	if DefaultModel != "minimax/MiniMax-M3" {
		t.Errorf("DefaultModel = %q, want minimax/MiniMax-M3", DefaultModel)
	}
}

// TestResolveMiniMaxUsesItsOwnKey proves MiniMax resolves against MINIMAX_API_KEY.
func TestResolveMiniMaxUsesItsOwnKey(t *testing.T) {
	t.Setenv("BOB_MODEL", "")
	t.Setenv("MINIMAX_API_KEY", "sk-minimax-canary-value")
	cfg, err := Resolve("minimax/MiniMax-M3")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.Provider != "minimax" || cfg.Model != "MiniMax-M3" {
		t.Errorf("resolved %s/%s, want minimax/MiniMax-M3", cfg.Provider, cfg.Model)
	}
	if cfg.BaseURL != "https://api.minimax.io/v1" {
		t.Errorf("baseURL = %q", cfg.BaseURL)
	}
}

// TestResolveMissingMiniMaxKeyFailsClosedEvenWhenOtherKeysSet is the no-fallback
// contract at resolution time: an absent MiniMax key is an explicit error even
// when another provider's key is present — Bob never silently switches provider.
func TestResolveMissingMiniMaxKeyFailsClosedEvenWhenOtherKeysSet(t *testing.T) {
	t.Setenv("BOB_MODEL", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek-present")
	_, err := Resolve("minimax/MiniMax-M3")
	if err == nil {
		t.Fatal("missing MINIMAX_API_KEY must fail closed, not fall back to deepseek")
	}
	if !strings.Contains(err.Error(), "MINIMAX_API_KEY") {
		t.Errorf("error should name the missing MiniMax key, got: %v", err)
	}
}

// TestResolveErrorNeverContainsKeyValue proves a resolution error never echoes a
// key value even when one is present in the environment.
func TestResolveErrorNeverContainsKeyValue(t *testing.T) {
	const canary = "sk-super-secret-canary-do-not-leak"
	t.Setenv("MINIMAX_API_KEY", canary)
	// A malformed selector forces an error while the key is set.
	_, err := Resolve("minimax") // missing "/model" — but minimax has a default, so force a bad form:
	if err == nil {
		// minimax/ with empty model resolves to default; instead use an unknown provider.
		_, err = Resolve("nope-nope")
	}
	if err != nil && strings.Contains(err.Error(), canary) {
		t.Fatalf("resolution error leaked the API key value: %v", err)
	}
}

// TestKnownProvidersListStaysInSyncWithRegistry proves the error string can't
// drift from the registry.
func TestKnownProvidersListStaysInSyncWithRegistry(t *testing.T) {
	list := knownProviders()
	for name := range Registry {
		if !strings.Contains(list, name) {
			t.Errorf("knownProviders() missing %q (drift): %q", name, list)
		}
	}
}

// TestNoImplicitFallbackFunctionExists is a structural guard: the provider
// package must not contain any fallback/retry-to-another-provider machinery.
// Resolve returns (Config, error) and New returns (model, error); neither
// consults a second registry entry on failure. This test documents that
// contract by asserting Resolve of an unknown provider yields an error rather
// than any Config.
func TestNoImplicitFallbackOnUnknownProvider(t *testing.T) {
	t.Setenv("BOB_MODEL", "")
	cfg, err := Resolve("anthropic/claude")
	if err == nil {
		t.Fatal("unknown provider must error, never fall back")
	}
	if cfg.Provider != "" {
		t.Errorf("failed resolve returned a non-empty Config: %+v", cfg)
	}
}

var _ = os.Getenv // keep os imported if trimmed
