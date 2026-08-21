// Package cred resolves API credentials from pass, a command, or the environment.
// Nothing in this package ever writes a secret anywhere but the returned string.
package cred

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/config"
)

// ErrEmpty is returned when a source succeeds but yields no usable key.
var ErrEmpty = errors.New("credential source returned an empty value")

// SourceError wraps a failure of the underlying source with a user hint.
type SourceError struct {
	Source string
	Err    error
	Hint   string
}

func (e *SourceError) Error() string {
	s := fmt.Sprintf("%s: %v", e.Source, e.Err)
	if e.Hint != "" {
		s += "\n  hint: " + e.Hint
	}
	return s
}

func (e *SourceError) Unwrap() error { return e.Err }

// Resolver yields a credential.
type Resolver interface {
	// Resolve returns the key. It must never print the key anywhere.
	Resolve(ctx context.Context) (string, error)
	// Describe returns a redacted, human-readable label for the source.
	Describe() string
}

// ForContext picks the resolver by precedence key_pass > key_cmd > key_env.
func ForContext(c config.Context, d config.Defaults) (Resolver, error) {
	switch {
	case c.KeyPass != "":
		return &Pass{Entry: c.KeyPass, StoreDir: d.StoreDir}, nil
	case c.KeyCmd != "":
		return &Cmd{Command: c.KeyCmd}, nil
	case c.KeyEnv != "":
		return &Env{Var: c.KeyEnv}, nil
	}
	return nil, errors.New("context has no credential source (key_pass, key_cmd, or key_env)")
}

// firstLine returns the first line with surrounding whitespace trimmed.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// check enforces the fail-loud contract.
func check(key string) (string, error) {
	if key == "" {
		return "", ErrEmpty
	}
	return key, nil
}
