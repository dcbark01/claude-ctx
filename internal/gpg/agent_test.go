package gpg

import (
	"testing"
	"time"
)

func TestParseOptions(t *testing.T) {
	out := "default-cache-ttl:24:0:expire cached PINs after N seconds:3:3:N:600::3600\n" +
		"max-cache-ttl:24:2:set maximum PIN cache lifetime to N seconds:3:3:N:7200::\n" +
		"pinentry-program:0:0:path:35:35::\n"
	got, err := ParseOptions(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Default != time.Hour || got.Max != 2*time.Hour {
		t.Errorf("got %+v", got)
	}
	if _, err := ParseOptions("nothing"); err == nil {
		t.Error("expected error")
	}
}
