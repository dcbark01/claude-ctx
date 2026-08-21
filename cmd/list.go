package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var listPlain bool

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List contexts (current one starred)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadConfig()
		if err != nil {
			return err
		}
		doc, err := loadSettings()
		if err != nil {
			return err
		}
		cur := currentName(doc)
		if listPlain {
			for _, n := range c.Names() {
				fmt.Println(n)
			}
			return nil
		}
		rows := [][]string{{"", "NAME", "MODEL", "BASE URL", "CREDENTIAL"}}
		for _, n := range c.Names() {
			cx := c.Contexts[n]
			mark := " "
			if n == cur {
				mark = ui.OK("*")
			}
			label := "?"
			if r, err := cred.ForContext(cx, c.Defaults); err == nil {
				label = r.Describe()
			}
			rows = append(rows, []string{mark, n, cx.Model, cx.BaseURL, label})
		}
		fmt.Print(ui.Table(rows))
		return nil
	},
}

func init() {
	listCmd.Flags().BoolVar(&listPlain, "plain", false, "names only, one per line")
	rootCmd.AddCommand(listCmd)
}
