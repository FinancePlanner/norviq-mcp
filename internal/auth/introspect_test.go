package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for the cache TTL.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (in *Introspector) cacheLen() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return len(in.cache)
}

// introspectionBackend answers every introspection call as active and counts
// the calls, so a test can tell a cache hit from a miss.
func introspectionBackend(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/oauth/introspect" || r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"sub":"u1","scope":"expenses:read","entitled":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newTestIntrospector(t *testing.T, maxEntries int) (*Introspector, *fakeClock, *atomic.Int64) {
	t.Helper()
	srv, calls := introspectionBackend(t)
	clock := &fakeClock{t: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	in := NewIntrospector(srv.URL, "s3cret")
	in.now = clock.now
	in.maxEntries = maxEntries
	return in, clock, calls
}

func TestIntrospectCachesForTTLThenRefetches(t *testing.T) {
	in, clock, calls := newTestIntrospector(t, defaultMaxEntries)
	ctx := context.Background()

	for range 3 {
		if _, err := in.Introspect(ctx, "nvq_pat_a"); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 backend call within the TTL, got %d", got)
	}

	clock.advance(61 * time.Second)
	if _, err := in.Introspect(ctx, "nvq_pat_a"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected a fresh backend call after the 60s TTL, got %d calls", got)
	}
}

func TestIntrospectCacheNeverHoldsTheRawToken(t *testing.T) {
	in, _, _ := newTestIntrospector(t, defaultMaxEntries)
	const token = "nvq_pat_raw_secret"
	if _, err := in.Introspect(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if len(in.cache) != 1 {
		t.Fatalf("expected one cached entry, got %d", len(in.cache))
	}
	for k := range in.cache {
		if k != keyFor(token) {
			t.Fatal("cache key is not the SHA-256 of the token")
		}
		if string(k[:]) == token {
			t.Fatal("cache key is the raw token")
		}
	}
}

func TestIntrospectSweepsExpiredEntries(t *testing.T) {
	in, clock, _ := newTestIntrospector(t, defaultMaxEntries)
	ctx := context.Background()

	for _, tok := range []string{"a", "b", "c", "d"} {
		if _, err := in.Introspect(ctx, tok); err != nil {
			t.Fatal(err)
		}
	}
	if got := in.cacheLen(); got != 4 {
		t.Fatalf("expected 4 entries, got %d", got)
	}

	// After the TTL the next insert sweeps every stale entry; only the new one
	// survives.
	clock.advance(61 * time.Second)
	if _, err := in.Introspect(ctx, "e"); err != nil {
		t.Fatal(err)
	}
	if got := in.cacheLen(); got != 1 {
		t.Fatalf("expected expired entries to be swept, %d remain", got)
	}
}

func TestIntrospectCacheIsCapped(t *testing.T) {
	in, _, calls := newTestIntrospector(t, 3)
	ctx := context.Background()

	for i := range 10 {
		if _, err := in.Introspect(ctx, string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if got := in.cacheLen(); got > 3 {
		t.Fatalf("cache grew past its cap: %d entries", got)
	}
	if got := calls.Load(); got != 10 {
		t.Fatalf("expected one backend call per distinct token, got %d", got)
	}
}
