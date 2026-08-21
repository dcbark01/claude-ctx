package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config.toml in $EDITOR and re-validate on save",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := configPath()
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(config.Template), 0o600); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "created", path)
		}
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		parts := strings.Fields(editor)
		c := exec.Command(parts[0], append(parts[1:], path)...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %w", editor, err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		warns, err := cfg.Validate()
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, ui.Warn("warning: "+w.String()))
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s %s is valid (%d contexts)\n", ui.OK("✓"), path, len(cfg.Contexts))
		return nil
	},
}

func init() { rootCmd.AddCommand(editCmd) }
