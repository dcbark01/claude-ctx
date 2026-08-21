// Package passstore enumerates entries in a password-store directory without
// decrypting anything (it only lists *.gpg files).
package passstore

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dir returns the store directory: explicit > $PASSWORD_STORE_DIR > ~/.password-store.
func Dir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if d := os.Getenv("PASSWORD_STORE_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".password-store"
	}
	return filepath.Join(home, ".password-store")
}

// List returns entry names (pass paths, no .gpg suffix) under namespace,
// sorted. An empty namespace lists the whole store. A missing store or
// namespace yields an empty list, not an error.
func List(storeDir, namespace string) ([]string, error) {
	root := filepath.Join(storeDir, filepath.FromSlash(namespace))
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || (p != root && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".gpg") {
			return nil
		}
		rel, err := filepath.Rel(storeDir, p)
		if err != nil {
			return err
		}
		out = append(out, strings.TrimSuffix(filepath.ToSlash(rel), ".gpg"))
		return nil
	})
	sort.Strings(out)
	return out, err
}
