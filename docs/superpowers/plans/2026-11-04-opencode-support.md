# opencode Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let every claude-ctx context drive opencode v2 as well as Claude Code (`use`, `run`, `current`, `unset`, `doctor`, `add`), with no secret ever written to disk.

**Architecture:** An opencode plugin embedded in the Go binary defines one provider per context (`ctx-<name>`) from `claude-ctx opencode providers --json` and injects `x-api-key` on every model request via the `model.request` hook, fetching it from `claude-ctx credential --context`. Foreground commands refresh a secret-free metadata cache (discovered models, `/model/info` limits). `use` edits only `model`/`small_model` in opencode's JSONC config (comment-preserving) and records what it wrote in a claude-ctx state file. `run` execs `opencode --standalone` with `OPENCODE_CONFIG_CONTENT`.

**Tech Stack:** Go 1.26 (cobra, go-toml/v2, new: `github.com/tailscale/hujson`), TypeScript plugin run by opencode (Bun), `bun test`.

**Spec:** `docs/design/opencode.md`

## Global Constraints

- No secret is written to any file, by Go or by the plugin. The plugin holds keys in memory only.
- opencode ≥ 2.0.0 only. `opencode --version` prints `opencode v2.2.5`; parse the first `vMAJOR.MINOR.PATCH`.
- Provider id = `ctx-` + context name. Model ref = `ctx-<name>/<model>`.
- Provider `baseURL` = context `base_url` with trailing `/` trimmed, plus `/v1`. Provider `package` `@opencode/ai/providers/anthropic` (verified in Task 0). Auth header `x-api-key`.
- Limit precedence: TOML `context_limit`/`output_limit` → `/model/info` → omit (opencode default 200000/32000).
- Files: cache `<config dir>/cache.json`, state `<config dir>/state.json` (config dir = directory of the active `config.toml`), both mode 0600, written atomically.
- opencode config dir: `$XDG_CONFIG_HOME/opencode`, else `~/.config/opencode`; overridable with the new global flag `--opencode-dir` (testing). Config file: `opencode.jsonc` if it exists, else `opencode.json`.
- Plugin path: `<opencode dir>/plugins/claude-ctx.ts`. First line: `// claude-ctx-plugin-version: <version>`.
- Credential fetch timeout in the plugin: 20s. Failure message: `credential for <ctx> unavailable — run \`claude-ctx doctor <ctx>\` (gpg-agent cache may be cold)`.
- `current --plain` output must not change.
- Existing Claude Code behavior of every command must not change when opencode is not installed.
- One commit per task, on branch `opencode-support`; one PR at the end.

## Review Focus

1. `opencode.jsonc` with comments and trailing commas: `use`/`unset` leave every byte outside the edited keys unchanged (Task 5).
2. No opencode config file at all: `use` creates `opencode.json` with `$schema` plus the keys; `unset` removes the keys and keeps the file (Task 5).
3. Gateway without `/model/info` (404 on both paths): cache refresh warns, `use`/`run` still succeed, limits fall back (Task 3).
4. `opencode providers --json` with a missing or invalid `config.toml`: nonzero exit, error on stderr, empty stdout; plugin setup does not throw and registers no providers (Tasks 4, 6).
5. Many concurrent model requests on a cold key cache: exactly one credential process runs (Task 6).

---

### Task 0: Verify the four source-reading assumptions

Manual spike against the installed opencode v2.2.5 and one real gateway context. No production code. **If any check fails, stop and report to the user before Task 1** — the design depends on all four.

**Files:**
- Scratch only (scratchpad dir): a throwaway plugin `spike.ts`.
- Modify: `docs/design/opencode.md` (append a `## Verification` section).

- [ ] **Step 1:** Write a scratch plugin to `~/.config/opencode/plugins/spike.ts`: default export plain object `{ id: "spike", setup }`, `import type` only. In `setup`, `ctx.provider.transform` adds provider `ctx-spike` (package `@ai-sdk/anthropic`, `settings.baseURL` = `<gateway>/v1`, no apiKey) with one model whose id contains `/` (e.g. `anthropic/claude-opus-5`); `ctx.session.hook("model.request", r => { r.headers["x-api-key"] = process.env.SPIKE_KEY })`. Use the exact plugin API shapes from opencode source `packages/plugin/src/promise/{plugin,provider,session}.ts` (clone at `anomalyco/opencode@0d226dac`).
- [ ] **Step 2:** Run `SPIKE_KEY=$(pass <entry>) OPENCODE_CONFIG_CONTENT='{"model":"ctx-spike/anthropic/claude-opus-5"}' opencode --standalone run "say ok"`.
  Expected: a model reply. This verifies assumptions 1 (plugin loads, header reaches gateway), 2 (Anthropic protocol vs LiteLLM), 3 (`--standalone` reads env) and 4 (`/` in model id).
- [ ] **Step 3:** Confirm `--standalone` is accepted before the subcommand and with no subcommand (TUI): `opencode --standalone --help`.
- [ ] **Step 4:** Record the exact working plugin API calls (provider `add` shape, `limit` field, `reload`, hook names) and the outcome of each assumption in `docs/design/opencode.md` under `## Verification`. Delete `spike.ts`.
- [ ] **Step 5: Commit**

```bash
git add docs/design/opencode.md docs/superpowers/plans/2026-11-04-opencode-support.md
git commit -m "docs: opencode support design, plan, and verification"
```

---

### Task 1: Config fields and `add` flags

**Files:**
- Modify: `internal/config/config.go`, `internal/config/append.go`, `cmd/add.go`, `testdata/config/valid.toml`
- Test: `internal/config/config_test.go`, `internal/config/append_test.go`

**Interfaces:**
- Produces: `Context.ContextLimit int64` (`toml:"context_limit"`), `Context.OutputLimit int64` (`toml:"output_limit"`), `Defaults.Client string` (`toml:"client"`), `const ClientClaude = "claude"`, `const ClientOpencode = "opencode"`.

- [ ] **Step 1: Write failing tests**
  - `TestValidateLimits`: `context_limit = -1` → error containing `context_limit must be >= 0`; same for `output_limit`.
  - `TestValidateClient`: `defaults.client = "vim"` → error containing `defaults.client must be "claude" or "opencode"`; `""`, `"claude"`, `"opencode"` valid.
  - `TestParseLimits`: `valid.toml` gains `context_limit = 128000` / `output_limit = 8192` on `local`; parsed values equal.
  - `TestRenderContextLimits`: `RenderContext` output contains `context_limit = 128000` and `output_limit = 8192` when > 0, and neither line when 0.
- [ ] **Step 2:** Run `go test ./internal/config/` → FAIL.
- [ ] **Step 3:** Add the fields, validation, `RenderContext` lines (after `discovery`), and template comment lines `# context_limit = 200000   # opencode only; else from gateway /model/info` and `# client = "claude"         # default for run --client` under `[defaults]`. In `cmd/add.go` add `--context-limit` and `--output-limit` (`Int64Var`) mapped onto the context.
- [ ] **Step 4:** Run `go test ./internal/config/ && go build ./...` → PASS.
- [ ] **Step 5: Commit** `feat(config): context_limit, output_limit, defaults.client`

---

### Task 2: `credential --context`

**Files:**
- Modify: `cmd/credential.go`
- Create test: `cmd/credential_test.go`

**Interfaces:**
- Produces: `func credentialContextName(flag, env, sentinel string) string` in `cmd/credential.go`; CLI `claude-ctx credential --context <name>`.

- [ ] **Step 1: Write failing test** `TestCredentialContextName` (table): `("a","b","c")→"a"`, `("","b","c")→"b"`, `("","","c")→"c"`, `("","","")→""`.
- [ ] **Step 2:** Run `go test ./cmd/ -run TestCredentialContextName` → FAIL.
- [ ] **Step 3:** Implement; the settings file is read only when flag and env are both empty. Register `--context` with `completeContexts`-style completion.
- [ ] **Step 4:** Run `go test ./cmd/` → PASS.
- [ ] **Step 5: Commit** `feat(credential): --context flag for the opencode plugin`

---

### Task 3: `/model/info` probe and metadata cache

**Files:**
- Create: `internal/gateway/modelinfo.go`, `internal/metacache/metacache.go`
- Modify: `cmd/root.go` (prime/refresh helpers)
- Test: `internal/gateway/modelinfo_test.go`, `internal/metacache/metacache_test.go`

**Interfaces:**
- Consumes: `gateway.ListModels`, `config.Context`.
- Produces:
  - `type gateway.Limits struct { Context int64 \`json:"context"\`; Output int64 \`json:"output"\` }`
  - `func gateway.ModelInfo(ctx context.Context, baseURL, key string) (map[string]Limits, error)`
  - `type metacache.Entry struct { Models []string \`json:"models,omitempty"\`; Limits map[string]gateway.Limits \`json:"limits,omitempty"\`; Updated time.Time \`json:"updated"\` }`
  - `type metacache.Cache struct { Contexts map[string]Entry \`json:"contexts"\` }`
  - `func metacache.PathFor(configPath string) string`, `func metacache.Load(path string) (*Cache, error)`, `func (c *Cache) Save(path string) error`
  - `func metacache.Refresh(ctx context.Context, cx config.Context, key string) (Entry, error)`
  - `cmd`: `func primeKey(c *config.Config, name string, timeout time.Duration) (string, error)` (existing `prime` wraps it and discards the key); `func refreshMeta(c *config.Config, name, key string)` (never fails; warns on stderr).

- [ ] **Step 1: Write failing tests** (httptest servers):
  - `TestModelInfoLiteLLM`: `/v1/model/info` returns `{"data":[{"model_name":"m1","model_info":{"max_input_tokens":128000,"max_output_tokens":8192}},{"model_name":"m2","model_info":{"max_tokens":200000}}]}` → `m1:{128000,8192}`, `m2:{200000,0}`. Request carries `x-api-key` and `Authorization: Bearer`.
  - `TestModelInfoFallbackPath`: `/v1/model/info` 404, `/model/info` 200 → parsed.
  - `TestModelInfoUnsupported`: both 404 → error; `errors.Is(err, gateway.ErrNoModelInfo)`.
  - `TestRefreshPartial`: discovery on, `/v1/models` OK, model info 404 → `Entry.Models` populated, `Limits` empty, error non-nil.
  - `TestRefreshNoDiscovery`: discovery off → `/v1/models` never requested.
  - `TestCacheRoundTrip`: Save then Load equal; file mode 0600; Load of a missing file → empty cache, nil error.
- [ ] **Step 2:** Run `go test ./internal/gateway/ ./internal/metacache/` → FAIL.
- [ ] **Step 3:** Implement. `ModelInfo` tries `<base>/v1/model/info` then `<base>/model/info`; context = `max_input_tokens`, else `max_tokens`. `Refresh` returns whatever succeeded plus `errors.Join` of failures. `refreshMeta` loads the cache, merges successful parts over the old entry (a failed lookup keeps the previous value), saves, prints one `warning:` line per failure.
- [ ] **Step 4:** Run `go test ./... -race` → PASS.
- [ ] **Step 5: Commit** `feat: gateway /model/info probe and secret-free metadata cache`

---

### Task 4: Provider model and `opencode providers`

**Files:**
- Create: `internal/opencode/providers.go`, `cmd/opencode.go`, `testdata/opencode/providers.golden.json`
- Modify: `cmd/root.go` (global `--opencode-dir` flag, `opencodeDir()` helper)
- Test: `internal/opencode/providers_test.go`

**Interfaces:**
- Consumes: `config.Config`, `metacache.Cache`, `gateway.Limits`.
- Produces:
  - `const opencode.ProviderPrefix = "ctx-"`, `func opencode.ProviderID(name string) string`, `func opencode.ModelRef(name, model string) string`
  - `func opencode.Refs(name string, cx config.Context) (model, small string)` — `small` is `""` with no haiku alias.
  - `type opencode.Limit struct { Context int64 \`json:"context"\`; Output int64 \`json:"output"\` }`
  - `type opencode.Model struct { ID string \`json:"id"\`; Name string \`json:"name"\`; Limit *Limit \`json:"limit,omitempty"\` }`
  - `type opencode.Provider struct { ID, Context, Name, BaseURL string; KeyTTLMs int64; Models []Model; DefaultModel, SmallModel string }` with JSON keys `id, context, name, baseURL, keyTtlMs, models, defaultModel, smallModel(omitempty)`
  - `func opencode.Build(c *config.Config, cache *metacache.Cache) []Provider` (sorted by context name)
  - `type opencode.Output struct { ConfigPath string \`json:"configPath"\`; CachePath string \`json:"cachePath"\`; Providers []Provider \`json:"providers"\` }` — the exact `--json` document; the plugin watches both paths.
  - `func opencode.DefaultDir() string` (`$XDG_CONFIG_HOME/opencode`, else `~/.config/opencode`)
  - `cmd`: `func opencodeDir() string` (`--opencode-dir` flag, else `opencode.DefaultDir()`); CLI `claude-ctx opencode providers [--json]` (table without `--json`: ID, MODELS, LIMIT SOURCE).

- [ ] **Step 1: Write failing tests**
  - `TestBuildGolden`: config with `opus-b` (aliases opus/haiku, display_name, `context_limit=150000`), `local` (discovery on), cache with `local` models `[q1,q2]` and limits `{q1:{32768,4096}}` and `opus-b` limits for its model `{200000,64000}` → equals `testdata/opencode/providers.golden.json`. The golden file must show: base URL `https://litellm-b.internal/v1`; models deduped in order model, aliases (sorted by family), discovered; `display_name` only on the primary model; `opus-b` limit context 150000 (TOML wins) output 64000 (from cache); `q2` no `limit`; `smallModel` `ctx-opus-b/<haiku>`.
  - `TestRefsNoHaiku`: no haiku alias → small `""`.
  - `TestBaseURLTrailingSlash`: `base_url = "https://x/"` → `https://x/v1`.
- [ ] **Step 2:** Run `go test ./internal/opencode/` → FAIL.
- [ ] **Step 3:** Implement. The `providers` command uses `config.Load` + `Validate` (warnings to stderr), prints nothing to stdout on error, and never resolves a credential or touches the network.
- [ ] **Step 4:** Run `go test ./... -race`, then `go run . --config testdata/config/valid.toml opencode providers --json | head` → valid JSON; `go run . --config /nonexistent opencode providers --json; echo $?` → empty stdout, exit 1.
- [ ] **Step 5: Commit** `feat(opencode): provider model and providers command`

---

### Task 5: opencode config editor and state file

**Files:**
- Create: `internal/opencode/occonfig.go`, `internal/state/state.go`, `testdata/opencode/*.jsonc` fixtures + goldens
- Modify: `go.mod`/`go.sum` (`github.com/tailscale/hujson`)
- Test: `internal/opencode/occonfig_test.go`, `internal/state/state_test.go`

**Interfaces:**
- Produces:
  - `func opencode.ConfigFile(dir string) string`
  - `const opencode.KeyModel = "model"`, `const opencode.KeySmallModel = "small_model"`
  - `func opencode.ReadKeys(path string, keys []string) (map[string]string, error)` (missing file/key → absent from map)
  - `func opencode.SetKeys(path string, set map[string]string, remove []string) error`
  - `type state.Opencode struct { Context string \`json:"context"\`; Path string \`json:"path"\`; Values map[string]string \`json:"values"\` }`
  - `type state.State struct { Opencode *Opencode \`json:"opencode,omitempty"\` }`
  - `func state.PathFor(configPath string) string`, `func state.Load(path string) (*State, error)`, `func (s *State) Save(path string) error`
  - `func (o *state.Opencode) Drift(current map[string]string) bool`

- [ ] **Step 1: Write failing tests**
  - `TestSetKeysPreservesComments`: fixture `commented.jsonc` (line + block comments, trailing comma, unrelated `provider` and `plugin` keys, existing `"model"`) → after `SetKeys(model=ctx-a/m, small_model=ctx-a/h)` equals `commented.golden.jsonc`; the only changed lines are those two keys.
  - `TestSetKeysRemove`: removing `small_model` deletes only that member, comments intact.
  - `TestSetKeysCreatesFile`: missing path → file `{"$schema": "https://opencode.ai/config.json", "model": …}`, mode 0600.
  - `TestSetKeysBackup`: existing file copied to `<path>.claude-ctx.bak` before write.
  - `TestConfigFilePrefersJSONC`: both files exist → `.jsonc`; neither → `opencode.json`.
  - `TestDrift`: recorded `{model:a}` vs current `{model:a}` → false; `{model:b}` → true; `{}` → true.
  - `TestStateRoundTrip`: Save/Load equal, mode 0600, missing file → empty state.
- [ ] **Step 2:** Run `go test ./internal/opencode/ ./internal/state/` → FAIL.
- [ ] **Step 3:** Implement `SetKeys` with `hujson.Parse` + `Value.Patch` (RFC 6902 `add`/`remove` on `/<key>`) and serialize with `Value.Pack()` (preserves comments and whitespace; never `Standardize`/`Format`). Atomic write mirrors `settings.Write` (temp file + rename, keep mode).
- [ ] **Step 4:** Run `go test ./... -race` → PASS.
- [ ] **Step 5: Commit** `feat(opencode): comment-preserving config editor and state file`

---

### Task 6: The plugin, `opencode install` / `uninstall`

**Files:**
- Create: `internal/opencode/plugin/claude-ctx.ts`, `internal/opencode/plugin/claude-ctx.test.ts`, `internal/opencode/plugin.go`
- Modify: `cmd/opencode.go`, `Makefile`
- Test: `internal/opencode/plugin_test.go`

**Interfaces:**
- Consumes: Task 0's recorded plugin API; `claude-ctx [--config X] opencode providers --json` (Task 4); `claude-ctx credential --context <name>` (Task 2).
- Produces:
  - Go: `func opencode.PluginPath(dir string) string`, `func opencode.PluginSource(bin, version string, extraArgs []string) []byte`, `func opencode.InstalledPluginVersion(path string) (string, bool)`, `func opencode.Version(ctx context.Context, bin string) (major int, raw string, err error)`
  - Placeholders in the `.ts` (replaced as JSON literals): `__CLAUDE_CTX_BIN__` (string), `__CLAUDE_CTX_VERSION__` (string), `__CLAUDE_CTX_ARGS__` (string array, e.g. `["--config","/abs/config.toml"]` or `[]`).
  - TS named exports: `contextFor(providerID: string): string | undefined`; `class KeyCache { constructor(fetchKey: (ctx: string) => Promise<string>, ttlMs: (ctx: string) => number, now?: () => number); get(ctx: string): Promise<string>; evict(ctx: string): void }`; `loadProviders(run: (args: string[]) => Promise<string>): Promise<Provider[]>` (returns `[]` and logs on any failure). Default export: plain `{ id: "claude-ctx", setup }`, type-only imports.
  - CLI: `claude-ctx opencode install [--force]`, `claude-ctx opencode uninstall`.

- [ ] **Step 1: Write failing tests**
  - Bun `claude-ctx.test.ts`: `contextFor("ctx-opus-b") === "opus-b"`, `contextFor("anthropic") === undefined`; `KeyCache` with a fake clock: second `get` within TTL does not call `fetchKey`, after TTL does; `evict` forces a refetch; 10 concurrent `get`s on a cold cache → `fetchKey` called once; a rejected fetch is not cached. `loadProviders` with a `run` that rejects → `[]`, no throw.
  - Go `TestPluginSourcePlaceholders`: output contains no `__CLAUDE_CTX_`, first line is `// claude-ctx-plugin-version: 1.2.3`, a bin path with a space and a quote is a valid JSON string.
  - Go `TestInstalledPluginVersion`: parses that first line; missing file → `false`.
  - Go `TestParseOpencodeVersion`: `"opencode v2.2.5\n"` → 2; `"1.9.0"` → 1; garbage → error.
- [ ] **Step 2:** Run `cd internal/opencode/plugin && bun test` and `go test ./internal/opencode/` → FAIL.
- [ ] **Step 3:** Implement the plugin with the API calls recorded in Task 0:
  - `setup`: `loadProviders` by spawning `[BIN or "claude-ctx" from PATH, ...ARGS, "opencode", "providers", "--json"]` (try PATH `claude-ctx` first, fall back to `BIN`) and parsing `opencode.Output`; `ctx.provider.transform` adds each provider with the exact `info`/model shapes recorded in the design doc's Verification section; `fs.watch` `configPath` and `cachePath` → re-run and `ctx.provider.reload()`, debounced 200ms.
  - `model.request` hook: `contextFor(model.providerID)`; if set, `headers["x-api-key"] = await cache.get(ctx)`. `fetchKey` spawns `credential --context <ctx>` with a 20s timeout; on failure throw `Error` with the exact Global Constraints message.
  - `http.response` hook: status 401 on a `ctx-*` provider → `cache.evict(ctx)`. The whole body is wrapped in try/catch — a throw here hangs opencode (Task 0).
  - Go `install`: check `opencode.Version` ≥ 2 (else error `opencode <raw> is not supported; claude-ctx needs opencode v2 or later`), write `PluginSource(selfPath(), version, configArgs)` to `PluginPath(opencodeDir())` mode 0644 (refuse to overwrite a file whose first line isn't the version header unless `--force`). `uninstall` removes it if the header matches.
  - Makefile `test`: after `go test`, `@if command -v bun >/dev/null 2>&1; then cd internal/opencode/plugin && bun test; else echo "bun not found; skipping plugin tests"; fi`.
- [ ] **Step 4:** Run `make test` → PASS. Then `go run . opencode install --opencode-dir "$(mktemp -d)"` → file written with the header.
- [ ] **Step 5: Commit** `feat(opencode): embedded key-injecting plugin with install/uninstall`

---

### Task 7: `run --client opencode`

**Files:**
- Modify: `cmd/run.go`
- Create test: `cmd/run_test.go`

**Interfaces:**
- Consumes: `opencode.Refs`, `opencode.PluginPath`, `primeKey`, `refreshMeta`, `config.ClientOpencode`.
- Produces: `func resolveClient(flag string, d config.Defaults) (string, error)`; `func opencodeLaunch(bin, name string, cx config.Context, rest []string) (argv []string, env []string)` (env = only the entries to add); flags `--client`, `--opencode-bin` (env `CLAUDE_CTX_OPENCODE_BIN`).

- [ ] **Step 1: Write failing tests**
  - `TestResolveClient`: flag beats defaults; empty both → `claude`; `"vim"` → error.
  - `TestOpencodeLaunch`: argv `["/bin/opencode","--standalone","run","hi"]` for rest `["run","hi"]`; env exactly `OPENCODE_CONFIG_CONTENT={"model":"ctx-opus-b/anthropic/claude-opus-5","small_model":"ctx-opus-b/anthropic/claude-haiku-4-5"}`; no `small_model` key without a haiku alias.
- [ ] **Step 2:** Run `go test ./cmd/ -run 'TestResolveClient|TestOpencodeLaunch'` → FAIL.
- [ ] **Step 3:** Implement. For opencode: error `opencode plugin not installed; run \`claude-ctx opencode install\`` if `PluginPath` is missing; `primeKey` → `refreshMeta` → child env = parent env minus any `OPENCODE_CONFIG_CONTENT`, plus `CLAUDE_CTX_CONFIG=<abs>` and the launch env; `--dry-run` prints argv and the added env. Claude path unchanged.
- [ ] **Step 4:** Run `go test ./... -race`; `go run . run opus-b --client opencode --dry-run` against a test config → expected argv/env.
- [ ] **Step 5: Commit** `feat(run): --client opencode via --standalone and OPENCODE_CONFIG_CONTENT`

---

### Task 8: `use --client`, `unset`, `current` for opencode

**Files:**
- Modify: `cmd/use.go`, `cmd/unset.go`, `cmd/current.go`
- Create: `cmd/opencode_apply.go`, test `cmd/opencode_apply_test.go`

**Interfaces:**
- Consumes: `opencode.ConfigFile`, `ReadKeys`, `SetKeys`, `Refs`, `PluginPath`; `state.*`.
- Produces: `func targetClients(flag string, opencodeInstalled bool) ([]string, error)`; `func applyOpencode(statePath, ocDir, name string, cx config.Context, force bool) error`; `func removeOpencode(statePath string) (prevCtx string, had bool, err error)`; `func opencodeCurrent(statePath string) (name string, drift bool)`.

- [ ] **Step 1: Write failing tests** (temp dirs):
  - `TestTargetClients`: `("",false)→[claude]`, `("",true)→[claude opencode]`, `("opencode",false)→` error containing `claude-ctx opencode install`, `("claude",true)→[claude]`.
  - `TestApplyOpencode`: writes `model`/`small_model`, state records context, path and values.
  - `TestApplyOpencodeDrift`: after apply, hand-edit `model` → second apply without force errors with `modified outside claude-ctx`; with force succeeds.
  - `TestApplyOpencodeDropsSmallModel`: switching to a context without haiku removes a previously managed `small_model`.
  - `TestRemoveOpencode`: removes managed keys, keeps the file and unrelated keys, clears state; second call → `had=false`.
- [ ] **Step 2:** Run `go test ./cmd/ -run Opencode` → FAIL.
- [ ] **Step 3:** Implement. `use`: compute targets, check drift for **every** target before writing any, prime once, `refreshMeta` when opencode is a target, apply each, print one `✓` line per client. `unset`: also `removeOpencode`. `current` (non-plain only): after the existing output, print `opencode: <ctx>` with `*` on drift when state has an opencode entry.
- [ ] **Step 4:** Run `go test ./... -race` → PASS. Manual: `use` → `current` → `unset` against `--settings-path`/`--opencode-dir` temp paths.
- [ ] **Step 5: Commit** `feat(use): switch opencode alongside Claude Code; unset and current aware`

---

### Task 9: `doctor` checks and interactive limits in `add`

**Files:**
- Modify: `cmd/doctor.go`, `cmd/add.go`
- Create test: `cmd/doctor_opencode_test.go`

**Interfaces:**
- Consumes: `opencode.Version`, `InstalledPluginVersion`, `PluginPath`, `ReadKeys`, `Build`, `metacache.*`, `gateway.ModelInfo`.
- Produces: `func opencodeChecks(c *config.Config, ocDir, opencodeBin string) []check`; `func limitSource(cx config.Context, e metacache.Entry) string` returning `"toml"`, `"model-info"` or `"default"`.

- [ ] **Step 1: Write failing tests**
  - `TestOpencodeChecksNotInstalled`: no opencode binary → no checks (empty slice).
  - `TestOpencodeChecksStalePlugin`: plugin header `0.9.0`, binary version `1.0.0` → `statusWarn` with `re-run claude-ctx opencode install`.
  - `TestOpencodeChecksUnknownModel`: opencode config `model: ctx-gone/x` → `statusFail` naming `gone`.
  - `TestLimitSource`: TOML set → `toml`; only cache → `model-info`; neither → `default`.
- [ ] **Step 2:** Run `go test ./cmd/ -run 'OpencodeChecks|LimitSource'` → FAIL.
- [ ] **Step 3:** Implement the five checks (scope `opencode`): version ≥ 2; plugin installed and version equal to `version`; baked `BIN` path in the installed plugin exists and is executable (else warn if `claude-ctx` on PATH, fail otherwise); default `model` names an existing context (skip if not a `ctx-` ref); per context `limits` → `statusOK` for `toml`/`model-info`, `statusWarn` for `default`. Doctor also calls `refreshMeta` per context after its credential check. `add`: replace `probeModels` with `probeGateway(cfg, cx) ([]string, map[string]gateway.Limits)`; after the model is chosen, if no limits are known for it, ask optional "Context limit (blank = opencode default)" / "Output limit" inputs. After appending the context, save what `probeGateway` learned into the cache entry for the new context (same merge rule as `refreshMeta`).
- [ ] **Step 4:** Run `go test ./... -race` and `make lint` → PASS.
- [ ] **Step 5: Commit** `feat(doctor,add): opencode checks and limit prompts`

---

### Task 10: README and end-to-end check

**Files:**
- Modify: `README.md`, `docs/design/opencode.md` (status line)

- [ ] **Step 1:** README: add an `## opencode` section after `use vs run` (requirements: opencode v2+; `claude-ctx opencode install`; `use` switches both; `run <ctx> --client opencode`; `defaults.client`; how the key reaches opencode without disk; the cache file and why lookups happen in the foreground). Add `context_limit`/`output_limit` rows to the field table and `client` to the defaults example. Add `opencode install|uninstall|providers` and `credential --context` to Commands; add the opencode checks to the doctor description.
- [ ] **Step 2:** End-to-end with a real context, recording each result in the PR body checklist: `opencode install` → `use <ctx>` → `opencode run "say ok"` replies → `current` shows both → `run <ctx2> --client opencode -- run "say ok"` replies while `use` context is unchanged → rotate key in `pass` and wait out TTL → still replies → `unset` → opencode config back to pre-`use` keys → `doctor` all ✓ → `opencode uninstall`.
- [ ] **Step 3:** Run `make test && make lint` → PASS.
- [ ] **Step 4: Commit** `docs: opencode support in README`
