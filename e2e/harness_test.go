//go:build e2e

// Package e2e drives the real claude-ctx binary, a real agentgateway, and the
// real opencode / Claude Code binaries against a mock Anthropic upstream:
//
//	client (opencode | claude) ──► agentgateway ──► mock upstream
//	        key: clientKey          checks clientKey     checks upstreamKey
//
// Run with `make e2e`. Tests skip when a binary they need is missing.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	clientKey   = "e2e-client-key-7f3a91"   // what claude-ctx hands the client
	upstreamKey = "e2e-upstream-key-c28d04" // what agentgateway sends upstream
	badKey      = "e2e-wrong-key-55e1b2"
	reply       = "E2E-OK"
	modelMain   = "e2e/opus"
	modelSmall  = "e2e/haiku"
)

var (
	ctxBin string // claude-ctx built from this checkout
	agwBin string
	ocBin  string
	ccBin  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "claude-ctx-e2e-bin")
	if err != nil {
		panic(err)
	}
	ctxBin = filepath.Join(dir, "claude-ctx")
	build := exec.Command("go", "build", "-o", ctxBin, "..")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building claude-ctx:", err)
		os.Exit(1)
	}
	agwBin = lookup("AGENTGATEWAY_BIN", "agentgateway", "../.bin/agentgateway")
	ocBin = lookup("OPENCODE_BIN", "opencode", "")
	ccBin = lookup("CLAUDE_BIN", "claude", "")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// lookup finds a binary from $env, then PATH, then a repo-local fallback.
func lookup(env, name, fallback string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if fallback != "" {
		if abs, err := filepath.Abs(fallback); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}

func need(t *testing.T, bin, name string) {
	t.Helper()
	if bin == "" {
		t.Skipf("%s not found", name)
	}
}

// request is one call the mock upstream received.
type request struct {
	Method, Path, Model string
	Stream              bool
	Header              http.Header
}

// upstream is a minimal Anthropic Messages API that only accepts upstreamKey.
type upstream struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []request
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) requests() []request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]request(nil), u.reqs...)
}

func (u *upstream) serve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, request{r.Method, r.URL.Path, body.Model, body.Stream, r.Header.Clone()})
	u.mu.Unlock()

	if r.Header.Get("x-api-key") != upstreamKey {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"upstream: bad key"}}`)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/count_tokens") {
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"input_tokens":5}`)
		return
	}
	msg := map[string]any{"id": "msg_e2e", "type": "message", "role": "assistant", "model": body.Model,
		"content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 5, "output_tokens": 0}}
	if !body.Stream {
		msg["content"] = []any{map[string]string{"type": "text", "text": reply}}
		msg["stop_reason"] = "end_turn"
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	events := []struct {
		name string
		data any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": msg}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": reply}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]int{"output_tokens": 2}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, e := range events {
		b, _ := json.Marshal(e.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startGateway runs agentgateway in front of up, accepting only clientKey
// (default location: Authorization: Bearer). It returns the gateway's base URL.
func startGateway(t *testing.T, up *upstream) string {
	t.Helper()
	dir := t.TempDir() // outside the client's HOME: holds both keys
	port := freePort(t)
	var models strings.Builder
	for _, m := range []string{modelMain, modelSmall} {
		fmt.Fprintf(&models, `
  - name: %q
    provider: anthropic
    params:
      apiKey: %q
      baseUrl: %q`, m, upstreamKey, up.srv.URL+"/v1")
	}
	cfg := fmt.Sprintf(`config:
  adminAddr: "127.0.0.1:%d"
  statsAddr: "127.0.0.1:%d"
  readinessAddr: "127.0.0.1:%d"
llm:
  port: %d
  discovery: disabled
  policies:
    apiKey:
      mode: strict
      keys:
      - key: %q
  models:%s
`, freePort(t), freePort(t), freePort(t), port, clientKey, models.String())
	path := filepath.Join(dir, "agentgateway.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	logf, err := os.Create(filepath.Join(dir, "agentgateway.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(agwBin, "-f", path)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		logf.Close()
		if t.Failed() {
			b, _ := os.ReadFile(logf.Name())
			t.Logf("agentgateway log:\n%s", tail(string(b), 4000))
		}
	})
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", url+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+clientKey)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return url
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("agentgateway did not become ready on %s", url)
	return ""
}

// env is one isolated machine: its own HOME, XDG dirs, PATH and config.
type env struct {
	t     *testing.T
	root  string
	gw    string
	up    *upstream
	vars  []string
	ocDir string
}

// contexts appended to every env's config.toml. %[1]s is the gateway URL.
const configTOML = `[defaults]
key_ttl_ms = 60000

[contexts.gw]
base_url     = %[1]q
model        = %[2]q
key_env      = "E2E_KEY"
display_name = "E2E Opus"
discovery    = true
  [contexts.gw.model_aliases]
  haiku = %[3]q

[contexts.badkey]
base_url = %[1]q
model    = %[2]q
key_env  = "E2E_BAD_KEY"

[contexts.broken]
base_url = %[1]q
model    = %[2]q
key_cmd  = "echo e2e-credential-source-down >&2; exit 3"
`

func newEnv(t *testing.T) *env {
	t.Helper()
	need(t, agwBin, "agentgateway (run `make e2e-deps`)")
	up := newUpstream(t)
	gw := startGateway(t, up)
	root := t.TempDir()
	e := &env{t: t, root: root, gw: gw, up: up}
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")
	for _, d := range []string{home, binDir, filepath.Join(home, ".claude", "context")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Only this checkout's claude-ctx is on PATH, never one from ~/go/bin.
	if err := os.Symlink(ctxBin, filepath.Join(binDir, "claude-ctx")); err != nil {
		t.Fatal(err)
	}
	path := binDir
	for _, b := range []string{ocBin, ccBin} {
		if b != "" {
			path += ":" + filepath.Dir(b)
		}
	}
	path += ":/usr/bin:/bin"
	e.ocDir = filepath.Join(root, "xdg", "config", "opencode")
	e.vars = []string{
		"HOME=" + home,
		"PATH=" + path,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "xdg", "config"),
		"XDG_DATA_HOME=" + filepath.Join(root, "xdg", "data"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "xdg", "cache"),
		"XDG_STATE_HOME=" + filepath.Join(root, "xdg", "state"),
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"E2E_KEY=" + clientKey,
		"E2E_BAD_KEY=" + badKey,
		"TERM=dumb",
		"NO_COLOR=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_AUTOUPDATER=1",
	}
	os.MkdirAll(filepath.Join(root, "tmp"), 0o700)
	cfg := fmt.Sprintf(configTOML, gw, modelMain, modelSmall)
	if err := os.WriteFile(filepath.Join(home, ".claude", "context", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.assertNoSecrets() })
	return e
}

type result struct {
	Stdout, Stderr string
	Err            error
}

func (r result) String() string {
	return fmt.Sprintf("err=%v\n--- stdout\n%s\n--- stderr\n%s", r.Err, tail(r.Stdout, 3000), tail(r.Stderr, 3000))
}

// exec runs bin with the env's variables, failing the test after timeout.
func (e *env) exec(timeout time.Duration, bin string, args ...string) result {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = e.vars
	cmd.Dir = e.root
	cmd.Stdin = strings.NewReader("")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		e.t.Fatalf("%s %s: timed out after %s\n%s", filepath.Base(bin), strings.Join(args, " "), timeout,
			result{out.String(), errb.String(), err})
	}
	return result{out.String(), errb.String(), err}
}

func (e *env) ctx(args ...string) result {
	e.t.Helper()
	return e.exec(90*time.Second, ctxBin, args...)
}

func (e *env) mustCtx(args ...string) result {
	e.t.Helper()
	r := e.ctx(args...)
	if r.Err != nil {
		e.t.Fatalf("claude-ctx %s: %s", strings.Join(args, " "), r)
	}
	return r
}

// assertServed checks the upstream answered model through agentgateway with
// the upstream key, and that the client's key never got past the gateway.
func (e *env) assertServed(model string) {
	e.t.Helper()
	found := false
	for _, r := range e.up.requests() {
		e.t.Logf("upstream got %s %s model=%s stream=%v", r.Method, r.Path, r.Model, r.Stream)
		if r.Model == model && r.Header.Get("x-api-key") == upstreamKey {
			found = true
		}
		for name, vals := range r.Header {
			for _, v := range vals {
				if strings.Contains(v, clientKey) {
					e.t.Errorf("client key forwarded upstream in %s", name)
				}
			}
		}
	}
	if !found {
		e.t.Errorf("upstream never served %s; requests: %+v", model, e.up.requests())
	}
}

// assertNoSecrets enforces claude-ctx's core invariant: neither key is ever
// written anywhere under the client machine (configs, opencode's db, logs,
// caches, Claude Code state).
func (e *env) assertNoSecrets() {
	e.t.Helper()
	keys := [][]byte{[]byte(clientKey), []byte(upstreamKey), []byte(badKey)}
	var scanned []string
	filepath.WalkDir(e.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(e.root, p)
		scanned = append(scanned, rel)
		for _, k := range keys {
			if bytes.Contains(b, k) {
				e.t.Errorf("secret %q written to disk: %s", k[:12], rel)
			}
		}
		return nil
	})
	e.t.Logf("no-secrets check scanned %d files: %s", len(scanned), strings.Join(scanned, " "))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
