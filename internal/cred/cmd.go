package cred

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Cmd resolves by running a shell command; trailing newlines are trimmed.
type Cmd struct {
	Command string
}

// Describe implements Resolver.
func (c *Cmd) Describe() string { return "cmd:" + c.Command }

// Resolve implements Resolver.
func (c *Cmd) Resolve(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			err = errors.New(msg)
		}
		return "", &SourceError{Source: c.Describe(), Err: err}
	}
	return check(strings.TrimRight(out.String(), "\r\n"))
}
