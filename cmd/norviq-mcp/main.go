// Command norviq-mcp is the remote MCP server that lets external AI clients
// operate on a user's norviq account over Streamable HTTP.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/server"
)

// How long to let in-flight MCP calls finish after SIGTERM.
//
// Must stay comfortably under the pod's terminationGracePeriodSeconds — the
// chart does not set one, so it is Kubernetes' default of 30s. Overshooting
// only converts a clean drain back into the SIGKILL this exists to avoid.
const shutdownTimeout = 20 * time.Second

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

	srv := &http.Server{Addr: addr, Handler: handler}

	// Kubernetes sends SIGTERM and waits before SIGKILL. With no handler the Go
	// runtime exits 143 immediately, which surfaces as an `Error` pod on every
	// single deploy and drops whatever tool call was mid-flight — an agent sees
	// a truncated response rather than a result. Draining turns both into a
	// clean exit 0.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("norviq-mcp listening on %s (backend=%s)", addr, backendURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		log.Fatal(err)
	case <-ctx.Done():
		// Restore default signal handling, so a second SIGTERM from an
		// impatient operator kills the process instead of being swallowed.
		stop()
		log.Print("norviq-mcp received shutdown signal, draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Streamable HTTP holds long-lived responses open, so a session still
		// streaming when the signal arrives will hit the deadline. Worth a line
		// in the log, but not worth a non-zero exit: the drain still ran.
		log.Printf("norviq-mcp shutdown did not complete cleanly: %v", err)
	}
	log.Print("norviq-mcp stopped")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
