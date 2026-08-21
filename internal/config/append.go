package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName reports whether s is an acceptable context name (bare TOML key).
func ValidName(s string) bool { return nameRe.MatchString(s) }

// RenderContext produces the TOML block for one context.
func RenderContext(name string, c Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[contexts.%s]\n", name)
	fmt.Fprintf(&b, "base_url = %s\n", tomlString(c.BaseURL))
	fmt.Fprintf(&b, "model    = %s\n", tomlString(c.Model))
	switch {
	case c.KeyPass != "":
		fmt.Fprintf(&b, "key_pass = %s\n", tomlString(c.KeyPass))
	case c.KeyCmd != "":
		fmt.Fprintf(&b, "key_cmd  = %s\n", tomlString(c.KeyCmd))
	case c.KeyEnv != "":
		fmt.Fprintf(&b, "key_env  = %s\n", tomlString(c.KeyEnv))
	}
	if c.DisplayName != "" {
		fmt.Fprintf(&b, "display_name = %s\n", tomlString(c.DisplayName))
	}
	if c.Discovery {
		b.WriteString("discovery = true\n")
	}
	if len(c.ModelAliases) > 0 {
		fams := make([]string, 0, len(c.ModelAliases))
		for f := range c.ModelAliases {
			fams = append(fams, f)
		}
		sort.Strings(fams)
		fmt.Fprintf(&b, "\n  [contexts.%s.model_aliases]\n", name)
		for _, f := range fams {
			fmt.Fprintf(&b, "  %-6s = %s\n", f, tomlString(c.ModelAliases[f]))
		}
	}
	return b.String()
}

// AppendContext adds a context block to the end of the config file, keeping
// existing content (including comments) byte-for-byte. The file is created
// from Template if missing. The result is re-parsed and validated before the
// original is replaced.
func AppendContext(path, name string, c Context) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid context name %q (use letters, digits, . _ -)", name)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		existing = []byte(Template)
	}
	if len(existing) > 0 {
		if cfg, err := Parse(existing); err == nil {
			if _, dup := cfg.Contexts[name]; dup {
				return fmt.Errorf("context %q already exists in %s", name, path)
			}
		}
	}
	var out bytes.Buffer
	out.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		out.WriteByte('\n')
	}
	if len(existing) > 0 {
		out.WriteByte('\n')
	}
	out.WriteString(RenderContext(name, c))

	cfg, err := Parse(out.Bytes())
	if err != nil {
		return fmt.Errorf("generated config does not parse: %w", err)
	}
	if _, err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Chmod(0o600)
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func tomlString(s string) string {
	// Basic strings: escape backslash and double quote; everything here is
	// single-line, so that is sufficient.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
