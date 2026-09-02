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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tax/dashboard":
			_, _ = w.Write([]byte(`{"taxYear":2026,"jurisdiction":"US","taxDrag":{"projectedYearEndTax":{"amount":1200,"currency":"USD"}},"locationOpportunities":[{"id":"location:1"}],"opportunities":[{"id":"lot-1","replacementCandidates":[{"symbol":"REPL"}]}],"disclaimer":"Educational estimate only."}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tax/loss-carryforwards":
			_, _ = w.Write([]byte(`{"asOfTaxYear":2026,"balances":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/budget/dca-capacity":
			_, _ = w.Write([]byte(`{"symbol":"VWCE","resolvedFrom":"default","quoteStale":true,"surplusAmount":240,"surplusUnits":1.82,"currencyCode":"EUR","categories":[],"disclaimer":"Equivalent at last price."}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/portfolio/summary":
			_, _ = w.Write([]byte(`{"totalMarketValue":10000}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/news/feed":
			_, _ = w.Write([]byte(`[{"id":"n1","symbol":"AAPL","headline":"Apple launches thing","source":"Norviq","url":"https://example.com/aapl","summary":"A short summary","publishedAt":"2026-09-01T10:00:00Z","createdAt":"2026-09-01T10:00:00Z","updatedAt":"2026-09-01T10:00:00Z"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/market/search":
			_, _ = w.Write([]byte(`[{"symbol":"AAPL","name":"Apple Inc.","exchange":"NASDAQ","currency":"USD","conid":"123"},{"symbol":"AAPLX","name":"Apple Holdings","exchange":"NYSE","currency":"USD","conid":"456"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/market/news":
			_, _ = w.Write([]byte(`[{"title":"Apple in focus","url":"https://example.com/market","date":"2026-09-01"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/market/news/general":
			_, _ = w.Write([]byte(`[{"title":"Markets rally","url":"https://example.com/rally","date":"2026-09-01"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// connect wires a client session to an in-memory server. A non-nil elicit
// handler advertises form-elicitation support, mirroring a client that can
// confirm financial writes.
func connect(t *testing.T, scopes map[string]bool, backendURL string, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "norviq", Version: "test"}, nil)
	client := api.NewClient(backendURL, "nvq_pat_test")
	tools.Register(s, client, &auth.Principal{Token: "nvq_pat_test", UserID: "u1", Scopes: scopes, Entitled: true})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = s.Run(context.Background(), serverTransport) }()

	var opts *mcp.ClientOptions
	if elicit != nil {
		opts = &mcp.ClientOptions{ElicitationHandler: elicit}
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, opts)
	cs, err := c.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestReadScopeOnlyExposesReadTools(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:read": true}, backend.URL, nil)

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

func acceptElicit(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
}

func declineElicit(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return &mcp.ElicitResult{Action: "decline"}, nil
}

func TestAddExpenseSendsIdempotencyKey(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:write": true}, backend.URL, acceptElicit)

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
	cs := connect(t, map[string]bool{"expenses:write": true}, backend.URL, declineElicit)

	args, _ := json.Marshal(map[string]any{"id": "e1"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "delete_expense", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range *seen {
		if strings.HasPrefix(s, "DELETE") {
			t.Error("delete_expense hit the backend without user confirmation")
		}
	}
	if res.IsError {
		t.Error("declined delete should be a soft message, not an error")
	}
}

func TestWriteToolBlockedWithoutElicitation(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"expenses:write": true}, backend.URL, nil)

	args, _ := json.Marshal(map[string]any{"id": "e1"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "delete_expense", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("write tool must error for a client without form elicitation")
	}
	for _, s := range *seen {
		if strings.HasPrefix(s, "DELETE") {
			t.Error("delete_expense hit the backend despite missing elicitation support")
		}
	}
}

func TestReportTool(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"reports:read": true}, backend.URL, nil)

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

func TestTaxReadScopeExposesAndCallsTaxTools(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"tax:read": true}, backend.URL, nil)

	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	if !names["get_tax_dashboard"] || !names["get_tax_loss_carryforwards"] {
		t.Fatalf("tax:read did not expose both tax tools: %v", names)
	}

	args, _ := json.Marshal(map[string]any{"jurisdiction": "US", "tax_year": 2026})
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_tax_dashboard", Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("get_tax_dashboard returned error: %+v", result.Content)
	}
	content, _ := json.Marshal(result.Content)
	for _, expected := range []string{"taxDrag", "locationOpportunities", "replacementCandidates"} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("enriched tax field %q was not preserved: %s", expected, content)
		}
	}
	if len(*seen) == 0 || (*seen)[len(*seen)-1] != "GET /v1/tax/dashboard" {
		t.Fatalf("backend did not receive tax dashboard request; saw %v", *seen)
	}
}

func TestMarketReadScopeExposesNewsTool(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"market:read": true}, backend.URL, nil)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	if !names["get_news"] {
		t.Fatal("expected get_news to be exposed for market:read")
	}
}

func TestGetNewsDefaultUsesTrackedFeed(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"market:read": true}, backend.URL, nil)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_news", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_news returned error: %+v", res.Content)
	}
	content, _ := json.Marshal(res.Content)
	if !strings.Contains(string(content), "Apple launches thing") {
		t.Fatalf("tracked feed item missing from result: %s", content)
	}
	found := false
	for _, s := range *seen {
		if s == "GET /v1/news/feed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("backend never received tracked news request; saw %v", *seen)
	}
}

func TestGetNewsQueryResolvesSymbolsAndFetchesMarketNews(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"market:read": true}, backend.URL, nil)

	args, _ := json.Marshal(map[string]any{"query": "Apple", "max_results": 3})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_news", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_news returned error: %+v", res.Content)
	}
	content, _ := json.Marshal(res.Content)
	for _, expected := range []string{"Apple in focus", "AAPL"} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("query news result missing %q: %s", expected, content)
		}
	}
	seenSearch := false
	seenMarket := false
	for _, s := range *seen {
		if s == "GET /v1/market/search" {
			seenSearch = true
		}
		if s == "GET /v1/market/news" {
			seenMarket = true
		}
	}
	if !seenSearch || !seenMarket {
		t.Fatalf("backend did not receive both search and market news requests; saw %v", *seen)
	}
}

func TestGetNewsGeneralSourceUsesArchiveWindow(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"market:read": true}, backend.URL, nil)

	args, _ := json.Marshal(map[string]any{"source": "general", "lookback_days": 3, "max_results": 3})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_news", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_news returned error: %+v", res.Content)
	}
	content, _ := json.Marshal(res.Content)
	if !strings.Contains(string(content), "Markets rally") {
		t.Fatalf("general news item missing from result: %s", content)
	}
	found := false
	for _, s := range *seen {
		if s == "GET /v1/market/news/general" {
			found = true
		}
	}
	if !found {
		t.Fatalf("backend never received general market news request; saw %v", *seen)
	}
}
