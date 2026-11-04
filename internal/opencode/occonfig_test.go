package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(golden(t, name))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetKeysPreservesComments(t *testing.T) {
	p := copyFixture(t, "commented.jsonc")
	if err := SetKeys(p, map[string]string{KeyModel: "ctx-a/m", KeySmallModel: "ctx-a/h"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want, _ := os.ReadFile(golden(t, "commented.golden.jsonc"))
	if string(got) != string(want) {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o644 {
		t.Errorf("mode changed to %v", st.Mode().Perm())
	}
}

func TestSetKeysRemove(t *testing.T) {
	p := copyFixture(t, "commented.golden.jsonc")
	if err := SetKeys(p, nil, []string{KeySmallModel}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if strings.Contains(s, "small_model") || !strings.Contains(s, "// my default model") || !strings.Contains(s, "/* plugins I use */") {
		t.Errorf("got:\n%s", s)
	}
	// removing an absent key is a no-op, not an error
	if err := SetKeys(p, nil, []string{KeySmallModel}); err != nil {
		t.Errorf("remove absent: %v", err)
	}
}

func TestSetKeysCreatesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "opencode.json")
	if err := SetKeys(p, map[string]string{KeyModel: "ctx-a/m"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := ReadKeys(p, []string{"$schema", KeyModel})
	if err != nil {
		t.Fatal(err)
	}
	if got["$schema"] != "https://opencode.ai/config.json" || got[KeyModel] != "ctx-a/m" {
		t.Errorf("got %v", got)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
}

func TestSetKeysBackup(t *testing.T) {
	p := copyFixture(t, "commented.jsonc")
	orig, _ := os.ReadFile(p)
	if err := SetKeys(p, map[string]string{KeyModel: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(p + BackupSuffix)
	if err != nil || string(bak) != string(orig) {
		t.Errorf("backup = %q, err %v", bak, err)
	}
}

func TestReadKeys(t *testing.T) {
	got, err := ReadKeys(copyFixture(t, "commented.jsonc"), []string{KeyModel, KeySmallModel})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyModel] != "anthropic/claude-sonnet-5" {
		t.Errorf("model = %q", got[KeyModel])
	}
	if _, ok := got[KeySmallModel]; ok {
		t.Error("absent key reported present")
	}
	none, err := ReadKeys(filepath.Join(t.TempDir(), "missing.json"), []string{KeyModel})
	if err != nil || len(none) != 0 {
		t.Errorf("missing file: %v %v", none, err)
	}
}

func TestConfigFilePrefersJSONC(t *testing.T) {
	d := t.TempDir()
	if got := ConfigFile(d); got != filepath.Join(d, "opencode.json") {
		t.Errorf("neither: %s", got)
	}
	os.WriteFile(filepath.Join(d, "opencode.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(d, "opencode.jsonc"), []byte("{}"), 0o600)
	if got := ConfigFile(d); got != filepath.Join(d, "opencode.jsonc") {
		t.Errorf("both: %s", got)
	}
}

func TestSetKeysKeepsTrailingCommentOnLastMember(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.jsonc")
	os.WriteFile(p, []byte("{\n  \"model\": \"x\",\n  \"theme\": \"dark\" // my theme\n}\n"), 0o644)
	if err := SetKeys(p, map[string]string{KeySmallModel: "ctx-a/h"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "// my theme") {
		t.Fatalf("comment lost after add:\n%s", got)
	}
	if err := SetKeys(p, nil, []string{KeySmallModel}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(p)
	if !strings.Contains(string(got), "// my theme") {
		t.Errorf("comment lost after remove:\n%s", got)
	}
}
