package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
)

// TestConfigDefaultsMatchProviderDefaultModel pins the resolved design point:
// config's seeded defaults are exactly the split of provider.DefaultModel.
func TestConfigDefaultsMatchProviderDefaultModel(t *testing.T) {
	prov, mod, ok := strings.Cut(provider.DefaultModel, "/")
	if !ok {
		t.Fatalf("provider.DefaultModel %q is not provider/model", provider.DefaultModel)
	}
	if config.DefaultProvider != prov {
		t.Errorf("config.DefaultProvider = %q, want %q", config.DefaultProvider, prov)
	}
	if config.DefaultModelID != mod {
		t.Errorf("config.DefaultModelID = %q, want %q", config.DefaultModelID, mod)
	}
}

func TestNoArgsPrintsUsageAndFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Error("no args must exit non-zero")
	}
	for _, want := range []string{"usage", "plan", "run", "verify", "evidence", "doctor"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("usage output missing %q:\n%s", want, stderr.String())
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay clean on usage error, got %q", stdout.String())
	}
}

// TestSubcommandDashHExitsZero: -h on a subcommand prints usage to stderr
// and is a successful help request, never exit 1.
func TestSubcommandDashHExitsZero(t *testing.T) {
	for _, cmd := range []string{"version", "doctor", "plan", "run", "verify"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{cmd, "-h"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Errorf("%s -h exit = %d, want 0\nstderr:\n%s", cmd, code, stderr.String())
		}
		if strings.Contains(stderr.String(), "error:") {
			t.Errorf("%s -h must not print an error line:\n%s", cmd, stderr.String())
		}
	}
}

func TestHelpGoesToStdoutAndSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Errorf("help exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "usage") {
		t.Errorf("help output missing usage: %q", stdout.String())
	}
}

// TestVersionSubcommandHumanOutput asserts identity + build + engine lines and
// the identity-contract invariant: no bare-`bob` machine token.
func TestVersionSubcommandHumanOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("version exit = %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"intent-bob-eino", "iam-bob-eino", "intent-agent-model/bob", "eino-go", "cloudwego/eino", "build:", "go:", "schemas:"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q:\n%s", want, out)
		}
	}
	// The bare persona may appear only inside a hierarchical id (agents/bob,
	// intent-agent-model/bob) or the human display line — never as a
	// standalone machine token like "component: bob".
	if strings.Contains(out, "component: bob\n") {
		t.Errorf("bare persona leaked as machine key:\n%s", out)
	}
}

func TestVersionSubcommandJSONIsDeterministic(t *testing.T) {
	run := func() map[string]any {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"version", "-json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("version -json exit = %d, stderr=%s", code, stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("version -json is not valid JSON: %v\n%s", err, stdout.String())
		}
		return m
	}
	a, b := run(), run()
	for _, key := range []string{"component", "agent", "runtime", "version", "build_commit", "build_date", "go_version", "engine", "engine_version", "evidence_schema_version", "receipt_schema_version", "plan_schema_version"} {
		if _, ok := a[key]; !ok {
			t.Errorf("version -json missing key %q", key)
		}
		if a[key] != b[key] {
			t.Errorf("version -json key %q not deterministic: %v vs %v", key, a[key], b[key])
		}
	}
	if a["component"] != "intent-bob-eino" {
		t.Errorf("component = %v, want intent-bob-eino", a["component"])
	}
}

// TestUnimplementedSubcommandsErrorHonestly: until each lands, the command
// words must NOT fall through to the flat one-shot form.
func TestUnimplementedSubcommandDoesNotFallThroughToFlatForm(t *testing.T) {
	for _, name := range []string{"doctor", "plan", "run", "verify", "evidence"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{name}, strings.NewReader(""), &stdout, &stderr)
		if code == 0 && stderr.Len() == 0 {
			t.Errorf("%s: expected either an implementation or an honest error, got silent success", name)
		}
		if strings.Contains(stderr.String(), "deprecated") {
			t.Errorf("%s fell through to the flat one-shot form:\n%s", name, stderr.String())
		}
	}
}

// TestDoubleDashForcesFlatForm: "--" must route to the one-shot form even when
// the task starts with a command word; without a provider key it fails later
// (at model build), proving it got past dispatch as a task.
func TestDoubleDashForcesFlatForm(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("INTENT_BOB_EINO_MODEL", "")
	t.Setenv("BOB_MODEL", "")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--", "-workspace", t.TempDir(), "run", "the", "tests"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Error("expected failure (no provider credential), got success")
	}
	if !strings.Contains(stderr.String(), "deprecated") {
		t.Errorf("flat form must print the deprecation note, stderr:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "not implemented") {
		t.Errorf("-- must not dispatch to a subcommand:\n%s", stderr.String())
	}
}

// TestCommonFlagsOnlyExplicitFlagsOverride proves fs.Visit semantics: an
// untouched flag never masks env/file/default values.
func TestCommonFlagsOnlyExplicitFlagsOverride(t *testing.T) {
	var stderr bytes.Buffer
	c := newCommonFlags("test", &stderr)
	if err := c.fs.Parse([]string{"-max-steps", "7"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.buildConfig(&stderr)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.MaxSteps != 7 {
		t.Errorf("MaxSteps = %d, want explicit 7", cfg.MaxSteps)
	}
	// Untouched flags: defaults survive (not the zero values of the flag vars).
	if cfg.Timeout != config.DefaultTimeout {
		t.Errorf("Timeout = %v, want default %v (untouched flag must not override)", cfg.Timeout, config.DefaultTimeout)
	}
	if cfg.Provider != config.DefaultProvider || cfg.Model != config.DefaultModelID {
		t.Errorf("provider/model = %s/%s, want seeded defaults", cfg.Provider, cfg.Model)
	}
}

func TestCommonFlagsModelSelectorSplits(t *testing.T) {
	var stderr bytes.Buffer
	c := newCommonFlags("test", &stderr)
	if err := c.fs.Parse([]string{"-model", "deepseek/deepseek-chat"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.buildConfig(&stderr)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" {
		t.Errorf("split = %s/%s, want deepseek/deepseek-chat", cfg.Provider, cfg.Model)
	}
}

func TestCommonFlagsModelSelectorRejectsHalfForms(t *testing.T) {
	for _, sel := range []string{"minimax", "minimax/", "/MiniMax-M3"} {
		var stderr bytes.Buffer
		c := newCommonFlags("test", &stderr)
		if err := c.fs.Parse([]string{"-model", sel}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.buildConfig(&stderr); err == nil {
			t.Errorf("-model %q: want provider/model form error, got nil", sel)
		}
	}
}

// TestYesGrantsNoCapability: -yes must select the auto approver without
// touching AllowWrites/AllowExec (brief items 45–53 anchor).
func TestYesGrantsNoCapability(t *testing.T) {
	var stderr bytes.Buffer
	c := newCommonFlags("test", &stderr)
	if err := c.fs.Parse([]string{"-yes"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.buildConfig(&stderr)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.AllowWrites || cfg.AllowExec {
		t.Errorf("-yes granted capabilities: writes=%v exec=%v", cfg.AllowWrites, cfg.AllowExec)
	}
}

// TestExecWithoutWritesStaysRejected pins resolved design point 3: the
// contradictory combination surfaces as the typed config error.
func TestExecWithoutWritesStaysRejected(t *testing.T) {
	var stderr bytes.Buffer
	c := newCommonFlags("test", &stderr)
	if err := c.fs.Parse([]string{"-allow-exec"}); err != nil {
		t.Fatal(err)
	}
	_, err := c.buildConfig(&stderr)
	if err == nil {
		t.Fatal("exec without writes must be rejected")
	}
	if !errors.Is(err, config.ErrContradictoryPermissions) {
		t.Errorf("err = %v, want config.ErrContradictoryPermissions", err)
	}
}

func TestPlansAndReceiptsDirsLiveInStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got, want := PlansDir(), "/xdg/intent-solutions/agents/bob/eino-go/plans"; got != want {
		t.Errorf("PlansDir = %q, want %q", got, want)
	}
	if got, want := ReceiptsDir(), "/xdg/intent-solutions/agents/bob/eino-go/receipts"; got != want {
		t.Errorf("ReceiptsDir = %q, want %q", got, want)
	}
}
