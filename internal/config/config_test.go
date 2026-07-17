package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// envOf returns a Getenv func backed by a map, so no test touches the real
// process environment (and tests stay parallel-safe with zero network use).
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// writeConfig writes body as dir/iam-bob-eino/config.json and returns the path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	cfgDir := filepath.Join(dir, "iam-bob-eino")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeFile writes body to an arbitrary path inside dir and returns it.
func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func strPtr(s string) *string               { return &s }
func intPtr(n int) *int                     { return &n }
func boolPtr(b bool) *bool                  { return &b }
func durPtr(d time.Duration) *time.Duration { return &d }

const minimalFile = `{"provider": "openai", "model": "gpt-4o-mini"}`

func TestDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(Options{
		CLI:     Overrides{Provider: strPtr("deepseek"), Model: strPtr("deepseek-chat")},
		Getenv:  envOf(nil),
		HomeDir: t.TempDir(), // no config file anywhere
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Provider:     "deepseek",
		Model:        "deepseek-chat",
		MaxSteps:     DefaultMaxSteps,
		Timeout:      DefaultTimeout,
		ApprovalMode: DefaultApprovalMode,
		OutputFormat: DefaultOutputFormat,
	}
	if cfg != want {
		t.Errorf("defaults mismatch:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestFileResolutionPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("explicit --config wins over everything", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		explicit := writeFile(t, tmp, "explicit.json", `{"model": "from-explicit"}`)
		envCfg := writeFile(t, tmp, "env.json", `{"model": "from-bob-config"}`)
		xdg := t.TempDir()
		writeConfig(t, xdg, `{"model": "from-xdg"}`)
		cfg, err := Load(Options{
			ConfigPath: explicit,
			CLI:        Overrides{Provider: strPtr("openai")},
			Getenv:     envOf(map[string]string{"BOB_CONFIG": envCfg, "XDG_CONFIG_HOME": xdg}),
			HomeDir:    t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Model != "from-explicit" {
			t.Errorf("Model = %q, want from-explicit", cfg.Model)
		}
	})

	t.Run("BOB_CONFIG wins over XDG and home", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		envCfg := writeFile(t, tmp, "env.json", `{"model": "from-bob-config"}`)
		xdg := t.TempDir()
		writeConfig(t, xdg, `{"model": "from-xdg"}`)
		cfg, err := Load(Options{
			CLI:     Overrides{Provider: strPtr("openai")},
			Getenv:  envOf(map[string]string{"BOB_CONFIG": envCfg, "XDG_CONFIG_HOME": xdg}),
			HomeDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Model != "from-bob-config" {
			t.Errorf("Model = %q, want from-bob-config", cfg.Model)
		}
	})

	t.Run("XDG_CONFIG_HOME wins over home", func(t *testing.T) {
		t.Parallel()
		xdg := t.TempDir()
		writeConfig(t, xdg, `{"model": "from-xdg"}`)
		home := t.TempDir()
		writeConfig(t, filepath.Join(home, ".config"), `{"model": "from-home"}`)
		cfg, err := Load(Options{
			CLI:     Overrides{Provider: strPtr("openai")},
			Getenv:  envOf(map[string]string{"XDG_CONFIG_HOME": xdg}),
			HomeDir: home,
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Model != "from-xdg" {
			t.Errorf("Model = %q, want from-xdg", cfg.Model)
		}
	})

	t.Run("falls back to ~/.config", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		writeConfig(t, filepath.Join(home, ".config"), `{"model": "from-home"}`)
		cfg, err := Load(Options{
			CLI:     Overrides{Provider: strPtr("openai")},
			Getenv:  envOf(nil),
			HomeDir: home,
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Model != "from-home" {
			t.Errorf("Model = %q, want from-home", cfg.Model)
		}
	})

	t.Run("missing explicit --config is an error", func(t *testing.T) {
		t.Parallel()
		_, err := Load(Options{
			ConfigPath: filepath.Join(t.TempDir(), "nope.json"),
			Getenv:     envOf(nil),
			HomeDir:    t.TempDir(),
		})
		if !errors.Is(err, ErrConfigFile) {
			t.Errorf("err = %v, want ErrConfigFile", err)
		}
	})

	t.Run("missing BOB_CONFIG target is an error", func(t *testing.T) {
		t.Parallel()
		_, err := Load(Options{
			Getenv:  envOf(map[string]string{"BOB_CONFIG": filepath.Join(t.TempDir(), "nope.json")}),
			HomeDir: t.TempDir(),
		})
		if !errors.Is(err, ErrConfigFile) {
			t.Errorf("err = %v, want ErrConfigFile", err)
		}
	})
}

func TestPrecedenceChain(t *testing.T) {
	t.Parallel()
	// One knob (Model) driven through all five tiers; the winner at each
	// tier proves the full CLI > BOB_* > provider env > file > default chain.
	tmp := t.TempDir()
	file := writeFile(t, tmp, "config.json",
		`{"provider": "minimax", "model": "from-file", "max_steps": 7}`)

	cases := []struct {
		name      string
		cli       Overrides
		env       map[string]string
		wantModel string
	}{
		{
			name:      "file beats default",
			env:       map[string]string{},
			wantModel: "from-file",
		},
		{
			name:      "provider env (MINIMAX_MODEL) beats file",
			env:       map[string]string{"MINIMAX_MODEL": "from-minimax-env"},
			wantModel: "from-minimax-env",
		},
		{
			name: "BOB_MODEL beats provider env and file",
			env: map[string]string{
				"MINIMAX_MODEL": "from-minimax-env",
				"BOB_MODEL":     "from-bob-env",
			},
			wantModel: "from-bob-env",
		},
		{
			name: "CLI beats BOB env, provider env, and file",
			cli:  Overrides{Model: strPtr("from-cli")},
			env: map[string]string{
				"MINIMAX_MODEL": "from-minimax-env",
				"BOB_MODEL":     "from-bob-env",
			},
			wantModel: "from-cli",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(Options{
				ConfigPath: file,
				CLI:        tc.cli,
				Getenv:     envOf(tc.env),
				HomeDir:    t.TempDir(),
			})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Model != tc.wantModel {
				t.Errorf("Model = %q, want %q", cfg.Model, tc.wantModel)
			}
			if cfg.MaxSteps != 7 {
				t.Errorf("MaxSteps = %d, want 7 (from file)", cfg.MaxSteps)
			}
		})
	}
}

func TestMinimaxEnvOnlyAppliesToMinimax(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	file := writeFile(t, tmp, "config.json", minimalFile) // provider openai
	cfg, err := Load(Options{
		ConfigPath: file,
		Getenv: envOf(map[string]string{
			"MINIMAX_MODEL":    "should-not-apply",
			"MINIMAX_BASE_URL": "https://minimax.example/v1",
		}),
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q; MINIMAX_MODEL leaked into a non-minimax run", cfg.Model)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q; MINIMAX_BASE_URL leaked into a non-minimax run", cfg.BaseURL)
	}
}

func TestMinimaxBaseURLApplies(t *testing.T) {
	t.Parallel()
	cfg, err := Load(Options{
		CLI: Overrides{Provider: strPtr("minimax"), Model: strPtr("m2")},
		Getenv: envOf(map[string]string{
			"MINIMAX_BASE_URL": "https://api.minimax.example/v1",
		}),
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "https://api.minimax.example/v1" {
		t.Errorf("BaseURL = %q, want the MINIMAX_BASE_URL value", cfg.BaseURL)
	}
}

func TestUnknownFieldRejection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"arbitrary unknown key", `{"provider": "openai", "model": "m", "banana": 1}`},
		{"api key smuggled into file", `{"provider": "openai", "model": "m", "api_key": "sk-PLACEHOLDER"}`},
		{"typoed known key", `{"provider": "openai", "model": "m", "max_step": 5}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := writeFile(t, t.TempDir(), "config.json", tc.body)
			_, err := Load(Options{ConfigPath: file, Getenv: envOf(nil), HomeDir: t.TempDir()})
			if !errors.Is(err, ErrUnknownField) {
				t.Errorf("err = %v, want ErrUnknownField", err)
			}
		})
	}
}

func TestMalformedConfigFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"not json", `provider = "openai"`},
		{"wrong type", `{"provider": "openai", "model": "m", "max_steps": "many"}`},
		{"trailing garbage", `{"provider": "openai", "model": "m"} {"again": true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := writeFile(t, t.TempDir(), "config.json", tc.body)
			_, err := Load(Options{ConfigPath: file, Getenv: envOf(nil), HomeDir: t.TempDir()})
			if !errors.Is(err, ErrConfigFile) {
				t.Errorf("err = %v, want ErrConfigFile", err)
			}
		})
	}
}

func TestInvalidDuration(t *testing.T) {
	t.Parallel()

	t.Run("unparseable in file", func(t *testing.T) {
		t.Parallel()
		file := writeFile(t, t.TempDir(), "config.json",
			`{"provider": "openai", "model": "m", "timeout": "ninety seconds"}`)
		_, err := Load(Options{ConfigPath: file, Getenv: envOf(nil), HomeDir: t.TempDir()})
		if !errors.Is(err, ErrInvalidDuration) {
			t.Errorf("err = %v, want ErrInvalidDuration", err)
		}
	})

	t.Run("unparseable in BOB_TIMEOUT", func(t *testing.T) {
		t.Parallel()
		_, err := Load(Options{
			CLI:     Overrides{Provider: strPtr("openai"), Model: strPtr("m")},
			Getenv:  envOf(map[string]string{"BOB_TIMEOUT": "soon"}),
			HomeDir: t.TempDir(),
		})
		if !errors.Is(err, ErrInvalidDuration) {
			t.Errorf("err = %v, want ErrInvalidDuration", err)
		}
	})

	t.Run("negative duration rejected", func(t *testing.T) {
		t.Parallel()
		_, err := Load(Options{
			CLI: Overrides{
				Provider: strPtr("openai"), Model: strPtr("m"),
				Timeout: durPtr(-time.Second),
			},
			Getenv:  envOf(nil),
			HomeDir: t.TempDir(),
		})
		if !errors.Is(err, ErrInvalidDuration) {
			t.Errorf("err = %v, want ErrInvalidDuration", err)
		}
	})

	t.Run("zero duration means no timeout and is allowed", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load(Options{
			CLI: Overrides{
				Provider: strPtr("openai"), Model: strPtr("m"),
				Timeout: durPtr(0),
			},
			Getenv:  envOf(nil),
			HomeDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Timeout != 0 {
			t.Errorf("Timeout = %v, want 0", cfg.Timeout)
		}
	})
}

func TestNonPositiveMaxSteps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts func(t *testing.T) Options
	}{
		{
			name: "negative via CLI",
			opts: func(t *testing.T) Options {
				return Options{
					CLI:     Overrides{Provider: strPtr("openai"), Model: strPtr("m"), MaxSteps: intPtr(-3)},
					Getenv:  envOf(nil),
					HomeDir: t.TempDir(),
				}
			},
		},
		{
			name: "zero via CLI",
			opts: func(t *testing.T) Options {
				return Options{
					CLI:     Overrides{Provider: strPtr("openai"), Model: strPtr("m"), MaxSteps: intPtr(0)},
					Getenv:  envOf(nil),
					HomeDir: t.TempDir(),
				}
			},
		},
		{
			name: "negative via file",
			opts: func(t *testing.T) Options {
				file := writeFile(t, t.TempDir(), "config.json",
					`{"provider": "openai", "model": "m", "max_steps": -1}`)
				return Options{ConfigPath: file, Getenv: envOf(nil), HomeDir: t.TempDir()}
			},
		},
		{
			name: "negative via env",
			opts: func(t *testing.T) Options {
				return Options{
					CLI:     Overrides{Provider: strPtr("openai"), Model: strPtr("m")},
					Getenv:  envOf(map[string]string{"BOB_MAX_STEPS": "-5"}),
					HomeDir: t.TempDir(),
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(tc.opts(t))
			if !errors.Is(err, ErrNonPositiveLimit) {
				t.Errorf("err = %v, want ErrNonPositiveLimit", err)
			}
		})
	}
}

func TestInvalidEnvValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"BOB_MAX_STEPS not an int", map[string]string{"BOB_MAX_STEPS": "many"}},
		{"BOB_ALLOW_WRITES not a bool", map[string]string{"BOB_ALLOW_WRITES": "yep"}},
		{"BOB_ALLOW_EXEC not a bool", map[string]string{"BOB_ALLOW_EXEC": "sure"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(Options{
				CLI:     Overrides{Provider: strPtr("openai"), Model: strPtr("m")},
				Getenv:  envOf(tc.env),
				HomeDir: t.TempDir(),
			})
			if !errors.Is(err, ErrInvalidEnv) {
				t.Errorf("err = %v, want ErrInvalidEnv", err)
			}
		})
	}
}

func TestMalformedBaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"empty allowed (provider default)", "", false},
		{"valid https", "https://api.example.com/v1", false},
		{"valid http", "http://localhost:8080/v1", false},
		{"no scheme", "api.example.com/v1", true},
		{"wrong scheme", "ftp://api.example.com", true},
		{"scheme only, no host", "https://", true},
		{"garbage", "http://[::1]:namedport", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(Options{
				CLI: Overrides{
					Provider: strPtr("openai"), Model: strPtr("m"),
					BaseURL: strPtr(tc.baseURL),
				},
				Getenv:  envOf(nil),
				HomeDir: t.TempDir(),
			})
			if tc.wantErr && !errors.Is(err, ErrMalformedURL) {
				t.Errorf("err = %v, want ErrMalformedURL", err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		})
	}
}

func TestEvidenceDirMustBeOutsideWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "evidence")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		evidenceDir string
		wantErr     bool
	}{
		{"inside workspace subdir", filepath.Join(ws, "sub"), true},
		{"workspace itself", ws, true},
		{"not-yet-existing path inside workspace", filepath.Join(ws, "future", "evidence"), true},
		{"sibling of workspace", outside, false},
		{"sibling with workspace-prefix name", filepath.Join(root, "workspace-evidence"), false},
		{"empty evidence dir (disabled) allowed", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(Options{
				CLI: Overrides{
					Provider: strPtr("openai"), Model: strPtr("m"),
					Workspace:   strPtr(ws),
					EvidenceDir: strPtr(tc.evidenceDir),
				},
				Getenv:  envOf(nil),
				HomeDir: t.TempDir(),
			})
			if tc.wantErr && !errors.Is(err, ErrUnsafeEvidenceDir) {
				t.Errorf("err = %v, want ErrUnsafeEvidenceDir", err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		})
	}
}

func TestEvidenceDirSymlinkIntoWorkspaceRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	target := filepath.Join(ws, "hidden-evidence")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "innocent-looking")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Load(Options{
		CLI: Overrides{
			Provider: strPtr("openai"), Model: strPtr("m"),
			Workspace:   strPtr(ws),
			EvidenceDir: strPtr(link),
		},
		Getenv:  envOf(nil),
		HomeDir: t.TempDir(),
	})
	if !errors.Is(err, ErrUnsafeEvidenceDir) {
		t.Errorf("err = %v, want ErrUnsafeEvidenceDir (symlink resolves into workspace)", err)
	}
}

// TestDefaultProviderModelSeeded: with no source setting provider/model, the
// merge seeds the documented operational default at the lowest tier.
func TestDefaultProviderModelSeeded(t *testing.T) {
	t.Parallel()
	cfg, err := Load(Options{Getenv: envOf(nil), HomeDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Load with defaults: %v", err)
	}
	if cfg.Provider != DefaultProvider || cfg.Model != DefaultModelID {
		t.Errorf("defaults = %s/%s, want %s/%s", cfg.Provider, cfg.Model, DefaultProvider, DefaultModelID)
	}
}

// TestMissingProviderModel: the defaults can still be explicitly blanked by a
// higher-precedence source, and that remains a typed validation error.
func TestMissingProviderModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cli  Overrides
	}{
		{"provider blanked", Overrides{Provider: strPtr("")}},
		{"model blanked", Overrides{Model: strPtr("")}},
		{"both blanked", Overrides{Provider: strPtr(""), Model: strPtr("")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(Options{CLI: tc.cli, Getenv: envOf(nil), HomeDir: t.TempDir()})
			if !errors.Is(err, ErrMissingModel) {
				t.Errorf("err = %v, want ErrMissingModel", err)
			}
		})
	}
}

func TestApprovalModeAndOutputFormat(t *testing.T) {
	t.Parallel()
	base := func(mut func(*Overrides)) Options {
		o := Overrides{Provider: strPtr("openai"), Model: strPtr("m")}
		mut(&o)
		return Options{CLI: o, Getenv: envOf(nil), HomeDir: t.TempDir()}
	}

	t.Run("valid modes accepted", func(t *testing.T) {
		t.Parallel()
		for _, mode := range []string{"auto", "prompt"} {
			if _, err := Load(base(func(o *Overrides) { o.ApprovalMode = strPtr(mode) })); err != nil {
				t.Errorf("mode %q: unexpected err %v", mode, err)
			}
		}
	})
	t.Run("invalid approval mode rejected", func(t *testing.T) {
		t.Parallel()
		_, err := Load(base(func(o *Overrides) { o.ApprovalMode = strPtr("yolo") }))
		if !errors.Is(err, ErrInvalidApprovalMode) {
			t.Errorf("err = %v, want ErrInvalidApprovalMode", err)
		}
	})
	t.Run("valid output formats accepted", func(t *testing.T) {
		t.Parallel()
		for _, f := range []string{"text", "json"} {
			if _, err := Load(base(func(o *Overrides) { o.OutputFormat = strPtr(f) })); err != nil {
				t.Errorf("format %q: unexpected err %v", f, err)
			}
		}
	})
	t.Run("invalid output format rejected", func(t *testing.T) {
		t.Parallel()
		_, err := Load(base(func(o *Overrides) { o.OutputFormat = strPtr("xml") }))
		if !errors.Is(err, ErrInvalidOutputFormat) {
			t.Errorf("err = %v, want ErrInvalidOutputFormat", err)
		}
	})
}

func TestContradictoryPermissions(t *testing.T) {
	t.Parallel()
	t.Run("exec without writes rejected", func(t *testing.T) {
		t.Parallel()
		_, err := Load(Options{
			CLI: Overrides{
				Provider: strPtr("openai"), Model: strPtr("m"),
				AllowExec: boolPtr(true), AllowWrites: boolPtr(false),
			},
			Getenv:  envOf(nil),
			HomeDir: t.TempDir(),
		})
		if !errors.Is(err, ErrContradictoryPermissions) {
			t.Errorf("err = %v, want ErrContradictoryPermissions", err)
		}
	})
	t.Run("exec with writes accepted", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load(Options{
			CLI: Overrides{
				Provider: strPtr("openai"), Model: strPtr("m"),
				AllowExec: boolPtr(true), AllowWrites: boolPtr(true),
			},
			Getenv:  envOf(nil),
			HomeDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.AllowExec || !cfg.AllowWrites {
			t.Errorf("permissions not carried: %+v", cfg)
		}
	})
}

func TestFullFileRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	ev := filepath.Join(root, "ev")
	body := `{
		"provider": "deepseek",
		"model": "deepseek-chat",
		"base_url": "https://api.deepseek.com/v1",
		"workspace": ` + jsonQuote(ws) + `,
		"max_steps": 12,
		"timeout": "90s",
		"allow_writes": true,
		"allow_exec": true,
		"approval_mode": "auto",
		"evidence_dir": ` + jsonQuote(ev) + `,
		"output_format": "json"
	}`
	file := writeFile(t, root, "config.json", body)
	cfg, err := Load(Options{ConfigPath: file, Getenv: envOf(nil), HomeDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Provider:     "deepseek",
		Model:        "deepseek-chat",
		BaseURL:      "https://api.deepseek.com/v1",
		Workspace:    ws,
		MaxSteps:     12,
		Timeout:      90 * time.Second,
		AllowWrites:  true,
		AllowExec:    true,
		ApprovalMode: "auto",
		EvidenceDir:  ev,
		OutputFormat: "json",
	}
	if cfg != want {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", cfg, want)
	}
}

// jsonQuote JSON-quotes a string (paths may contain characters needing escape).
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err) // marshaling a string cannot fail
	}
	return string(b)
}

func TestExampleConfigParsesClean(t *testing.T) {
	t.Parallel()
	// The committed repo-root example must always parse with zero unknown
	// fields, so it can never teach a user to put an API key in the file.
	example := filepath.Join("..", "..", "config.example.json")
	if _, err := os.Stat(example); err != nil {
		t.Fatalf("config.example.json missing at repo root: %v", err)
	}
	cfg, err := Load(Options{ConfigPath: example, Getenv: envOf(nil), HomeDir: t.TempDir()})
	if err != nil {
		t.Fatalf("example config must load cleanly: %v", err)
	}
	if cfg.Provider == "" || cfg.Model == "" {
		t.Errorf("example config should carry placeholder provider/model, got %+v", cfg)
	}
}
