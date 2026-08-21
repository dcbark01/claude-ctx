package passstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"work/litellm/a/gpt5.gpg", "work/litellm/b/opus.gpg", "work/other.gpg", ".git/x.gpg", "work/litellm/.gpg-id", "personal/k.gpg"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(dir, f), nil, 0o600)
	}
	got, err := List(dir, "work/litellm")
	if err != nil || strings.Join(got, ",") != "work/litellm/a/gpt5,work/litellm/b/opus" {
		t.Fatalf("got %v err %v", got, err)
	}
	all, _ := List(dir, "")
	if len(all) != 4 {
		t.Errorf("all = %v", all)
	}
	none, err := List(dir, "nope")
	if err != nil || len(none) != 0 {
		t.Errorf("none = %v err %v", none, err)
	}
}

func TestDir(t *testing.T) {
	t.Setenv("PASSWORD_STORE_DIR", "/x")
	if Dir("") != "/x" || Dir("/y") != "/y" {
		t.Error("precedence")
	}
}
