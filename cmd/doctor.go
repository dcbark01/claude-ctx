package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/gpg"
	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

type status int

const (
	statusOK status = iota
	statusWarn
	statusFail
	statusSkip
)

type check struct {
	scope  string // "global" or context name
	name   string
	status status
	detail string
}

var (
	doctorTimeout   time.Duration
	doctorNoNetwork bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor [context]",
	Short: "Validate contexts: credentials, gateway reachability, model presence, gpg TTL, settings drift, opencode",
	Long: `Runs a health check per context (all contexts with no argument) plus global
checks on the environment and user settings. Credentials are resolved in the
foreground, so pinentry may appear. Exits 1 if any check fails.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeContexts,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadConfig()
		if err != nil {
			return err
		}
		doc, err := loadSettings()
		if err != nil {
			return err
		}
		names := c.Names()
		if len(args) == 1 {
			if _, err := c.Get(args[0]); err != nil {
				return err
			}
			names = args[:1]
		}
		var checks []check
		checks = append(checks, globalChecks(c, doc)...)
		ocInstalled := opencodeInstalled()
		for _, n := range names {
			checks = append(checks, contextChecks(c, n, ocInstalled)...)
		}
		checks = append(checks, opencodeChecks(c, names, opencodeDir(), opencodeBinary())...)
		printChecks(checks)
		for _, ch := range checks {
			if ch.status == statusFail {
				return errors.New("doctor found problems")
			}
		}
		return nil
	},
}

func globalChecks(c *config.Config, doc map[string]any) []check {
	var out []check
	g := func(name string, st status, detail string) {
		out = append(out, check{"global", name, st, detail})
	}

	// Helper binary resolvable from a child shell.
	if p, err := exec.LookPath("claude-ctx"); err == nil {
		g("claude-ctx on PATH", statusOK, p)
	} else {
		g("claude-ctx on PATH", statusWarn, "not found; `use` will embed an absolute path (run `go install`)")
	}

	// Shadowing env vars.
	for _, v := range render.ShadowingEnvVars {
		if os.Getenv(v) != "" {
			g("env "+v, statusWarn, "set in this shell; it overrides apiKeyHelper for sessions started here")
		}
	}

	// Settings state.
	cur := settings.Current(doc)
	switch {
	case cur == "":
		g("active context", statusSkip, "none (nothing managed in "+settingsPath()+")")
	case settings.Drift(doc):
		g("settings drift", statusWarn, fmt.Sprintf("managed keys edited outside claude-ctx since `use %s` (re-run with --force)", cur))
	default:
		g("settings drift", statusOK, "managed keys match last `use "+cur+"`")
	}
	if cur != "" {
		if _, err := c.Get(cur); err != nil {
			g("active context", statusFail, fmt.Sprintf("settings reference %q but it is not in config", cur))
		}
	}
	if h, ok := doc[settings.HelperKey].(string); ok && cur != "" && !strings.Contains(h, "credential") {
		g("apiKeyHelper", statusWarn, "set to something other than claude-ctx: "+h)
	}

	// Managed (enterprise) settings overriding our keys.
	for _, p := range managedSettingsPaths() {
		mdoc, err := settings.Load(p)
		if err != nil || len(mdoc) == 0 {
			continue
		}
		var hits []string
		if _, ok := mdoc[settings.HelperKey]; ok {
			hits = append(hits, settings.HelperKey)
		}
		if env, ok := mdoc["env"].(map[string]any); ok {
			for _, k := range render.AllManagedEnvKeys {
				if _, ok := env[k]; ok {
					hits = append(hits, "env."+k)
				}
			}
		}
		if len(hits) > 0 {
			g("managed settings", statusFail, p+" sets "+strings.Join(hits, ", ")+" — managed policy always wins over claude-ctx")
		} else {
			g("managed settings", statusOK, p+" present, does not touch managed keys")
		}
	}

	// gpg-agent TTL vs helper TTL (only relevant if any context uses pass).
	usesPass := false
	for _, cx := range c.Contexts {
		if cx.KeyPass != "" {
			usesPass = true
		}
	}
	if usesPass {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ttl, err := gpg.AgentTTL(ctx)
		want := time.Duration(c.Defaults.KeyTTLMs) * time.Millisecond
		switch {
		case err != nil:
			g("gpg-agent TTL", statusWarn, "could not read gpgconf: "+err.Error())
		case ttl.Default < want || ttl.Max < want:
			g("gpg-agent TTL", statusWarn, fmt.Sprintf("default-cache-ttl=%s max-cache-ttl=%s < key_ttl_ms=%s; helper re-runs may need pinentry inside Claude Code. Raise them in ~/.gnupg/gpg-agent.conf", ttl.Default, ttl.Max, want))
		default:
			g("gpg-agent TTL", statusOK, fmt.Sprintf("default=%s max=%s ≥ key_ttl_ms=%s", ttl.Default, ttl.Max, want))
		}
	}
	return out
}

func contextChecks(c *config.Config, name string, refreshOpencode bool) []check {
	var out []check
	add := func(n string, st status, detail string) {
		out = append(out, check{name, n, st, detail})
	}
	cx := c.Contexts[name]

	r, err := cred.ForContext(cx, c.Defaults)
	if err != nil {
		add("credential", statusFail, err.Error())
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	key, err := r.Resolve(ctx)
	if err != nil {
		add("credential", statusFail, strings.ReplaceAll(err.Error(), "\n", " "))
		return out
	}
	switch {
	case strings.ContainsAny(key, " \t\r\n"):
		add("credential", statusFail, r.Describe()+" contains whitespace; check the entry's first line")
	case len(key) < 8:
		add("credential", statusWarn, fmt.Sprintf("%s resolved but is only %d chars", r.Describe(), len(key)))
	default:
		add("credential", statusOK, fmt.Sprintf("%s (%d chars)", r.Describe(), len(key)))
	}

	if doctorNoNetwork {
		add("gateway", statusSkip, "--no-network")
		return out
	}
	if refreshOpencode {
		refreshMeta(c, name, key)
	}
	gctx, gcancel := context.WithTimeout(context.Background(), gateway.DefaultTimeout)
	defer gcancel()
	ids, err := gateway.ListModels(gctx, cx.BaseURL, key)
	if err != nil {
		add("gateway", statusFail, err.Error())
		return out
	}
	add("gateway", statusOK, fmt.Sprintf("%s/v1/models → %d models", strings.TrimRight(cx.BaseURL, "/"), len(ids)))

	have := map[string]bool{}
	for _, id := range ids {
		have[id] = true
	}
	modelCheck := func(label, model string) {
		if have[model] {
			add(label, statusOK, model)
		} else if len(ids) == 0 {
			add(label, statusWarn, model+" (gateway returned no models to compare against)")
		} else {
			add(label, statusFail, model+" not in gateway list"+nearest(model, ids))
		}
	}
	modelCheck("model", cx.Model)
	for _, fam := range config.AliasFamilies {
		if m, ok := cx.ModelAliases[fam]; ok {
			modelCheck("alias "+fam, m)
		}
	}
	return out
}

// nearest suggests gateway models sharing a substring with the wanted one.
func nearest(want string, ids []string) string {
	w := strings.ToLower(want)
	if i := strings.LastIndex(w, "/"); i >= 0 {
		w = w[i+1:]
	}
	var hits []string
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), w) || strings.Contains(w, strings.ToLower(id)) {
			hits = append(hits, id)
		}
	}
	if len(hits) == 0 {
		if len(ids) <= 5 {
			return " (available: " + strings.Join(ids, ", ") + ")"
		}
		return ""
	}
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return " (did you mean: " + strings.Join(hits, ", ") + "?)"
}

func managedSettingsPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Library/Application Support/ClaudeCode/managed-settings.json"}
	case "windows":
		return []string{filepath.Join(os.Getenv("ProgramData"), "ClaudeCode", "managed-settings.json")}
	default:
		return []string{"/etc/claude-code/managed-settings.json"}
	}
}

func printChecks(checks []check) {
	rows := [][]string{{"", "SCOPE", "CHECK", "DETAIL"}}
	for _, ch := range checks {
		var mark string
		switch ch.status {
		case statusOK:
			mark = ui.OK("✓")
		case statusWarn:
			mark = ui.Warn("!")
		case statusFail:
			mark = ui.Fail("✗")
		case statusSkip:
			mark = ui.Muted("-")
		}
		rows = append(rows, []string{mark, ch.scope, ch.name, ch.detail})
	}
	fmt.Print(ui.Table(rows))
}

func init() {
	doctorCmd.Flags().DurationVar(&doctorTimeout, "timeout", 30*time.Second, "give up waiting for a credential source")
	doctorCmd.Flags().BoolVar(&doctorNoNetwork, "no-network", false, "skip gateway probes")
	rootCmd.AddCommand(doctorCmd)
}
