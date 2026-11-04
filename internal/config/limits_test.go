package config

import (
	"strings"
	"testing"
)

func validateTOML(t *testing.T, src string) error {
	t.Helper()
	c, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Validate()
	return err
}

const minimalCtx = "[contexts.a]\nbase_url = \"http://a\"\nmodel = \"m\"\nkey_env = \"K\"\n"

func TestValidateLimits(t *testing.T) {
	for _, field := range []string{"context_limit", "output_limit"} {
		err := validateTOML(t, minimalCtx+field+" = -1\n")
		if err == nil || !strings.Contains(err.Error(), field+" must be >= 0") {
			t.Errorf("%s: err = %v", field, err)
		}
	}
}

func TestValidateClient(t *testing.T) {
	err := validateTOML(t, "[defaults]\nclient = \"vim\"\n"+minimalCtx)
	if err == nil || !strings.Contains(err.Error(), `defaults.client must be "claude" or "opencode"`) {
		t.Errorf("vim: err = %v", err)
	}
	for _, ok := range []string{"", ClientClaude, ClientOpencode} {
		if err := validateTOML(t, "[defaults]\nclient = \""+ok+"\"\n"+minimalCtx); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestParseLimits(t *testing.T) {
	c, err := Load(fixture(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	l := c.Contexts["local-llama"]
	if l.ContextLimit != 128000 || l.OutputLimit != 8192 {
		t.Errorf("limits = %d/%d", l.ContextLimit, l.OutputLimit)
	}
}

func TestRenderContextLimits(t *testing.T) {
	base := Context{BaseURL: "http://a", Model: "m", KeyEnv: "K"}
	if s := RenderContext("a", base); strings.Contains(s, "context_limit") || strings.Contains(s, "output_limit") {
		t.Errorf("zero limits rendered:\n%s", s)
	}
	base.ContextLimit, base.OutputLimit = 128000, 8192
	s := RenderContext("a", base)
	if !strings.Contains(s, "context_limit = 128000\n") || !strings.Contains(s, "output_limit = 8192\n") {
		t.Errorf("limits missing:\n%s", s)
	}
}
