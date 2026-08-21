package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// SentinelKey is the top-level key holding claude-ctx metadata.
const SentinelKey = "_claudeCtx"

// SentinelVersion is bumped when the sentinel shape changes.
const SentinelVersion = 1

// Sentinel records what claude-ctx wrote so it can be removed or checked later.
type Sentinel struct {
	Context     string   `json:"context"`
	ManagedKeys []string `json:"managedKeys"` // "env.FOO" or top-level "apiKeyHelper"
	Version     int      `json:"version"`
	Hash        string   `json:"hash"` // sha256 over managed values as written
}

// ReadSentinel extracts the sentinel from a settings document, if present.
func ReadSentinel(doc map[string]any) (*Sentinel, bool) {
	raw, ok := doc[SentinelKey]
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var s Sentinel
	if err := json.Unmarshal(b, &s); err != nil || s.Context == "" {
		return nil, false
	}
	return &s, true
}

// lookup resolves a managed-key path ("env.X" or "X") in doc.
func lookup(doc map[string]any, key string) (any, bool) {
	if rest, ok := strings.CutPrefix(key, "env."); ok {
		env, ok := doc["env"].(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := env[rest]
		return v, ok
	}
	v, ok := doc[key]
	return v, ok
}

// hashManaged computes a stable digest of the managed keys' current values.
func hashManaged(doc map[string]any, keys []string) string {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, k := range sorted {
		v, ok := lookup(doc, k)
		h.Write([]byte(k))
		h.Write([]byte{0})
		if ok {
			b, _ := json.Marshal(v)
			h.Write(b)
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Drift reports whether managed values differ from what claude-ctx last wrote.
// Returns false when there is no sentinel.
func Drift(doc map[string]any) bool {
	s, ok := ReadSentinel(doc)
	if !ok || s.Hash == "" {
		return false
	}
	return hashManaged(doc, s.ManagedKeys) != s.Hash
}
