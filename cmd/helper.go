package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/render"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

// selfPath returns this binary's absolute path, or "" if unknown.
func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

// helperForUse returns the apiKeyHelper string to persist. It prefers the
// stable "claude-ctx credential" when claude-ctx is on PATH; otherwise it
// falls back to the absolute path of the running binary and warns, since a
// relocated binary would break the helper.
func helperForUse() string {
	bin := "claude-ctx"
	if _, err := exec.LookPath("claude-ctx"); err != nil {
		if p := selfPath(); p != "" {
			fmt.Fprintln(os.Stderr, ui.Warn("warning: claude-ctx is not on PATH; writing apiKeyHelper with the absolute path "+p+" (install with `go install` for a stable helper string)"))
			bin = shellQuote(p)
		}
	}
	return bin + configArg() + " credential"
}

// helperForRun always uses the absolute path: nothing is persisted, and the
// child shell may not share this process's PATH.
func helperForRun() string {
	if p := selfPath(); p != "" {
		return shellQuote(p) + " credential"
	}
	return render.HelperCommand
}

// configArg returns " --config <abs>" when a non-default config is in use,
// so the persisted helper reads the same file. Empty otherwise.
func configArg() string {
	if flagConfig == "" {
		return ""
	}
	abs, err := filepath.Abs(flagConfig)
	if err != nil {
		abs = flagConfig
	}
	return " --config " + shellQuote(abs)
}

// configArgs is configArg in argv form: ["--config", <abs>] when a
// non-default config is in use, nil otherwise.
func configArgs() []string {
	if flagConfig == "" {
		return nil
	}
	abs, err := filepath.Abs(flagConfig)
	if err != nil {
		abs = flagConfig
	}
	return []string{"--config", abs}
}

// absConfigPath is the config path to hand to child processes.
func absConfigPath() string {
	abs, err := filepath.Abs(configPath())
	if err != nil {
		return configPath()
	}
	return abs
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
