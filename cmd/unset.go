package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var unsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove every claude-ctx-managed key from user settings (and undo `use` in opencode)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		doc, err := settings.Load(settingsPath())
		if err != nil {
			return err
		}
		prev := settings.Current(doc)
		had, err := settings.Remove(doc)
		if err != nil {
			return err
		}
		if !had {
			fmt.Println("nothing managed by claude-ctx in", settingsPath())
		} else {
			if err := settings.Write(settingsPath(), doc); err != nil {
				return err
			}
			fmt.Printf("%s removed context %s from %s\n", ui.OK("✓"), prev, settingsPath())
		}
		ocPrev, ocHad, err := removeOpencode(statePath())
		if err != nil {
			return err
		}
		if ocHad {
			fmt.Printf("%s removed context %s from opencode config\n", ui.OK("✓"), ocPrev)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(unsetCmd) }
