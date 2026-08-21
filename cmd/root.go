// Package cmd implements the claude-ctx CLI.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var (
	flagConfig       string
	flagSettingsPath string
	version          = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "claude-ctx",
	Short: "Switch Claude Code between gateway/model/credential contexts",
	Long: `claude-ctx manages named contexts (base URL + model + credential source)
for Claude Code, with docker-context / kubectx ergonomics.

Secrets never touch disk: contexts reference a pass entry, a command, or an
environment variable, and claude-ctx itself acts as Claude Code's apiKeyHelper.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version,
}

// Execute runs the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "config file (default $CLAUDE_CTX_CONFIG or ~/.claude/context/config.toml)")
	rootCmd.PersistentFlags().StringVar(&flagSettingsPath, "settings-path", "", "Claude Code user settings file (default ~/.claude/settings.json)")
}

func configPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	return config.DefaultPath()
}

func settingsPath() string {
	if flagSettingsPath != "" {
		return flagSettingsPath
	}
	return settings.DefaultPath()
}

// loadConfig loads + validates, printing warnings to stderr.
func loadConfig() (*config.Config, error) {
	c, err := config.Load(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s (run `claude-ctx edit` to create one)", configPath())
		}
		return nil, err
	}
	warns, err := c.Validate()
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, ui.Warn("warning: "+w.String()))
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func loadSettings() (map[string]any, error) {
	return settings.Load(settingsPath())
}

// currentName resolves the active context: CLAUDE_CTX env, then sentinel.
func currentName(doc map[string]any) string {
	if n := os.Getenv(render.EnvActiveCtx); n != "" {
		return n
	}
	return settings.Current(doc)
}

// prime resolves the credential in the foreground (pinentry lands in this
// terminal) and validates that it is non-empty. The key is discarded.
func prime(c *config.Config, name string, timeout time.Duration) error {
	cx, err := c.Get(name)
	if err != nil {
		return err
	}
	r, err := cred.ForContext(cx, c.Defaults)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := r.Resolve(ctx); err != nil {
		return fmt.Errorf("credential check failed for %q: %w", name, err)
	}
	return nil
}

// completeContexts is the cobra completion function for context-name args.
func completeContexts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c, err := config.Load(configPath())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return c.Names(), cobra.ShellCompDirectiveNoFileComp
}
