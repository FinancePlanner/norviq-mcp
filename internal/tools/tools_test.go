package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBackend records the requests the tools make and returns canned responses.
func fakeBackend(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer nvq_pat_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/expenses":
			_, _ = w.Write([]byte(`[{"id":"e1","title":"Coffee","amount":4,"pillar":"fundamentals","occurredOn":"2026-07-02"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/expenses":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("add_expense did not send an Idempotency-Key")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"e2","title":"Lunch","amount":12,"pillar":"lifestyle","occurredOn":"2026-07-03"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/reports/overview":
			_, _ = w.Write([]byte(`{"total":100}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func connect(t *testing.T, scopes map[string]bool, backendURL string) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "norviq", Version: "test"}, nil)
	client := api.NewClient(backendURL, "nvq_pat_test")
	tools.Register(s, client, &auth.Principal{Token: "nvq_pat_test", UserID: "u1", Scopes: scopes, Entitled: true})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = s.Run(context.Background(), serverTransport) }()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := c.Connect(context.Background(), clientTransport)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestReadScopeOnlyExposesReadTools(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:read": true}, backend.URL)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	if !names["list_expenses"] {
		t.Error("expected list_expenses to be exposed")
	}
	if names["add_expense"] {
		t.Error("add_expense must not be exposed without expenses:write")
	}
}

func TestAddExpenseSendsIdempotencyKey(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:write": true}, backend.URL)

	args, _ := json.Marshal(map[string]any{
		"title": "Lunch", "amount": 12.0, "pillar": "lifestyle", "occurred_on": "2026-07-03",
	})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_expense", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("add_expense returned error: %+v", res.Content)
	}
	found := false
	for _, s := range *seen {
		if s == "POST /v1/expenses" {
			found = true
		}
	}
	if !found {
		t.Errorf("backend never received POST /v1/expenses; saw %v", *seen)
	}
}

func TestDeleteRequiresConfirm(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:write": true}, backend.URL)

	args, _ := json.Marshal(map[string]any{"id": "e1", "confirm": false})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "delete_expense", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range *seen {
		if strings.HasPrefix(s, "DELETE") {
			t.Error("delete_expense hit the backend without confirm=true")
		}
	}
	if res.IsError {
		t.Error("delete without confirm should be a soft message, not an error")
	}
}

func TestReportTool(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"reports:read": true}, backend.URL)

	args, _ := json.Marshal(map[string]any{"kind": "overview"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_spending_report", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("report tool errored: %+v", res.Content)
	}
}
