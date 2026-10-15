package tools_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/tools"
)

const marketBriefJSON = `{
  "enabled": true,
  "tradingDate": "2026-10-15",
  "slot": "morning",
  "language": "en",
  "greeting": "Good morning. Futures are softer ahead of the open.",
  "groups": [
    {"id": "futures", "title": "US futures", "tone": "down", "rows": [
      {"symbol": "ES", "flag": "🇺🇸", "name": "S&P 500 futures", "level": "5,812.25", "changePercent": "0.42", "direction": "down"},
      {"symbol": "NQ", "flag": "🇺🇸", "name": "Nasdaq 100 futures", "level": "20,410.50", "changePercent": "0.15", "direction": "up"},
      {"symbol": "YM", "flag": "🇺🇸", "name": "Dow futures", "level": "42,100", "changePercent": "0.00", "direction": "sideways"}
    ]}
  ],
  "items": [
    {"kind": "earnings", "text": "Apple reports after the close.", "tickers": ["AAPL"], "sourceUrl": "https://www.reuters.com/markets/apple-earnings"},
    {"kind": "rumour", "text": "Unknown kinds still render.", "tickers": [], "sourceUrl": null}
  ],
  "generatedAt": "2026-10-15T07:15:03Z",
  "degraded": true,
  "previous": {"slot": "evening", "tradingDate": "2026-10-14"}
}`

var marketScopes = map[string]bool{"market:read": true}

// briefBackend serves GET /v1/market/brief with a fixed status and body and
// records every query it receives.
func briefBackend(t *testing.T, status int, body string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/v1/market/brief" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":true,"reason":"Not Found"}`))
			return
		}
		seen = append(seen, r.URL.Query())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestMarketBriefToolIsReadOnlyAndGatedOnMarketRead(t *testing.T) {
	backend, _ := briefBackend(t, http.StatusOK, marketBriefJSON)

	cs := connect(t, marketScopes, backend.URL, nil)
	var found bool
	for _, tool := range listPilotTools(t, cs) {
		if tool.Name != "get_market_brief" {
			continue
		}
		found = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("get_market_brief must carry ReadOnlyHint")
		}
	}
	if !found {
		t.Fatal("expected get_market_brief to be exposed for market:read")
	}
	for _, name := range tools.WriteToolNames() {
		if name == "get_market_brief" {
			t.Error("get_market_brief is read-only but is listed as a write tool")
		}
	}

	other := connect(t, map[string]bool{"portfolio:read": true}, backend.URL, nil)
	for _, tool := range listPilotTools(t, other) {
		if tool.Name == "get_market_brief" {
			t.Error("get_market_brief must not be exposed without market:read")
		}
	}
}

func TestGetMarketBriefLatest(t *testing.T) {
	backend, seen := briefBackend(t, http.StatusOK, marketBriefJSON)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", nil)
	if isErr {
		t.Fatalf("get_market_brief failed: %s", text)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected one backend call, got %d", len(*seen))
	}
	q := (*seen)[0]
	if q.Get("lang") != "en" || q.Has("slot") || q.Has("date") {
		t.Errorf("latest brief must send lang=en and no slot or date, got %v", q)
	}
	for _, want := range []string{
		"morning pre-market brief for 2026-10-15 (en)",
		"generated 2026-10-15T07:15:03Z",
		"same one",
		"degraded",
		`Previous brief: evening market-close recap for 2026-10-14. To read it, call get_market_brief with slot="evening" and date="2026-10-14".`,
		`<untrusted_data source="market_brief">`,
		"Good morning. Futures are softer",
		"US futures (down)",
		"- 🇺🇸 S&P 500 futures (ES): 5,812.25, -0.42%",
		"- 🇺🇸 Nasdaq 100 futures (NQ): 20,410.50, +0.15%",
		"- 🇺🇸 Dow futures (YM): 42,100, 0.00%",
		"- [earnings] Apple reports after the close. (tickers: AAPL; source: reuters.com)",
		"- [rumour] Unknown kinds still render.\n",
		`"changePercent": "0.42"`,
		`"sourceUrl": "https://www.reuters.com/markets/apple-earnings"`,
		"</untrusted_data>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestGetMarketBriefPassesSlotDateAndLanguage(t *testing.T) {
	ptBrief := strings.Replace(marketBriefJSON, `"language": "en"`, `"language": "pt-PT"`, 1)
	backend, seen := briefBackend(t, http.StatusOK, ptBrief)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", map[string]any{
		"language": "pt", "slot": " Evening ", "date": "2026-10-14",
	})
	if isErr {
		t.Fatalf("get_market_brief failed: %s", text)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected one backend call, got %d", len(*seen))
	}
	q := (*seen)[0]
	if q.Get("lang") != "pt-PT" || q.Get("slot") != "evening" || q.Get("date") != "2026-10-14" {
		t.Errorf("expected lang=pt-PT slot=evening date=2026-10-14, got %v", q)
	}
	if !strings.Contains(text, `and language="pt-PT"`) {
		t.Errorf("previous pointer must carry the non-default language: %s", text)
	}
}

func TestGetMarketBriefRejectsBadArgumentsWithoutCallingBackend(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"slot without date", map[string]any{"slot": "morning"}, "date is required with slot"},
		{"date without slot", map[string]any{"date": "2026-10-14"}, "slot is required with date"},
		{"unknown slot", map[string]any{"slot": "midday", "date": "2026-10-14"}, "not a brief slot"},
		{"malformed date", map[string]any{"slot": "morning", "date": "14/10/2026"}, "yyyy-MM-dd"},
		{"unknown language", map[string]any{"language": "pt-BR"}, "en (English) or pt-PT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, seen := briefBackend(t, http.StatusOK, marketBriefJSON)
			cs := connect(t, marketScopes, backend.URL, nil)

			text, isErr := callPilotTool(t, cs, "get_market_brief", tc.args)
			if !isErr {
				t.Errorf("expected a validation error, got %s", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Errorf("expected %q in %s", tc.want, text)
			}
			if len(*seen) != 0 {
				t.Errorf("validation must happen before any backend call, saw %v", *seen)
			}
		})
	}
}

func TestGetMarketBriefDisabled(t *testing.T) {
	body := `{"enabled":false,"tradingDate":null,"slot":null,"language":"en","greeting":null,"groups":[],"items":[],"generatedAt":null,"degraded":false,"previous":null}`
	backend, _ := briefBackend(t, http.StatusOK, body)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", nil)
	if isErr {
		t.Errorf("a disabled feature is not an error: %s", text)
	}
	if !strings.Contains(text, "not enabled") {
		t.Errorf("expected a not-enabled message, got %s", text)
	}
}

func TestGetMarketBriefNothingGeneratedYet(t *testing.T) {
	body := `{"enabled":true,"tradingDate":null,"slot":null,"language":"en","greeting":null,"groups":[],"items":[],"generatedAt":null,"degraded":false,"previous":null}`
	backend, _ := briefBackend(t, http.StatusOK, body)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", nil)
	if isErr {
		t.Errorf("no brief yet is not an error: %s", text)
	}
	if !strings.Contains(text, "No market brief has been generated yet") || !strings.Contains(text, "08:15") {
		t.Errorf("expected a no-brief-yet message with the schedule, got %s", text)
	}
}

func TestGetMarketBriefMissingSlotAndDate(t *testing.T) {
	backend, _ := briefBackend(t, http.StatusNotFound, `{"error":true,"reason":"No market brief for that date and slot."}`)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", map[string]any{"slot": "evening", "date": "2026-10-10"})
	if isErr {
		t.Errorf("a missing brief is an answer, not an error: %s", text)
	}
	if !strings.Contains(text, "There is no evening brief for 2026-10-10 in en") {
		t.Errorf("expected a missing-brief message, got %s", text)
	}
	if strings.Contains(text, "your norviq account") {
		t.Errorf("a shared brief must not read as missing account data: %s", text)
	}
}

func TestGetMarketBriefBackendRejection(t *testing.T) {
	backend, _ := briefBackend(t, http.StatusBadRequest, `{"error":true,"reason":"date must be yyyy-MM-dd when slot is given"}`)
	cs := connect(t, marketScopes, backend.URL, nil)

	text, isErr := callPilotTool(t, cs, "get_market_brief", map[string]any{"slot": "morning", "date": "2026-10-14"})
	if !isErr {
		t.Errorf("a 400 is an error: %s", text)
	}
	if !strings.Contains(text, "date must be yyyy-MM-dd when slot is given") {
		t.Errorf("expected the backend reason, got %s", text)
	}
}
