package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/settings"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var unsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove every claude-ctx-managed key from user settings",
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
			return nil
		}
		if err := settings.Write(settingsPath(), doc); err != nil {
			return err
		}
		fmt.Printf("%s removed context %s from %s\n", ui.OK("✓"), prev, settingsPath())
		return nil
	},
}

func init() { rootCmd.AddCommand(unsetCmd) }
