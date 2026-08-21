package cred

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcbark01/claude-ctx/internal/config"
)

// fakePass installs a `pass` script on PATH that echoes $FAKE_PASS_OUT and
// exits $FAKE_PASS_EXIT, recording PASSWORD_STORE_DIR to $FAKE_PASS_LOG.
func fakePass(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"[ -n \"$FAKE_PASS_LOG\" ] && echo \"store=$PASSWORD_STORE_DIR args=$*\" > \"$FAKE_PASS_LOG\"\n" +
		"[ -n \"$FAKE_PASS_ERR\" ] && echo \"$FAKE_PASS_ERR\" >&2\n" +
		"printf '%s' \"$FAKE_PASS_OUT\"\n" +
		"exit ${FAKE_PASS_EXIT:-0}\n"
	if err := os.WriteFile(filepath.Join(dir, "pass"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PASS_LOG", filepath.Join(dir, "log"))
}

func TestPassFirstLine(t *testing.T) {
	fakePass(t)
	t.Setenv("FAKE_PASS_OUT", "sk-secret\nurl: https://example\nuser: x\n")
	p := &Pass{Entry: "work/a", StoreDir: "/store"}
	got, err := p.Resolve(context.Background())
	if err != nil || got != "sk-secret" {
		t.Fatalf("got %q err %v", got, err)
	}
	log, _ := os.ReadFile(os.Getenv("FAKE_PASS_LOG"))
	if !strings.Contains(string(log), "store=/store args=show work/a") {
		t.Errorf("log = %s", log)
	}
}

func TestPassEmpty(t *testing.T) {
	fakePass(t)
	t.Setenv("FAKE_PASS_OUT", "\n\nurl: x")
	_, err := (&Pass{Entry: "e"}).Resolve(context.Background())
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v", err)
	}
}

func TestPassFailure(t *testing.T) {
	fakePass(t)
	t.Setenv("FAKE_PASS_EXIT", "1")
	t.Setenv("FAKE_PASS_ERR", "gpg: decryption failed: No secret key")
	_, err := (&Pass{Entry: "e"}).Resolve(context.Background())
	var se *SourceError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "No secret key") || !strings.Contains(err.Error(), "re-prime") {
		t.Fatalf("err = %v", err)
	}
}

func TestPassTimeout(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pass"), []byte("#!/bin/sh\nsleep 5\n"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := (&Pass{Entry: "e"}).Resolve(ctx)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestCmd(t *testing.T) {
	got, err := (&Cmd{Command: "printf 'k e y\\n\\n'"}).Resolve(context.Background())
	if err != nil || got != "k e y" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := (&Cmd{Command: "echo boom >&2; exit 3"}).Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := (&Cmd{Command: "true"}).Resolve(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("CC_TEST_KEY", " abc ")
	got, err := (&Env{Var: "CC_TEST_KEY"}).Resolve(context.Background())
	if err != nil || got != "abc" {
		t.Fatalf("got %q err %v", got, err)
	}
	t.Setenv("CC_TEST_KEY", "")
	if _, err := (&Env{Var: "CC_TEST_KEY"}).Resolve(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v", err)
	}
	os.Unsetenv("CC_TEST_KEY")
	if _, err := (&Env{Var: "CC_TEST_KEY"}).Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("err = %v", err)
	}
}

func TestForContextPrecedence(t *testing.T) {
	r, _ := ForContext(config.Context{KeyPass: "p", KeyCmd: "c", KeyEnv: "E"}, config.Defaults{})
	if r.Describe() != "pass:p" {
		t.Error(r.Describe())
	}
	r, _ = ForContext(config.Context{KeyCmd: "c", KeyEnv: "E"}, config.Defaults{})
	if r.Describe() != "cmd:c" {
		t.Error(r.Describe())
	}
	r, _ = ForContext(config.Context{KeyEnv: "E"}, config.Defaults{})
	if r.Describe() != "env:E" {
		t.Error(r.Describe())
	}
	if _, err := ForContext(config.Context{}, config.Defaults{}); err == nil {
		t.Error("expected error")
	}
}
