//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opencode runs opencode with a private server (--standalone), so a test never
// starts or talks to a background service.
func (e *env) opencode(args ...string) result {
	e.t.Helper()
	return e.exec(120*time.Second, ocBin, append([]string{"--standalone"}, args...)...)
}

func newOpencodeEnv(t *testing.T) *env {
	t.Helper()
	need(t, ocBin, "opencode")
	e := newEnv(t)
	e.mustCtx("opencode", "install")
	return e
}

func TestOpencodeRun(t *testing.T) {
	e := newOpencodeEnv(t)
	r := e.ctx("run", "gw", "--client", "opencode", "--", "run", "say ok")
	if r.Err != nil || !strings.Contains(r.Stdout, reply) {
		t.Fatalf("run gw --client opencode: %s", r)
	}
	e.assertServed(modelMain)
	if _, err := os.Stat(filepath.Join(e.ocDir, "opencode.json")); err == nil {
		t.Error("run wrote opencode.json; it must not touch opencode config")
	}
}

func TestOpencodeUseThenPlainOpencode(t *testing.T) {
	e := newOpencodeEnv(t)
	r := e.mustCtx("use", "gw")
	if strings.Contains(r.Stderr, "warning") {
		t.Errorf("use printed a warning against a gateway without /model/info:\n%s", r.Stderr)
	}

	// Discovery went through agentgateway's /v1/models.
	var doc struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	out := e.mustCtx("opencode", "providers", "--json")
	if err := json.Unmarshal([]byte(out.Stdout), &doc); err != nil {
		t.Fatalf("providers --json: %v\n%s", err, out.Stdout)
	}
	got := map[string]bool{}
	for _, p := range doc.Providers {
		if p.ID == "ctx-gw" {
			for _, m := range p.Models {
				got[m.ID] = true
			}
		}
	}
	if !got[modelMain] || !got[modelSmall] {
		t.Errorf("ctx-gw models = %v, want %s and %s", got, modelMain, modelSmall)
	}

	if cur := e.mustCtx("current"); !strings.Contains(cur.Stdout, "opencode: gw") {
		t.Errorf("current: %s", cur)
	}

	// Plain opencode, no claude-ctx in front: the plugin supplies the key.
	r = e.opencode("run", "say ok")
	if r.Err != nil || !strings.Contains(r.Stdout, reply) {
		t.Fatalf("opencode run after use: %s", r)
	}
	e.assertServed(modelMain)

	e.mustCtx("unset")
	b, _ := os.ReadFile(filepath.Join(e.ocDir, "opencode.json"))
	if strings.Contains(string(b), "ctx-gw") {
		t.Errorf("unset left ctx-gw in opencode.json:\n%s", b)
	}
}

func TestOpencodeWrongKeyFailsFast(t *testing.T) {
	e := newOpencodeEnv(t)
	start := time.Now()
	r := e.ctx("run", "badkey", "--client", "opencode", "--", "run", "say ok")
	if r.Err == nil || strings.Contains(r.Stdout, reply) {
		t.Fatalf("expected failure with a key the gateway rejects: %s", r)
	}
	if !strings.Contains(r.Stdout+r.Stderr, "401") && !strings.Contains(strings.ToLower(r.Stdout+r.Stderr), "auth") {
		t.Errorf("error does not mention the 401: %s", r)
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Errorf("took %s to fail", d)
	}
	if n := len(e.up.requests()); n != 0 {
		t.Errorf("gateway let %d request(s) through with a bad key", n)
	}
}

func TestOpencodeCredentialFailureMessage(t *testing.T) {
	e := newOpencodeEnv(t)
	// Bypass claude-ctx's foreground priming so the plugin's own fetch fails.
	r := e.opencode("run", "-m", "ctx-broken/"+modelMain, "say ok")
	if r.Err == nil {
		t.Fatalf("expected failure: %s", r)
	}
	if !strings.Contains(r.Stdout+r.Stderr, "credential for broken unavailable") {
		t.Errorf("missing the plugin's credential error: %s", r)
	}
	if n := len(e.up.requests()); n != 0 {
		t.Errorf("%d request(s) reached upstream without a credential", n)
	}
}
