package opencode

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed plugin/claude-ctx.ts
var pluginTemplate []byte

// versionHeader prefixes the first line of an installed plugin.
const versionHeader = "// claude-ctx-plugin-version: "

// MinMajor is the oldest supported opencode major version.
const MinMajor = 2

// PluginPath is where the plugin lives inside an opencode config directory.
func PluginPath(dir string) string { return filepath.Join(dir, "plugins", "claude-ctx.ts") }

// PluginSource renders the embedded plugin with the installing binary's path,
// version, and extra global args (e.g. --config) baked in.
func PluginSource(bin, version string, extraArgs []string) []byte {
	if extraArgs == nil {
		extraArgs = []string{}
	}
	q := func(v any) []byte { b, _ := json.Marshal(v); return b }
	src := bytes.ReplaceAll(pluginTemplate, []byte(`"__CLAUDE_CTX_BIN__"`), q(bin))
	src = bytes.ReplaceAll(src, []byte(`"__CLAUDE_CTX_VERSION__"`), q(version))
	src = bytes.ReplaceAll(src, []byte(`[/*__CLAUDE_CTX_ARGS__*/]`), q(extraArgs))
	return append([]byte(versionHeader+version+"\n"), src...)
}

// InstalledPluginVersion reads the version header of an installed plugin.
// ok is false when the file is missing or was not written by claude-ctx.
func InstalledPluginVersion(path string) (version string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	v, found := strings.CutPrefix(strings.TrimRight(line, "\r\n"), versionHeader)
	return v, found && v != ""
}

var binLineRe = regexp.MustCompile(`(?m)^const BIN: string = (.*)$`)

// InstalledPluginBin returns the claude-ctx path baked into an installed plugin.
func InstalledPluginBin(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	m := binLineRe.FindSubmatch(data)
	var bin string
	if m == nil || json.Unmarshal(m[1], &bin) != nil {
		return "", false
	}
	return bin, true
}

var semverRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// ParseVersion extracts the first vMAJOR.MINOR.PATCH from `opencode --version`.
func ParseVersion(out string) (major int, raw string, err error) {
	m := semverRe.FindStringSubmatch(out)
	if m == nil {
		return 0, "", fmt.Errorf("cannot parse opencode version from %q", strings.TrimSpace(out))
	}
	major, _ = strconv.Atoi(m[1])
	return major, strings.TrimPrefix(m[0], "v"), nil
}

// Version runs `<bin> --version`.
func Version(ctx context.Context, bin string) (major int, raw string, err error) {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return 0, "", fmt.Errorf("%s --version: %w", bin, err)
	}
	return ParseVersion(string(out))
}
