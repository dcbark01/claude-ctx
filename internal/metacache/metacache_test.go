package metacache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/gateway"
)

func TestRefreshPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	e, err := Refresh(context.Background(), config.Context{BaseURL: srv.URL, Model: "a", Discovery: true}, "k")
	if err == nil {
		t.Error("want error for missing /model/info")
	}
	if !reflect.DeepEqual(e.Models, []string{"a", "b"}) || len(e.Limits) != 0 {
		t.Errorf("entry = %+v", e)
	}
	if e.Updated.IsZero() {
		t.Error("Updated not set")
	}
}

func TestRefreshNoDiscovery(t *testing.T) {
	var modelsHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelsHits.Add(1)
			w.Write([]byte(`{"data":[{"id":"a"}]}`))
		case "/v1/model/info":
			w.Write([]byte(`{"data":[{"model_name":"a","model_info":{"max_input_tokens":1000,"max_output_tokens":100}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e, err := Refresh(context.Background(), config.Context{BaseURL: srv.URL, Model: "a"}, "k")
	if err != nil {
		t.Fatal(err)
	}
	if modelsHits.Load() != 0 || e.Models != nil {
		t.Errorf("discovery off but /v1/models hit %d times, models=%v", modelsHits.Load(), e.Models)
	}
	if e.Limits["a"] != (gateway.Limits{Context: 1000, Output: 100}) {
		t.Errorf("limits = %v", e.Limits)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "cache.json")
	empty, err := Load(p)
	if err != nil || len(empty.Contexts) != 0 {
		t.Fatalf("missing file: %+v %v", empty, err)
	}
	c := &Cache{Contexts: map[string]Entry{"x": {
		Models:  []string{"a"},
		Limits:  map[string]gateway.Limits{"a": {Context: 1, Output: 2}},
		Updated: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
	}}}
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("got %+v err %v", got, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
}

func TestPathFor(t *testing.T) {
	if got := PathFor("/a/b/config.toml"); got != "/a/b/cache.json" {
		t.Errorf("got %s", got)
	}
}

func TestMergeKeepsOldOnFailure(t *testing.T) {
	t1, t2 := time.Unix(1, 0), time.Unix(2, 0)
	old := Entry{Models: []string{"a"}, Limits: map[string]gateway.Limits{"a": {Context: 1}}, Updated: t1}
	got := Merge(old, Entry{Updated: t2})
	if !reflect.DeepEqual(got.Models, old.Models) || !reflect.DeepEqual(got.Limits, old.Limits) || got.Updated != t2 {
		t.Errorf("failed refresh clobbered: %+v", got)
	}
	got = Merge(old, Entry{Models: []string{"b"}, Updated: t2})
	if !reflect.DeepEqual(got.Models, []string{"b"}) || !reflect.DeepEqual(got.Limits, old.Limits) {
		t.Errorf("partial merge: %+v", got)
	}
}
