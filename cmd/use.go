package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var (
	useForce   bool
	useTimeout time.Duration
	useClient  string
)

var useCmd = &cobra.Command{
	Use:   "use [context]",
	Short: "Persistently switch context (Claude Code settings, and opencode when installed)",
	Long: `Merges the context's env block and apiKeyHelper into the Claude Code user
settings file. Only claude-ctx-managed keys are touched; permissions, hooks,
plugins and Claude-written state pass through untouched.

When the claude-ctx opencode plugin is installed, also sets "model" (and
"small_model" for a haiku alias) in opencode's global config, preserving its
comments. --client limits the switch to one client. With no argument an
interactive picker is shown.`,
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
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else {
			if len(c.Names()) == 0 {
				return errors.New("no contexts defined; run `claude-ctx edit`")
			}
			name, err = ui.Pick(c.Names(), settings.Current(doc))
			if err != nil {
				return err
			}
		}
		cx, err := c.Get(name)
		if err != nil {
			return err
		}
		clients, err := targetClients(useClient, opencodeInstalled())
		if err != nil {
			return err
		}
		doClaude, doOpencode := slices.Contains(clients, config.ClientClaude), slices.Contains(clients, config.ClientOpencode)
		// Check every target for external edits before writing any of them.
		if doClaude && settings.Drift(doc) && !useForce {
			return fmt.Errorf("managed settings were modified outside claude-ctx since the last switch (context %q); review %s or re-run with --force",
				settings.Current(doc), settingsPath())
		}
		if doOpencode {
			if err := opencodePreflight(statePath(), opencodeDir(), useForce); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "checking credential for %s...\n", name)
		key, err := primeKey(c, name, useTimeout)
		if err != nil {
			return err
		}
		if doOpencode {
			refreshMeta(c, name, key)
		}
		if doClaude {
			if err := settings.Apply(doc, name, render.Render(cx, c.Defaults), helperForUse()); err != nil {
				return err
			}
			if err := settings.Write(settingsPath(), doc); err != nil {
				return err
			}
			fmt.Printf("%s switched to %s  %s\n", ui.OK("✓"), name, ui.Muted(fmt.Sprintf("(%s → %s)", cx.Model, cx.BaseURL)))
		}
		if doOpencode {
			if err := applyOpencode(statePath(), opencodeDir(), name, cx, true); err != nil {
				return err
			}
			model, _ := opencode.Refs(name, cx)
			fmt.Printf("%s opencode switched to %s  %s\n", ui.OK("✓"), name, ui.Muted("("+model+")"))
		}
		fmt.Fprintln(os.Stderr, ui.Warn("note: affects new sessions. A running session may partially pick this up (apiKeyHelper/env live-reload; model does not). Use `claude-ctx run` for per-terminal switching."))
		return nil
	},
}

func init() {
	useCmd.Flags().BoolVar(&useForce, "force", false, "overwrite managed keys even if they were edited externally")
	useCmd.Flags().StringVar(&useClient, "client", "", "only switch this client: claude | opencode (default: all installed)")
	useCmd.Flags().DurationVar(&useTimeout, "timeout", 30*time.Second, "give up waiting for the credential source")
	rootCmd.AddCommand(useCmd)
}
