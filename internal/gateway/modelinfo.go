package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNoModelInfo means the gateway exposes no LiteLLM-style /model/info.
var ErrNoModelInfo = errors.New("gateway has no /model/info endpoint")

// Limits are a model's token limits; 0 means unknown.
type Limits struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

// ModelInfo reads per-model token limits from LiteLLM's /v1/model/info,
// falling back to /model/info. Context is max_input_tokens; output is
// max_output_tokens, else the legacy max_tokens.
func ModelInfo(ctx context.Context, baseURL, key string) (map[string]Limits, error) {
	base := strings.TrimRight(baseURL, "/")
	var lastErr error = ErrNoModelInfo
	for _, path := range []string{"/v1/model/info", "/model/info"} {
		body, status, err := get(ctx, base+path, key)
		if err != nil {
			return nil, err
		}
		if noEndpoint(status) {
			continue
		}
		if status/100 != 2 {
			lastErr = fmt.Errorf("GET %s: HTTP %d", base+path, status)
			continue
		}
		var parsed struct {
			Data []struct {
				ModelName string `json:"model_name"`
				ModelInfo struct {
					MaxInputTokens  int64 `json:"max_input_tokens"`
					MaxTokens       int64 `json:"max_tokens"`
					MaxOutputTokens int64 `json:"max_output_tokens"`
				} `json:"model_info"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("GET %s: not JSON: %w", base+path, err)
		}
		out := map[string]Limits{}
		for _, d := range parsed.Data {
			if d.ModelName == "" {
				continue
			}
			l := Limits{Context: d.ModelInfo.MaxInputTokens, Output: d.ModelInfo.MaxOutputTokens}
			if l.Output == 0 {
				l.Output = d.ModelInfo.MaxTokens // LiteLLM's legacy field means output, not context
			}
			out[d.ModelName] = l
		}
		return out, nil
	}
	return nil, lastErr
}

// noEndpoint reports whether status means the path does not exist. Gateways
// other than LiteLLM answer unknown paths with 404, 405, 501 or (agentgateway)
// 400; auth and server errors are real failures.
func noEndpoint(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

// get performs an authenticated GET and returns the (size-limited) body.
func get(ctx context.Context, url, key string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("x-api-key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, resp.StatusCode, err
}
