# opencode support

Status: implemented 2026-11-04 (branch `opencode-support`). Implementation plan: `docs/superpowers/plans/2026-11-04-opencode-support.md`.

## Goal

The same `[contexts.*]` in `~/.claude/context/config.toml` drive both Claude Code and
[opencode](https://opencode.ai) (v2+). The security model is unchanged: **no secret is
ever written to disk** — not to opencode's config, not to its credential store.

## Why a plugin

opencode has no `apiKeyHelper`. Its options all fail the no-secrets invariant:

| Mechanism | Problem |
| --- | --- |
| `opencode auth login` / `/credential` HTTP API | Plaintext JSON in `~/.local/share/opencode/opencode.db` (`credential.value`); `credential.list` returns raw keys over HTTP |
| `command` integration method | Runs once at connect, then persists the output as a plaintext key |
| `{env:VAR}` in config | Static for the process lifetime, no rotation; the key must already be in the env |

opencode v2 plugins can register a `model.request` session hook, which runs before every
model call (`primary`, `compaction`, `title`, `generate`) and may set `headers` and `baseURL`.
claude-ctx ships a plugin that sets `x-api-key` there, from `claude-ctx credential --context <name>`,
held only in opencode's process memory.

## Design

### Providers
- One opencode provider per context, id `ctx-<name>`, defined **by the plugin at startup** via
  `ctx.provider.transform`, from `claude-ctx opencode providers --json`. Nothing per-context is
  written into opencode config. The plugin watches `config.toml` and reloads providers on change.
- Wire protocol: Anthropic only — provider `package` `@opencode/ai/providers/anthropic` (the native
  package opencode maps `@ai-sdk/anthropic` to; plugin-added providers must use it directly),
  `baseURL = base_url + "/v1"`. Every context
  already proves its gateway speaks Anthropic for that model.

### Field mapping
| Context field | opencode |
| --- | --- |
| `model` | model `<model>` on `ctx-<name>`; default model ref `ctx-<name>/<model>` |
| `model_aliases.*` | additional models on the same provider (deduped) |
| `model_aliases.haiku` | also `small_model` (title generation) |
| `display_name` | model display name |
| `discovery = true` | every model from the gateway's `/v1/models` |
| `context_limit` / `output_limit` (new, optional) | model `limit` |

Limit precedence: explicit TOML value → gateway `/model/info` (LiteLLM) → opencode default (200k / 32k).

### Metadata cache
Discovery and `/model/info` need the key. They run **only in foreground commands**
(`use`, `run`, `doctor`, `add`), where pinentry can reach the terminal, and are stored in
`~/.claude/context/cache.json` (model lists and limits only — never secrets). The plugin and
`opencode providers` read config + cache and never resolve a key. Keys resolve lazily on a
context's first model request.

### Plugin
- Embedded in the Go binary; `claude-ctx opencode install` writes it to
  `<opencode config dir>/plugins/claude-ctx.ts` with the installing binary's absolute path and
  version baked in. `opencode uninstall` removes it.
- Calls `claude-ctx` from PATH, falling back to the baked absolute path.
- Key cache per context for `key_ttl_ms`; concurrent requests share one in-flight resolve.
  A 401 on a `ctx-*` provider evicts the cached key (no automatic retry).
- Credential fetch timeout 20s; failure surfaces as: `credential for <ctx> unavailable — run
  \`claude-ctx doctor <ctx>\` (gpg-agent cache may be cold)`.
- Never crashes opencode: if `providers --json` fails, it logs and registers nothing.

### Commands
- `credential --context <name>`: precedence flag → `$CLAUDE_CTX` → settings sentinel.
- `use [--client claude|opencode]`: with no flag, updates every installed client (Claude Code
  always; opencode when the plugin is installed). For opencode it sets `model` and (if there is
  a haiku alias) `small_model` in `opencode.jsonc` if present else `opencode.json`, preserving
  comments/formatting. What was written is recorded in `~/.claude/context/state.json`, not in
  opencode's file. External edits are detected against that record; `--force` overrides.
- `run --client opencode` (default from `defaults.client`, else `claude`): execs
  `opencode --standalone` with `OPENCODE_CONFIG_CONTENT={"model":…,"small_model":…}`. Zero file
  mutation. Requires the plugin to be installed.
- `current`: adds an `opencode: <ctx>[*]` line; `--plain` unchanged.
- `unset`: also reverts what `use` wrote to opencode config; leaves the plugin.
- `doctor` (when opencode is installed): opencode ≥ 2 on PATH; plugin installed and matches the
  binary's version; `claude-ctx` resolvable from opencode's PATH; opencode default `model` names
  an existing context; limits resolvable per context.
- `add`: `--context-limit` / `--output-limit`; interactive prompts only when `/model/info` has nothing.
- `opencode install | uninstall | providers [--json]`: new.

### Out of scope
OpenAI-compatible protocol per context; opencode v1; renaming the tool.

## Unverified assumptions (checked first in the plan)
These come from reading opencode's source (`anomalyco/opencode@0d226dac`), not from running it:
1. A `.ts` file in `~/.config/opencode/plugins/` loads without a build step, as a plain
   `{ id, setup }` default export with type-only imports, and `ctx.session.hook("model.request")`
   header changes reach the gateway.
2. `@ai-sdk/anthropic` provider works against LiteLLM's `/v1/messages` with `x-api-key`.
3. `opencode --standalone` reads `OPENCODE_CONFIG_CONTENT` from the launching environment.
4. Model refs whose model id contains `/` (`ctx-opus-b/anthropic/claude-opus-5`) split at the first `/`.

## Verification (2026-11-04, opencode v2.2.5)

Run against a local mock Anthropic gateway (fake key) with an isolated `XDG_CONFIG_HOME`; the real
gateway (`qwen38`, localhost:8080) was not running.

| # | Assumption | Result |
| --- | --- | --- |
| 1 | `.ts` plugin in `<config>/plugins/` loads; plain `{ id, setup }` default export, `import type` only; `model.request` header reaches the gateway | ✅ `x-api-key` arrived; no `Authorization` header sent |
| 2 | Anthropic protocol against LiteLLM | ⚠️ Partial: opencode sends `POST <baseURL>/v1/messages` (stream) in Anthropic format. Real LiteLLM check moved to the end-to-end run |
| 3 | `--standalone` reads `OPENCODE_CONFIG_CONTENT` from the launching env | ✅ model selected from it |
| 4 | Model ids containing `/` | ✅ `ctx-spike/anthropic/claude-opus-5` works; refs split at the first `/`. A `#` in a model id would be parsed as a variant |

Working plugin API (from `packages/plugin/src/promise/*`):
- `ctx.provider.transform(editor => editor.add({ info, models }))`, `ctx.provider.reload()`.
- `info`: `{ id, name, activation: "enabled", package: "@opencode/ai/providers/anthropic", settings: { baseURL } }`.
  `package: "@ai-sdk/anthropic"` fails: "does not export model(modelID, settings)".
- Each model: `{ id, modelID, providerID, name, capabilities: { tools: true, input: ["text","image"], output: ["text"] },
  variants: [], time: { released: 0 }, cost: [], status: "active", enabled: true, limit: { context, output } }`.
- `ctx.session.hook("model.request", r => …)`: `r.model.providerID`, mutable `r.headers`. Throwing aborts the
  request and opencode prints the error message (`Error: <message>`), exit 1 — usable for credential failures.
- `ctx.session.hook("http.response", r => …)`: `r.model.providerID`, `r.response.status`. **A throw here hangs
  the request** — this hook must never throw.
- `opencode --standalone` is a global flag accepted before any subcommand.
