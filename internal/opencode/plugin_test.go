package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPluginSourcePlaceholders(t *testing.T) {
	bin := `/Users/me/my "go" bin/claude-ctx`
	src := string(PluginSource(bin, "1.2.3", []string{"--config", "/a b/config.toml"}))
	if strings.Contains(src, "__CLAUDE_CTX_") {
		t.Error("placeholder left in output")
	}
	if first, _, _ := strings.Cut(src, "\n"); first != "// claude-ctx-plugin-version: 1.2.3" {
		t.Errorf("first line = %q", first)
	}
	m := regexp.MustCompile(`(?m)^const BIN: string = (.*)$`).FindStringSubmatch(src)
	var got string
	if m == nil || json.Unmarshal([]byte(m[1]), &got) != nil || got != bin {
		t.Errorf("BIN line = %v", m)
	}
	if !strings.Contains(src, `const ARGS: string[] = ["--config","/a b/config.toml"]`) {
		t.Error("ARGS not substituted")
	}
	if !strings.Contains(string(PluginSource(bin, "1", nil)), "const ARGS: string[] = []") {
		t.Error("nil ARGS not rendered as []")
	}
}

func TestInstalledPluginVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "claude-ctx.ts")
	if _, ok := InstalledPluginVersion(p); ok {
		t.Error("missing file reported installed")
	}
	os.WriteFile(p, PluginSource("/b", "1.2.3", nil), 0o644)
	if v, ok := InstalledPluginVersion(p); !ok || v != "1.2.3" {
		t.Errorf("got %q %v", v, ok)
	}
	os.WriteFile(p, []byte("export default {}\n"), 0o644)
	if _, ok := InstalledPluginVersion(p); ok {
		t.Error("foreign file reported as ours")
	}
}

func TestParseOpencodeVersion(t *testing.T) {
	if major, raw, err := ParseVersion("opencode v2.2.5\n"); err != nil || major != 2 || raw != "2.2.5" {
		t.Errorf("got %d %q %v", major, raw, err)
	}
	if major, _, err := ParseVersion("1.9.0"); err != nil || major != 1 {
		t.Errorf("got %d %v", major, err)
	}
	if _, _, err := ParseVersion("garbage"); err == nil {
		t.Error("garbage parsed")
	}
}

func TestPluginPath(t *testing.T) {
	if got := PluginPath("/oc"); got != "/oc/plugins/claude-ctx.ts" {
		t.Errorf("got %s", got)
	}
}

func TestInstalledPluginBin(t *testing.T) {
	p := filepath.Join(t.TempDir(), "claude-ctx.ts")
	os.WriteFile(p, PluginSource(`/a "b"/claude-ctx`, "1", nil), 0o644)
	if bin, ok := InstalledPluginBin(p); !ok || bin != `/a "b"/claude-ctx` {
		t.Errorf("got %q %v", bin, ok)
	}
	if _, ok := InstalledPluginBin(filepath.Join(t.TempDir(), "missing.ts")); ok {
		t.Error("missing file")
	}
}
