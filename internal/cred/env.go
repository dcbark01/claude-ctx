package cred

import (
	"context"
	"errors"
	"os"
	"strings"
)

// Env resolves from an environment variable.
type Env struct {
	Var string
}

// Describe implements Resolver.
func (e *Env) Describe() string { return "env:" + e.Var }

// Resolve implements Resolver.
func (e *Env) Resolve(context.Context) (string, error) {
	v, ok := os.LookupEnv(e.Var)
	if !ok {
		return "", &SourceError{Source: e.Describe(), Err: errors.New("variable not set"),
			Hint: "export " + e.Var + " in the shell that launches claude"}
	}
	return check(strings.TrimSpace(v))
}
