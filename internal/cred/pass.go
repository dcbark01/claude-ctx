package cred

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Pass resolves via `pass show <entry>`, taking the first line only.
type Pass struct {
	Entry    string
	StoreDir string // optional PASSWORD_STORE_DIR
}

// Describe implements Resolver.
func (p *Pass) Describe() string { return "pass:" + p.Entry }

// Resolve implements Resolver. stderr (pinentry diagnostics) passes through.
func (p *Pass) Resolve(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "pass", "show", p.Entry)
	cmd.Env = os.Environ()
	if p.StoreDir != "" {
		cmd.Env = append(cmd.Env, "PASSWORD_STORE_DIR="+p.StoreDir)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Stdin = os.Stdin // pinentry-tty / curses need the terminal
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg != "" {
			err = errors.New(msg)
		}
		hint := "run `claude-ctx use <ctx>` (or `pass show " + p.Entry + "`) in a terminal to re-prime gpg-agent"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			hint = "timed out waiting for pass/gpg — pinentry may be blocked; " + hint
		}
		return "", &SourceError{Source: p.Describe(), Err: err, Hint: hint}
	}
	return check(firstLine(out.String()))
}
