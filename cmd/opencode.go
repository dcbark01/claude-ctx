package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/metacache"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var opencodeCmd = &cobra.Command{
	Use:   "opencode",
	Short: "opencode integration: install the plugin, inspect generated providers",
}

var providersJSON bool

var opencodeProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Print the opencode providers generated from config (used by the plugin)",
	Long: `Renders every context as an opencode provider (ctx-<name>) from config.toml
and the gateway metadata cache. Never resolves a credential or touches the
network. The claude-ctx opencode plugin runs this with --json at startup.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Load(configPath())
		if err != nil {
			return err
		}
		warns, err := c.Validate()
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, ui.Warn("warning: "+w.String()))
		}
		if err != nil {
			return err
		}
		cache, err := metacache.Load(cachePath())
		if err != nil {
			fmt.Fprintln(os.Stderr, ui.Warn("warning: ignoring unreadable metadata cache: "+err.Error()))
			cache = &metacache.Cache{Contexts: map[string]metacache.Entry{}}
		}
		providers := opencode.Build(c, cache)
		if providersJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(opencode.Output{ConfigPath: absConfigPath(), CachePath: cachePath(), Providers: providers})
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tMODELS\tLIMIT SOURCE")
		for _, p := range providers {
			ids := make([]string, len(p.Models))
			for i, m := range p.Models {
				ids[i] = m.ID
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, strings.Join(ids, ", "), opencode.LimitSource(c.Contexts[p.Context], cache.Contexts[p.Context]))
		}
		return w.Flush()
	},
}

func init() {
	opencodeProvidersCmd.Flags().BoolVar(&providersJSON, "json", false, "machine-readable output for the plugin")
	opencodeCmd.AddCommand(opencodeProvidersCmd)
	rootCmd.AddCommand(opencodeCmd)
}
