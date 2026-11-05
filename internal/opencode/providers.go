// Package opencode renders claude-ctx contexts for opencode v2: provider
// definitions consumed by the claude-ctx plugin, the comment-preserving edit
// of opencode's config file, and the embedded plugin itself.
package opencode

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/metacache"
)

// ProviderPrefix namespaces claude-ctx providers inside opencode.
const ProviderPrefix = "ctx-"

// opencode's own defaults for a model with no limit (Model.Info.default).
const (
	DefaultContextLimit int64 = 200000
	DefaultOutputLimit  int64 = 32000
)

// Limit is an opencode model limit.
type Limit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

// Model is one model of a provider.
type Model struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Limit *Limit `json:"limit,omitempty"`
}

// Provider is one context rendered as an opencode provider.
type Provider struct {
	ID           string  `json:"id"`
	Context      string  `json:"context"`
	Name         string  `json:"name"`
	BaseURL      string  `json:"baseURL"`
	KeyTTLMs     int64   `json:"keyTtlMs"`
	Models       []Model `json:"models"`
	DefaultModel string  `json:"defaultModel"`
	SmallModel   string  `json:"smallModel,omitempty"`
}

// Output is the `claude-ctx opencode providers --json` document.
type Output struct {
	ConfigPath string     `json:"configPath"`
	CachePath  string     `json:"cachePath"`
	Providers  []Provider `json:"providers"`
}

// ProviderID maps a context name to its opencode provider id.
func ProviderID(name string) string { return ProviderPrefix + name }

// ModelRef is the opencode "provider/model" reference.
func ModelRef(name, model string) string { return ProviderID(name) + "/" + model }

// Refs returns the default and small model refs for a context; small is ""
// without a haiku alias.
func Refs(name string, cx config.Context) (model, small string) {
	model = ModelRef(name, cx.Model)
	if h := cx.ModelAliases["haiku"]; h != "" {
		small = ModelRef(name, h)
	}
	return model, small
}

// DefaultDir is $XDG_CONFIG_HOME/opencode, else ~/.config/opencode.
func DefaultDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

// Build renders every context, sorted by name. It never touches the network
// or resolves a credential.
func Build(c *config.Config, cache *metacache.Cache) []Provider {
	out := make([]Provider, 0, len(c.Contexts))
	for _, name := range c.Names() {
		cx := c.Contexts[name]
		entry := cache.Contexts[name]
		p := Provider{
			ID:       ProviderID(name),
			Context:  name,
			Name:     "claude-ctx: " + name,
			BaseURL:  strings.TrimRight(cx.BaseURL, "/") + "/v1",
			KeyTTLMs: c.Defaults.KeyTTLMs,
		}
		p.DefaultModel, p.SmallModel = Refs(name, cx)
		for _, id := range modelIDs(cx, entry) {
			m := Model{ID: id, Name: id}
			if id == cx.Model && cx.DisplayName != "" {
				m.Name = cx.DisplayName
			}
			m.Limit = limitFor(cx, entry, id)
			p.Models = append(p.Models, m)
		}
		out = append(out, p)
	}
	return out
}

// modelIDs: primary model, alias models by family order, discovered models; deduped.
func modelIDs(cx config.Context, e metacache.Entry) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	add(cx.Model)
	for _, fam := range config.AliasFamilies {
		add(cx.ModelAliases[fam])
	}
	if cx.Discovery {
		for _, m := range e.Models {
			add(m)
		}
	}
	return ids
}

// limitFor applies the precedence TOML (primary model only) → /model/info.
// A one-sided limit gets opencode's default for the other side; nothing known → nil.
func limitFor(cx config.Context, e metacache.Entry, id string) *Limit {
	l := Limit{Context: e.Limits[id].Context, Output: e.Limits[id].Output}
	if id == cx.Model {
		if cx.ContextLimit > 0 {
			l.Context = cx.ContextLimit
		}
		if cx.OutputLimit > 0 {
			l.Output = cx.OutputLimit
		}
	}
	if l.Context == 0 && l.Output == 0 {
		return nil
	}
	if l.Context == 0 {
		l.Context = DefaultContextLimit
	}
	if l.Output == 0 {
		l.Output = DefaultOutputLimit
	}
	return &l
}

// LimitSource reports where the primary model's context limit comes from:
// "toml", "model-info", or "default".
func LimitSource(cx config.Context, e metacache.Entry) string {
	switch {
	case cx.ContextLimit > 0 || cx.OutputLimit > 0:
		return "toml"
	case e.Limits[cx.Model] != (gateway.Limits{}):
		return "model-info"
	}
	return "default"
}
