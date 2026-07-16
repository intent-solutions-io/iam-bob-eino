package provider

import (
	"strings"
	"testing"
)

func TestRegistryHasNoGoogle(t *testing.T) {
	for name := range Registry {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "google") || strings.Contains(lower, "gemini") || strings.Contains(lower, "vertex") {
			t.Fatalf("registry must be zero-Google, found %q", name)
		}
	}
}

func TestDefaultModelIsNotGoogle(t *testing.T) {
	provider, _, _ := strings.Cut(DefaultModel, "/")
	if provider == "google" || provider == "gemini" || provider == "vertex" {
		t.Fatalf("default model %q must not be Google", DefaultModel)
	}
	if _, ok := Registry[provider]; !ok {
		t.Fatalf("default provider %q must be in the registry", provider)
	}
}

func TestResolveRejectsGoogle(t *testing.T) {
	for _, sel := range []string{"google/gemini-1.5", "gemini/x", "vertex/y"} {
		if _, err := Resolve(sel); err == nil {
			t.Errorf("Resolve(%q) = nil error, want zero-Google rejection", sel)
		}
	}
}

func TestResolveUnknownProvider(t *testing.T) {
	if _, err := Resolve("acme/model"); err == nil {
		t.Error("Resolve(unknown) = nil error, want error")
	}
}

func TestResolveRequiresKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := Resolve("deepseek/deepseek-chat"); err == nil {
		t.Error("Resolve without key = nil error, want BYOK error")
	}
}

func TestResolveOllamaNeedsNoKey(t *testing.T) {
	cfg, err := Resolve("ollama/llama3.1")
	if err != nil {
		t.Fatalf("Resolve(ollama): %v", err)
	}
	if cfg.BaseURL == "" {
		t.Error("ollama should have a local base URL")
	}
}

func TestResolveMalformedSelector(t *testing.T) {
	if _, err := Resolve("deepseek-chat"); err == nil {
		t.Error("Resolve without provider/ prefix = nil, want error")
	}
}
