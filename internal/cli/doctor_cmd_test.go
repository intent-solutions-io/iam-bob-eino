package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// doctorEnv pins every input the doctor reads from the process environment so
// the test is hermetic regardless of the developer's shell.
func doctorEnv(t *testing.T, key string) string {
	t.Helper()
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MINIMAX_API_KEY", key)
	for _, v := range []string{"INTENT_BOB_EINO_PROVIDER", "INTENT_BOB_EINO_MODEL", "INTENT_BOB_EINO_CONFIG",
		"BOB_PROVIDER", "BOB_MODEL", "BOB_CONFIG", "INTENT_BOB_EINO_LIVE_SMOKE", "MINIMAX_BASE_URL", "MINIMAX_MODEL"} {
		t.Setenv(v, "")
	}
	return ws
}

func TestDoctorHealthyExitsZero(t *testing.T) {
	ws := doctorEnv(t, "sentinel-key-value-4242")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "-workspace", ws}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, name := range []string{"workspace.path", "credential.presence", "binary.rg", "live_tests.flag"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("doctor output missing check %q:\n%s", name, stdout.String())
		}
	}
	if strings.Contains(stdout.String()+stderr.String(), "sentinel-key-value-4242") {
		t.Fatal("doctor leaked key material")
	}
}

func TestDoctorMissingCredentialExitsNonZero(t *testing.T) {
	ws := doctorEnv(t, "")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "-workspace", ws}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("doctor with missing credential must exit non-zero\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "MINIMAX_API_KEY") {
		t.Errorf("doctor should name the missing variable:\n%s", stdout.String())
	}
}

func TestDoctorJSONShape(t *testing.T) {
	ws := doctorEnv(t, "k")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "-workspace", ws, "-json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor -json exit = %d\n%s", code, stderr.String())
	}
	var payload struct {
		Checks []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Required bool   `json:"required"`
		} `json:"checks"`
		RequiredFailure bool `json:"required_failure"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("doctor -json not parseable: %v\n%s", err, stdout.String())
	}
	if len(payload.Checks) == 0 || payload.RequiredFailure {
		t.Errorf("unexpected payload: %+v", payload)
	}
}

func TestDoctorRunsEvenOnBrokenConfig(t *testing.T) {
	ws := doctorEnv(t, "k")
	t.Setenv("INTENT_BOB_EINO_PROVIDER", "not-a-provider")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "-workspace", ws}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("unknown provider must produce a required failure")
	}
	if !strings.Contains(stdout.String(), "provider.selection") {
		t.Errorf("doctor must still render checks on a broken config:\n%s", stdout.String())
	}
}
