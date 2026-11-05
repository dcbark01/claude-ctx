# claude-ctx

Context switching for [Claude Code](https://docs.anthropic.com/en/docs/claude-code) and [opencode](https://opencode.ai): named bundles of **gateway URL + model + credential source**, switched with one command. Ergonomics modeled on `docker context` / `kubectx`.

```
$ claude-ctx list
   NAME        MODEL                    BASE URL                          CREDENTIAL
 * opus-b      anthropic/claude-opus-5  https://litellm-b.internal        pass:work/litellm/b/anthropic
   gpt5-prod   gpt-5                    https://litellm-a.internal        pass:work/litellm/a/gpt5
   local       Qwen3.8-27B              http://localhost:8080             env:LOCAL_KEY

$ claude-ctx use gpt5-prod          # persistent: merged into ~/.claude/settings.json
$ claude-ctx run local -- -p "hi"   # ephemeral: this invocation only, nothing written
```

## Quickstart

Five minutes from zero to switching contexts.

**1. Install**

```sh
go install github.com/dcbark01/claude-ctx@latest
claude-ctx --version   # if "command not found", add $(go env GOPATH)/bin to PATH
```

**2. Put a key somewhere claude-ctx can read it** — pick one:

```sh
pass insert work/litellm/opus          # GPG-encrypted via `pass` (recommended)
export LITELLM_KEY=sk-...              # or: an environment variable
```

**3. Define a context** (`claude-ctx edit` opens `~/.claude/context/config.toml` with a template)

```toml
[contexts.opus]
base_url = "https://litellm.example.internal"
model    = "anthropic/claude-opus-5"
key_pass = "work/litellm/opus"        # or: key_env = "LITELLM_KEY"
```

**4. Use it**

```sh
claude-ctx run opus                   # this terminal only — nothing written to disk
claude-ctx use opus                   # or: make it the default for all new sessions
```

`run` checks the key first (you'll see pinentry here if GPG needs it), then launches `claude` pointed at your gateway. Add more `[contexts.*]` blocks and switch with `claude-ctx use` (no argument → picker) or `claude-ctx run <name>`.

**5. See what's active**

```sh
claude-ctx list       # all contexts, current starred
claude-ctx show       # exact env block a context renders to
claude-ctx unset      # undo `use`, restoring your settings.json
```

Everything below is detail: what gets written, why, and how to keep it secure.

## Why

Claude Code has no profile concept. Model (`ANTHROPIC_MODEL`), endpoint (`ANTHROPIC_BASE_URL`) and credential (`apiKeyHelper`) are configured independently. With several LiteLLM deployments and per-model-group keys, switching means hand-editing settings files. claude-ctx makes it one command **without ever writing a secret to disk**.

## Security model

- **No secrets on disk.** `config.toml` holds a `pass` path, a command, or an env var name. `settings.json` holds only `"apiKeyHelper": "claude-ctx credential"`. GPG (via `pass`) is the single trust boundary.
- **claude-ctx is the helper.** Claude Code calls `claude-ctx credential`; it resolves the active context's key and prints it. Rotate in `pass`, and the next TTL expiry picks it up.
- **Never project scope.** claude-ctx refuses to write any `.claude/settings.json` inside a git repo — `apiKeyHelper` there is arbitrary code execution on project open for everyone who clones.
- **Surgical merge.** Only an explicit set of keys is touched; permissions, hooks, plugins, and anything Claude Code writes itself (`model`, `effortLevel`, …) pass through untouched. A `_claudeCtx` sentinel records exactly what was written so `unset` removes exactly that.

## Install

```sh
go install github.com/dcbark01/claude-ctx@latest
```

Requires macOS/Linux, Go 1.22+, and (for `key_pass`) [`pass`](https://www.passwordstore.org/). Make sure `$GOPATH/bin` is on `PATH` so the persisted helper string `claude-ctx credential` resolves; if it isn't, `use` falls back to the binary's absolute path and warns.

## Config: `~/.claude/context/config.toml`

`claude-ctx edit` creates a commented template. Full example:

```toml
[defaults]
key_ttl_ms = 3600000                  # → CLAUDE_CODE_API_KEY_HELPER_TTL_MS
# client = "claude"                   # default for `run --client` (claude | opencode)
# store_dir = "~/.password-store-work"  # → PASSWORD_STORE_DIR (split pass stores)

[contexts.gpt5-prod]
base_url = "https://litellm-a.internal"
model    = "gpt-5"
key_pass = "work/litellm/deployment-a/gpt5"

[contexts.opus-b]
base_url = "https://litellm-b.internal"
model    = "anthropic/claude-opus-5"
key_pass = "work/litellm/deployment-b/anthropic"
display_name = "Opus 5 (deployment B)"   # → ANTHROPIC_CUSTOM_MODEL_OPTION_NAME
discovery    = true                      # → CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1

  # Repoint /model opus|sonnet|haiku to THIS deployment (same key).
  # Background calls use the haiku slot — keep it on the same key!
  [contexts.opus-b.model_aliases]
  opus   = "anthropic/claude-opus-5"
  sonnet = "anthropic/claude-sonnet-5"
  haiku  = "anthropic/claude-haiku-4-5"

[contexts.local]
base_url = "http://localhost:8080"
model    = "Qwen3.8-27B"
key_env  = "LOCAL_KEY"                   # alternative sources:
# key_cmd = "op read op://work/x/credential"
```

| Field | Required | Emits |
| --- | --- | --- |
| `base_url` | yes | `ANTHROPIC_BASE_URL` |
| `model` | yes | `ANTHROPIC_MODEL` |
| `key_pass` / `key_cmd` / `key_env` | one | nothing on disk — resolved by `credential` |
| `model_aliases.{opus,sonnet,haiku,fable}` | no | `ANTHROPIC_DEFAULT_<FAMILY>_MODEL` |
| `display_name` | no | `ANTHROPIC_CUSTOM_MODEL_OPTION` + `_NAME` |
| `discovery` | no | `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` |
| `context_limit` / `output_limit` | no | opencode only: the model's token limits (else from the gateway's `/model/info`, else opencode's 200k / 32k) |

Credential precedence when several are set: `key_pass` → `key_cmd` → `key_env` (with a warning). For `key_pass`, only the **first line** of the entry is used, per `pass` convention. Unknown TOML keys are rejected.

## Commands

```
claude-ctx use [<ctx>]           persistent switch (no arg → interactive picker); --force to overwrite external edits;
                                 --client claude|opencode (default: every installed client)
claude-ctx run <ctx> -- [args]   exec claude (or --client opencode) with the context in its environment; zero file mutation
claude-ctx show [<ctx>]          rendered env block + redacted credential source; --json
claude-ctx list                  all contexts, current starred; --plain
claude-ctx current [--plain]     active context; --plain prints `name` (or `name*` on drift) for statuslines
claude-ctx add [<name>]          interactive form (pass-entry + gateway-model pickers) or flags
claude-ctx unset                 remove every claude-ctx-managed key from user settings, and undo `use` in opencode
claude-ctx doctor [<ctx>]        health check: credential, gateway, model presence, gpg TTL, drift, opencode; --no-network
claude-ctx credential            apiKeyHelper entrypoint (stdout = key, stderr = diagnostics, nonzero on failure);
                                 --context <ctx> picks the context (used by the opencode plugin)
claude-ctx opencode install      install/upgrade the opencode plugin (opencode v2+); `uninstall` removes it
claude-ctx opencode providers    the opencode providers generated from config (--json is what the plugin reads)
claude-ctx edit                  $EDITOR on config.toml, re-validate on save
claude-ctx completion <shell>    shell completions
```

Global flags: `--config <file>` (or `$CLAUDE_CTX_CONFIG`), `--settings-path <file>` (for testing; default `~/.claude/settings.json`).

### `use` vs `run`

- **`use`** mutates `~/.claude/settings.json` and affects *new* sessions. A running session may partially pick it up: Claude Code live-reloads `env` and `apiKeyHelper`, but `model` is read once at startup — so prefer `run` for switching mid-work.
- **`run`** injects the env block plus `CLAUDE_CTX=<ctx>` into a child `claude` and passes `--settings '{"apiKeyHelper": …}'`. Two terminals can run two contexts with no shared state. It also strips `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_API_KEY` from the child (they would shadow the helper) and warns.

Both commands **prime** the credential in the foreground first: pinentry appears in your terminal, gpg-agent's cache is warm for Claude Code's later helper calls, and a missing/empty key fails *before* anything is written.

### opencode

The same contexts drive [opencode](https://opencode.ai) v2+. opencode has no `apiKeyHelper`, and the keys it saves itself sit in plaintext in its database, so claude-ctx ships an opencode plugin instead:

```sh
claude-ctx opencode install          # writes ~/.config/opencode/plugins/claude-ctx.ts
claude-ctx use opus-b                # switches Claude Code *and* opencode
opencode                             # every context is a provider: ctx-<name>/<model>
claude-ctx run local --client opencode -- run "hello"   # this terminal only
```

- **Providers.** The plugin asks `claude-ctx opencode providers --json` for one provider per context (`ctx-<name>`, Anthropic protocol at `<base_url>/v1`). It lists `model` plus any `model_aliases` (and, with `discovery = true`, every gateway model), and reloads when `config.toml` changes. All contexts sit in opencode's model picker at once.
- **Keys never touch disk.** Before each model request the plugin sets `x-api-key` and `Authorization: Bearer` (as Claude Code does) from `claude-ctx credential --context <name>`, held in opencode's memory for `key_ttl_ms`. A 401 drops the cached key. If the key can't be fetched (e.g. a cold gpg-agent; there is no terminal for pinentry inside opencode), the request fails with a message to run `claude-ctx doctor <ctx>`. `use` and `run` check the credential in the foreground first, which warms the cache.
- **`use`** sets `model` (and `small_model`, from a `haiku` alias) in `~/.config/opencode/opencode.json[c]`, keeping your comments and formatting. What it wrote is recorded in `~/.claude/context/state.json`; edits made outside claude-ctx are detected (`--force` overrides), and `unset` restores the values you had before.
- **`run --client opencode`** execs `opencode --standalone` with `OPENCODE_CONFIG_CONTENT` selecting the context's models. Nothing is written; set `defaults.client = "opencode"` to make it the default.
- **Model limits.** opencode needs a context window to know when to compact. `use`, `run`, `doctor` and `add` read it from LiteLLM's `/model/info` in the foreground and keep it in `~/.claude/context/cache.json` (model lists and limits only, never keys). Set `context_limit` / `output_limit` to override.

### add

Interactive: `claude-ctx add` asks for name and gateway, then offers the `pass` entries found under `defaults.namespace` (or an env var / command), checks the credential, queries the gateway's `/v1/models` and offers those as the model and alias choices, previews the TOML and appends it — existing comments are preserved.

Scripted:

```sh
claude-ctx add opus-b --base-url https://litellm-b.internal --model anthropic/claude-opus-5 \
  --key-pass work/litellm/b/anthropic --display-name "Opus 5 (B)" \
  --alias opus=anthropic/claude-opus-5 --alias haiku=anthropic/claude-haiku-4-5
```

### doctor

```
$ claude-ctx doctor
   SCOPE   CHECK               DETAIL
✓  global  claude-ctx on PATH  /Users/me/go/bin/claude-ctx
!  global  gpg-agent TTL       default-cache-ttl=10m0s < key_ttl_ms=1h0m0s; raise it in ~/.gnupg/gpg-agent.conf
✓  global  settings drift      managed keys match last `use opus-b`
✓  opus-b  credential          pass:work/litellm/b/anthropic (51 chars)
✓  opus-b  gateway             https://litellm-b.internal/v1/models → 42 models
✓  opus-b  model               anthropic/claude-opus-5
✗  opus-b  alias haiku         anthropic/claude-haiku-4 not in gateway list (did you mean: anthropic/claude-haiku-4-5?)
```

Per context: the credential resolves and is clean, `GET <base_url>/v1/models` succeeds with it, and `model` plus every `model_aliases` value exists on the gateway. Globally: `claude-ctx` is on PATH, no `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_API_KEY` shadows the helper, managed settings aren't drifted, enterprise managed-settings don't override our keys, and gpg-agent's cache TTL covers `key_ttl_ms`. When opencode is installed: it is v2+, the plugin is installed and matches this binary, the `claude-ctx` the plugin will call is the same version, opencode's default `model` names an existing context, and each context's token limits are known. Exits 1 on any ✗.

### Statusline

```json
{ "statusLine": { "type": "command", "command": "printf 'ctx:%s' \"$(claude-ctx current --plain)\"" } }
```

### gpg-agent tip

Set `default-cache-ttl` / `max-cache-ttl` in `~/.gnupg/gpg-agent.conf` above `key_ttl_ms` (default 1h) so helper re-runs never need pinentry from inside Claude Code, where it may hang without a TTY.

## What `use` writes

```jsonc
{
  "apiKeyHelper": "claude-ctx credential",
  "env": {
    "ANTHROPIC_BASE_URL": "https://litellm-b.internal",
    "ANTHROPIC_MODEL": "anthropic/claude-opus-5",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "anthropic/claude-opus-5",
    "CLAUDE_CODE_API_KEY_HELPER_TTL_MS": "3600000"
    // … plus your own pre-existing env entries, untouched
  },
  "_claudeCtx": { "context": "opus-b", "managedKeys": ["…"], "version": 1, "hash": "…" }
  // … every other key in the file, untouched
}
```

Writes are atomic (temp file + rename, mode preserved) and the previous contents are kept in `settings.json.claude-ctx.bak`. The top-level `model` key is deliberately **not** managed — Claude Code's `/model` owns it; `ANTHROPIC_MODEL` is claude-ctx's channel.

## Development

```sh
make build   # → ./claude-ctx
make test    # go test ./... -race, plus the plugin's `bun test` when bun is installed
make lint
make e2e     # end-to-end; fetches a pinned agentgateway into .bin/ on first run
```

`make e2e` builds this checkout and runs it against a real [agentgateway](https://agentgateway.dev) (which requires a client key) in front of a mock Anthropic upstream (which requires a different key), with the real `opencode` and `claude` binaries in a throwaway `HOME`. It covers `run` and `use` for both clients, wrong-key and credential-failure paths, and checks that no key reaches any file the clients write (including opencode's database and logs). Tests for a client that isn't on PATH are skipped; override binaries with `AGENTGATEWAY_BIN`, `OPENCODE_BIN`, `CLAUDE_BIN`.

Test against scratch files, never your real settings:

```sh
./claude-ctx --config scratch/config.toml --settings-path scratch/settings.json use local
```

## Roadmap

- **v0.2** ✓ — `doctor`, `add`, statusline `--plain`, backups, opencode support.
- **v0.3** — `--scope local`, arbitrary per-context `env` passthrough, OS keychain resolver, goreleaser / Homebrew tap.
