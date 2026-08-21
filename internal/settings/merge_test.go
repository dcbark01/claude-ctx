package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/render"
)

var hashRe = regexp.MustCompile(`"hash": "[0-9a-f]{64}"`)

func fixture(name string) string { return filepath.Join("..", "..", "testdata", "settings", name) }

func opusB() render.Rendered {
	return render.Render(config.Context{
		BaseURL: "https://litellm-b.myorg.internal", Model: "anthropic/claude-opus-5",
		DisplayName: "Opus 5 (deployment B)", Discovery: true,
		ModelAliases: map[string]string{"opus": "anthropic/claude-opus-5", "sonnet": "anthropic/claude-sonnet-5", "haiku": "anthropic/claude-haiku-4-5"},
	}, config.Defaults{KeyTTLMs: 3600000})
}

func gpt5() render.Rendered {
	return render.Render(config.Context{BaseURL: "https://a", Model: "gpt-5"}, config.Defaults{KeyTTLMs: 3600000})
}

func mustMarshal(t *testing.T, doc map[string]any) string {
	t.Helper()
	b, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return hashRe.ReplaceAllString(string(b), `"hash": "HASH"`)
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyGolden(t *testing.T) {
	doc, err := Load(fixture("user.in.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(doc, "opus-b", opusB(), ""); err != nil {
		t.Fatal(err)
	}
	if got, want := mustMarshal(t, doc), golden(t, "user.golden.json"); got != want {
		t.Errorf("mismatch\n--- got\n%s\n--- want\n%s", got, want)
	}
	if Current(doc) != "opus-b" || Drift(doc) {
		t.Errorf("current=%q drift=%v", Current(doc), Drift(doc))
	}
}

func TestReapplyScrubsStaleKeys(t *testing.T) {
	doc, _ := Load(fixture("user.in.json"))
	_ = Apply(doc, "opus-b", opusB(), "")
	if err := Apply(doc, "gpt5-prod", gpt5(), ""); err != nil {
		t.Fatal(err)
	}
	env := doc["env"].(map[string]any)
	for _, k := range []string{"ANTHROPIC_CUSTOM_MODEL_OPTION", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME", "ANTHROPIC_DEFAULT_OPUS_MODEL", "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"} {
		if _, ok := env[k]; ok {
			t.Errorf("stale key %s survived", k)
		}
	}
	if env["ANTHROPIC_MODEL"] != "gpt-5" || env["FOO"] != "bar" {
		t.Errorf("env = %v", env)
	}
}

func TestScrubHonorsOldSentinelKeys(t *testing.T) {
	// A newer version stopped emitting LEGACY_KEY; sentinel still lists it.
	doc, _ := Load(fixture("user.in.json"))
	_ = Apply(doc, "opus-b", opusB(), "")
	doc["env"].(map[string]any)["LEGACY_KEY"] = "x"
	s, _ := ReadSentinel(doc)
	s.ManagedKeys = append(s.ManagedKeys, "env.LEGACY_KEY")
	doc[SentinelKey] = *s
	if _, err := Remove(doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["env"].(map[string]any)["LEGACY_KEY"]; ok {
		t.Error("LEGACY_KEY survived unset")
	}
}

func TestUnsetGolden(t *testing.T) {
	doc, _ := Load(fixture("user.in.json"))
	_ = Apply(doc, "opus-b", opusB(), "")
	had, err := Remove(doc)
	if err != nil || !had {
		t.Fatalf("had=%v err=%v", had, err)
	}
	if got, want := mustMarshal(t, doc), golden(t, "user.unset.golden.json"); got != want {
		t.Errorf("mismatch\n--- got\n%s\n--- want\n%s", got, want)
	}
	had, _ = Remove(doc)
	if had {
		t.Error("second Remove should report nothing managed")
	}
}

func TestUnsetDropsEmptyEnv(t *testing.T) {
	doc := map[string]any{}
	_ = Apply(doc, "x", gpt5(), "")
	_, _ = Remove(doc)
	if len(doc) != 0 {
		t.Errorf("doc = %v", doc)
	}
}

func TestDrift(t *testing.T) {
	doc := map[string]any{}
	_ = Apply(doc, "x", gpt5(), "")
	doc["env"].(map[string]any)["ANTHROPIC_MODEL"] = "edited"
	if !Drift(doc) {
		t.Error("expected drift after external edit")
	}
}

func TestEnvNotObject(t *testing.T) {
	doc := map[string]any{"env": "nope"}
	if err := Apply(doc, "x", gpt5(), ""); err == nil || !strings.Contains(err.Error(), "expected an object") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadMissingAndInvalid(t *testing.T) {
	doc, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(doc) != 0 {
		t.Errorf("doc=%v err=%v", doc, err)
	}
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("{"), 0o600)
	if _, err := Load(p); err == nil {
		t.Error("expected error")
	}
}

func TestWriteAtomicPreservesModeAndBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte(`{"a":1}`), 0o644)
	doc, _ := Load(p)
	_ = Apply(doc, "x", gpt5(), "")
	if err := Write(p, doc); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v", st.Mode())
	}
	bak, _ := os.ReadFile(p + BackupSuffix)
	if string(bak) != `{"a":1}` {
		t.Errorf("backup = %q", bak)
	}
	var back map[string]any
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &back); err != nil || back["a"] != float64(1) {
		t.Errorf("roundtrip: %v %v", back, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("leftover temp files: %v", entries)
	}
}

func TestWriteCreatesMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "settings.json")
	if err := Write(p, map[string]any{"x": "y"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode())
	}
}

func TestProjectScopeGuard(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	os.MkdirAll(filepath.Join(repo, "sub", ".claude"), 0o755)
	p := filepath.Join(repo, "sub", ".claude", "settings.json")
	if err := Write(p, map[string]any{}); err == nil || !strings.Contains(err.Error(), "project-scope") {
		t.Errorf("err = %v", err)
	}
	// Outside any repo, a .claude/settings.json is fine.
	ok := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := Write(ok, map[string]any{}); err != nil {
		t.Errorf("non-repo .claude rejected: %v", err)
	}
}
