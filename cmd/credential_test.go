package cmd

import "testing"

func TestCredentialContextName(t *testing.T) {
	cases := []struct{ flag, env, sentinel, want string }{
		{"a", "b", "c", "a"},
		{"", "b", "c", "b"},
		{"", "", "c", "c"},
		{"", "", "", ""},
	}
	for _, tc := range cases {
		calls := 0
		got := credentialContextName(tc.flag, tc.env, func() string { calls++; return tc.sentinel })
		if got != tc.want {
			t.Errorf("(%q,%q,%q) = %q, want %q", tc.flag, tc.env, tc.sentinel, got, tc.want)
		}
		if (tc.flag != "" || tc.env != "") && calls != 0 {
			t.Errorf("(%q,%q): settings read although flag/env set", tc.flag, tc.env)
		}
	}
}
