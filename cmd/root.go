// Package cmd implements the claude-ctx CLI.
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/metacache"
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
	_, err := primeKey(c, name, timeout)
	return err
}

// primeKey is prime that hands the key back for in-process use (gateway
// metadata refresh). The caller must never print or persist it.
func primeKey(c *config.Config, name string, timeout time.Duration) (string, error) {
	cx, err := c.Get(name)
	if err != nil {
		return "", err
	}
	r, err := cred.ForContext(cx, c.Defaults)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	key, err := r.Resolve(ctx)
	if err != nil {
		return "", fmt.Errorf("credential check failed for %q: %w", name, err)
	}
	return key, nil
}

// cachePath is the metadata cache next to the active config file.
func cachePath() string { return metacache.PathFor(absConfigPath()) }

// refreshMeta updates the gateway metadata cache for one context. It never
// fails the caller: problems are printed as warnings and the previous entry
// is kept for whatever could not be refreshed.
func refreshMeta(c *config.Config, name, key string) {
	cx, err := c.Get(name)
	if err != nil {
		return
	}
	cache, err := metacache.Load(cachePath())
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Warn("warning: metadata cache unreadable, rebuilding: "+err.Error()))
		cache = &metacache.Cache{Contexts: map[string]metacache.Entry{}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*gateway.DefaultTimeout)
	defer cancel()
	fresh, rerr := metacache.Refresh(ctx, cx, key)
	if rerr != nil {
		for _, line := range strings.Split(rerr.Error(), "\n") {
			fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("warning: %s: %s", name, line)))
		}
	}
	cache.Contexts[name] = metacache.Merge(cache.Contexts[name], fresh)
	if err := cache.Save(cachePath()); err != nil {
		fmt.Fprintln(os.Stderr, ui.Warn("warning: could not save metadata cache: "+err.Error()))
	}
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
