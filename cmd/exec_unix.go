//go:build !windows

package cmd

import "syscall"

// execChild replaces the current process with the child.
func execChild(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
