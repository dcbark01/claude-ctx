// Package gateway probes an OpenAI/Anthropic-compatible gateway for doctor.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds a single probe.
const DefaultTimeout = 10 * time.Second

// ListModels GETs <baseURL>/v1/models with a bearer token and returns the
// model ids. It tolerates both OpenAI ({"data":[{"id"}]}) and bare
// ({"models":[{"name"}]}) shapes.
func ListModels(ctx context.Context, baseURL, key string) ([]string, error) {
	u := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("x-api-key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return nil, fmt.Errorf("GET %s: HTTP %d %s", u, resp.StatusCode, snippet)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("GET %s: not JSON: %w", u, err)
	}
	seen := map[string]bool{}
	var ids []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	for _, m := range parsed.Data {
		add(m.ID)
	}
	for _, m := range parsed.Models {
		add(m.ID)
		add(m.Name)
	}
	return ids, nil
}
