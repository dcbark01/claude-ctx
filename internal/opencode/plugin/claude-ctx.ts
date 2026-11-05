// claude-ctx opencode plugin. Installed by `claude-ctx opencode install`; do not edit.
//
// Defines one provider per claude-ctx context (ctx-<name>) and sets x-api-key
// and Authorization: Bearer on every model request from
// `claude-ctx credential --context <name>`. Keys live only in this process's
// memory; nothing is written to disk.
import type { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"
import { watch, type FSWatcher } from "node:fs"
import { basename, dirname } from "node:path"

const BIN: string = "__CLAUDE_CTX_BIN__"
const VERSION: string = "__CLAUDE_CTX_VERSION__"
const ARGS: string[] = [/*__CLAUDE_CTX_ARGS__*/]

const PREFIX = "ctx-"
const TIMEOUT_MS = 20_000
const DEFAULT_LIMIT = { context: 200_000, output: 32_000 }

export interface ProviderModel {
  id: string
  name: string
  limit?: { context: number; output: number }
}

export interface Provider {
  id: string
  context: string
  name: string
  baseURL: string
  keyTtlMs: number
  models: ProviderModel[]
  defaultModel: string
  smallModel?: string
}

export interface ProvidersOutput {
  configPath?: string
  cachePath?: string
  providers: Provider[]
}

/** Maps a provider id to its claude-ctx context name, or undefined for other providers. */
export function contextFor(providerID: string): string | undefined {
  if (!providerID.startsWith(PREFIX) || providerID.length === PREFIX.length) return undefined
  return providerID.slice(PREFIX.length)
}

export function credentialError(name: string): Error {
  return new Error(`credential for ${name} unavailable — run \`claude-ctx doctor ${name}\` (gpg-agent cache may be cold)`)
}

/** Request headers carrying key: x-api-key and a bearer token, as Claude Code's apiKeyHelper sends. */
export function authHeaders(key: string): Record<string, string> {
  return { "x-api-key": key, authorization: `Bearer ${key}` }
}

/** In-memory key cache: one in-flight fetch per context, entries expire after ttlMs. */
export class KeyCache {
  private keys = new Map<string, { key: string; expires: number }>()
  private inflight = new Map<string, Promise<string>>()

  constructor(
    private fetchKey: (ctx: string) => Promise<string>,
    private ttlMs: (ctx: string) => number,
    private now: () => number = Date.now,
  ) {}

  get(ctx: string): Promise<string> {
    const hit = this.keys.get(ctx)
    if (hit && this.now() < hit.expires) return Promise.resolve(hit.key)
    const pending = this.inflight.get(ctx)
    if (pending) return pending
    const p = this.fetchKey(ctx)
      .then((key) => {
        this.keys.set(ctx, { key, expires: this.now() + this.ttlMs(ctx) })
        return key
      })
      .finally(() => this.inflight.delete(ctx))
    this.inflight.set(ctx, p)
    return p
  }

  evict(ctx: string): void {
    this.keys.delete(ctx)
  }
}

/** Runs `claude-ctx opencode providers --json`; never throws (logs and returns no providers). */
export async function loadProviders(run: (args: string[]) => Promise<string>): Promise<ProvidersOutput> {
  try {
    const out = JSON.parse(await run(["opencode", "providers", "--json"])) as ProvidersOutput
    if (!out || !Array.isArray(out.providers)) throw new Error("unexpected providers document")
    return out
  } catch (err) {
    console.error(`[claude-ctx] no providers registered: ${err instanceof Error ? err.message : String(err)}`)
    return { providers: [] }
  }
}

export function providerInfo(p: Provider) {
  return {
    id: p.id,
    name: p.name,
    activation: "enabled" as const,
    package: "@opencode/ai/providers/anthropic",
    settings: { baseURL: p.baseURL },
  }
}

export function modelInfo(p: Provider, m: ProviderModel) {
  return {
    id: m.id,
    modelID: m.id,
    providerID: p.id,
    name: m.name,
    capabilities: { tools: true, input: ["text", "image"], output: ["text"] },
    variants: [],
    time: { released: 0 },
    cost: [],
    status: "active" as const,
    enabled: true,
    limit: m.limit ?? DEFAULT_LIMIT,
  }
}

/** Runs claude-ctx (PATH first, then the binary that installed this plugin) and returns stdout. */
export function runClaudeCtx(args: string[], timeoutMs = TIMEOUT_MS): Promise<string> {
  const attempt = (bin: string) =>
    new Promise<string>((resolve, reject) => {
      const child = spawn(bin, [...ARGS, ...args], { stdio: ["ignore", "pipe", "pipe"] })
      let stdout = ""
      let stderr = ""
      const timer = setTimeout(() => {
        child.kill()
        reject(new Error(`claude-ctx ${args[0]} timed out after ${timeoutMs}ms`))
      }, timeoutMs)
      child.stdout.on("data", (d) => (stdout += d))
      child.stderr.on("data", (d) => (stderr += d))
      child.on("error", (err) => {
        clearTimeout(timer)
        reject(err)
      })
      child.on("close", (code) => {
        clearTimeout(timer)
        if (code === 0) resolve(stdout)
        else reject(new Error(stderr.trim() || `claude-ctx exited ${code}`))
      })
    })
  return attempt("claude-ctx").catch((err: NodeJS.ErrnoException) => {
    if (err.code === "ENOENT" && BIN && !BIN.startsWith("__")) return attempt(BIN)
    throw err
  })
}

const plugin: Plugin = {
  id: "claude-ctx",
  setup: async (ctx) => {
    let current = await loadProviders((args) => runClaudeCtx(args))
    const ttl = (name: string) => current.providers.find((p) => p.context === name)?.keyTtlMs ?? 0
    const keys = new KeyCache(async (name) => {
      try {
        const key = (await runClaudeCtx(["credential", "--context", name])).trim()
        if (!key) throw new Error("empty key")
        return key
      } catch {
        throw credentialError(name)
      }
    }, ttl)

    await ctx.provider.transform((editor) => {
      for (const p of current.providers) {
        editor.add({ info: providerInfo(p), models: p.models.map((m) => modelInfo(p, m)) } as any)
      }
    })

    await ctx.session.hook("model.request", async (r) => {
      const name = contextFor(r.model.providerID)
      if (!name || !current.providers.some((p) => p.context === name)) return
      Object.assign(r.headers, authHeaders(await keys.get(name)))
    })

    // A throw in this hook hangs the request (verified on opencode v2.2.5), so never throw.
    await ctx.session.hook("http.response", (r) => {
      try {
        const name = contextFor(r.model.providerID)
        if (name && r.response.status === 401) keys.evict(name)
      } catch {}
    })

    // Reload providers when config.toml or the metadata cache changes.
    const watchers: FSWatcher[] = []
    let timer: ReturnType<typeof setTimeout> | undefined
    const reload = () => {
      clearTimeout(timer)
      timer = setTimeout(async () => {
        try {
          current = await loadProviders((args) => runClaudeCtx(args))
          await ctx.provider.reload()
        } catch (err) {
          console.error(`[claude-ctx] reload failed: ${err instanceof Error ? err.message : String(err)}`)
        }
      }, 200)
    }
    const files = [current.configPath, current.cachePath].filter((f): f is string => !!f)
    for (const dir of new Set(files.map((f) => dirname(f)))) {
      const names = new Set(files.filter((f) => dirname(f) === dir).map((f) => basename(f)))
      try {
        watchers.push(watch(dir, (_event, file) => file && names.has(String(file)) && reload()))
      } catch {}
    }
    console.error(`[claude-ctx] plugin ${VERSION}: ${current.providers.length} context(s)`)
    return () => {
      clearTimeout(timer)
      for (const w of watchers) w.close()
    }
  },
}

export default plugin
