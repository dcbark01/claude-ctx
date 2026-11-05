package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/metacache"
	"github.com/dcbark01/claude-ctx/internal/opencode"
)

// opencodeChecks validates the opencode integration. It returns nothing when
// opencode is not installed, so Claude-only users see no change.
func opencodeChecks(c *config.Config, names []string, ocDir, opencodeBin string) []check {
	ocPath, err := exec.LookPath(opencodeBin)
	if err != nil {
		return nil
	}
	var out []check
	g := func(name string, st status, detail string) {
		out = append(out, check{"opencode", name, st, detail})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	major, raw, err := opencode.Version(ctx, ocPath)
	switch {
	case err != nil:
		g("opencode version", statusFail, err.Error())
	case major < opencode.MinMajor:
		g("opencode version", statusFail, fmt.Sprintf("%s at %s; claude-ctx needs opencode v2 or later", raw, ocPath))
	default:
		g("opencode version", statusOK, raw+" ("+ocPath+")")
	}

	pluginPath := opencode.PluginPath(ocDir)
	pv, installed := opencode.InstalledPluginVersion(pluginPath)
	switch {
	case !installed:
		g("plugin", statusWarn, "not installed; run `claude-ctx opencode install` to use contexts in opencode")
	case pv != version:
		g("plugin", statusWarn, fmt.Sprintf("plugin %s, claude-ctx %s; re-run `claude-ctx opencode install`", pv, version))
	default:
		g("plugin", statusOK, pluginPath)
	}

	if installed {
		g(pluginBinaryCheck(pluginPath, pv))
	}

	if model := defaultOpencodeModel(ocDir); model != "" {
		if name, ok := ctxOfModelRef(model); ok {
			if _, err := c.Get(name); err != nil {
				g("default model", statusFail, fmt.Sprintf("%s names context %q, which is not in config", model, name))
			} else {
				g("default model", statusOK, model)
			}
		} else {
			g("default model", statusSkip, model+" (not a claude-ctx model)")
		}
	}

	cache, err := metacache.Load(cachePath())
	if err != nil {
		cache = &metacache.Cache{Contexts: map[string]metacache.Entry{}}
	}
	for _, n := range names {
		cx := c.Contexts[n]
		switch src := opencode.LimitSource(cx, cache.Contexts[n]); src {
		case "default":
			out = append(out, check{n, "limits", statusWarn, "unknown; opencode assumes 200000/32000 (set context_limit / output_limit, or use a gateway with /model/info)"})
		default:
			out = append(out, check{n, "limits", statusOK, "from " + src})
		}
	}
	return out
}

// pluginBinaryCheck verifies the plugin can reach a matching claude-ctx: it
// tries PATH first, then the binary baked in at install time.
func pluginBinaryCheck(pluginPath, pluginVersion string) (string, status, string) {
	const name = "plugin binary"
	if p, err := exec.LookPath("claude-ctx"); err == nil {
		v := binaryVersion(p)
		if v != pluginVersion {
			return name, statusWarn, fmt.Sprintf("%s is version %s but the plugin is %s; the plugin calls PATH first (run `go install`, then `claude-ctx opencode install`)", p, v, pluginVersion)
		}
		return name, statusOK, p + " on PATH"
	}
	bin, ok := opencode.InstalledPluginBin(pluginPath)
	if !ok {
		return name, statusFail, "no claude-ctx on PATH and no path baked into the plugin"
	}
	if st, err := os.Stat(bin); err != nil || st.Mode()&0o111 == 0 {
		return name, statusFail, bin + " (baked into the plugin) is missing or not executable, and claude-ctx is not on PATH"
	}
	return name, statusWarn, "claude-ctx not on PATH; the plugin falls back to " + bin
}

// binaryVersion runs `<bin> --version` and returns its last field.
func binaryVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return "unknown"
	}
	return f[len(f)-1]
}

func defaultOpencodeModel(ocDir string) string {
	keys, err := opencode.ReadKeys(opencode.ConfigFile(ocDir), []string{opencode.KeyModel})
	if err != nil {
		return ""
	}
	return keys[opencode.KeyModel]
}

// ctxOfModelRef extracts the context from "ctx-<name>/<model>".
func ctxOfModelRef(ref string) (string, bool) {
	provider, _, ok := strings.Cut(ref, "/")
	if !ok || !strings.HasPrefix(provider, opencode.ProviderPrefix) {
		return "", false
	}
	return strings.TrimPrefix(provider, opencode.ProviderPrefix), true
}
