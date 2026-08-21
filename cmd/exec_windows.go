//go:build windows

package cmd

import (
	"errors"
	"os"
	"os/exec"
)

// execChild runs the child and mirrors its exit code.
func execChild(path string, argv, env []string) error {
	c := exec.Command(path, argv[1:]...)
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}
