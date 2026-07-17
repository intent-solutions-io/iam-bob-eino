package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
)

// envMap returns a Getenv func backed by a map (no process env touched).
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// minimalCfgFile writes a config file providing provider+model so Load
// survives validation when a test only cares about one variable.
func minimalCfgFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"provider":"minimax","model":"MiniMax-M3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestEnvPrefixDerivesFromComponentID pins the canonical namespace to the
// identity contract: the prefix IS the component id, uppercased, hyphens to
// underscores. If either side changes, this fails and the drift is caught.
func TestEnvPrefixDerivesFromComponentID(t *testing.T) {
	want := strings.ToUpper(strings.ReplaceAll(identity.ComponentID, "-", "_")) + "_"
	if EnvPrefix != want {
		t.Fatalf("EnvPrefix = %q, want %q (derived from identity.ComponentID %q)",
			EnvPrefix, want, identity.ComponentID)
	}
	if LegacyEnvPrefix != strings.ToUpper(identity.PersonaID)+"_" {
		t.Fatalf("LegacyEnvPrefix = %q, want the bare persona namespace it deprecates", LegacyEnvPrefix)
	}
}

// TestProviderModelEnvMatchesNamespace pins internal/provider's standalone
// MODEL env constants to this package's namespace so the two packages can
// never drift apart.
func TestProviderModelEnvMatchesNamespace(t *testing.T) {
	if provider.ModelEnv != EnvPrefix+"MODEL" {
		t.Errorf("provider.ModelEnv = %q, config namespace says %q", provider.ModelEnv, EnvPrefix+"MODEL")
	}
	if provider.LegacyModelEnv != LegacyEnvPrefix+"MODEL" {
		t.Errorf("provider.LegacyModelEnv = %q, config namespace says %q", provider.LegacyModelEnv, LegacyEnvPrefix+"MODEL")
	}
}

// TestTwelveVarNamespace asserts the full namespace is exactly the 12
// documented variables and every one resolves through the canonical prefix.
func TestTwelveVarNamespace(t *testing.T) {
	if len(envNames) != 12 {
		t.Fatalf("namespace has %d vars, want 12: %v", len(envNames), envNames)
	}
	env := map[string]string{
		EnvPrefix + "PROVIDER":      "minimax",
		EnvPrefix + "MODEL":         "MiniMax-M3",
		EnvPrefix + "BASE_URL":      "https://api.minimax.io/v1",
		EnvPrefix + "WORKSPACE":     "/tmp/ws",
		EnvPrefix + "MAX_STEPS":     "7",
		EnvPrefix + "TIMEOUT":       "90s",
		EnvPrefix + "ALLOW_WRITES":  "true",
		EnvPrefix + "ALLOW_EXEC":    "true",
		EnvPrefix + "APPROVAL_MODE": "auto",
		EnvPrefix + "EVIDENCE_DIR":  "/tmp/ev",
		EnvPrefix + "OUTPUT_FORMAT": "json",
		// CONFIG is exercised separately (file lookup tier).
	}
	var warn bytes.Buffer
	cfg, err := Load(Options{Getenv: envMap(env), HomeDir: t.TempDir(), Warn: &warn})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "minimax" || cfg.Model != "MiniMax-M3" ||
		cfg.BaseURL != "https://api.minimax.io/v1" || cfg.Workspace != "/tmp/ws" ||
		cfg.MaxSteps != 7 || cfg.Timeout != 90*time.Second ||
		!cfg.AllowWrites || !cfg.AllowExec || cfg.ApprovalMode != "auto" ||
		cfg.EvidenceDir != "/tmp/ev" || cfg.OutputFormat != "json" {
		t.Fatalf("canonical env not honored: %+v", cfg)
	}
	if warn.Len() != 0 {
		t.Fatalf("canonical namespace must not warn, got %q", warn.String())
	}
}

// TestCanonicalWinsOverLegacyPerVariable proves precedence variable-by-
// variable: canonical set → canonical wins, silently; only legacy set →
// legacy honored with a warning.
func TestCanonicalWinsOverLegacyPerVariable(t *testing.T) {
	env := map[string]string{
		EnvPrefix + "PROVIDER":       "minimax",
		LegacyEnvPrefix + "PROVIDER": "openai",
		EnvPrefix + "MODEL":          "MiniMax-M3",
		LegacyEnvPrefix + "MODEL":    "gpt-4o",
		LegacyEnvPrefix + "TIMEOUT":  "45s", // legacy-only: honored + warned
	}
	var warn bytes.Buffer
	cfg, err := Load(Options{Getenv: envMap(env), HomeDir: t.TempDir(), Warn: &warn})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "minimax" || cfg.Model != "MiniMax-M3" {
		t.Errorf("canonical must beat legacy: %+v", cfg)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("legacy-only variable must still apply: %v", cfg.Timeout)
	}
	out := warn.String()
	if strings.Contains(out, "BOB_PROVIDER") || strings.Contains(out, "BOB_MODEL") {
		t.Errorf("no warning when canonical shadows legacy, got %q", out)
	}
	if !strings.Contains(out, "BOB_TIMEOUT") || !strings.Contains(out, EnvPrefix+"TIMEOUT") {
		t.Errorf("legacy fallback must warn naming both variables, got %q", out)
	}
	if strings.Contains(out, "45s") {
		t.Errorf("warning leaked the value: %q", out)
	}
}

// TestLegacyWarnsOncePerVariable proves the warning does not repeat for the
// same variable within one Load (PROVIDER is read by both effectiveProvider
// and applyEnv).
func TestLegacyWarnsOncePerVariable(t *testing.T) {
	env := map[string]string{
		LegacyEnvPrefix + "PROVIDER": "minimax",
		LegacyEnvPrefix + "MODEL":    "MiniMax-M3",
	}
	var warn bytes.Buffer
	if _, err := Load(Options{Getenv: envMap(env), HomeDir: t.TempDir(), Warn: &warn}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := warn.String()
	if got := strings.Count(out, "BOB_PROVIDER is deprecated"); got != 1 {
		t.Errorf("BOB_PROVIDER warned %d times, want exactly 1: %q", got, out)
	}
	if got := strings.Count(out, "BOB_MODEL is deprecated"); got != 1 {
		t.Errorf("BOB_MODEL warned %d times, want exactly 1: %q", got, out)
	}
}

// TestEnvErrorNamesTheActualVariable proves error attribution points at the
// concrete variable the bad value came from — canonical or legacy.
func TestEnvErrorNamesTheActualVariable(t *testing.T) {
	for _, tc := range []struct{ setVar, wantField string }{
		{EnvPrefix + "MAX_STEPS", EnvPrefix + "MAX_STEPS"},
		{LegacyEnvPrefix + "MAX_STEPS", LegacyEnvPrefix + "MAX_STEPS"},
	} {
		var warn bytes.Buffer
		_, err := Load(Options{
			Getenv:  envMap(map[string]string{tc.setVar: "not-a-number"}),
			HomeDir: t.TempDir(),
			Warn:    &warn,
		})
		var fe *FieldError
		if !errors.As(err, &fe) || !errors.Is(err, ErrInvalidEnv) {
			t.Fatalf("%s: err = %v, want FieldError wrapping ErrInvalidEnv", tc.setVar, err)
		}
		if fe.Field != tc.wantField {
			t.Errorf("error field = %q, want %q", fe.Field, tc.wantField)
		}
	}
}

// TestCanonicalConfigEnvBeatsLegacy proves INTENT_BOB_EINO_CONFIG outranks
// BOB_CONFIG in the file lookup tier.
func TestCanonicalConfigEnvBeatsLegacy(t *testing.T) {
	canonical := minimalCfgFile(t)
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"provider":"openai","model":"gpt-4o"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	cfg, err := Load(Options{
		Getenv: envMap(map[string]string{
			EnvPrefix + "CONFIG":       canonical,
			LegacyEnvPrefix + "CONFIG": legacy,
		}),
		HomeDir: t.TempDir(),
		Warn:    &warn,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "minimax" {
		t.Errorf("canonical CONFIG must win, got provider %q", cfg.Provider)
	}
	if strings.Contains(warn.String(), "BOB_CONFIG") {
		t.Errorf("no warning when canonical CONFIG shadows legacy, got %q", warn.String())
	}
}

// TestConfigFileLocations proves the canonical XDG location wins over the
// legacy location, and a legacy-only file is honored with a warning.
func TestConfigFileLocations(t *testing.T) {
	t.Run("canonical location wins", func(t *testing.T) {
		xdg := t.TempDir()
		writeCfg(t, filepath.Join(xdg, "intent-solutions", "agents", "bob", "eino-go"), `{"provider":"minimax","model":"MiniMax-M3"}`)
		writeCfg(t, filepath.Join(xdg, "iam-bob-eino"), `{"provider":"openai","model":"gpt-4o"}`)
		var warn bytes.Buffer
		cfg, err := Load(Options{
			Getenv:  envMap(map[string]string{"XDG_CONFIG_HOME": xdg}),
			HomeDir: t.TempDir(),
			Warn:    &warn,
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Provider != "minimax" {
			t.Errorf("canonical location must win, got provider %q", cfg.Provider)
		}
		if warn.Len() != 0 {
			t.Errorf("no warning when canonical location used, got %q", warn.String())
		}
	})
	t.Run("legacy-only location warned", func(t *testing.T) {
		xdg := t.TempDir()
		writeCfg(t, filepath.Join(xdg, "iam-bob-eino"), `{"provider":"minimax","model":"MiniMax-M3"}`)
		var warn bytes.Buffer
		cfg, err := Load(Options{
			Getenv:  envMap(map[string]string{"XDG_CONFIG_HOME": xdg}),
			HomeDir: t.TempDir(),
			Warn:    &warn,
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Provider != "minimax" {
			t.Errorf("legacy location must still be honored, got %+v", cfg)
		}
		if !strings.Contains(warn.String(), "legacy location") {
			t.Errorf("legacy-only location must warn, got %q", warn.String())
		}
	})
}

func writeCfg(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
