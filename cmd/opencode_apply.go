package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/state"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var managedOpencodeKeys = []string{opencode.KeyModel, opencode.KeySmallModel}

// targetClients decides which clients `use` updates: with no flag, Claude Code
// plus opencode when its plugin is installed.
func targetClients(flag string, opencodeInstalled bool) ([]string, error) {
	switch flag {
	case "":
		if opencodeInstalled {
			return []string{config.ClientClaude, config.ClientOpencode}, nil
		}
		return []string{config.ClientClaude}, nil
	case config.ClientClaude:
		return []string{config.ClientClaude}, nil
	case config.ClientOpencode:
		if !opencodeInstalled {
			return nil, fmt.Errorf("opencode plugin not installed; run `claude-ctx opencode install` first")
		}
		return []string{config.ClientOpencode}, nil
	}
	return nil, fmt.Errorf(`unknown client %q (want "claude" or "opencode")`, flag)
}

// opencodeDriftError reports external edits to what the last `use` wrote.
func opencodeDriftError(statePath string) error {
	st, err := state.Load(statePath)
	if err != nil || st.Opencode == nil {
		return err
	}
	cur, err := opencode.ReadKeys(st.Opencode.Path, managedOpencodeKeys)
	if err != nil {
		return err
	}
	if st.Opencode.Drift(cur) {
		return fmt.Errorf("opencode config %s was modified outside claude-ctx since `use %s`; review it or re-run with --force",
			st.Opencode.Path, st.Opencode.Context)
	}
	return nil
}

// opencodePreflight runs every check `use` needs before it writes anything:
// the target config parses, and (unless force) it was not edited since the
// last `use`.
func opencodePreflight(statePath, ocDir string, force bool) error {
	if _, err := opencode.ReadKeys(opencode.ConfigFile(ocDir), managedOpencodeKeys); err != nil {
		return err
	}
	if force {
		return nil
	}
	return opencodeDriftError(statePath)
}

// applyOpencode points opencode's default (and small) model at a context.
func applyOpencode(statePath, ocDir, name string, cx config.Context, force bool) error {
	if !force {
		if err := opencodeDriftError(statePath); err != nil {
			return err
		}
	}
	st, err := state.Load(statePath)
	if err != nil {
		return err
	}
	path := opencode.ConfigFile(ocDir)
	var previous map[string]string
	if st.Opencode != nil && st.Opencode.Path == path {
		previous = st.Opencode.Previous
	} else {
		if previous, err = opencode.ReadKeys(path, managedOpencodeKeys); err != nil {
			return err
		}
	}
	model, small := opencode.Refs(name, cx)
	set := map[string]string{opencode.KeyModel: model}
	var remove []string
	if small != "" {
		set[opencode.KeySmallModel] = small
	} else if st.Opencode != nil && st.Opencode.Values[opencode.KeySmallModel] != "" {
		if p, ok := previous[opencode.KeySmallModel]; ok {
			set[opencode.KeySmallModel] = p
		} else {
			remove = append(remove, opencode.KeySmallModel)
		}
	}
	if err := opencode.SetKeys(path, set, remove); err != nil {
		return err
	}
	values := map[string]string{opencode.KeyModel: model}
	if small != "" {
		values[opencode.KeySmallModel] = small
	}
	st.Opencode = &state.Opencode{Context: name, Path: path, Values: values, Previous: previous}
	return st.Save(statePath)
}

// removeOpencode undoes `use` in opencode's config: managed keys get their
// pre-`use` values back (or are removed), then the state entry is cleared.
func removeOpencode(statePath string) (prevCtx string, had bool, err error) {
	st, err := state.Load(statePath)
	if err != nil || st.Opencode == nil {
		return "", false, err
	}
	o := st.Opencode
	cur, err := opencode.ReadKeys(o.Path, managedOpencodeKeys)
	if err != nil {
		return "", false, err
	}
	set := map[string]string{}
	var remove, kept []string
	for k, v := range o.Values {
		if c, ok := cur[k]; !ok || c != v {
			kept = append(kept, k) // edited (or file removed) since `use`: the user's value wins
			continue
		}
		if p, ok := o.Previous[k]; ok {
			set[k] = p
		} else {
			remove = append(remove, k)
		}
	}
	if len(set) > 0 || len(remove) > 0 {
		if err := opencode.SetKeys(o.Path, set, remove); err != nil {
			return "", false, err
		}
	}
	if len(kept) > 0 {
		sort.Strings(kept)
		fmt.Fprintln(os.Stderr, ui.Warn("note: left "+strings.Join(kept, ", ")+" in "+o.Path+" as is (changed since `use "+o.Context+"`)"))
	}
	st.Opencode = nil
	return o.Context, true, st.Save(statePath)
}

// opencodeCurrent returns the context last `use`d for opencode and whether
// opencode's config was edited since.
func opencodeCurrent(statePath string) (name string, drift bool) {
	st, err := state.Load(statePath)
	if err != nil || st.Opencode == nil {
		return "", false
	}
	cur, err := opencode.ReadKeys(st.Opencode.Path, managedOpencodeKeys)
	return st.Opencode.Context, err != nil || st.Opencode.Drift(cur)
}

// statePath is state.json next to the active config file.
func statePath() string { return state.PathFor(absConfigPath()) }
