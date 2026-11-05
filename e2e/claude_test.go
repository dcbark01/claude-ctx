//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestClaudeCodeUse(t *testing.T) {
	need(t, ccBin, "claude")
	e := newEnv(t)
	e.mustCtx("use", "gw", "--client", "claude")
	r := e.exec(120*time.Second, ccBin, "-p", "say ok")
	if r.Err != nil || !strings.Contains(r.Stdout, reply) {
		t.Fatalf("claude -p after use: %s", r)
	}
	e.assertServed(modelMain)
}

func TestClaudeCodeRun(t *testing.T) {
	need(t, ccBin, "claude")
	e := newEnv(t)
	r := e.ctx("run", "gw", "--client", "claude", "--", "-p", "say ok")
	if r.Err != nil || !strings.Contains(r.Stdout, reply) {
		t.Fatalf("run gw --client claude: %s", r)
	}
	e.assertServed(modelMain)
}
