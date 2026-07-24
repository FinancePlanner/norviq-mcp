// Command norviq-mcp is the remote MCP server that lets external AI clients
// operate on a user's norviq account over Streamable HTTP.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/server"
)

func main() {
	backendURL := env("BACKEND_BASE_URL", "http://localhost:8080")
	backendPublicURL := env("BACKEND_PUBLIC_URL", backendURL)
	publicURL := env("MCP_PUBLIC_URL", "http://localhost:8087")
	addr := env("LISTEN_ADDR", ":8087")
	secret := os.Getenv("MCP_INTROSPECTION_SECRET")
	if secret == "" {
		log.Fatal("MCP_INTROSPECTION_SECRET is required")
	}

	handler := server.New(server.Config{
		BackendURL:       backendURL,
		BackendPublicURL: backendPublicURL,
		PublicURL:        publicURL,
		Introspector:     auth.NewIntrospector(backendURL, secret),
	})

	log.Printf("norviq-mcp listening on %s (backend=%s)", addr, backendURL)
	srv := &http.Server{Addr: addr, Handler: handler}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
