package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/metacache"
)

func golden(t *testing.T, name string) string {
	return filepath.Join("..", "..", "testdata", "opencode", name)
}

func testConfig() *config.Config {
	return &config.Config{
		Defaults: config.Defaults{KeyTTLMs: config.DefaultKeyTTLMs},
		Contexts: map[string]config.Context{
			"opus-b": {
				BaseURL: "https://litellm-b.internal", Model: "anthropic/claude-opus-5", KeyPass: "x",
				DisplayName: "Opus 5 (B)", ContextLimit: 150000,
				ModelAliases: map[string]string{"opus": "anthropic/claude-opus-5", "haiku": "anthropic/claude-haiku-4-5"},
			},
			"local": {BaseURL: "http://localhost:8080", Model: "q1", KeyEnv: "K", Discovery: true},
		},
	}
}

func testCache() *metacache.Cache {
	return &metacache.Cache{Contexts: map[string]metacache.Entry{
		"local":  {Models: []string{"q1", "q2"}, Limits: map[string]gateway.Limits{"q1": {Context: 32768, Output: 4096}}},
		"opus-b": {Limits: map[string]gateway.Limits{"anthropic/claude-opus-5": {Context: 200000, Output: 64000}}},
	}}
}

func TestBuildGolden(t *testing.T) {
	got, err := json.MarshalIndent(Build(testConfig(), testCache()), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(golden(t, "providers.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRefsNoHaiku(t *testing.T) {
	model, small := Refs("local", config.Context{Model: "q1", ModelAliases: map[string]string{"opus": "o"}})
	if model != "ctx-local/q1" || small != "" {
		t.Errorf("got %q %q", model, small)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	c := &config.Config{Contexts: map[string]config.Context{"x": {BaseURL: "https://x/", Model: "m", KeyEnv: "K"}}}
	if got := Build(c, &metacache.Cache{})[0].BaseURL; got != "https://x/v1" {
		t.Errorf("got %s", got)
	}
}

func TestOneSidedLimitGetsDefaultOtherSide(t *testing.T) {
	c := &config.Config{Contexts: map[string]config.Context{"x": {BaseURL: "https://x", Model: "m", KeyEnv: "K", OutputLimit: 4096}}}
	l := Build(c, &metacache.Cache{})[0].Models[0].Limit
	if l == nil || *l != (Limit{Context: DefaultContextLimit, Output: 4096}) {
		t.Errorf("limit = %+v", l)
	}
}

func TestLimitSource(t *testing.T) {
	cx := config.Context{Model: "m"}
	e := metacache.Entry{Limits: map[string]gateway.Limits{"m": {Context: 1}}}
	if s := LimitSource(cx, metacache.Entry{}); s != "default" {
		t.Errorf("none: %s", s)
	}
	if s := LimitSource(cx, e); s != "model-info" {
		t.Errorf("cache: %s", s)
	}
	cx.ContextLimit = 5
	if s := LimitSource(cx, e); s != "toml" {
		t.Errorf("toml: %s", s)
	}
}
