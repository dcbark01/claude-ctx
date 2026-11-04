package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var (
	runClaudeBin   string
	runOpencodeBin string
	runClient      string
	runTimeout     time.Duration
	runDryRun      bool
)

// resolveClient picks the client: --client, then defaults.client, then claude.
func resolveClient(flag string, d config.Defaults) (string, error) {
	c := flag
	if c == "" {
		c = d.Client
	}
	switch c {
	case "":
		return config.ClientClaude, nil
	case config.ClientClaude, config.ClientOpencode:
		return c, nil
	}
	return "", fmt.Errorf(`unknown client %q (want "claude" or "opencode")`, c)
}

// opencodeLaunch builds `opencode --standalone <rest>` and the env entries that
// select the context's models for this process only.
func opencodeLaunch(bin, name string, cx config.Context, rest []string) (argv, env []string) {
	model, small := opencode.Refs(name, cx)
	content := map[string]string{opencode.KeyModel: model}
	if small != "" {
		content[opencode.KeySmallModel] = small
	}
	b, _ := json.Marshal(content)
	argv = append([]string{bin, "--standalone"}, rest...)
	return argv, []string{envConfigContent + "=" + string(b)}
}

const envConfigContent = "OPENCODE_CONFIG_CONTENT"

var runCmd = &cobra.Command{
	Use:   "run <context> [-- client args...]",
	Short: "Run claude (or opencode) with a context injected via environment (no file changes)",
	Long: `Execs claude with the context's env block in the child environment plus
CLAUDE_CTX=<context> and --settings '{"apiKeyHelper":"claude-ctx credential"}'.

With --client opencode (or defaults.client = "opencode") it execs
'opencode --standalone' with OPENCODE_CONFIG_CONTENT selecting the context's
models; the claude-ctx plugin supplies the key. Requires 'claude-ctx opencode install'.

Nothing is written to disk; two terminals can run different contexts at once.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeContexts,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadConfig()
		if err != nil {
			return err
		}
		name, rest := args[0], args[1:]
		cx, err := c.Get(name)
		if err != nil {
			return err
		}
		client, err := resolveClient(runClient, c.Defaults)
		if err != nil {
			return err
		}
		if client == config.ClientOpencode {
			return runOpencode(c, name, cx, rest)
		}
		fmt.Fprintf(os.Stderr, "checking credential for %s...\n", name)
		if err := prime(c, name, runTimeout); err != nil {
			return err
		}
		env := childEnv(render.Render(cx, c.Defaults), name)
		helper, _ := json.Marshal(map[string]string{settings.HelperKey: helperForRun()})
		bin := runClaudeBin
		if bin == "" {
			bin = os.Getenv("CLAUDE_CTX_CLAUDE_BIN")
		}
		if bin == "" {
			bin = "claude"
		}
		path, err := exec.LookPath(bin)
		if err != nil {
			return fmt.Errorf("cannot find %q on PATH: %w", bin, err)
		}
		argv := append([]string{path, "--settings", string(helper)}, rest...)
		if runDryRun {
			fmt.Println(strings.Join(argv, " "))
			for _, kv := range env {
				if strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_") || strings.HasPrefix(kv, "PASSWORD_STORE_DIR=") {
					fmt.Println(kv)
				}
			}
			return nil
		}
		return execChild(path, argv, env)
	},
}

// runOpencode execs opencode for one context; the plugin must be installed.
func runOpencode(c *config.Config, name string, cx config.Context, rest []string) error {
	if !opencodeInstalled() {
		return fmt.Errorf("opencode plugin not installed; run `claude-ctx opencode install`")
	}
	bin := runOpencodeBin
	if bin == "" {
		bin = opencodeBinary()
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("cannot find %q on PATH: %w", bin, err)
	}
	argv, add := opencodeLaunch(path, name, cx, rest)
	if runDryRun {
		fmt.Println(strings.Join(argv, " "))
		for _, kv := range add {
			fmt.Println(kv)
		}
		return nil
	}
	fmt.Fprintf(os.Stderr, "checking credential for %s...\n", name)
	key, err := primeKey(c, name, runTimeout)
	if err != nil {
		return err
	}
	refreshMeta(c, name, key)
	drop := map[string]bool{envConfigContent: true, "CLAUDE_CTX_CONFIG": true}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !drop[k] {
			env = append(env, kv)
		}
	}
	env = append(env, "CLAUDE_CTX_CONFIG="+absConfigPath())
	env = append(env, add...)
	return execChild(path, argv, env)
}

// childEnv builds the child's environment: the parent's, minus stale managed
// keys and shadowing token vars, plus the rendered block and CLAUDE_CTX.
func childEnv(r render.Rendered, name string) []string {
	drop := map[string]bool{render.EnvActiveCtx: true, "CLAUDE_CTX_CONFIG": true}
	for _, k := range render.AllManagedEnvKeys {
		drop[k] = true
	}
	for _, k := range render.ShadowingEnvVars {
		drop[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] {
			for _, s := range render.ShadowingEnvVars {
				if k == s {
					fmt.Fprintln(os.Stderr, ui.Warn("warning: dropping "+k+" from the child environment (it would shadow apiKeyHelper)"))
				}
			}
			continue
		}
		env = append(env, kv)
	}
	for _, k := range r.Keys() {
		env = append(env, k+"="+r.Env[k])
	}
	env = append(env, render.EnvActiveCtx+"="+name, "CLAUDE_CTX_CONFIG="+absConfigPath())
	return env
}

func init() {
	runCmd.Flags().StringVar(&runClaudeBin, "claude-bin", "", "claude executable (default $CLAUDE_CTX_CLAUDE_BIN or \"claude\")")
	runCmd.Flags().StringVar(&runOpencodeBin, "opencode-bin", "", "opencode executable (default $CLAUDE_CTX_OPENCODE_BIN or \"opencode\")")
	runCmd.Flags().StringVar(&runClient, "client", "", "claude | opencode (default defaults.client, else claude)")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 30*time.Second, "give up waiting for the credential source")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "print the command and injected env instead of executing")
	rootCmd.AddCommand(runCmd)
}
