package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDrift(t *testing.T) {
	o := &Opencode{Context: "a", Path: "/x", Values: map[string]string{"model": "a"}}
	if o.Drift(map[string]string{"model": "a"}) {
		t.Error("same values reported as drift")
	}
	if !o.Drift(map[string]string{"model": "b"}) {
		t.Error("changed value not drift")
	}
	if !o.Drift(map[string]string{}) {
		t.Error("removed key not drift")
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	empty, err := Load(p)
	if err != nil || empty.Opencode != nil {
		t.Fatalf("missing: %+v %v", empty, err)
	}
	s := &State{Opencode: &Opencode{Context: "a", Path: "/x/opencode.json", Values: map[string]string{"model": "ctx-a/m"}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("got %+v err %v", got, err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if PathFor("/a/config.toml") != "/a/state.json" {
		t.Error("PathFor")
	}
}
