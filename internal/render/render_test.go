package render

import (
	"reflect"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
)

func TestRenderFull(t *testing.T) {
	got := Render(config.Context{
		BaseURL: "https://b", Model: "anthropic/claude-opus-5",
		DisplayName: "Opus B", Discovery: true,
		ModelAliases: map[string]string{"opus": "o", "haiku": "h"},
	}, config.Defaults{KeyTTLMs: 42, StoreDir: "/s"}).Env
	want := map[string]string{
		"ANTHROPIC_BASE_URL": "https://b", "ANTHROPIC_MODEL": "anthropic/claude-opus-5",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "o", "ANTHROPIC_DEFAULT_HAIKU_MODEL": "h",
		"ANTHROPIC_CUSTOM_MODEL_OPTION": "anthropic/claude-opus-5", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME": "Opus B",
		"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1", "CLAUDE_CODE_API_KEY_HELPER_TTL_MS": "42",
		"PASSWORD_STORE_DIR": "/s",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	for k := range got {
		found := false
		for _, m := range AllManagedEnvKeys {
			if m == k {
				found = true
			}
		}
		if !found {
			t.Errorf("%s not in AllManagedEnvKeys", k)
		}
	}
}

func TestRenderMinimal(t *testing.T) {
	got := Render(config.Context{BaseURL: "http://l", Model: "m"}, config.Defaults{KeyTTLMs: 1}).Env
	if len(got) != 3 {
		t.Errorf("minimal render should have 3 keys, got %v", got)
	}
}
