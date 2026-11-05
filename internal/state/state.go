// Package state records what claude-ctx wrote into other tools' config files,
// so it can detect external edits and undo exactly its own changes.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dcbark01/claude-ctx/internal/metacache"
)

// Opencode records the last `use` written into opencode's config.
type Opencode struct {
	Context string            `json:"context"`
	Path    string            `json:"path"`
	Values  map[string]string `json:"values"`
	// Previous holds the values these keys had before the first `use`, so
	// `unset` can restore them; a key absent here was absent from the file.
	Previous map[string]string `json:"previous,omitempty"`
}

// State is the root of state.json.
type State struct {
	Opencode *Opencode `json:"opencode,omitempty"`
}

// PathFor returns state.json next to the given config file.
func PathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "state.json")
}

// Load reads the state; a missing file yields an empty state.
func Load(path string) (*State, error) {
	s := &State{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Save atomically writes the state with mode 0600.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return metacache.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// Drift reports whether any recorded value differs from (or is missing in) current.
func (o *Opencode) Drift(current map[string]string) bool {
	for k, v := range o.Values {
		if cur, ok := current[k]; !ok || cur != v {
			return true
		}
	}
	return false
}
