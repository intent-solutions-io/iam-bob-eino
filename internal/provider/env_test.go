package provider

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// resetLegacyWarn re-arms the once-per-process warning and captures its output.
func resetLegacyWarn() *bytes.Buffer {
	var buf bytes.Buffer
	legacyWarnOnce = sync.Once{}
	legacyWarnOut = &buf
	return &buf
}

func TestResolvePrecedenceCLIWinsOverEnv(t *testing.T) {
	buf := resetLegacyWarn()
	t.Setenv(ModelEnv, "openai/env-model")
	t.Setenv(LegacyModelEnv, "openai/legacy-model")
	t.Setenv("OPENAI_API_KEY", "test-key")
	cfg, err := Resolve("openai/cli-model")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Model != "cli-model" {
		t.Errorf("model = %q, want cli-model (CLI wins)", cfg.Model)
	}
	if buf.Len() != 0 {
		t.Errorf("no warning expected when CLI selector set, got %q", buf.String())
	}
}

func TestResolvePrecedenceCanonicalEnvBeatsLegacy(t *testing.T) {
	buf := resetLegacyWarn()
	t.Setenv(ModelEnv, "openai/canonical-model")
	t.Setenv(LegacyModelEnv, "openai/legacy-model")
	t.Setenv("OPENAI_API_KEY", "test-key")
	cfg, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Model != "canonical-model" {
		t.Errorf("model = %q, want canonical-model (INTENT_BOB_EINO_MODEL wins)", cfg.Model)
	}
	if buf.Len() != 0 {
		t.Errorf("legacy warning must not fire when canonical env used, got %q", buf.String())
	}
}

func TestResolveLegacyEnvFallbackWarnsOnceWithoutValue(t *testing.T) {
	buf := resetLegacyWarn()
	t.Setenv(ModelEnv, "")
	t.Setenv(LegacyModelEnv, "openai/secret-model-name")
	t.Setenv("OPENAI_API_KEY", "test-key")

	for i := 0; i < 3; i++ {
		cfg, err := Resolve("")
		if err != nil {
			t.Fatalf("Resolve #%d: %v", i, err)
		}
		if cfg.Model != "secret-model-name" {
			t.Errorf("model = %q, want legacy fallback", cfg.Model)
		}
	}
	out := buf.String()
	if got := strings.Count(out, "deprecated"); got != 1 {
		t.Errorf("legacy warning fired %d times, want exactly 1: %q", got, out)
	}
	if !strings.Contains(out, "BOB_MODEL") || !strings.Contains(out, "INTENT_BOB_EINO_MODEL") {
		t.Errorf("warning must name both variables: %q", out)
	}
	// Never print values.
	if strings.Contains(out, "secret-model-name") {
		t.Errorf("warning leaked the env value: %q", out)
	}
}

func TestResolveDefaultWhenNoEnv(t *testing.T) {
	resetLegacyWarn()
	t.Setenv(ModelEnv, "")
	t.Setenv(LegacyModelEnv, "")
	// Satisfy the default provider's BYOK requirement whatever it is, so this
	// test tracks DefaultModel instead of hardcoding a provider.
	defProvider, _, _ := strings.Cut(DefaultModel, "/")
	if entry, ok := Registry[defProvider]; ok && entry.keyEnv != "" {
		t.Setenv(entry.keyEnv, "test-key")
	}
	cfg, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Provider+"/"+cfg.Model != DefaultModel {
		t.Errorf("got %s/%s, want default %s", cfg.Provider, cfg.Model, DefaultModel)
	}
}
