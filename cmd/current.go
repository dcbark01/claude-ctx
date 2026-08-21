package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var currentPlain bool

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Print the active context name",
	Long: `Prints the active context: $CLAUDE_CTX if set (inside a 'claude-ctx run'
session), otherwise the context recorded in user settings. With --plain the
output is undecorated — 'name', or 'name*' if managed settings have drifted —
suitable for a Claude Code statusline. Exits 1 with no output if none is active.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		doc, err := settings.Load(settingsPath())
		if err != nil {
			return err
		}
		name := currentName(doc)
		if name == "" {
			if !currentPlain {
				fmt.Fprintln(os.Stderr, "no active context")
			}
			os.Exit(1)
		}
		drift := os.Getenv(render.EnvActiveCtx) == "" && settings.Drift(doc)
		if currentPlain {
			if drift {
				name += "*"
			}
			fmt.Println(name)
			return nil
		}
		fmt.Println(name)
		if drift {
			fmt.Fprintln(os.Stderr, ui.Warn("warning: managed settings were modified outside claude-ctx (run `claude-ctx use "+name+" --force` to re-apply)"))
		}
		return nil
	},
}

func init() {
	currentCmd.Flags().BoolVar(&currentPlain, "plain", false, "undecorated output for statusline embedding")
	rootCmd.AddCommand(currentCmd)
}
