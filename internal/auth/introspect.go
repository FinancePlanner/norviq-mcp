// Package auth validates incoming bearer tokens against the norviq-backend
// introspection endpoint and gates the MCP HTTP surface.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Introspection is the subset of the backend's RFC 7662-shaped response the
// MCP service needs.
type Introspection struct {
	Active    bool   `json:"active"`
	Sub       string `json:"sub"`
	Scope     string `json:"scope"`
	TokenType string `json:"tokenType"`
	Exp       int64  `json:"exp"`
	Entitled  bool   `json:"entitled"`
}

func (i Introspection) Scopes() map[string]bool {
	set := map[string]bool{}
	for _, s := range strings.Fields(i.Scope) {
		set[s] = true
	}
	return set
}

type cacheEntry struct {
	result   Introspection
	cachedAt time.Time
}

// Introspector calls the backend introspection endpoint and caches results
// briefly (bounding revocation lag to the TTL).
type Introspector struct {
	backendURL string
	secret     string
	http       *http.Client
	ttl        time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func NewIntrospector(backendURL, secret string) *Introspector {
	return &Introspector{
		backendURL: backendURL,
		secret:     secret,
		http:       &http.Client{Timeout: 10 * time.Second},
		ttl:        60 * time.Second,
		cache:      map[string]cacheEntry{},
	}
}

func (in *Introspector) Introspect(ctx context.Context, token string) (Introspection, error) {
	in.mu.Lock()
	if entry, ok := in.cache[token]; ok && time.Since(entry.cachedAt) < in.ttl {
		in.mu.Unlock()
		return entry.result, nil
	}
	in.mu.Unlock()

	body, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.backendURL+"/v1/oauth/introspect", bytes.NewReader(body))
	if err != nil {
		return Introspection{}, err
	}
	req.Header.Set("Authorization", "Bearer "+in.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := in.http.Do(req)
	if err != nil {
		return Introspection{}, fmt.Errorf("introspection call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Introspection{}, fmt.Errorf("introspection returned %d: %s", resp.StatusCode, payload)
	}
	var result Introspection
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Introspection{}, fmt.Errorf("decode introspection: %w", err)
	}

	in.mu.Lock()
	in.cache[token] = cacheEntry{result: result, cachedAt: time.Now()}
	in.mu.Unlock()
	return result, nil
}
