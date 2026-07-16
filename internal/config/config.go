// Package config is Bob's typed configuration system.
//
// Format choice: the config file is JSON (config.example.json), parsed with
// the standard library only. TOML was considered, but go.mod carries no direct
// TOML dependency (github.com/pelletier/go-toml/v2 is indirect-only via Eino),
// and adopting it would promote a new direct dependency. encoding/json needs
// zero new dependencies, so JSON wins.
//
// Precedence (highest wins):
//
//	explicit CLI values > INTENT_BOB_EINO_* env > BOB_* env (legacy, warned
//	once per variable; values never printed) > provider-specific env
//	(MINIMAX_BASE_URL / MINIMAX_MODEL, applied only when the effective
//	provider is "minimax") > config file > safe defaults.
//
// The canonical env namespace is INTENT_BOB_EINO_* — derived from the
// component id in the identity contract (intent-bob-eino; see
// 000-docs/005-DR-STND-bob-eino-identity-contract.md). The legacy BOB_*
// namespace collides with iam-bob-pydantic and is kept only as a warned
// compatibility alias; for any variable, the canonical name wins.
//
// Config file lookup precedence:
//
//	--config path arg > $INTENT_BOB_EINO_CONFIG > $BOB_CONFIG (legacy, warned)
//	> <base>/intent-solutions/agents/bob/eino-go/config.json (canonical)
//	> <base>/iam-bob-eino/config.json (legacy location, warned)
//	for each base in ($XDG_CONFIG_HOME, ~/.config) > none (defaults only).
//
// SECURITY: API keys are NEVER read from, or written to, the config file.
// Keys stay env-only (e.g. OPENAI_API_KEY, MINIMAX_API_KEY) and are resolved
// by the provider layer at build time. Because the file parser rejects every
// unknown key, a stray "api_key" field in a config file is a hard error, not
// a silently honored secret. Config values are content-safe to log; there is
// deliberately no key-bearing field on Config.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Environment namespaces. EnvPrefix is the canonical namespace, derived from
// the identity contract's component id (intent-bob-eino → INTENT_BOB_EINO_);
// a test asserts the derivation so the prefix can never drift from
// internal/identity.ComponentID. LegacyEnvPrefix is the pre-contract BOB_*
// namespace, kept as a warned compatibility alias only.
const (
	EnvPrefix       = "INTENT_BOB_EINO_"
	LegacyEnvPrefix = "BOB_"
)

// envNames lists the logical variable names of the full 12-var namespace.
// Each exists in both the canonical (INTENT_BOB_EINO_<NAME>) and legacy
// (BOB_<NAME>) form; the canonical form always wins.
var envNames = []string{
	"CONFIG", "PROVIDER", "MODEL", "BASE_URL", "WORKSPACE", "MAX_STEPS",
	"TIMEOUT", "ALLOW_WRITES", "ALLOW_EXEC", "APPROVAL_MODE", "EVIDENCE_DIR",
	"OUTPUT_FORMAT",
}

// envReader resolves one logical variable against both namespaces. Reading a
// value from the legacy namespace emits a once-per-variable deprecation
// warning that names both variables but NEVER prints the value.
type envReader struct {
	getenv func(string) string
	warn   io.Writer
	warned map[string]bool
}

func newEnvReader(getenv func(string) string, warn io.Writer) *envReader {
	if warn == nil {
		warn = os.Stderr
	}
	return &envReader{getenv: getenv, warn: warn, warned: map[string]bool{}}
}

// get returns the effective value for the logical variable name plus the
// concrete environment variable it came from (for error attribution).
func (e *envReader) get(name string) (value, sourceVar string) {
	if v := e.getenv(EnvPrefix + name); v != "" {
		return v, EnvPrefix + name
	}
	if v := e.getenv(LegacyEnvPrefix + name); v != "" {
		if !e.warned[name] {
			e.warned[name] = true
			fmt.Fprintf(e.warn, "warning: %s is deprecated; use %s\n",
				LegacyEnvPrefix+name, EnvPrefix+name)
		}
		return v, LegacyEnvPrefix + name
	}
	return "", ""
}

// Typed sentinel errors. Every validation failure wraps exactly one of these,
// so callers can errors.Is-dispatch without string matching.
var (
	// ErrUnknownField is returned when a config file contains a key this
	// package does not recognize (including any key-like "api_key" field).
	ErrUnknownField = errors.New("unknown config field")
	// ErrInvalidDuration is returned for an unparseable or negative timeout.
	ErrInvalidDuration = errors.New("invalid duration")
	// ErrNonPositiveLimit is returned for a zero or negative numeric limit
	// (MaxSteps).
	ErrNonPositiveLimit = errors.New("limit must be a positive integer")
	// ErrMalformedURL is returned for a BaseURL that does not parse as an
	// absolute http(s) URL with a host.
	ErrMalformedURL = errors.New("malformed base URL")
	// ErrMissingModel is returned when the provider/model combo is incomplete.
	ErrMissingModel = errors.New("provider and model must both be set")
	// ErrUnsafeEvidenceDir is returned when EvidenceDir resolves inside the
	// workspace: the Intent Agent Model must never be able to rewrite its own
	// evidence trail through workspace writes.
	ErrUnsafeEvidenceDir = errors.New("evidence dir must not be inside the workspace")
	// ErrContradictoryPermissions is returned for permission combinations
	// that cannot be enforced (exec granted while writes are denied: a shell
	// can always write, so the write denial would be a fiction).
	ErrContradictoryPermissions = errors.New("contradictory permissions")
	// ErrInvalidApprovalMode is returned for an ApprovalMode outside
	// {"auto", "prompt"}.
	ErrInvalidApprovalMode = errors.New(`approval mode must be "auto" or "prompt"`)
	// ErrInvalidOutputFormat is returned for an OutputFormat outside
	// {"text", "json"}.
	ErrInvalidOutputFormat = errors.New(`output format must be "text" or "json"`)
	// ErrConfigFile is returned when an explicitly requested config file
	// (--config, INTENT_BOB_EINO_CONFIG, or legacy BOB_CONFIG) cannot be
	// read or parsed.
	ErrConfigFile = errors.New("config file error")
	// ErrInvalidEnv is returned when an INTENT_BOB_EINO_* (or legacy BOB_*) environment variable holds an
	// unparseable value.
	ErrInvalidEnv = errors.New("invalid environment value")
)

// FieldError attaches the offending field (and optional detail) to one of the
// sentinel errors above. errors.Is(err, Err...) still matches.
type FieldError struct {
	Field  string // config field or env var name
	Detail string // human context; never contains secret values
	Err    error  // one of the sentinels above
}

func (e *FieldError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("config: field %q: %v", e.Field, e.Err)
	}
	return fmt.Sprintf("config: field %q: %v (%s)", e.Field, e.Err, e.Detail)
}

func (e *FieldError) Unwrap() error { return e.Err }

func fieldErr(field string, sentinel error, detail string) error {
	return &FieldError{Field: field, Detail: detail, Err: sentinel}
}

// Safe defaults.
const (
	DefaultMaxSteps     = 32
	DefaultTimeout      = 2 * time.Minute
	DefaultApprovalMode = "prompt"
	DefaultOutputFormat = "text"
	DefaultEvidenceDir  = "" // provider of the dir (CLI layer) decides; empty = disabled
)

// Config is the fully merged, validated runtime configuration for Bob.
// It intentionally carries no API keys — keys are env-only (see package doc).
type Config struct {
	Provider     string        // model provider name, e.g. "deepseek", "minimax"
	Model        string        // model id at that provider
	BaseURL      string        // optional endpoint override (must be http/https)
	Workspace    string        // root the Intent Agent Model may operate in
	MaxSteps     int           // max agent loop steps, > 0
	Timeout      time.Duration // overall run timeout, >= 0 (0 = no timeout)
	AllowWrites  bool          // permit file writes (still approval-gated)
	AllowExec    bool          // permit command execution (still approval-gated)
	ApprovalMode string        // "auto" | "prompt"
	EvidenceDir  string        // evidence sink; must resolve OUTSIDE Workspace
	OutputFormat string        // "text" | "json"
}

// Overrides carries explicit CLI values. A nil field means "not set on the
// command line"; a non-nil field always wins over every other source.
type Overrides struct {
	Provider     *string
	Model        *string
	BaseURL      *string
	Workspace    *string
	MaxSteps     *int
	Timeout      *time.Duration
	AllowWrites  *bool
	AllowExec    *bool
	ApprovalMode *string
	EvidenceDir  *string
	OutputFormat *string
}

// Options parameterizes Load. Zero value is usable: real env, real home.
type Options struct {
	// ConfigPath is the --config CLI argument. When set, the file MUST exist
	// and parse; a missing explicit file is an error, never a silent skip.
	ConfigPath string
	// CLI holds explicit command-line values (highest precedence).
	CLI Overrides
	// Getenv is the environment source; nil means os.Getenv. Tests inject a
	// map-backed function so no test touches the process environment.
	Getenv func(string) string
	// HomeDir overrides the user home for the ~/.config lookup tier; empty
	// means os.UserHomeDir.
	HomeDir string
	// Warn receives legacy-namespace deprecation warnings (never values);
	// nil means os.Stderr. Tests inject a buffer.
	Warn io.Writer
}

// fileConfig mirrors the JSON config file. Pointer fields distinguish
// "absent" from zero values; Timeout is a Go duration string ("90s", "2m").
// json.Decoder.DisallowUnknownFields makes every unrecognized key an error.
// "_comment" is the one blessed extra key (JSON has no comment syntax); its
// value is read and discarded.
type fileConfig struct {
	Comment      *string `json:"_comment"` // ignored; JSON's stand-in for comments
	Provider     *string `json:"provider"`
	Model        *string `json:"model"`
	BaseURL      *string `json:"base_url"`
	Workspace    *string `json:"workspace"`
	MaxSteps     *int    `json:"max_steps"`
	Timeout      *string `json:"timeout"`
	AllowWrites  *bool   `json:"allow_writes"`
	AllowExec    *bool   `json:"allow_exec"`
	ApprovalMode *string `json:"approval_mode"`
	EvidenceDir  *string `json:"evidence_dir"`
	OutputFormat *string `json:"output_format"`
}

// Load merges every configuration source per the package-doc precedence and
// returns a validated Config. On any failure it returns a typed error that
// wraps one of the package sentinels.
func Load(opts Options) (Config, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	env := newEnvReader(getenv, opts.Warn)

	cfg := Config{
		MaxSteps:     DefaultMaxSteps,
		Timeout:      DefaultTimeout,
		ApprovalMode: DefaultApprovalMode,
		OutputFormat: DefaultOutputFormat,
		EvidenceDir:  DefaultEvidenceDir,
	}

	// Tier: config file.
	path, explicit, err := resolveConfigPath(opts, env)
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		if err := applyFile(&cfg, path, explicit); err != nil {
			return Config{}, err
		}
	}

	// Tier: provider-specific env. Applied only when the EFFECTIVE provider
	// (considering the higher-precedence CLI and env tiers too) is minimax,
	// so MINIMAX_* left over in an environment cannot leak into a run
	// against a different provider. MINIMAX_* is provider-native, not part
	// of the INTENT_BOB_EINO_* namespace, and is not deprecation-warned.
	if effectiveProvider(cfg, opts.CLI, env) == "minimax" {
		if v := getenv("MINIMAX_BASE_URL"); v != "" {
			cfg.BaseURL = v
		}
		if v := getenv("MINIMAX_MODEL"); v != "" {
			cfg.Model = v
		}
	}

	// Tier: INTENT_BOB_EINO_* env (canonical), with BOB_* legacy fallback.
	if err := applyEnv(&cfg, env); err != nil {
		return Config{}, err
	}

	// Tier: explicit CLI values.
	applyOverrides(&cfg, opts.CLI)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Config-file directory fragments under each base dir. The canonical
// location follows the identity contract's state-path convention
// (org/agents/persona/runtime); the legacy pre-contract location is still
// discovered, with a once-per-load warning.
const (
	canonicalConfigDir = "intent-solutions/agents/bob/eino-go"
	legacyConfigDir    = "iam-bob-eino"
)

// resolveConfigPath walks the file lookup precedence. It returns the chosen
// path (empty = none), whether the path was explicitly requested (explicit
// paths must exist), and an error only for an explicit path that is missing.
func resolveConfigPath(opts Options, env *envReader) (path string, explicit bool, err error) {
	if opts.ConfigPath != "" {
		if _, statErr := os.Stat(opts.ConfigPath); statErr != nil {
			return "", true, fieldErr("--config", ErrConfigFile, statErr.Error())
		}
		return opts.ConfigPath, true, nil
	}
	if p, source := env.get("CONFIG"); p != "" {
		if _, statErr := os.Stat(p); statErr != nil {
			return "", true, fieldErr(source, ErrConfigFile, statErr.Error())
		}
		return p, true, nil
	}
	var dirs []string
	if xdg := env.getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, xdg)
	}
	home := opts.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir() // best-effort; empty just skips the tier
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".config"))
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, filepath.FromSlash(canonicalConfigDir), "config.json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, false, nil
		}
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, legacyConfigDir, "config.json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			fmt.Fprintf(env.warn, "warning: config found at legacy location %s; move it to %s\n",
				candidate, filepath.Join(dir, filepath.FromSlash(canonicalConfigDir), "config.json"))
			return candidate, false, nil
		}
	}
	return "", false, nil
}

// applyFile parses one JSON config file into cfg. Unknown keys are rejected
// (this is also what keeps API keys out of config files by construction).
func applyFile(cfg *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fieldErr(path, ErrConfigFile, err.Error())
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var fc fileConfig
	if err := dec.Decode(&fc); err != nil {
		if name, ok := unknownFieldName(err); ok {
			return fieldErr(name, ErrUnknownField,
				fmt.Sprintf("in %s; note: API keys never go in config files, set them in the environment", path))
		}
		return fieldErr(path, ErrConfigFile, err.Error())
	}
	// Reject trailing garbage after the JSON object.
	if dec.More() {
		return fieldErr(path, ErrConfigFile, "trailing data after config object")
	}
	_ = explicit

	if fc.Provider != nil {
		cfg.Provider = *fc.Provider
	}
	if fc.Model != nil {
		cfg.Model = *fc.Model
	}
	if fc.BaseURL != nil {
		cfg.BaseURL = *fc.BaseURL
	}
	if fc.Workspace != nil {
		cfg.Workspace = *fc.Workspace
	}
	if fc.MaxSteps != nil {
		cfg.MaxSteps = *fc.MaxSteps
	}
	if fc.Timeout != nil {
		d, perr := time.ParseDuration(*fc.Timeout)
		if perr != nil {
			return fieldErr("timeout", ErrInvalidDuration, *fc.Timeout)
		}
		cfg.Timeout = d
	}
	if fc.AllowWrites != nil {
		cfg.AllowWrites = *fc.AllowWrites
	}
	if fc.AllowExec != nil {
		cfg.AllowExec = *fc.AllowExec
	}
	if fc.ApprovalMode != nil {
		cfg.ApprovalMode = *fc.ApprovalMode
	}
	if fc.EvidenceDir != nil {
		cfg.EvidenceDir = *fc.EvidenceDir
	}
	if fc.OutputFormat != nil {
		cfg.OutputFormat = *fc.OutputFormat
	}
	return nil
}

// unknownFieldName extracts the field name from encoding/json's
// DisallowUnknownFields error ('json: unknown field "foo"').
func unknownFieldName(err error) (string, bool) {
	const marker = `json: unknown field `
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	name := strings.Trim(msg[i+len(marker):], `"`)
	return name, true
}

// applyEnv folds the environment tier into cfg: for each logical variable
// the canonical INTENT_BOB_EINO_<NAME> wins, with BOB_<NAME> honored as a
// warned legacy fallback (see envReader).
func applyEnv(cfg *Config, env *envReader) error {
	if v, _ := env.get("PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v, _ := env.get("MODEL"); v != "" {
		cfg.Model = v
	}
	if v, _ := env.get("BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v, _ := env.get("WORKSPACE"); v != "" {
		cfg.Workspace = v
	}
	if v, source := env.get("MAX_STEPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fieldErr(source, ErrInvalidEnv, "not an integer")
		}
		cfg.MaxSteps = n
	}
	if v, source := env.get("TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fieldErr(source, ErrInvalidDuration, v)
		}
		cfg.Timeout = d
	}
	if v, source := env.get("ALLOW_WRITES"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fieldErr(source, ErrInvalidEnv, "not a boolean")
		}
		cfg.AllowWrites = b
	}
	if v, source := env.get("ALLOW_EXEC"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fieldErr(source, ErrInvalidEnv, "not a boolean")
		}
		cfg.AllowExec = b
	}
	if v, _ := env.get("APPROVAL_MODE"); v != "" {
		cfg.ApprovalMode = v
	}
	if v, _ := env.get("EVIDENCE_DIR"); v != "" {
		cfg.EvidenceDir = v
	}
	if v, _ := env.get("OUTPUT_FORMAT"); v != "" {
		cfg.OutputFormat = v
	}
	return nil
}

// applyOverrides folds explicit CLI values (highest precedence) into cfg.
func applyOverrides(cfg *Config, o Overrides) {
	if o.Provider != nil {
		cfg.Provider = *o.Provider
	}
	if o.Model != nil {
		cfg.Model = *o.Model
	}
	if o.BaseURL != nil {
		cfg.BaseURL = *o.BaseURL
	}
	if o.Workspace != nil {
		cfg.Workspace = *o.Workspace
	}
	if o.MaxSteps != nil {
		cfg.MaxSteps = *o.MaxSteps
	}
	if o.Timeout != nil {
		cfg.Timeout = *o.Timeout
	}
	if o.AllowWrites != nil {
		cfg.AllowWrites = *o.AllowWrites
	}
	if o.AllowExec != nil {
		cfg.AllowExec = *o.AllowExec
	}
	if o.ApprovalMode != nil {
		cfg.ApprovalMode = *o.ApprovalMode
	}
	if o.EvidenceDir != nil {
		cfg.EvidenceDir = *o.EvidenceDir
	}
	if o.OutputFormat != nil {
		cfg.OutputFormat = *o.OutputFormat
	}
}

// effectiveProvider answers "which provider will this run actually use?"
// so the provider-specific env tier only fires for its own provider. It
// shares the envReader with applyEnv, so the legacy warning for PROVIDER
// still fires at most once per Load.
func effectiveProvider(fileMerged Config, cli Overrides, env *envReader) string {
	if cli.Provider != nil {
		return strings.ToLower(*cli.Provider)
	}
	if v, _ := env.get("PROVIDER"); v != "" {
		return strings.ToLower(v)
	}
	return strings.ToLower(fileMerged.Provider)
}

// Validate checks a merged Config and returns a typed error (wrapping one of
// the package sentinels) on the first violation found.
func (c Config) Validate() error {
	if c.Provider == "" || c.Model == "" {
		return fieldErr("provider/model", ErrMissingModel,
			fmt.Sprintf("provider=%q model=%q", c.Provider, c.Model))
	}
	if c.MaxSteps <= 0 {
		return fieldErr("max_steps", ErrNonPositiveLimit, strconv.Itoa(c.MaxSteps))
	}
	if c.Timeout < 0 {
		return fieldErr("timeout", ErrInvalidDuration, c.Timeout.String())
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fieldErr("base_url", ErrMalformedURL, c.BaseURL)
		}
	}
	switch c.ApprovalMode {
	case "auto", "prompt":
	default:
		return fieldErr("approval_mode", ErrInvalidApprovalMode, c.ApprovalMode)
	}
	switch c.OutputFormat {
	case "text", "json":
	default:
		return fieldErr("output_format", ErrInvalidOutputFormat, c.OutputFormat)
	}
	if c.AllowExec && !c.AllowWrites {
		return fieldErr("allow_exec/allow_writes", ErrContradictoryPermissions,
			"exec implies filesystem mutation; granting exec while denying writes is unenforceable")
	}
	if c.EvidenceDir != "" && c.Workspace != "" {
		inside, err := pathInside(c.EvidenceDir, c.Workspace)
		if err != nil {
			return fieldErr("evidence_dir", ErrUnsafeEvidenceDir, err.Error())
		}
		if inside {
			return fieldErr("evidence_dir", ErrUnsafeEvidenceDir,
				fmt.Sprintf("%s resolves inside workspace %s", c.EvidenceDir, c.Workspace))
		}
	}
	return nil
}

// pathInside reports whether child resolves to a path at or under parent.
// Symlinks are resolved when the paths exist, so a link out of the workspace
// pointing back in is still caught.
func pathInside(child, parent string) (bool, error) {
	cAbs, err := resolvePath(child)
	if err != nil {
		return false, err
	}
	pAbs, err := resolvePath(parent)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(pAbs, cAbs)
	if err != nil {
		// Different volumes etc.: cannot be inside.
		return false, nil //nolint:nilerr // unrelatable paths are definitionally outside
	}
	if rel == "." {
		return true, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// resolvePath returns an absolute, symlink-resolved form of p. If p (or an
// ancestor) does not exist yet, it falls back to lexical Abs+Clean.
func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// Path does not exist yet: resolve the deepest existing ancestor and
	// re-append the remainder lexically.
	dir, rest := abs, ""
	for {
		parent := filepath.Dir(dir)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if parent == dir {
			return filepath.Clean(abs), nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}
