package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/settings"
)

var credentialTimeout time.Duration

var credentialCmd = &cobra.Command{
	Use:   "credential",
	Short: "apiKeyHelper entrypoint: print the active context's API key to stdout",
	Long: `Resolves the credential for the active context and prints it to stdout.
Claude Code invokes this as its apiKeyHelper. The active context is taken from
$CLAUDE_CTX (set by 'claude-ctx run') or from the _claudeCtx sentinel in the
user settings file. All diagnostics go to stderr; any failure exits nonzero.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Deliberately minimal: no validation warnings, no UI, no network.
		c, err := config.Load(configPath())
		if err != nil {
			return err
		}
		name := os.Getenv("CLAUDE_CTX")
		if name == "" {
			doc, err := settings.Load(settingsPath())
			if err != nil {
				return err
			}
			name = settings.Current(doc)
		}
		if name == "" {
			return errors.New("no active context: run `claude-ctx use <ctx>` or set CLAUDE_CTX")
		}
		cx, err := c.Get(name)
		if err != nil {
			return err
		}
		r, err := cred.ForContext(cx, c.Defaults)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), credentialTimeout)
		defer cancel()
		key, err := r.Resolve(ctx)
		if err != nil {
			return fmt.Errorf("context %q: %w", name, err)
		}
		_, err = fmt.Fprint(os.Stdout, key)
		return err
	},
}

func init() {
	credentialCmd.Flags().DurationVar(&credentialTimeout, "timeout", 20*time.Second, "give up waiting for the credential source")
	rootCmd.AddCommand(credentialCmd)
}
