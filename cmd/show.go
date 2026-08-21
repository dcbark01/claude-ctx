package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var showJSON bool

var showCmd = &cobra.Command{
	Use:               "show [context]",
	Short:             "Render a context: resolved env block and (redacted) credential source",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeContexts,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadConfig()
		if err != nil {
			return err
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else {
			doc, err := loadSettings()
			if err != nil {
				return err
			}
			name = currentName(doc)
			if name == "" {
				return errors.New("no active context; pass a name or run `claude-ctx use`")
			}
		}
		cx, err := c.Get(name)
		if err != nil {
			return err
		}
		r := render.Render(cx, c.Defaults)
		res, err := cred.ForContext(cx, c.Defaults)
		if err != nil {
			return err
		}
		if showJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"context": name, "env": r.Env, "apiKeyHelper": render.HelperCommand, "credential": res.Describe(),
			})
		}
		fmt.Printf("%s\n", ui.OK(name))
		pairs := [][2]string{{"credential", res.Describe()}, {"apiKeyHelper", render.HelperCommand}}
		for _, k := range r.Keys() {
			pairs = append(pairs, [2]string{k, r.Env[k]})
		}
		fmt.Print(ui.KV(pairs))
		return nil
	},
}

func init() {
	showCmd.Flags().BoolVar(&showJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(showCmd)
}
