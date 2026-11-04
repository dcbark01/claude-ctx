package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

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

var (
	providersJSON bool
	installForce  bool
)

// opencodeBinary is the opencode executable: $CLAUDE_CTX_OPENCODE_BIN or "opencode".
func opencodeBinary() string {
	if b := os.Getenv("CLAUDE_CTX_OPENCODE_BIN"); b != "" {
		return b
	}
	return "opencode"
}

// checkOpencodeVersion fails unless opencode v2+ is installed.
func checkOpencodeVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	major, raw, err := opencode.Version(ctx, opencodeBinary())
	if err != nil {
		return "", err
	}
	if major < opencode.MinMajor {
		return raw, fmt.Errorf("opencode %s is not supported; claude-ctx needs opencode v2 or later", raw)
	}
	return raw, nil
}

// opencodeInstalled reports whether the claude-ctx plugin is installed.
func opencodeInstalled() bool {
	_, ok := opencode.InstalledPluginVersion(opencode.PluginPath(opencodeDir()))
	return ok
}

var opencodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install (or upgrade) the claude-ctx plugin into opencode's config directory",
	Long: `Writes the claude-ctx plugin to <opencode config dir>/plugins/claude-ctx.ts.
The plugin defines one provider per context (ctx-<name>) and fetches keys from
'claude-ctx credential --context <name>' into opencode's memory only. Re-run
after upgrading claude-ctx. Requires opencode v2 or later.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := checkOpencodeVersion()
		if err != nil {
			return err
		}
		path := opencode.PluginPath(opencodeDir())
		if _, err := os.Stat(path); err == nil && !installForce {
			if _, ours := opencode.InstalledPluginVersion(path); !ours {
				return fmt.Errorf("%s exists and was not written by claude-ctx; re-run with --force to replace it", path)
			}
		}
		bin := selfPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := metacache.WriteFileAtomic(path, opencode.PluginSource(bin, version, configArgs()), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s installed claude-ctx plugin %s for opencode %s  %s\n", ui.OK("✓"), version, raw, ui.Muted("("+path+")"))
		return nil
	},
}

var opencodeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the claude-ctx plugin from opencode",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := opencode.PluginPath(opencodeDir())
		if _, ours := opencode.InstalledPluginVersion(path); !ours {
			fmt.Println("claude-ctx plugin not installed at", path)
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("%s removed %s\n", ui.OK("✓"), path)
		return nil
	},
}

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
	opencodeInstallCmd.Flags().BoolVar(&installForce, "force", false, "replace a plugin file not written by claude-ctx")
	opencodeCmd.AddCommand(opencodeProvidersCmd, opencodeInstallCmd, opencodeUninstallCmd)
	rootCmd.AddCommand(opencodeCmd)
}
