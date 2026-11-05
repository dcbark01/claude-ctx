// Package metacache stores gateway metadata (discovered models, token limits)
// learned by foreground commands, so the opencode plugin never needs a key at
// startup. It never holds secrets.
package metacache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/gateway"
)

// Entry is what is known about one context's gateway.
type Entry struct {
	Models  []string                  `json:"models,omitempty"`
	Limits  map[string]gateway.Limits `json:"limits,omitempty"`
	Updated time.Time                 `json:"updated"`
}

// Cache maps context name to Entry.
type Cache struct {
	Contexts map[string]Entry `json:"contexts"`
}

// PathFor returns cache.json next to the given config file.
func PathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "cache.json")
}

// Load reads the cache; a missing file yields an empty cache.
func Load(path string) (*Cache, error) {
	c := &Cache{Contexts: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Contexts == nil {
		c.Contexts = map[string]Entry{}
	}
	return c, nil
}

// Save atomically writes the cache with mode 0600.
func (c *Cache) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// Refresh queries the gateway with key: /v1/models when discovery is on, and
// /model/info always. It returns whatever succeeded plus the joined failures.
func Refresh(ctx context.Context, cx config.Context, key string) (Entry, error) {
	e := Entry{Updated: time.Now().UTC()}
	var errs []error
	if cx.Discovery {
		models, err := gateway.ListModels(ctx, cx.BaseURL, key)
		if err != nil {
			errs = append(errs, fmt.Errorf("model discovery: %w", err))
		} else {
			e.Models = models
		}
	}
	limits, err := gateway.ModelInfo(ctx, cx.BaseURL, key)
	if err != nil {
		errs = append(errs, fmt.Errorf("model limits: %w", err))
	} else if len(limits) > 0 {
		e.Limits = limits
	}
	return e, errors.Join(errs...)
}

// Merge overlays the parts of fresh that succeeded onto old.
func Merge(old, fresh Entry) Entry {
	out := old
	out.Updated = fresh.Updated
	if fresh.Models != nil {
		out.Models = fresh.Models
	}
	if fresh.Limits != nil {
		out.Limits = fresh.Limits
	}
	return out
}

// WriteFileAtomic writes data via a temp file + rename, creating the parent dir.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
