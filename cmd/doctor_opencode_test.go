package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/opencode"
)

// fakeBins puts executable scripts on a fresh PATH.
func fakeBins(t *testing.T, scripts map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for name, body := range scripts {
		os.WriteFile(filepath.Join(d, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	}
	t.Setenv("PATH", d)
	t.Setenv("CLAUDE_CTX_CONFIG", filepath.Join(t.TempDir(), "config.toml")) // isolate cache.json
	return d
}

func findCheck(checks []check, name string) *check {
	for i := range checks {
		if checks[i].name == name {
			return &checks[i]
		}
	}
	return nil
}

func docConfig() *config.Config {
	return &config.Config{Contexts: map[string]config.Context{"a": {BaseURL: "http://a", Model: "m", KeyEnv: "K"}}}
}

func installFake(t *testing.T, ocDir, pluginVersion, bin string) {
	t.Helper()
	p := opencode.PluginPath(ocDir)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, opencode.PluginSource(bin, pluginVersion, nil), 0o644)
}

func TestOpencodeChecksNotInstalled(t *testing.T) {
	fakeBins(t, nil)
	if got := opencodeChecks(docConfig(), []string{"a"}, t.TempDir(), "opencode"); len(got) != 0 {
		t.Errorf("checks without opencode: %+v", got)
	}
}

func TestOpencodeChecksOldOpencode(t *testing.T) {
	fakeBins(t, map[string]string{"opencode": `echo "opencode v1.9.0"`})
	c := findCheck(opencodeChecks(docConfig(), []string{"a"}, t.TempDir(), "opencode"), "opencode version")
	if c == nil || c.status != statusFail {
		t.Errorf("got %+v", c)
	}
}

func TestOpencodeChecksStalePlugin(t *testing.T) {
	old := version
	version = "1.0.0"
	t.Cleanup(func() { version = old })
	bins := fakeBins(t, map[string]string{"opencode": `echo "opencode v2.2.5"`, "claude-ctx": `echo "claude-ctx version 1.0.0"`})
	oc := t.TempDir()
	installFake(t, oc, "0.9.0", filepath.Join(bins, "claude-ctx"))
	c := findCheck(opencodeChecks(docConfig(), []string{"a"}, oc, "opencode"), "plugin")
	if c == nil || c.status != statusWarn || !strings.Contains(c.detail, "re-run `claude-ctx opencode install`") {
		t.Errorf("got %+v", c)
	}
}

func TestOpencodeChecksPathVersionSkew(t *testing.T) {
	old := version
	version = "1.0.0"
	t.Cleanup(func() { version = old })
	bins := fakeBins(t, map[string]string{"opencode": `echo "opencode v2.2.5"`, "claude-ctx": `echo "claude-ctx version 0.1.0"`})
	oc := t.TempDir()
	installFake(t, oc, "1.0.0", filepath.Join(bins, "claude-ctx"))
	c := findCheck(opencodeChecks(docConfig(), []string{"a"}, oc, "opencode"), "plugin binary")
	if c == nil || c.status != statusWarn || !strings.Contains(c.detail, "0.1.0") {
		t.Errorf("got %+v", c)
	}
}

func TestOpencodeChecksUnknownModel(t *testing.T) {
	fakeBins(t, map[string]string{"opencode": `echo "opencode v2.2.5"`})
	oc := t.TempDir()
	os.WriteFile(filepath.Join(oc, "opencode.json"), []byte(`{"model": "ctx-gone/x"}`), 0o644)
	c := findCheck(opencodeChecks(docConfig(), []string{"a"}, oc, "opencode"), "default model")
	if c == nil || c.status != statusFail || !strings.Contains(c.detail, `"gone"`) {
		t.Errorf("got %+v", c)
	}
}

func TestOpencodeChecksLimits(t *testing.T) {
	fakeBins(t, map[string]string{"opencode": `echo "opencode v2.2.5"`})
	checks := opencodeChecks(docConfig(), []string{"a"}, t.TempDir(), "opencode")
	c := findCheck(checks, "limits")
	if c == nil || c.scope != "a" || c.status != statusWarn {
		t.Errorf("got %+v", c)
	}
}

func TestParseLimitInput(t *testing.T) {
	for in, want := range map[string]int64{"": 0, " 128000 ": 128000} {
		if got, err := parseLimitInput(in); err != nil || got != want {
			t.Errorf("%q → %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"-1", "abc", "0"} {
		if _, err := parseLimitInput(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestWantLimitPrompt(t *testing.T) {
	cx := config.Context{Model: "m"}
	if wantLimitPrompt(false, cx, nil) {
		t.Error("prompted although opencode plugin not installed")
	}
	if !wantLimitPrompt(true, cx, nil) {
		t.Error("no prompt with opencode and no known limits")
	}
	if wantLimitPrompt(true, cx, map[string]gateway.Limits{"m": {Context: 1}}) {
		t.Error("prompted although /model/info knows the limits")
	}
	cx.ContextLimit = 5
	if wantLimitPrompt(true, cx, nil) {
		t.Error("prompted although --context-limit given")
	}
}
