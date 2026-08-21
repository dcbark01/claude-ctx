package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var (
	runClaudeBin string
	runTimeout   time.Duration
	runDryRun    bool
)

var runCmd = &cobra.Command{
	Use:   "run <context> [-- claude args...]",
	Short: "Run claude with a context injected via environment (no file changes)",
	Long: `Execs claude with the context's env block in the child environment plus
CLAUDE_CTX=<context> and --settings '{"apiKeyHelper":"claude-ctx credential"}'.
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
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 30*time.Second, "give up waiting for the credential source")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "print the command and injected env instead of executing")
	rootCmd.AddCommand(runCmd)
}
