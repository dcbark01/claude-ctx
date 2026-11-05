import { describe, expect, test } from "bun:test"
import plugin, { KeyCache, authHeaders, contextFor, credentialError, loadProviders, modelInfo, providerInfo } from "./claude-ctx.ts"

describe("contextFor", () => {
  test("maps ctx- providers to context names", () => {
    expect(contextFor("ctx-opus-b")).toBe("opus-b")
    expect(contextFor("anthropic")).toBeUndefined()
    expect(contextFor("ctx-")).toBeUndefined()
  })
})

describe("authHeaders", () => {
  // Gateways differ: LiteLLM/Anthropic read x-api-key, agentgateway (by default)
  // and many proxies read Authorization: Bearer. Claude Code sends both.
  test("sends the key as x-api-key and as a bearer token", () => {
    expect(authHeaders("k1")).toEqual({ "x-api-key": "k1", authorization: "Bearer k1" })
  })
})

describe("KeyCache", () => {
  function setup(ttl = 1000) {
    let now = 0
    let calls = 0
    const cache = new KeyCache(
      async (ctx) => {
        calls++
        return `${ctx}-key-${calls}`
      },
      () => ttl,
      () => now,
    )
    return { cache, tick: (ms: number) => (now += ms), calls: () => calls }
  }

  test("reuses a key within the TTL and refetches after", async () => {
    const s = setup()
    expect(await s.cache.get("a")).toBe("a-key-1")
    s.tick(999)
    expect(await s.cache.get("a")).toBe("a-key-1")
    s.tick(1)
    expect(await s.cache.get("a")).toBe("a-key-2")
    expect(s.calls()).toBe(2)
  })

  test("evict forces a refetch", async () => {
    const s = setup()
    await s.cache.get("a")
    s.cache.evict("a")
    expect(await s.cache.get("a")).toBe("a-key-2")
  })

  test("concurrent gets on a cold cache share one fetch", async () => {
    const s = setup()
    const keys = await Promise.all(Array.from({ length: 10 }, () => s.cache.get("a")))
    expect(new Set(keys).size).toBe(1)
    expect(s.calls()).toBe(1)
  })

  test("a failed fetch is not cached", async () => {
    let calls = 0
    const cache = new KeyCache(
      async () => {
        calls++
        if (calls === 1) throw new Error("boom")
        return "k"
      },
      () => 1000,
      () => 0,
    )
    await expect(cache.get("a")).rejects.toThrow("boom")
    expect(await cache.get("a")).toBe("k")
  })
})

describe("loadProviders", () => {
  test("returns no providers and does not throw when claude-ctx fails", async () => {
    const out = await loadProviders(async () => {
      throw new Error("no config")
    })
    expect(out.providers).toEqual([])
  })

  test("returns no providers on invalid JSON", async () => {
    expect((await loadProviders(async () => "not json")).providers).toEqual([])
  })

  test("parses the providers document", async () => {
    const doc = { configPath: "/c/config.toml", cachePath: "/c/cache.json", providers: [{ id: "ctx-a", context: "a" }] }
    const out = await loadProviders(async (args) => {
      expect(args).toEqual(["opencode", "providers", "--json"])
      return JSON.stringify(doc)
    })
    expect(out.configPath).toBe("/c/config.toml")
    expect(out.providers[0].context).toBe("a")
  })
})

describe("opencode shapes", () => {
  const p = {
    id: "ctx-a", context: "a", name: "claude-ctx: a", baseURL: "http://gw/v1", keyTtlMs: 1000,
    models: [{ id: "x/y", name: "Y", limit: { context: 1, output: 2 } }, { id: "z", name: "z" }],
    defaultModel: "ctx-a/x/y",
  }
  test("provider info uses the native anthropic package and no api key", () => {
    expect(providerInfo(p)).toEqual({
      id: "ctx-a", name: "claude-ctx: a", activation: "enabled",
      package: "@opencode/ai/providers/anthropic", settings: { baseURL: "http://gw/v1" },
    })
  })
  test("model info carries limits only when known", () => {
    expect(modelInfo(p, p.models[0]).limit).toEqual({ context: 1, output: 2 })
    expect(modelInfo(p, p.models[1]).limit).toEqual({ context: 200000, output: 32000 })
    expect(modelInfo(p, p.models[0])).toMatchObject({ id: "x/y", modelID: "x/y", providerID: "ctx-a", name: "Y", enabled: true })
  })
})

test("credential error message", () => {
  expect(credentialError("opus-b").message).toBe(
    "credential for opus-b unavailable — run `claude-ctx doctor opus-b` (gpg-agent cache may be cold)",
  )
})

test("default export is a plain plugin object", () => {
  expect(plugin.id).toBe("claude-ctx")
  expect(typeof plugin.setup).toBe("function")
})
