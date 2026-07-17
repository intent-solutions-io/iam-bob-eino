package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validPlan returns a fully-populated, sealed plan that passes Validate. Each
// test mutates a copy to probe one rule. The provider/model fields name a
// deterministic model stub — no network is involved anywhere in this package.
func validPlan(t *testing.T) Plan {
	t.Helper()
	p := Plan{
		SchemaVersion:        SchemaVersion,
		Task:                 "add a retry wrapper around the provider call",
		WorkspaceIdentity:    "ws-4f2a9c1e",
		WorkspaceStartSHA:    "0123456789abcdef0123456789abcdef01234567",
		Provider:             "stub",
		Model:                "deterministic-model-stub-v1",
		CreatedAt:            "2026-07-16T00:00:00Z",
		ProposedActions:      []string{"read provider.go", "write retry.go", "run tests"},
		ProposedFiles:        []string{"internal/provider/retry.go", "internal/provider/retry_test.go"},
		ProposedCommands:     []string{"go build ./...", "go test ./internal/provider/..."},
		RequiredCapabilities: []string{"read", "write", "execute"},
		AcceptanceChecks:     []string{"go test ./..."},
		Risks:                []string{"retry may mask a persistent provider failure"},
		Assumptions:          []string{"provider errors are transient"},
		Questions:            []string{},
		Status:               "proposed",
		Authority:            AuthorityLocalUntrusted,
	}
	p.Finalize()
	if err := Validate(p); err != nil {
		t.Fatalf("validPlan fixture must validate, got: %v", err)
	}
	return p
}

// reseal recomputes id + hash after a test mutates plan content, so Validate
// failures reflect the rule under test rather than a stale hash.
func reseal(p Plan) Plan {
	p.Finalize()
	return p
}

func TestFinalizeDeterministic(t *testing.T) {
	a := validPlan(t)
	b := validPlan(t)
	if a.PlanID != b.PlanID {
		t.Errorf("identical content produced different plan ids: %q vs %q", a.PlanID, b.PlanID)
	}
	if a.ContentHash != b.ContentHash {
		t.Errorf("identical content produced different hashes: %q vs %q", a.ContentHash, b.ContentHash)
	}
	if !strings.HasPrefix(a.PlanID, "plan-") || len(a.PlanID) != len("plan-")+24 {
		t.Errorf("plan id %q is not plan-<24 hex chars>", a.PlanID)
	}
	if !strings.HasPrefix(a.ContentHash, "sha256:") || len(a.ContentHash) != len("sha256:")+64 {
		t.Errorf("content hash %q is not sha256:<64 hex chars>", a.ContentHash)
	}
}

func TestFinalizeIdempotent(t *testing.T) {
	p := validPlan(t)
	id, hash := p.PlanID, p.ContentHash
	p.Finalize()
	if p.PlanID != id || p.ContentHash != hash {
		t.Errorf("re-finalizing unchanged content changed identity: id %q->%q hash %q->%q", id, p.PlanID, hash, p.ContentHash)
	}
}

func TestHashChangesOnContentChange(t *testing.T) {
	base := validPlan(t)
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{"task changed", func(p *Plan) { p.Task = "a different task" }},
		{"file added", func(p *Plan) { p.ProposedFiles = append(p.ProposedFiles, "internal/provider/extra.go") }},
		{"command changed", func(p *Plan) { p.ProposedCommands = []string{"go vet ./..."} }},
		{"risk added", func(p *Plan) { p.Risks = append(p.Risks, "another risk") }},
		{"start sha changed", func(p *Plan) { p.WorkspaceStartSHA = "fedcba9876543210fedcba9876543210fedcba98" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			// Copy slices so mutations don't alias the base fixture.
			p.ProposedFiles = append([]string(nil), base.ProposedFiles...)
			p.ProposedCommands = append([]string(nil), base.ProposedCommands...)
			p.Risks = append([]string(nil), base.Risks...)
			tc.mutate(&p)
			p.Finalize()
			if p.ContentHash == base.ContentHash {
				t.Errorf("hash did not change after mutation")
			}
			if p.PlanID == base.PlanID {
				t.Errorf("plan id did not change after mutation")
			}
		})
	}
}

func TestValidateSchemaVersion(t *testing.T) {
	for _, v := range []string{"", "0", "2", "1.0", "v1"} {
		t.Run("version "+v, func(t *testing.T) {
			p := validPlan(t)
			p.SchemaVersion = v
			p = reseal(p)
			if err := Validate(p); !errors.Is(err, ErrSchemaVersion) {
				t.Errorf("Validate = %v, want ErrSchemaVersion", err)
			}
		})
	}
}

func TestValidatePlanID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"wrong prefix", "task-0123456789abcdef01234567"},
		{"too short", "plan-0123456789abcdef"},
		{"path traversal attempt", "plan-../../etc/passwd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan(t)
			p.PlanID = tc.id
			p.ContentHash = CanonicalHash(p)
			if err := Validate(p); !errors.Is(err, ErrPlanID) {
				t.Errorf("Validate = %v, want ErrPlanID", err)
			}
		})
	}
}

func TestValidateAuthorityMustBeLocalUntrusted(t *testing.T) {
	for _, a := range []string{"", "trusted", "approved", "LOCAL_UNTRUSTED", "operator"} {
		t.Run("authority "+a, func(t *testing.T) {
			p := validPlan(t)
			p.Authority = a
			p = reseal(p)
			if err := Validate(p); !errors.Is(err, ErrAuthority) {
				t.Errorf("Validate = %v, want ErrAuthority", err)
			}
		})
	}
}

func TestValidateForbiddenFiles(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"absolute path", "/etc/passwd"},
		{"home-relative path", "~/notes.txt"},
		{"parent traversal", "../outside/main.go"},
		{"embedded traversal", "internal/../../escape.go"},
		{"git internals", ".git/config"},
		{"nested git internals", "vendor/.git/hooks/pre-commit"},
		{"dotenv", ".env"},
		{"dotenv variant", "config/.env.production"},
		{"ssh private key", "id_rsa"},
		{"ssh key in dir", "keys/id_ed25519"},
		{"aws credentials", ".aws/credentials"},
		{"pem key material", "certs/server.pem"},
		{"key extension", "tls/private.key"},
		{"windows-style traversal", "internal\\..\\escape.go"},
		{"empty path", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan(t)
			p.ProposedFiles = []string{tc.file}
			p = reseal(p)
			if err := Validate(p); !errors.Is(err, ErrForbiddenFile) {
				t.Errorf("Validate(file=%q) = %v, want ErrForbiddenFile", tc.file, err)
			}
		})
	}
}

func TestValidateAllowsOrdinaryWorkspaceFiles(t *testing.T) {
	p := validPlan(t)
	p.ProposedFiles = []string{
		"main.go",
		"internal/agent/agent.go",
		"docs/design.md",
		"Makefile",
		"testdata/fixture.json",
	}
	p = reseal(p)
	if err := Validate(p); err != nil {
		t.Errorf("Validate = %v, want nil for ordinary workspace files", err)
	}
}

func TestValidateProposedFileCap(t *testing.T) {
	p := validPlan(t)
	files := make([]string, MaxProposedFiles+1)
	for i := range files {
		files[i] = filepath.Join("internal", "gen", "file"+strings.Repeat("a", i%5)+".go")
	}
	// File names must be unique-ish but the cap check fires on count alone.
	p.ProposedFiles = files
	p = reseal(p)
	if err := Validate(p); !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("Validate with %d files = %v, want ErrTooManyFiles", len(files), err)
	}

	p.ProposedFiles = files[:MaxProposedFiles]
	p = reseal(p)
	if err := Validate(p); err != nil {
		t.Errorf("Validate with exactly %d files = %v, want nil", MaxProposedFiles, err)
	}
}

func TestValidateForbiddenCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"shell", "bash -c anything"},
		{"remove", "rm -rf ."},
		{"network fetch", "curl http://example.com"},
		{"interpreter", "python setup.py install"},
		{"sudo", "sudo make install"},
		{"empty", ""},
		{"whitespace only", "   "},
		{"pipe metacharacter", "go test ./pkg | tee out.log"},
		{"command chaining", "go build; rm -rf ."},
		{"subshell", "go run $(cat cmd.txt)"},
		{"redirect", "go test > results.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan(t)
			p.ProposedCommands = []string{tc.cmd}
			p = reseal(p)
			if err := Validate(p); !errors.Is(err, ErrForbiddenCommand) {
				t.Errorf("Validate(cmd=%q) = %v, want ErrForbiddenCommand", tc.cmd, err)
			}
		})
	}
}

func TestValidateAllowedCommands(t *testing.T) {
	p := validPlan(t)
	p.ProposedCommands = []string{
		"go build ./...",
		"go test ./...",
		"make lint",
		"pytest tests/",
		"npm test",
		"pnpm run check",
		"cargo test",
		"git status",
	}
	p = reseal(p)
	if err := Validate(p); err != nil {
		t.Errorf("Validate = %v, want nil for allowed dev commands", err)
	}
}

func TestValidateAcceptanceChecks(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		p := validPlan(t)
		p.AcceptanceChecks = nil
		p = reseal(p)
		if err := Validate(p); !errors.Is(err, ErrNoAcceptance) {
			t.Errorf("Validate = %v, want ErrNoAcceptance", err)
		}
	})
	t.Run("empty slice", func(t *testing.T) {
		p := validPlan(t)
		p.AcceptanceChecks = []string{}
		p = reseal(p)
		if err := Validate(p); !errors.Is(err, ErrNoAcceptance) {
			t.Errorf("Validate = %v, want ErrNoAcceptance", err)
		}
	})
	t.Run("shelled-out check rejected", func(t *testing.T) {
		p := validPlan(t)
		p.AcceptanceChecks = []string{"bash run-checks.sh"}
		p = reseal(p)
		if err := Validate(p); !errors.Is(err, ErrForbiddenCommand) {
			t.Errorf("Validate = %v, want ErrForbiddenCommand", err)
		}
	})
}

func TestValidateContentHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"wrong algo", "sha1:0123456789abcdef0123456789abcdef01234567"},
		{"truncated", "sha256:abcdef"},
		{"uppercase hex", "sha256:" + strings.Repeat("A", 64)},
		{"no prefix", strings.Repeat("a", 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan(t)
			p.ContentHash = tc.hash
			if err := Validate(p); !errors.Is(err, ErrContentHash) {
				t.Errorf("Validate(hash=%q) = %v, want ErrContentHash", tc.hash, err)
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := validPlan(t)

	path, err := Save(p, dir)
	if err != nil {
		t.Fatalf("Save = %v", err)
	}
	if want := filepath.Join(dir, p.PlanID+".json"); path != want {
		t.Errorf("Save path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("plan file mode = %o, want 0600", got)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if !plansEqual(loaded, p) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", loaded, p)
	}
}

// plansEqual compares two plans by their canonical JSON encoding (Plan
// contains slices, so direct == is not allowed).
func plansEqual(a, b Plan) bool {
	ab, _ := canonicalJSON(a)
	bb, _ := canonicalJSON(b)
	return string(ab) == string(bb)
}

func TestSaveRejectsInvalidPlan(t *testing.T) {
	dir := t.TempDir()

	t.Run("unsealed plan", func(t *testing.T) {
		p := validPlan(t)
		p.ContentHash = ""
		if _, err := Save(p, dir); !errors.Is(err, ErrContentHash) {
			t.Errorf("Save = %v, want ErrContentHash", err)
		}
	})
	t.Run("stale hash after edit", func(t *testing.T) {
		p := validPlan(t)
		p.Task = "silently changed after sealing"
		if _, err := Save(p, dir); !errors.Is(err, ErrHashMismatch) {
			t.Errorf("Save = %v, want ErrHashMismatch", err)
		}
	})
}

func TestLoadRejectsTamperedContent(t *testing.T) {
	dir := t.TempDir()
	p := validPlan(t)
	path, err := Save(p, dir)
	if err != nil {
		t.Fatalf("Save = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	tampered := strings.Replace(string(raw), p.Task, "do something else entirely", 1)
	if tampered == string(raw) {
		t.Fatal("test setup: task text not found in saved plan")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("WriteFile = %v", err)
	}

	if _, err := Load(path); !errors.Is(err, ErrHashMismatch) {
		t.Errorf("Load tampered plan = %v, want ErrHashMismatch", err)
	}
}

func TestLoadRejectsTamperedHash(t *testing.T) {
	dir := t.TempDir()
	p := validPlan(t)
	path, err := Save(p, dir)
	if err != nil {
		t.Fatalf("Save = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	// Flip the hash to a different well-formed value so only the mismatch
	// check (not the format check) can catch it.
	forged := "sha256:" + strings.Repeat("0", 64)
	tampered := strings.Replace(string(raw), p.ContentHash, forged, 1)
	if tampered == string(raw) {
		t.Fatal("test setup: content hash not found in saved plan")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("WriteFile = %v", err)
	}

	if _, err := Load(path); !errors.Is(err, ErrHashMismatch) {
		t.Errorf("Load forged-hash plan = %v, want ErrHashMismatch", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	p := validPlan(t)
	path, err := Save(p, dir)
	if err != nil {
		t.Fatalf("Save = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	tampered := strings.Replace(string(raw), "\"schema_version\"", "\"smuggled_field\": \"x\",\n  \"schema_version\"", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("WriteFile = %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load plan with unknown field succeeded, want decode error")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "plan-000000000000000000000000.json")); err == nil {
		t.Error("Load missing file succeeded, want error")
	}
}
