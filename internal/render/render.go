// Package render turns a Context into the environment block claude-ctx manages.
package render

import (
	"sort"
	"strconv"
	"strings"

	"github.com/dcbark01/claude-ctx/internal/config"
)

// HelperCommand is the apiKeyHelper string written into settings.
const HelperCommand = "claude-ctx credential"

// Env var names.
const (
	EnvBaseURL     = "ANTHROPIC_BASE_URL"
	EnvModel       = "ANTHROPIC_MODEL"
	EnvCustomOpt   = "ANTHROPIC_CUSTOM_MODEL_OPTION"
	EnvCustomName  = "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME"
	EnvDiscovery   = "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"
	EnvKeyTTL      = "CLAUDE_CODE_API_KEY_HELPER_TTL_MS"
	EnvStoreDir    = "PASSWORD_STORE_DIR"
	EnvActiveCtx   = "CLAUDE_CTX" // set by `run`, read by `credential`/`current`
	EnvAuthToken   = "ANTHROPIC_AUTH_TOKEN"
	EnvAPIKey      = "ANTHROPIC_API_KEY"
	aliasEnvPrefix = "ANTHROPIC_DEFAULT_"
	aliasEnvSuffix = "_MODEL"
)

// ShadowingEnvVars are ambient vars that override apiKeyHelper if present.
var ShadowingEnvVars = []string{EnvAuthToken, EnvAPIKey}

// AllManagedEnvKeys is the full set of env keys claude-ctx may ever write.
// `unset` and re-`use` scrub all of these regardless of the current context.
var AllManagedEnvKeys = func() []string {
	keys := []string{EnvBaseURL, EnvModel, EnvCustomOpt, EnvCustomName, EnvDiscovery, EnvKeyTTL, EnvStoreDir}
	for _, f := range config.AliasFamilies {
		keys = append(keys, AliasEnvKey(f))
	}
	sort.Strings(keys)
	return keys
}()

// AliasEnvKey maps a family ("opus") to its env var (ANTHROPIC_DEFAULT_OPUS_MODEL).
func AliasEnvKey(family string) string {
	return aliasEnvPrefix + strings.ToUpper(family) + aliasEnvSuffix
}

// Rendered is the output of Render.
type Rendered struct {
	Env map[string]string
}

// Keys returns the env keys sorted.
func (r Rendered) Keys() []string {
	keys := make([]string, 0, len(r.Env))
	for k := range r.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Render computes the env block for a context.
func Render(c config.Context, d config.Defaults) Rendered {
	env := map[string]string{
		EnvBaseURL: c.BaseURL,
		EnvModel:   c.Model,
		EnvKeyTTL:  strconv.FormatInt(d.KeyTTLMs, 10),
	}
	for fam, model := range c.ModelAliases {
		if model != "" {
			env[AliasEnvKey(fam)] = model
		}
	}
	if c.DisplayName != "" {
		env[EnvCustomOpt] = c.Model
		env[EnvCustomName] = c.DisplayName
	}
	if c.Discovery {
		env[EnvDiscovery] = "1"
	}
	if d.StoreDir != "" {
		env[EnvStoreDir] = d.StoreDir
	}
	return Rendered{Env: env}
}
