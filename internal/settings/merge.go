// Package settings performs the surgical merge of claude-ctx-managed keys into
// a Claude Code settings.json, leaving every other key untouched.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/render"
)

// HelperKey is the top-level settings key for the credential helper.
const HelperKey = "apiKeyHelper"

// BackupSuffix is appended to the settings path for the pre-write backup.
const BackupSuffix = ".claude-ctx.bak"

// ErrProjectScope is returned when a write targets a repo-shipped settings file.
var ErrProjectScope = errors.New("refusing to write a project-scope .claude/settings.json (it may be committed; apiKeyHelper there is arbitrary code execution on project open)")

// DefaultPath is ~/.claude/settings.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Load reads a settings file into a generic document. A missing file yields an
// empty document. Numbers are preserved via json.Number.
func Load(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// envMap returns doc["env"] as a map, creating it when create is set.
func envMap(doc map[string]any, create bool) (map[string]any, error) {
	raw, ok := doc["env"]
	if !ok || raw == nil {
		if !create {
			return nil, nil
		}
		m := map[string]any{}
		doc["env"] = m
		return m, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings \"env\" is %T, expected an object", raw)
	}
	return m, nil
}

// scrub removes every previously-managed key and every key claude-ctx could
// ever emit, so stale values from older contexts/versions never linger.
func scrub(doc map[string]any) error {
	keys := append([]string(nil), render.AllManagedEnvKeys...)
	if s, ok := ReadSentinel(doc); ok {
		for _, k := range s.ManagedKeys {
			keys = append(keys, strings.TrimPrefix(k, "env."))
		}
	}
	env, err := envMap(doc, false)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k == HelperKey {
			continue
		}
		if env != nil {
			delete(env, k)
		}
	}
	delete(doc, HelperKey)
	delete(doc, SentinelKey)
	if env != nil && len(env) == 0 {
		delete(doc, "env")
	}
	return nil
}

// Apply merges the rendered context into doc in place.
// helper is the apiKeyHelper command to write (normally render.HelperCommand).
func Apply(doc map[string]any, ctxName string, r render.Rendered, helper string) error {
	if err := scrub(doc); err != nil {
		return err
	}
	env, err := envMap(doc, true)
	if err != nil {
		return err
	}
	managed := make([]string, 0, len(r.Env)+1)
	for _, k := range r.Keys() {
		env[k] = r.Env[k]
		managed = append(managed, "env."+k)
	}
	if helper == "" {
		helper = render.HelperCommand
	}
	doc[HelperKey] = helper
	managed = append(managed, HelperKey)
	sort.Strings(managed)
	doc[SentinelKey] = Sentinel{
		Context:     ctxName,
		ManagedKeys: managed,
		Version:     SentinelVersion,
		Hash:        hashManaged(doc, managed),
	}
	return nil
}

// Remove strips everything claude-ctx manages. Returns false if nothing was managed.
func Remove(doc map[string]any) (bool, error) {
	_, had := ReadSentinel(doc)
	_, hadHelper := doc[HelperKey]
	if err := scrub(doc); err != nil {
		return false, err
	}
	return had || hadHelper, nil
}

// Current returns the active context name recorded in doc, or "".
func Current(doc map[string]any) string {
	if s, ok := ReadSentinel(doc); ok {
		return s.Context
	}
	return ""
}

// Marshal renders the document with 2-space indentation and a trailing newline.
func Marshal(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write atomically replaces path with doc, preserving mode and keeping a
// backup of the previous contents. It refuses project-scope paths.
func Write(path string, doc map[string]any) error {
	if err := guardProjectScope(path); err != nil {
		return err
	}
	data, err := Marshal(doc)
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		if prev, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(path+BackupSuffix, prev, mode)
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// guardProjectScope rejects <repo>/.claude/settings.json outside $HOME/.claude.
func guardProjectScope(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if filepath.Base(abs) != "settings.json" || filepath.Base(filepath.Dir(abs)) != ".claude" {
		return nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		if h, err := filepath.Abs(filepath.Join(home, ".claude")); err == nil && filepath.Dir(abs) == h {
			return nil
		}
	}
	// Inside a git worktree? Walk up from the .claude dir's parent.
	for dir := filepath.Dir(filepath.Dir(abs)); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return fmt.Errorf("%w: %s", ErrProjectScope, abs)
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}
