package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/dcbark01/claude-ctx/internal/config"
	"github.com/dcbark01/claude-ctx/internal/cred"
	"github.com/dcbark01/claude-ctx/internal/gateway"
	"github.com/dcbark01/claude-ctx/internal/metacache"
	"github.com/dcbark01/claude-ctx/internal/passstore"
	"github.com/dcbark01/claude-ctx/internal/ui"
)

var addFlags struct {
	baseURL, model, keyPass, keyCmd, keyEnv, displayName string
	discovery                                            bool
	contextLimit, outputLimit                            int64
	aliases                                              []string
	noVerify                                             bool
}

var addCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a context (interactive, with a pass-entry picker; or via flags)",
	Long: `Appends a context to config.toml. With no flags an interactive form walks
through name, gateway, credential (offering entries found under
defaults.namespace in your pass store) and model (offered from the gateway's
/v1/models when reachable). Flags make it non-interactive:

  claude-ctx add opus-b --base-url https://litellm-b --model anthropic/claude-opus-5 \
      --key-pass work/litellm/b/anthropic --alias haiku=anthropic/claude-haiku-4-5

Existing file content and comments are preserved.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load(configPath()) // may not exist yet; nil is fine
		if cfg == nil {
			cfg = &config.Config{Defaults: config.Defaults{KeyTTLMs: config.DefaultKeyTTLMs}}
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		cx := config.Context{
			BaseURL: addFlags.baseURL, Model: addFlags.model,
			KeyPass: addFlags.keyPass, KeyCmd: addFlags.keyCmd, KeyEnv: addFlags.keyEnv,
			DisplayName: addFlags.displayName, Discovery: addFlags.discovery,
			ContextLimit: addFlags.contextLimit, OutputLimit: addFlags.outputLimit,
		}
		for _, a := range addFlags.aliases {
			fam, model, ok := strings.Cut(a, "=")
			if !ok || fam == "" || model == "" {
				return fmt.Errorf("--alias %q: want family=model", a)
			}
			if cx.ModelAliases == nil {
				cx.ModelAliases = map[string]string{}
			}
			cx.ModelAliases[fam] = model
		}
		var probe gatewayProbe
		nonInteractive := cmd.Flags().NFlag() > 0 || !ui.IsTTY()
		if !nonInteractive {
			if err := addForm(cfg, &name, &cx, &probe); err != nil {
				return err
			}
		}
		if name == "" {
			if !ui.IsTTY() {
				return errors.New("no terminal for the interactive form; pass a name and --base-url/--model/--key-* flags")
			}
			return errors.New("context name is required")
		}
		if err := config.AppendContext(configPath(), name, cx); err != nil {
			return err
		}
		if opencodeInstalled() {
			saveProbe(name, cx, probe.models, probe.limits)
		}
		fmt.Printf("%s added context %s to %s\n", ui.OK("✓"), name, configPath())
		fmt.Println(ui.Muted("next: claude-ctx doctor " + name + "  ·  claude-ctx use " + name))
		return nil
	},
}

// gatewayProbe is what the interactive form learned from the gateway.
type gatewayProbe struct {
	models []string
	limits map[string]gateway.Limits
}

// addForm drives the interactive flow. It mutates name/cx/probe in place.
func addForm(cfg *config.Config, name *string, cx *config.Context, probe *gatewayProbe) error {
	// --- Step 1: name + gateway ---
	step1 := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Context name").Placeholder("opus-b").Value(name).
			Validate(func(s string) error {
				switch {
				case !config.ValidName(s):
					return errors.New("letters, digits, . _ - only")
				case cfg.Contexts[s].BaseURL != "":
					return errors.New("already exists")
				}
				return nil
			}),
		huh.NewInput().Title("Gateway base URL").Placeholder("https://litellm.example.internal").Value(&cx.BaseURL).
			Validate(func(s string) error {
				if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
					return errors.New("must start with http:// or https://")
				}
				return nil
			}),
	))
	if err := step1.Run(); err != nil {
		return err
	}

	// --- Step 2: credential source ---
	entries, _ := passstore.List(passstore.Dir(cfg.Defaults.StoreDir), cfg.Defaults.Namespace)
	source := "pass"
	if len(entries) == 0 {
		source = "env"
	}
	srcOpts := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("pass entry (%d found under %q)", len(entries), cfg.Defaults.Namespace), "pass"),
		huh.NewOption("environment variable", "env"),
		huh.NewOption("shell command", "cmd"),
	}
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Credential source").Options(srcOpts...).Value(&source),
	)).Run(); err != nil {
		return err
	}
	switch source {
	case "pass":
		const other = "\x00other"
		opts := make([]huh.Option[string], 0, len(entries)+1)
		for _, e := range entries {
			opts = append(opts, huh.NewOption(e, e))
		}
		opts = append(opts, huh.NewOption("(type a path…)", other))
		choice := other
		if len(entries) > 0 {
			choice = entries[0]
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("pass entry").Options(opts...).Filtering(true).Value(&choice),
		)).Run(); err != nil {
			return err
		}
		if choice == other {
			if err := huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("pass path").Placeholder("work/litellm/b/anthropic").Value(&cx.KeyPass).Validate(nonEmpty),
			)).Run(); err != nil {
				return err
			}
		} else {
			cx.KeyPass = choice
		}
	case "env":
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Environment variable name").Placeholder("LITELLM_KEY").Value(&cx.KeyEnv).Validate(nonEmpty),
		)).Run(); err != nil {
			return err
		}
	case "cmd":
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Shell command that prints the key").Placeholder("op read op://work/x/credential").Value(&cx.KeyCmd).Validate(nonEmpty),
		)).Run(); err != nil {
			return err
		}
	}

	// --- Step 3: model, offered from the gateway when we can reach it ---
	var models []string
	if !addFlags.noVerify {
		fmt.Fprintln(os.Stderr, ui.Muted("checking credential and querying "+cx.BaseURL+"/v1/models…"))
		models, probe.limits = probeGateway(cfg, *cx)
		probe.models = models
	}
	if len(models) > 0 {
		const other = "\x00other"
		opts := make([]huh.Option[string], 0, len(models)+1)
		for _, m := range models {
			opts = append(opts, huh.NewOption(m, m))
		}
		opts = append(opts, huh.NewOption("(type a model id…)", other))
		choice := models[0]
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title(fmt.Sprintf("Model (%d on gateway)", len(models))).Options(opts...).Filtering(true).Value(&choice),
		)).Run(); err != nil {
			return err
		}
		if choice != other {
			cx.Model = choice
		}
	}
	if cx.Model == "" {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Model id").Placeholder("anthropic/claude-opus-5").Value(&cx.Model).Validate(nonEmpty),
		)).Run(); err != nil {
			return err
		}
	}

	// --- Step 3b: token limits for opencode, only when the gateway can't say ---
	if wantLimitPrompt(opencodeInstalled(), *cx, probe.limits) {
		var ctxIn, outIn string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Context limit in tokens (opencode; blank = opencode default)").Placeholder("128000").
				Value(&ctxIn).Validate(func(s string) error { _, err := parseLimitInput(s); return err }),
			huh.NewInput().Title("Output limit in tokens (blank = opencode default)").Placeholder("8192").
				Value(&outIn).Validate(func(s string) error { _, err := parseLimitInput(s); return err }),
		)).Run(); err != nil {
			return err
		}
		cx.ContextLimit, _ = parseLimitInput(ctxIn)
		cx.OutputLimit, _ = parseLimitInput(outIn)
	}

	// --- Step 4: optional aliases ---
	wantAliases := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Repoint /model opus|sonnet|haiku to this gateway?").
			Description("Background tasks use the haiku slot; aliasing keeps every call on this deployment and key.").
			Value(&wantAliases),
	)).Run(); err != nil {
		return err
	}
	if wantAliases {
		cx.ModelAliases = map[string]string{}
		for _, fam := range []string{"opus", "sonnet", "haiku"} {
			v, err := pickModel(fmt.Sprintf("Model for %q alias (empty to skip)", fam), models, cx.Model)
			if err != nil {
				return err
			}
			if v != "" {
				cx.ModelAliases[fam] = v
			}
		}
		if len(cx.ModelAliases) == 0 {
			cx.ModelAliases = nil
		}
	}

	// --- Step 5: optional display name ---
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Display name for /model (optional)").Placeholder("Opus 5 (deployment B)").Value(&cx.DisplayName),
	)).Run(); err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(config.RenderContext(*name, *cx))
	fmt.Println()
	confirm := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Append to " + configPath() + "?").Value(&confirm),
	)).Run(); err != nil {
		return err
	}
	if !confirm {
		return errors.New("aborted")
	}
	return nil
}

// pickModel offers gateway models (if any) or a free-text input.
func pickModel(title string, models []string, def string) (string, error) {
	if len(models) == 0 {
		v := ""
		err := huh.NewForm(huh.NewGroup(huh.NewInput().Title(title).Placeholder(def).Value(&v))).Run()
		return v, err
	}
	const skip = "\x00skip"
	opts := []huh.Option[string]{huh.NewOption("(skip)", skip)}
	for _, m := range models {
		opts = append(opts, huh.NewOption(m, m))
	}
	choice := def
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Options(opts...).Filtering(true).Value(&choice),
	)).Run()
	if choice == skip {
		choice = ""
	}
	return choice, err
}

// probeGateway resolves the key and asks the gateway for its models and (if it
// is LiteLLM) their token limits. Failures are warnings; results may be empty.
func probeGateway(cfg *config.Config, cx config.Context) ([]string, map[string]gateway.Limits) {
	r, err := cred.ForContext(cx, cfg.Defaults)
	if err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key, err := r.Resolve(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Warn("credential check failed: "+strings.ReplaceAll(err.Error(), "\n", " ")))
		return nil, nil
	}
	gctx, gcancel := context.WithTimeout(context.Background(), 2*gateway.DefaultTimeout)
	defer gcancel()
	models, err := gateway.ListModels(gctx, cx.BaseURL, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Warn("gateway probe failed: "+err.Error()))
	}
	limits, _ := gateway.ModelInfo(gctx, cx.BaseURL, key) // optional: LiteLLM only
	return models, limits
}

// wantLimitPrompt: ask for token limits only for opencode users, and only when
// neither a flag nor the gateway's /model/info supplied them.
func wantLimitPrompt(opencodeInstalled bool, cx config.Context, limits map[string]gateway.Limits) bool {
	return opencodeInstalled && cx.ContextLimit == 0 && cx.OutputLimit == 0 && limits[cx.Model] == (gateway.Limits{})
}

// parseLimitInput parses an optional token limit typed into the form.
func parseLimitInput(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("a positive number of tokens, or blank")
	}
	return n, nil
}

// saveProbe records what probeGateway learned in the metadata cache.
func saveProbe(name string, cx config.Context, models []string, limits map[string]gateway.Limits) {
	if len(models) == 0 && len(limits) == 0 {
		return
	}
	cache, err := metacache.Load(cachePath())
	if err != nil {
		return
	}
	fresh := metacache.Entry{Updated: time.Now().UTC(), Limits: limits}
	if cx.Discovery {
		fresh.Models = models
	}
	cache.Contexts[name] = metacache.Merge(cache.Contexts[name], fresh)
	if err := cache.Save(cachePath()); err != nil {
		fmt.Fprintln(os.Stderr, ui.Warn("warning: could not save metadata cache: "+err.Error()))
	}
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("required")
	}
	return nil
}

func init() {
	f := addCmd.Flags()
	f.StringVar(&addFlags.baseURL, "base-url", "", "gateway base URL")
	f.StringVar(&addFlags.model, "model", "", "model id")
	f.StringVar(&addFlags.keyPass, "key-pass", "", "pass entry path")
	f.StringVar(&addFlags.keyCmd, "key-cmd", "", "shell command printing the key")
	f.StringVar(&addFlags.keyEnv, "key-env", "", "environment variable holding the key")
	f.StringVar(&addFlags.displayName, "display-name", "", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME")
	f.BoolVar(&addFlags.discovery, "discovery", false, "enable gateway model discovery")
	f.Int64Var(&addFlags.contextLimit, "context-limit", 0, "opencode context window (0 = from gateway /model/info)")
	f.Int64Var(&addFlags.outputLimit, "output-limit", 0, "opencode max output tokens (0 = from gateway /model/info)")
	f.StringArrayVar(&addFlags.aliases, "alias", nil, "family=model (repeatable; family ∈ opus,sonnet,haiku,fable)")
	f.BoolVar(&addFlags.noVerify, "no-verify", false, "interactive: skip the credential/gateway probe")
	rootCmd.AddCommand(addCmd)
}
