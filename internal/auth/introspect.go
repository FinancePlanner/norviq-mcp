// Package auth validates incoming bearer tokens against the norviq-backend
// introspection endpoint and gates the MCP HTTP surface.
package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// tokenKey is the cache key: a SHA-256 of the bearer, so a heap dump or a
// debugger attached to the process never sees a usable token in the map.
type tokenKey [sha256.Size]byte

func keyFor(token string) tokenKey { return sha256.Sum256([]byte(token)) }

// defaultMaxEntries bounds the cache. Each entry is a few hundred bytes, so
// this caps it at a few MiB even if every request carries a fresh token.
const defaultMaxEntries = 10_000

// Introspector calls the backend introspection endpoint and caches results
// briefly (bounding revocation lag to the TTL).
//
// Expired entries are swept at most once per TTL, and the map never grows
// past maxEntries: without both, every distinct bearer ever presented —
// including garbage from unauthenticated probes — stays in memory for the
// life of the pod.
type Introspector struct {
	backendURL string
	secret     string
	http       *http.Client
	ttl        time.Duration
	maxEntries int
	now        func() time.Time

	mu        sync.Mutex
	cache     map[tokenKey]cacheEntry
	lastSweep time.Time
}

func NewIntrospector(backendURL, secret string) *Introspector {
	return &Introspector{
		backendURL: backendURL,
		secret:     secret,
		http:       &http.Client{Timeout: 10 * time.Second},
		ttl:        60 * time.Second,
		maxEntries: defaultMaxEntries,
		now:        time.Now,
		cache:      map[tokenKey]cacheEntry{},
	}
}

func (in *Introspector) Introspect(ctx context.Context, token string) (Introspection, error) {
	key := keyFor(token)
	in.mu.Lock()
	if entry, ok := in.cache[key]; ok {
		if in.now().Sub(entry.cachedAt) < in.ttl {
			in.mu.Unlock()
			return entry.result, nil
		}
		delete(in.cache, key)
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Introspection{}, fmt.Errorf("introspection returned %d: %s", resp.StatusCode, payload)
	}
	var result Introspection
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Introspection{}, fmt.Errorf("decode introspection: %w", err)
	}

	in.store(key, result)
	return result, nil
}

// store inserts a result, sweeping expired entries first when a sweep is due or
// the cache is full. If it is still full after the sweep — a burst of distinct
// live tokens inside one TTL — an arbitrary entry is dropped; the cost of that
// is one extra introspection call, never a wrong answer.
func (in *Introspector) store(key tokenKey, result Introspection) {
	in.mu.Lock()
	defer in.mu.Unlock()
	now := in.now()
	if now.Sub(in.lastSweep) >= in.ttl || len(in.cache) >= in.maxEntries {
		for k, e := range in.cache {
			if now.Sub(e.cachedAt) >= in.ttl {
				delete(in.cache, k)
			}
		}
		in.lastSweep = now
	}
	if _, exists := in.cache[key]; !exists && len(in.cache) >= in.maxEntries {
		for k := range in.cache {
			delete(in.cache, k)
			break
		}
	}
	in.cache[key] = cacheEntry{result: result, cachedAt: now}
}
