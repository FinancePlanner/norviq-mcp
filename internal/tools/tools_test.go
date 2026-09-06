package tools_test

import (
	"context"
	"encoding/json"
	"io"
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/watchlist":
			_, _ = w.Write([]byte(`[{"id":"w1","symbol":"AVGO","note":"Buy at $345-$355","status":"waiting"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/watchlist":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("upsert_watchlist_items did not send an Idempotency-Key")
			}
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"w1","symbol":"` + req["symbol"].(string) + `","status":"waiting"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/watchlist/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/transactions":
			_, _ = w.Write([]byte(`[{"id":"t1","accountId":"a1","instrumentId":"AVGO","type":"buy","quantity":10,"price":350,"currency":"USD","tradeDate":"2026-09-01"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/transactions":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("record_trades did not send an Idempotency-Key")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"t2","accountId":"a1","instrumentId":"AVGO","type":"buy","quantity":10,"price":350,"currency":"USD","tradeDate":"2026-09-01"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/stocks":
			_, _ = w.Write([]byte(`{"items":[{"id":"s1","symbol":"AVGO","shares":10,"buyPrice":350,"buyDate":"2026-09-01","category":"stock","createdAt":"2026-09-01T10:00:00Z"}],"nextCursor":null}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/stocks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"s2","symbol":"TSM","shares":5,"buyPrice":410,"buyDate":"2026-09-02","category":"stock"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sell"):
			_, _ = w.Write([]byte(`{"id":"s1","symbol":"AVGO","shares":5,"buyPrice":350,"buyDate":"2026-09-01","category":"stock"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/targets":
			_, _ = w.Write([]byte(`[{"id":"tg1","symbol":"AVGO","scenario":"base","targetPrice":400}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/research":
			_, _ = w.Write([]byte(`[{"id":"r1","symbol":"AVGO","thesis":"Networking demand"}]`))
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

// --- Watchlist -------------------------------------------------------------

func TestWatchlistBatchWriteAsksOneConfirmation(t *testing.T) {
	backend, seen := fakeBackend(t)
	var prompts int
	elicit := func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		return acceptElicit(ctx, req)
	}
	cs := connect(t, map[string]bool{"watchlist:write": true}, backend.URL, elicit)

	// The seven-row case that could not be written before.
	items := []map[string]any{}
	for _, sym := range []string{"AVGO", "TSM", "AMAT", "LRCX", "KLAC", "ASML", "MRVL"} {
		items = append(items, map[string]any{"symbol": sym, "status": "waiting", "note": "zone"})
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "upsert_watchlist_items",
		Arguments: map[string]any{"items": items},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("batch upsert failed: %s", mustJSON(t, res.Content))
	}
	if prompts != 1 {
		t.Errorf("expected exactly 1 confirmation for a 7-row batch, got %d", prompts)
	}
	var posts int
	for _, entry := range *seen {
		if entry == "POST /v1/watchlist" {
			posts++
		}
	}
	if posts != 7 {
		t.Errorf("expected 7 backend writes, got %d", posts)
	}
}

func TestWatchlistRejectsUnknownStatusBeforeWriting(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"watchlist:write": true}, backend.URL, acceptElicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "upsert_watchlist_items",
		Arguments: map[string]any{
			"items": []map[string]any{{"symbol": "AVGO", "status": "waitng"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected a typo'd status to be rejected")
	}
	for _, entry := range *seen {
		if strings.HasPrefix(entry, "POST /v1/watchlist") {
			t.Error("an invalid status must not reach the backend")
		}
	}
}

func TestWatchlistDeclinedRemovalDoesNotDelete(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"watchlist:write": true}, backend.URL, declineElicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "remove_watchlist_items",
		Arguments: map[string]any{"ids": []string{"w1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Error("a declined confirmation is a soft outcome, not an error")
	}
	for _, entry := range *seen {
		if strings.HasPrefix(entry, "DELETE ") {
			t.Error("declined removal must not reach the backend")
		}
	}
}

func TestWatchlistReadScopeHidesWrites(t *testing.T) {
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"watchlist:read": true}, backend.URL, acceptElicit)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	if !names["list_watchlist"] {
		t.Error("expected list_watchlist with watchlist:read")
	}
	for _, blocked := range []string{"upsert_watchlist_items", "remove_watchlist_items"} {
		if names[blocked] {
			t.Errorf("%s must not be exposed without watchlist:write", blocked)
		}
	}
}

// --- Trades ----------------------------------------------------------------

func TestRecordTradesBatchesOneConfirmation(t *testing.T) {
	backend, seen := fakeBackend(t)
	var prompts int
	elicit := func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		return acceptElicit(ctx, req)
	}
	cs := connect(t, map[string]bool{"transactions:write": true}, backend.URL, elicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_trades",
		Arguments: map[string]any{"trades": []map[string]any{
			{"symbol": "AVGO", "type": "buy", "quantity": 10, "price": 350, "trade_date": "2026-09-01"},
			{"symbol": "TSM", "type": "sell", "quantity": 5, "price": 410, "trade_date": "2026-09-02"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record_trades failed: %s", mustJSON(t, res.Content))
	}
	if prompts != 1 {
		t.Errorf("expected 1 confirmation for a 2-trade batch, got %d", prompts)
	}
	var posts int
	for _, entry := range *seen {
		if entry == "POST /v1/transactions" {
			posts++
		}
	}
	if posts != 2 {
		t.Errorf("expected 2 trade writes, got %d", posts)
	}
}

func TestRecordTradesRejectsBadSideBeforeWriting(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"transactions:write": true}, backend.URL, acceptElicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_trades",
		Arguments: map[string]any{"trades": []map[string]any{
			{"symbol": "AVGO", "type": "short", "quantity": 10, "price": 350, "trade_date": "2026-09-01"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected an unsupported trade side to be rejected")
	}
	for _, entry := range *seen {
		if entry == "POST /v1/transactions" {
			t.Error("an invalid trade must not reach the backend")
		}
	}
}

func TestNewWriteToolsRequireElicitation(t *testing.T) {
	backend, seen := fakeBackend(t)
	// No elicitation handler: the client cannot confirm, so a write must refuse
	// rather than proceed unconfirmed. (server.go additionally strips these tools
	// from the session on initialize; this harness registers them directly, so
	// here we assert the second line of defence in the handler itself.)
	cs := connect(t, map[string]bool{
		"watchlist:write": true, "transactions:write": true,
	}, backend.URL, nil)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"upsert_watchlist_items", map[string]any{
			"items": []map[string]any{{"symbol": "AVGO", "status": "waiting"}},
		}},
		{"remove_watchlist_items", map[string]any{"ids": []string{"w1"}}},
		{"record_trades", map[string]any{"trades": []map[string]any{
			{"symbol": "AVGO", "type": "buy", "quantity": 10, "price": 350, "trade_date": "2026-09-01"},
		}}},
	}
	for _, tc := range cases {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name: tc.name, Arguments: tc.args,
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !res.IsError {
			t.Errorf("%s must error for a client without form elicitation", tc.name)
		}
	}
	for _, entry := range *seen {
		if strings.HasPrefix(entry, "POST") || strings.HasPrefix(entry, "DELETE") {
			t.Errorf("unconfirmed write reached the backend: %s", entry)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// --- Holdings, targets, research -------------------------------------------

func TestSellPositionRequiresConfirmationAndWarns(t *testing.T) {
	backend, seen := fakeBackend(t)
	var prompt string
	elicit := func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		if req.Params != nil {
			prompt = req.Params.Message
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}
	cs := connect(t, map[string]bool{"holdings:write": true}, backend.URL, elicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "sell_position",
		Arguments: map[string]any{
			"id": "s1", "shares_to_sell": 5, "sell_price": 400, "sell_date": "2026-09-05",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("sell_position failed: %s", mustJSON(t, res.Content))
	}
	// The confirmation must say plainly that no order is placed, so nobody reads
	// "sell" as instructing the broker.
	if !strings.Contains(prompt, "does not place an order") {
		t.Errorf("sell confirmation should state that no order is placed, got: %q", prompt)
	}
	var sold bool
	for _, entry := range *seen {
		if strings.HasSuffix(entry, "/sell") {
			sold = true
		}
	}
	if !sold {
		t.Error("expected the sell to reach the backend after confirmation")
	}
}

func TestAddPositionRejectsUnknownCategory(t *testing.T) {
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"holdings:write": true}, backend.URL, acceptElicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_position",
		Arguments: map[string]any{
			"symbol": "AVGO", "shares": 10, "buy_price": 350,
			"buy_date": "2026-09-01", "category": "equity",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected an unknown asset category to be rejected")
	}
	for _, entry := range *seen {
		if entry == "POST /v1/stocks" {
			t.Error("an invalid category must not reach the backend")
		}
	}
}

func TestPerDomainScopesDoNotLeakAcrossDomains(t *testing.T) {
	backend, _ := fakeBackend(t)
	// A watchlist-only token must not see holdings, trade, target or research tools.
	cs := connect(t, map[string]bool{
		"watchlist:read": true, "watchlist:write": true,
	}, backend.URL, acceptElicit)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	if !names["upsert_watchlist_items"] {
		t.Fatal("expected the watchlist write tool to be present")
	}
	for _, leaked := range []string{
		"list_positions", "add_position", "sell_position",
		"record_trades", "list_transactions",
		"list_price_targets", "create_price_target",
		"list_research_notes", "add_research_note",
		"list_expenses", "add_expense",
	} {
		if names[leaked] {
			t.Errorf("%s must not be exposed to a watchlist-only token", leaked)
		}
	}
}

func TestEveryMutatingToolIsInTheWriteAllowlist(t *testing.T) {
	backend, _ := fakeBackend(t)
	// Every scope at once, with elicitation available.
	all := map[string]bool{}
	for _, scope := range []string{
		"watchlist:read", "watchlist:write", "holdings:read", "holdings:write",
		"transactions:read", "transactions:write", "targets:read", "targets:write",
		"research:read", "research:write", "expenses:read", "expenses:write",
		"budget:read", "budget:write", "goals:read", "goals:write",
		"reports:read", "market:read", "portfolio:read", "insights:read", "tax:read",
	} {
		all[scope] = true
	}
	cs := connect(t, all, backend.URL, acceptElicit)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	allowlist := map[string]bool{}
	for _, name := range tools.WriteToolNames() {
		allowlist[name] = true
	}
	// A tool that mutates but is missing from the allowlist would stay exposed to a
	// client that cannot confirm, which is exactly what the allowlist prevents.
	for _, tool := range res.Tools {
		mutating := tool.Annotations == nil ||
			(!tool.Annotations.ReadOnlyHint && tool.Annotations.DestructiveHint != nil) ||
			(!tool.Annotations.ReadOnlyHint && tool.Annotations.IdempotentHint)
		if mutating && !allowlist[tool.Name] {
			t.Errorf("%s mutates but is not in WriteToolNames()", tool.Name)
		}
	}
}
