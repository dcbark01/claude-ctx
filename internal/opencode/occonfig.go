package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/dcbark01/claude-ctx/internal/metacache"
)

// Top-level opencode config keys claude-ctx manages.
const (
	KeyModel      = "model"
	KeySmallModel = "small_model"
)

// BackupSuffix is appended to the config path for the pre-write backup.
const BackupSuffix = ".claude-ctx.bak"

const schemaURL = "https://opencode.ai/config.json"

// ConfigFile returns opencode.jsonc in dir if it exists, else opencode.json.
func ConfigFile(dir string) string {
	jsonc := filepath.Join(dir, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err == nil {
		return jsonc
	}
	return filepath.Join(dir, "opencode.json")
}

// ReadKeys returns the string values of the given top-level keys. Missing
// files and keys (and non-string values) are simply absent from the map.
func ReadKeys(path string, keys []string) (map[string]string, error) {
	out := map[string]string{}
	v, _, err := load(path)
	if err != nil || v == nil {
		return out, err
	}
	std := v.Clone()
	std.Standardize()
	var doc map[string]any
	if err := json.Unmarshal(std.Pack(), &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range keys {
		if s, ok := doc[k].(string); ok {
			out[k] = s
		}
	}
	return out, nil
}

// SetKeys sets and removes top-level string keys, preserving every other byte
// (comments, trailing commas, formatting). A missing file is created with
// $schema plus the set keys, mode 0600; an existing file keeps its mode and is
// backed up to <path>.claude-ctx.bak first.
func SetKeys(path string, set map[string]string, remove []string) error {
	v, mode, err := load(path)
	if err != nil {
		return err
	}
	existed := v != nil
	if !existed {
		parsed, err := hujson.Parse([]byte(`{"$schema": "` + schemaURL + `"}`))
		if err != nil {
			return err
		}
		v, mode = &parsed, 0o600
	}
	trailingComma := hasTrailingComma(v)
	added := map[string]bool{}
	var ops []map[string]any
	for _, k := range remove {
		if v.Find(pointer(k)) != nil {
			ops = append(ops, map[string]any{"op": "remove", "path": pointer(k)})
		}
	}
	for _, k := range sortedKeys(set) {
		op := "add"
		if v.Find(pointer(k)) != nil {
			op = "replace"
		} else {
			added[k] = true
		}
		ops = append(ops, map[string]any{"op": op, "path": pointer(k), "value": set[k]})
	}
	if len(ops) > 0 {
		patch, err := json.Marshal(ops)
		if err != nil {
			return err
		}
		if err := v.Patch(patch); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		formatAdded(v, added, trailingComma)
	}
	if !existed {
		v.Format() // nothing of the user's to preserve
	} else {
		if prev, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile(path+BackupSuffix, prev, mode); err != nil {
				return err
			}
		}
	}
	return metacache.WriteFileAtomic(path, v.Pack(), mode)
}

// load parses path; (nil, 0, nil) when the file does not exist.
func load(path string) (*hujson.Value, fs.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	return &v, st.Mode().Perm(), nil
}

// hasTrailingComma reports whether the root object's last member ends with a comma.
func hasTrailingComma(v *hujson.Value) bool {
	obj, ok := v.Value.(*hujson.Object)
	if !ok || len(obj.Members) == 0 {
		return false
	}
	return obj.Members[len(obj.Members)-1].Value.AfterExtra != nil
}

// formatAdded gives members appended by a patch the same line indentation as
// the existing members ("\n  " by default) and a space after the colon, and
// keeps a trailing comma if the object had one.
func formatAdded(v *hujson.Value, added map[string]bool, trailingComma bool) {
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return
	}
	indent := []byte("\n  ")
	for _, m := range obj.Members {
		if name, ok := m.Name.Value.(hujson.Literal); ok && added[name.String()] {
			continue
		}
		if i := bytes.LastIndexByte(m.Name.BeforeExtra, '\n'); i >= 0 {
			indent = append([]byte(nil), m.Name.BeforeExtra[i:]...)
			break
		}
	}
	for i := range obj.Members {
		m := &obj.Members[i]
		name, ok := m.Name.Value.(hujson.Literal)
		if !ok || !added[name.String()] {
			continue
		}
		// hujson moves a same-line comment of the previous member here; keep it.
		lead := bytes.TrimRight(m.Name.BeforeExtra, " \t\r\n")
		m.Name.BeforeExtra = append(append([]byte(nil), lead...), indent...)
		m.Name.AfterExtra = nil
		m.Value.BeforeExtra = []byte(" ")
	}
	if n := len(obj.Members); n > 0 {
		if trailingComma && obj.Members[n-1].Value.AfterExtra == nil {
			obj.Members[n-1].Value.AfterExtra = []byte{}
		}
	}
	if len(obj.AfterExtra) == 0 {
		obj.AfterExtra = []byte("\n")
	}
}

// pointer is the RFC 6901 pointer for a top-level key.
func pointer(key string) string {
	r := []rune{}
	for _, c := range key {
		switch c {
		case '~':
			r = append(r, '~', '0')
		case '/':
			r = append(r, '~', '1')
		default:
			r = append(r, c)
		}
	}
	return "/" + string(r)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
