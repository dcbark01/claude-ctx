package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "config", name)
}

func TestLoadValid(t *testing.T) {
	c, err := Load(fixture(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	warns, err := c.Validate()
	if err != nil || len(warns) != 0 {
		t.Fatalf("validate: err=%v warns=%v", err, warns)
	}
	if c.Defaults.KeyTTLMs != 1800000 {
		t.Errorf("ttl = %d", c.Defaults.KeyTTLMs)
	}
	home, _ := os.UserHomeDir()
	if c.Defaults.StoreDir != filepath.Join(home, ".password-store-work") {
		t.Errorf("store_dir not expanded: %q", c.Defaults.StoreDir)
	}
	if got := c.Names(); strings.Join(got, ",") != "gpt5-prod,local-llama,opus-b" {
		t.Errorf("names = %v", got)
	}
	ob := c.Contexts["opus-b"]
	if !ob.Discovery || ob.ModelAliases["haiku"] != "anthropic/claude-haiku-4-5" {
		t.Errorf("opus-b = %+v", ob)
	}
}

func TestDefaultTTL(t *testing.T) {
	c, err := Parse([]byte(`[contexts.a]
base_url="http://x"
model="m"
key_env="K"`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Defaults.KeyTTLMs != DefaultKeyTTLMs {
		t.Errorf("ttl = %d", c.Defaults.KeyTTLMs)
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]string{
		"invalid_unknown_key.toml": "unknown keys",
		"invalid_no_cred.toml":     "one of key_pass",
		"invalid_alias.toml":       "unknown family",
	}
	for f, want := range cases {
		c, err := Load(fixture(t, f))
		if err == nil {
			_, err = c.Validate()
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v, want containing %q", f, err, want)
		}
	}
}

func TestWarnMultiCred(t *testing.T) {
	c, err := Load(fixture(t, "warn_multi_cred.toml"))
	if err != nil {
		t.Fatal(err)
	}
	warns, err := c.Validate()
	if err != nil || len(warns) != 1 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
}

func TestGetMissing(t *testing.T) {
	c, _ := Load(fixture(t, "valid.toml"))
	if _, err := c.Get("nope"); err == nil || !strings.Contains(err.Error(), "gpt5-prod") {
		t.Errorf("err = %v", err)
	}
}
