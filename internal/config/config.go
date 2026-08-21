// Package config loads and validates the claude-ctx TOML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// DefaultKeyTTLMs is the default CLAUDE_CODE_API_KEY_HELPER_TTL_MS (1 hour).
const DefaultKeyTTLMs int64 = 3600000

// AliasFamilies are the Claude Code model families that may be repointed via
// model_aliases (rendered as ANTHROPIC_DEFAULT_<FAMILY>_MODEL).
var AliasFamilies = []string{"opus", "sonnet", "haiku", "fable"}

// Config is the root of config.toml.
type Config struct {
	Defaults Defaults           `toml:"defaults"`
	Contexts map[string]Context `toml:"contexts"`
}

// Defaults holds global settings.
type Defaults struct {
	KeyTTLMs  int64  `toml:"key_ttl_ms"`
	Namespace string `toml:"namespace"`
	StoreDir  string `toml:"store_dir"`
}

// Context is a single named gateway+model+credential bundle.
type Context struct {
	BaseURL      string            `toml:"base_url"`
	Model        string            `toml:"model"`
	KeyPass      string            `toml:"key_pass"`
	KeyCmd       string            `toml:"key_cmd"`
	KeyEnv       string            `toml:"key_env"`
	DisplayName  string            `toml:"display_name"`
	Discovery    bool              `toml:"discovery"`
	ModelAliases map[string]string `toml:"model_aliases"`
}

// Warning is a non-fatal validation finding.
type Warning struct {
	Context string
	Message string
}

func (w Warning) String() string {
	if w.Context == "" {
		return w.Message
	}
	return fmt.Sprintf("context %q: %s", w.Context, w.Message)
}

// DefaultPath returns $CLAUDE_CTX_CONFIG or ~/.claude/context/config.toml.
func DefaultPath() string {
	if p := os.Getenv("CLAUDE_CTX_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "context", "config.toml")
	}
	return filepath.Join(home, ".claude", "context", "config.toml")
}

// Load reads and strictly decodes a config file. It does not validate; call
// Validate separately so callers can decide how to surface warnings.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes TOML bytes. Unknown keys are an error so typos surface early.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		var sme *toml.StrictMissingError
		if errors.As(err, &sme) {
			return nil, fmt.Errorf("config: unknown keys:\n%s", sme.String())
		}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("config: %s (line %d, col %d)", de.Error(), row, col)
		}
		return nil, fmt.Errorf("config: %w", err)
	}
	if c.Defaults.KeyTTLMs == 0 {
		c.Defaults.KeyTTLMs = DefaultKeyTTLMs
	}
	if c.Defaults.StoreDir != "" {
		c.Defaults.StoreDir = ExpandHome(c.Defaults.StoreDir)
	}
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	return &c, nil
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Validate checks structural requirements. It returns a hard error for
// unusable configs and a list of warnings for suspicious-but-usable ones.
func (c *Config) Validate() ([]Warning, error) {
	var warns []Warning
	var errs []string
	if c.Defaults.KeyTTLMs < 0 {
		errs = append(errs, "defaults.key_ttl_ms must be >= 0")
	}
	for _, name := range c.Names() {
		ctx := c.Contexts[name]
		pre := fmt.Sprintf("context %q: ", name)
		if ctx.BaseURL == "" {
			errs = append(errs, pre+"base_url is required")
		} else if !strings.HasPrefix(ctx.BaseURL, "http://") && !strings.HasPrefix(ctx.BaseURL, "https://") {
			errs = append(errs, pre+"base_url must start with http:// or https://")
		}
		if ctx.Model == "" {
			errs = append(errs, pre+"model is required")
		}
		n := 0
		for _, v := range []string{ctx.KeyPass, ctx.KeyCmd, ctx.KeyEnv} {
			if v != "" {
				n++
			}
		}
		switch {
		case n == 0:
			errs = append(errs, pre+"one of key_pass, key_cmd, key_env is required")
		case n > 1:
			warns = append(warns, Warning{name, "multiple credential sources set; precedence is key_pass > key_cmd > key_env"})
		}
		for fam := range ctx.ModelAliases {
			if !isAliasFamily(fam) {
				errs = append(errs, pre+fmt.Sprintf("model_aliases.%s: unknown family (want one of %s)", fam, strings.Join(AliasFamilies, ", ")))
			}
		}
	}
	if len(errs) > 0 {
		return warns, errors.New("config: " + strings.Join(errs, "; "))
	}
	return warns, nil
}

// Names returns context names sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Contexts))
	for n := range c.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Get returns a context by name.
func (c *Config) Get(name string) (Context, error) {
	ctx, ok := c.Contexts[name]
	if !ok {
		return Context{}, fmt.Errorf("no such context %q (available: %s)", name, strings.Join(c.Names(), ", "))
	}
	return ctx, nil
}

func isAliasFamily(s string) bool {
	for _, f := range AliasFamilies {
		if f == s {
			return true
		}
	}
	return false
}

// Template is the commented starter config written by `claude-ctx edit`.
const Template = `# claude-ctx configuration
# Docs: https://github.com/dcbark01/claude-ctx

[defaults]
key_ttl_ms = 3600000            # emitted as CLAUDE_CODE_API_KEY_HELPER_TTL_MS
# namespace = "work/litellm"    # pass subtree offered by ` + "`add`" + `
# store_dir = "~/.password-store-work"

# [contexts.example]
# base_url = "https://litellm.example.internal"
# model    = "anthropic/claude-opus-5"
# key_pass = "work/litellm/example/anthropic"   # or key_cmd = "..." / key_env = "VAR"
# display_name = "Opus 5 (example)"
# discovery = true
#   [contexts.example.model_aliases]
#   opus   = "anthropic/claude-opus-5"
#   sonnet = "anthropic/claude-sonnet-5"
#   haiku  = "anthropic/claude-haiku-4-5"
`
