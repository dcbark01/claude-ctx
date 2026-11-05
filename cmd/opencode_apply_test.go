package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/state"
)

var (
	ctxWithHaiku = config.Context{Model: "m", ModelAliases: map[string]string{"haiku": "h"}}
	ctxNoHaiku   = config.Context{Model: "q"}
)

func ocFixture(t *testing.T, content string) (statePath, ocDir, cfg string) {
	t.Helper()
	d := t.TempDir()
	ocDir = filepath.Join(d, "opencode")
	os.MkdirAll(ocDir, 0o755)
	cfg = filepath.Join(ocDir, "opencode.json")
	if content != "" {
		os.WriteFile(cfg, []byte(content), 0o644)
	}
	return filepath.Join(d, "state.json"), ocDir, cfg
}

func readOC(t *testing.T, path string) map[string]string {
	t.Helper()
	got, err := opencode.ReadKeys(path, []string{opencode.KeyModel, opencode.KeySmallModel, "theme"})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTargetClients(t *testing.T) {
	cases := []struct {
		flag      string
		installed bool
		want      []string
		errSub    string
	}{
		{"", false, []string{"claude"}, ""},
		{"", true, []string{"claude", "opencode"}, ""},
		{"opencode", false, nil, "claude-ctx opencode install"},
		{"opencode", true, []string{"opencode"}, ""},
		{"claude", true, []string{"claude"}, ""},
		{"vim", true, nil, "unknown client"},
	}
	for _, tc := range cases {
		got, err := targetClients(tc.flag, tc.installed)
		if tc.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("(%q,%v) err = %v", tc.flag, tc.installed, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("(%q,%v) = %v, %v", tc.flag, tc.installed, got, err)
		}
	}
}

func TestApplyOpencode(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{"model": "mine", "theme": "dark"}`)
	if err := applyOpencode(sp, dir, "a", ctxWithHaiku, false); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"model": "ctx-a/m", "small_model": "ctx-a/h", "theme": "dark"}
	if got := readOC(t, cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("config = %v", got)
	}
	st, _ := state.Load(sp)
	if st.Opencode == nil || st.Opencode.Context != "a" || st.Opencode.Path != cfg ||
		!reflect.DeepEqual(st.Opencode.Values, map[string]string{"model": "ctx-a/m", "small_model": "ctx-a/h"}) {
		t.Errorf("state = %+v", st.Opencode)
	}
}

func TestApplyOpencodeDrift(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{}`)
	if err := applyOpencode(sp, dir, "a", ctxWithHaiku, false); err != nil {
		t.Fatal(err)
	}
	opencode.SetKeys(cfg, map[string]string{"model": "hand-edited"}, nil)
	err := applyOpencode(sp, dir, "b", ctxNoHaiku, false)
	if err == nil || !strings.Contains(err.Error(), "modified outside claude-ctx") {
		t.Fatalf("err = %v", err)
	}
	if err := applyOpencode(sp, dir, "b", ctxNoHaiku, true); err != nil {
		t.Fatalf("force: %v", err)
	}
}

func TestApplyOpencodeDropsSmallModel(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{}`)
	applyOpencode(sp, dir, "a", ctxWithHaiku, false)
	if err := applyOpencode(sp, dir, "b", ctxNoHaiku, false); err != nil {
		t.Fatal(err)
	}
	if got := readOC(t, cfg); !reflect.DeepEqual(got, map[string]string{"model": "ctx-b/q"}) {
		t.Errorf("config = %v", got)
	}
}

func TestRemoveOpencodeRestoresPrevious(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{"model": "mine", "theme": "dark"}`)
	applyOpencode(sp, dir, "a", ctxWithHaiku, false)
	applyOpencode(sp, dir, "b", ctxWithHaiku, false) // previous values must survive a second use
	prev, had, err := removeOpencode(sp)
	if err != nil || !had || prev != "b" {
		t.Fatalf("prev=%q had=%v err=%v", prev, had, err)
	}
	if got := readOC(t, cfg); !reflect.DeepEqual(got, map[string]string{"model": "mine", "theme": "dark"}) {
		t.Errorf("config = %v", got)
	}
	if st, _ := state.Load(sp); st.Opencode != nil {
		t.Errorf("state not cleared: %+v", st.Opencode)
	}
	if _, had, _ := removeOpencode(sp); had {
		t.Error("second remove reported had=true")
	}
}

func TestRemoveOpencodeNoFileBefore(t *testing.T) {
	sp, dir, cfg := ocFixture(t, "")
	applyOpencode(sp, dir, "a", ctxNoHaiku, false)
	if _, _, err := removeOpencode(sp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf("file removed: %v", err)
	}
	if got := readOC(t, cfg); len(got) != 0 {
		t.Errorf("config = %v", got)
	}
}

func TestOpencodeCurrent(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{}`)
	if n, _ := opencodeCurrent(sp); n != "" {
		t.Errorf("none: %q", n)
	}
	applyOpencode(sp, dir, "a", ctxNoHaiku, false)
	if n, d := opencodeCurrent(sp); n != "a" || d {
		t.Errorf("clean: %q %v", n, d)
	}
	opencode.SetKeys(cfg, map[string]string{"model": "x"}, nil)
	if n, d := opencodeCurrent(sp); n != "a" || !d {
		t.Errorf("drift: %q %v", n, d)
	}
}

func TestRemoveOpencodeKeepsHandEdits(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{"model": "mine"}`)
	applyOpencode(sp, dir, "a", ctxWithHaiku, false)
	opencode.SetKeys(cfg, map[string]string{"model": "hand-edited"}, nil)
	if _, had, err := removeOpencode(sp); err != nil || !had {
		t.Fatalf("had=%v err=%v", had, err)
	}
	got := readOC(t, cfg)
	if got["model"] != "hand-edited" {
		t.Errorf("hand edit overwritten: %v", got)
	}
	if _, ok := got["small_model"]; ok {
		t.Errorf("unedited managed key not reverted: %v", got)
	}
	if st, _ := state.Load(sp); st.Opencode != nil {
		t.Error("state not cleared")
	}
}

func TestRemoveOpencodeMissingFile(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{}`)
	applyOpencode(sp, dir, "a", ctxNoHaiku, false)
	os.Remove(cfg)
	if _, _, err := removeOpencode(sp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("unset recreated a deleted config file")
	}
}

func TestOpencodePreflightInvalidConfig(t *testing.T) {
	sp, dir, _ := ocFixture(t, `{"model": `)
	if err := opencodePreflight(sp, dir, true); err == nil {
		t.Error("unparseable opencode config passed preflight (even --force must not skip parsing)")
	}
	sp, dir, _ = ocFixture(t, `{"model": "x"}`)
	if err := opencodePreflight(sp, dir, false); err != nil {
		t.Errorf("valid config: %v", err)
	}
}

func TestOpencodePreflightDrift(t *testing.T) {
	sp, dir, cfg := ocFixture(t, `{}`)
	applyOpencode(sp, dir, "a", ctxNoHaiku, false)
	opencode.SetKeys(cfg, map[string]string{"model": "hand"}, nil)
	if err := opencodePreflight(sp, dir, false); err == nil || !strings.Contains(err.Error(), "modified outside claude-ctx") {
		t.Errorf("err = %v", err)
	}
	if err := opencodePreflight(sp, dir, true); err != nil {
		t.Errorf("force: %v", err)
	}
}
