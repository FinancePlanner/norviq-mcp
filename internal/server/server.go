// Package server wires the HTTP surface: bearer auth, the streamable MCP
// handler, health checks, and OAuth protected-resource metadata.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	BackendURL   string // e.g. https://api.norviqa.io
	PublicURL    string // this service's public URL, e.g. https://mcp.norviqa.io
	Introspector *auth.Introspector
}

// New returns the top-level http.Handler for the MCP service.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	// RFC 9728 protected-resource metadata: points clients at the backend as the
	// authorization server.
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              cfg.PublicURL,
			"authorization_servers": []string{cfg.BackendURL},
			"scopes_supported": []string{
				"expenses:read", "expenses:write", "reports:read", "market:read", "insights:read", "tax:read",
			},
		})
	})

	// The streamable MCP handler builds a per-session server bound to the
	// request's authenticated principal.
	streamable := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		p, ok := auth.PrincipalFrom(req.Context())
		if !ok {
			// Should never happen: authMiddleware runs first. Return an empty
			// server so no tools are exposed.
			return mcp.NewServer(&mcp.Implementation{Name: "norviq", Version: "0.1.0"}, nil)
		}
		s := mcp.NewServer(&mcp.Implementation{Name: "norviq", Version: "0.1.0"}, nil)
		client := api.NewClient(cfg.BackendURL, p.Token)
		tools.Register(s, client, p)
		return s
	}, nil)

	mux.Handle("/mcp", authMiddleware(cfg, streamable))

	return mux
}

// authMiddleware validates the bearer via backend introspection and stashes the
// principal in the request context. On failure it returns 401 with a
// WWW-Authenticate header pointing at the protected-resource metadata (RFC 9728).
func authMiddleware(cfg Config, next http.Handler) http.Handler {
	challenge := fmt.Sprintf(
		`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`,
		strings.TrimRight(cfg.PublicURL, "/"),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token := bearerToken(req)
		if token == "" {
			unauthorized(w, challenge, "missing bearer token")
			return
		}
		result, err := cfg.Introspector.Introspect(req.Context(), token)
		if err != nil {
			http.Error(w, "authorization service unavailable", http.StatusServiceUnavailable)
			return
		}
		if !result.Active {
			unauthorized(w, challenge, "invalid or expired token")
			return
		}
		if !result.Entitled {
			http.Error(w, "norviq Pro is required to use the MCP connector. Upgrade at norviqa.io.", http.StatusForbidden)
			return
		}
		p := &auth.Principal{
			Token:    token,
			UserID:   result.Sub,
			Scopes:   result.Scopes(),
			Entitled: result.Entitled,
		}
		next.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), p)))
	})
}

func bearerToken(req *http.Request) string {
	h := req.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func unauthorized(w http.ResponseWriter, challenge, msg string) {
	w.Header().Set("WWW-Authenticate", challenge)
	http.Error(w, msg, http.StatusUnauthorized)
}

var _ = context.Background
