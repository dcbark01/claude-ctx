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

var (
	credentialTimeout time.Duration
	credentialContext string
)

// credentialContextName picks the context: --context, then $CLAUDE_CTX, then
// the settings sentinel. sentinel is only called when the first two are empty,
// so the settings file is not read when the caller already named a context.
func credentialContextName(flag, env string, sentinel func() string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	return sentinel()
}

var credentialCmd = &cobra.Command{
	Use:   "credential",
	Short: "apiKeyHelper entrypoint: print the active context's API key to stdout",
	Long: `Resolves the credential for the active context and prints it to stdout.
Claude Code invokes this as its apiKeyHelper; the opencode plugin invokes it
with --context. The context is taken from --context, then $CLAUDE_CTX (set by
'claude-ctx run'), then the _claudeCtx sentinel in the user settings file.
All diagnostics go to stderr; any failure exits nonzero.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Deliberately minimal: no validation warnings, no UI, no network.
		c, err := config.Load(configPath())
		if err != nil {
			return err
		}
		var loadErr error
		name := credentialContextName(credentialContext, os.Getenv("CLAUDE_CTX"), func() string {
			doc, err := settings.Load(settingsPath())
			if err != nil {
				loadErr = err
				return ""
			}
			return settings.Current(doc)
		})
		if loadErr != nil {
			return loadErr
		}
		if name == "" {
			return errors.New("no active context: run `claude-ctx use <ctx>`, set CLAUDE_CTX, or pass --context")
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
	credentialCmd.Flags().StringVar(&credentialContext, "context", "", "context to resolve (default $CLAUDE_CTX, then the active context)")
	_ = credentialCmd.RegisterFlagCompletionFunc("context", completeContexts)
	rootCmd.AddCommand(credentialCmd)
}
