package cmd

import (
	"fmt"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/opencode"
	"github.com/dcbark01/claude-ctx/internal/state"
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
	set := map[string]string{}
	var remove []string
	for k := range o.Values {
		if p, ok := o.Previous[k]; ok {
			set[k] = p
		} else {
			remove = append(remove, k)
		}
	}
	if err := opencode.SetKeys(o.Path, set, remove); err != nil {
		return "", false, err
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
