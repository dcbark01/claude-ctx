// Package gpg inspects gpg-agent configuration for doctor.
package gpg

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// TTL holds gpg-agent's passphrase cache lifetimes.
type TTL struct {
	Default time.Duration // default-cache-ttl
	Max     time.Duration // max-cache-ttl
}

// AgentTTL runs `gpgconf --list-options gpg-agent` and parses the cache TTLs.
func AgentTTL(ctx context.Context) (TTL, error) {
	out, err := exec.CommandContext(ctx, "gpgconf", "--list-options", "gpg-agent").Output()
	if err != nil {
		return TTL{}, err
	}
	return ParseOptions(string(out))
}

// ParseOptions parses gpgconf's colon-separated option listing. Fields:
// name:flags:level:description:type:alt-type:argname:default:argdef:value.
func ParseOptions(out string) (TTL, error) {
	t := TTL{Default: 600 * time.Second, Max: 7200 * time.Second} // gpg-agent built-in defaults
	found := false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 10 {
			continue
		}
		var dst *time.Duration
		switch f[0] {
		case "default-cache-ttl":
			dst = &t.Default
		case "max-cache-ttl":
			dst = &t.Max
		default:
			continue
		}
		found = true
		val := f[9]
		if val == "" {
			val = f[7]
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(val, "+")); err == nil {
			*dst = time.Duration(n) * time.Second
		}
	}
	if !found {
		return t, errors.New("gpgconf output did not include cache-ttl options")
	}
	return t, nil
}
