package cmd

import (
	"reflect"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/config"
)

func TestResolveClient(t *testing.T) {
	cases := []struct {
		flag, def, want string
		err             bool
	}{
		{"opencode", "claude", "opencode", false},
		{"", "opencode", "opencode", false},
		{"", "", "claude", false},
		{"vim", "", "", true},
	}
	for _, tc := range cases {
		got, err := resolveClient(tc.flag, config.Defaults{Client: tc.def})
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("(%q,%q) = %q, %v", tc.flag, tc.def, got, err)
		}
	}
}

func TestOpencodeLaunch(t *testing.T) {
	cx := config.Context{Model: "anthropic/claude-opus-5", ModelAliases: map[string]string{"haiku": "anthropic/claude-haiku-4-5"}}
	argv, env := opencodeLaunch("/bin/opencode", "opus-b", cx, []string{"run", "hi"})
	if !reflect.DeepEqual(argv, []string{"/bin/opencode", "--standalone", "run", "hi"}) {
		t.Errorf("argv = %v", argv)
	}
	want := []string{`OPENCODE_CONFIG_CONTENT={"model":"ctx-opus-b/anthropic/claude-opus-5","small_model":"ctx-opus-b/anthropic/claude-haiku-4-5"}`}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("env = %v", env)
	}
	_, env = opencodeLaunch("/bin/opencode", "local", config.Context{Model: "q"}, nil)
	if !reflect.DeepEqual(env, []string{`OPENCODE_CONFIG_CONTENT={"model":"ctx-local/q"}`}) {
		t.Errorf("no-haiku env = %v", env)
	}
}
