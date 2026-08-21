package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var (
	useForce   bool
	useTimeout time.Duration
)

var useCmd = &cobra.Command{
	Use:   "use [context]",
	Short: "Persistently switch context (merges into user settings)",
	Long: `Merges the context's env block and apiKeyHelper into the Claude Code user
settings file. Only claude-ctx-managed keys are touched; permissions, hooks,
plugins and Claude-written state pass through untouched. With no argument an
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
		if settings.Drift(doc) && !useForce {
			return fmt.Errorf("managed settings were modified outside claude-ctx since the last switch (context %q); review %s or re-run with --force",
				settings.Current(doc), settingsPath())
		}
		fmt.Fprintf(os.Stderr, "checking credential for %s...\n", name)
		if err := prime(c, name, useTimeout); err != nil {
			return err
		}
		if err := settings.Apply(doc, name, render.Render(cx, c.Defaults), helperForUse()); err != nil {
			return err
		}
		if err := settings.Write(settingsPath(), doc); err != nil {
			return err
		}
		fmt.Printf("%s switched to %s  %s\n", ui.OK("✓"), name, ui.Muted(fmt.Sprintf("(%s → %s)", cx.Model, cx.BaseURL)))
		fmt.Fprintln(os.Stderr, ui.Warn("note: affects new sessions. A running session may partially pick this up (apiKeyHelper/env live-reload; model does not). Use `claude-ctx run` for per-terminal switching."))
		return nil
	},
}

func init() {
	useCmd.Flags().BoolVar(&useForce, "force", false, "overwrite managed keys even if they were edited externally")
	useCmd.Flags().DurationVar(&useTimeout, "timeout", 30*time.Second, "give up waiting for the credential source")
	rootCmd.AddCommand(useCmd)
}
