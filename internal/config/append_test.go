package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendContext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("# my comment\n[defaults]\nkey_ttl_ms = 5\n\n[contexts.a]\nbase_url = \"http://a\"\nmodel = \"m\"\nkey_env = \"K\""), 0o600)
	err := AppendContext(p, "b", Context{BaseURL: "http://b", Model: `q"x`, KeyPass: "w/b", DisplayName: "B", Discovery: true,
		ModelAliases: map[string]string{"sonnet": "s", "opus": "o"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	s := string(data)
	if !strings.HasPrefix(s, "# my comment\n") {
		t.Error("comment lost")
	}
	if !strings.Contains(s, "\n\n[contexts.b]\n") || !strings.Contains(s, "  opus   = \"o\"\n  sonnet = \"s\"\n") {
		t.Errorf("block:\n%s", s)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Contexts["b"].Model != `q"x` || c.Contexts["a"].KeyEnv != "K" || c.Defaults.KeyTTLMs != 5 {
		t.Errorf("%+v", c)
	}
	if err := AppendContext(p, "b", Context{BaseURL: "http://x", Model: "m", KeyEnv: "E"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("dup: %v", err)
	}
	if err := AppendContext(p, "bad name", Context{}); err == nil {
		t.Error("bad name accepted")
	}
	if err := AppendContext(p, "c", Context{BaseURL: "http://x", Model: "m"}); err == nil {
		t.Error("invalid context accepted")
	}
}

func TestAppendCreatesFromTemplate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := AppendContext(p, "x", Context{BaseURL: "http://x", Model: "m", KeyEnv: "E"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(data), "# claude-ctx configuration") || !strings.Contains(string(data), "[contexts.x]") {
		t.Errorf("%s", data)
	}
}
