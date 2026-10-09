package tools_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	terminalRowA    = "11111111-1111-4111-8111-111111111111"
	terminalRowB    = "22222222-2222-4222-8222-222222222222"
	terminalRowTSLA = "33333333-3333-4333-8333-333333333333"

	terminalDisclaimerText = "Terminal prices are your assumptions, not forecasts. Not financial advice."

	// Swift omits nil optionals: an invalid scenario has no derived keys at all.
	invalidTerminalRowJSON = `{"id":"44444444-4444-4444-8444-444444444444","ticker":"VG","terminalShareCount":0,"terminalMarketCap":8000000000,"valueWanted":500000,"sharesOwned":0,"sortOrder":0,"scenarioError":"share_count_not_positive","createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"}`

	shareFactsJSON = `{"ticker":"AMZN","sharesOutstanding":10600000000,"currentSharePrice":201.5,"currency":"USD","asOf":"2026-10-08","sources":["https://ir.aboutamazon.com/quarterly-results"]}`
)

// terminalRowJSON is a position as the backend renders it, using the spec's
// AMZN inputs (10T cap, 11B shares, 1M wanted, 750 owned). The derived values
// are canned and deliberately NOT what those inputs give (123.45 instead of
// 909.09…). MCP must pass Norviq's numbers through; a recomputation would show
// up as 909.09.
func terminalRowJSON(id, ticker string, sortOrder int) string {
	return fmt.Sprintf(`{"id":%q,"ticker":%q,"sharesOutstanding":10600000000,"terminalShareCount":11000000000,"terminalMarketCap":10000000000000,"valueWanted":1000000,"sharesOwned":750,"currentSharePrice":200,"sortOrder":%d,"terminalSharePrice":123.45,"sharesNeeded":4321,"capitalAtTodayPrice":864200,"progress":0.1736,"sharesStillNeeded":3571,"gapValueAtTerminal":440839.95,"createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"}`,
		id, ticker, sortOrder)
}

type terminalCall struct {
	Method, Path, Query, IdempotencyKey string
	Body                                map[string]any
}

type terminalFailure struct {
	status int
	body   string
}

// terminalFake fakes the backend's terminal-position routes and records calls.
type terminalFake struct {
	// rows maps an upper-case ticker to the row JSON objects GET ?ticker= answers
	// with; an unknown ticker answers an empty list.
	rows map[string][]string
	// ignoreFilter makes every GET answer with every row, as a backend that
	// ignored ?ticker= would.
	ignoreFilter bool
	// fail maps "METHOD /path" to a canned error response.
	fail  map[string]terminalFailure
	calls []terminalCall
}

func newTerminalFake() *terminalFake {
	return &terminalFake{rows: map[string][]string{}, fail: map[string]terminalFailure{}}
}

func (f *terminalFake) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := terminalCall{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			IdempotencyKey: r.Header.Get("Idempotency-Key"),
		}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.Body)
		}
		f.calls = append(f.calls, call)
		w.Header().Set("Content-Type", "application/json")
		if failure, ok := f.fail[r.Method+" "+r.URL.Path]; ok {
			w.WriteHeader(failure.status)
			_, _ = w.Write([]byte(failure.body))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/terminal-positions":
			ticker := strings.ToUpper(r.URL.Query().Get("ticker"))
			var rows []string
			for key, list := range f.rows {
				if ticker == "" || f.ignoreFilter || key == ticker {
					rows = append(rows, list...)
				}
			}
			_, _ = w.Write([]byte(`{"currency":"USD","positions":[` + strings.Join(rows, ",") + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/terminal-positions":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(terminalRowJSON(terminalRowTSLA, "TSLA", 0)))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/v1/terminal-positions/"):
			_, _ = w.Write([]byte(terminalRowJSON(strings.TrimPrefix(r.URL.Path, "/v1/terminal-positions/"), "AMZN", 0)))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/terminal-positions/ai/share-facts":
			_, _ = w.Write([]byte(shareFactsJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":true,"reason":"Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writes counts calls that would change a terminal position.
func (f *terminalFake) writes() int {
	n := 0
	for _, c := range f.calls {
		create := c.Method == http.MethodPost && c.Path == "/v1/terminal-positions"
		if create || c.Method == http.MethodPatch || c.Method == http.MethodDelete {
			n++
		}
	}
	return n
}

func terminalTools(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	byName := map[string]*mcp.Tool{}
	for _, tool := range listPilotTools(t, cs) {
		byName[tool.Name] = tool
	}
	return byName
}

// assertTerminalNotice pins what every terminal tool must tell the model before
// it says anything about a terminal scenario.
func assertTerminalNotice(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	for _, want := range []string{
		"planning math, not advice",
		"must come from the user or from sources you cite",
		"terminalSharePrice = terminalMarketCap / terminalShareCount",
		"sharesNeeded = valueWanted × terminalShareCount / terminalMarketCap",
		"Never compute, round or invent them yourself",
		terminalDisclaimerText,
	} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("%s description is missing %q", tool.Name, want)
		}
	}
}

var terminalReadTools = []string{"get_terminal_positions", "get_terminal_position"}

func TestTerminalReadToolsNeedPlanningRead(t *testing.T) {
	f := newTerminalFake()
	backendURL := f.server(t).URL

	without := terminalTools(t, connect(t, map[string]bool{"goals:read": true, "portfolio:read": true}, backendURL, nil))
	for _, name := range terminalReadTools {
		if without[name] != nil {
			t.Errorf("%s exposed without planning:read", name)
		}
	}

	with := terminalTools(t, connect(t, map[string]bool{"planning:read": true}, backendURL, nil))
	for _, name := range terminalReadTools {
		tool := with[name]
		if tool == nil {
			t.Errorf("%s not exposed with planning:read", name)
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must carry ReadOnlyHint", name)
		}
		if slices.Contains(tools.WriteToolNames(), name) {
			t.Errorf("%s is a read tool and must not be in WriteToolNames()", name)
		}
		assertTerminalNotice(t, tool)
	}
}

func TestGetTerminalPositionsPassesNorviqsNumbersThrough(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	f.rows["VG"] = []string{invalidTerminalRowJSON}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_positions", nil)
	if isErr {
		t.Fatalf("get_terminal_positions failed: %s", text)
	}
	for _, want := range []string{
		`"currency": "USD"`,
		`"terminalSharePrice": 123.45`,
		`"sharesNeeded": 4321`,
		`"scenarioError": "share_count_not_positive"`,
		terminalDisclaimerText,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "909.09") {
		t.Error("MCP computed a terminal share price instead of passing Norviq's through")
	}
	if len(f.calls) != 1 || f.calls[0].Query != "" {
		t.Errorf("calls = %+v, want one unfiltered GET", f.calls)
	}
	if f.writes() != 0 {
		t.Error("a read tool wrote to the backend")
	}
}

func TestGetTerminalPositionNormalizesTheTicker(t *testing.T) {
	f := newTerminalFake()
	f.rows["BRK.B"] = []string{terminalRowJSON(terminalRowA, "BRK.B", 0)}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": "  brk.b "})
	if isErr {
		t.Fatalf("get_terminal_position failed: %s", text)
	}
	if len(f.calls) != 1 || f.calls[0].Query != "ticker=BRK.B" {
		t.Fatalf("calls = %+v, want one GET with ticker=BRK.B", f.calls)
	}
	if !strings.Contains(text, `"ticker": "BRK.B"`) || !strings.Contains(text, terminalDisclaimerText) {
		t.Errorf("unexpected result:\n%s", text)
	}
}

func TestGetTerminalPositionRejectsANonTickerWithoutCallingNorviq(t *testing.T) {
	for _, bad := range []string{"", "Amazon.com Inc", "$AMZN", "ABCDEFGHIJKLM"} {
		f := newTerminalFake()
		cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)
		text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": bad})
		if !isErr || !strings.Contains(text, "not a valid ticker") {
			t.Errorf("ticker %q: got %q (error=%v), want a not-a-valid-ticker error", bad, text, isErr)
		}
		if len(f.calls) != 0 {
			t.Errorf("ticker %q reached the backend: %+v", bad, f.calls)
		}
	}
}

func TestGetTerminalPositionPicksTheFirstRowBySortOrder(t *testing.T) {
	f := newTerminalFake()
	// Deliberately out of order: the first row in the response is not first by sortOrder.
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowB, "AMZN", 4), terminalRowJSON(terminalRowA, "AMZN", 1)}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": "AMZN"})
	if isErr {
		t.Fatalf("get_terminal_position failed: %s", text)
	}
	if !strings.Contains(text, `"id": "`+terminalRowA+`"`) || strings.Contains(text, terminalRowB) {
		t.Errorf("want only the sortOrder-1 row %s:\n%s", terminalRowA, text)
	}
	if !strings.Contains(text, `"scenariosForTicker": 2`) {
		t.Errorf("want scenariosForTicker 2:\n%s", text)
	}
}

func TestGetTerminalPositionIgnoresRowsForOtherTickers(t *testing.T) {
	f := newTerminalFake()
	f.ignoreFilter = true
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	f.rows["TSLA"] = []string{terminalRowJSON(terminalRowTSLA, "TSLA", 1)}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": "TSLA"})
	if isErr {
		t.Fatalf("get_terminal_position failed: %s", text)
	}
	if !strings.Contains(text, terminalRowTSLA) || strings.Contains(text, terminalRowA) {
		t.Errorf("want only the TSLA row even when the backend returns others:\n%s", text)
	}
	if !strings.Contains(text, `"scenariosForTicker": 1`) {
		t.Errorf("want scenariosForTicker 1:\n%s", text)
	}
}

func TestGetTerminalPositionSaysWhenThereIsNone(t *testing.T) {
	f := newTerminalFake()
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": "tsla"})
	if isErr {
		t.Fatalf("no row is an answer, not an error: %s", text)
	}
	if !strings.Contains(text, "No terminal scenario for TSLA") {
		t.Errorf("got %q", text)
	}
	if !strings.Contains(text, terminalDisclaimerText) {
		t.Errorf("no-row answer is missing the disclaimer:\n%s", text)
	}
}

func TestGetTerminalPositionReportsAnInvalidScenario(t *testing.T) {
	f := newTerminalFake()
	f.rows["VG"] = []string{invalidTerminalRowJSON}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "get_terminal_position", map[string]any{"ticker": "VG"})
	if isErr {
		t.Fatalf("get_terminal_position failed: %s", text)
	}
	for _, want := range []string{
		`"scenarioError": "share_count_not_positive"`,
		`"terminalSharePrice": null`,
		`"sharesNeeded": null`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %s:\n%s", want, text)
		}
	}
}

const (
	shareFactsPath = "POST /v1/terminal-positions/ai/share-facts"

	// BillingErrorMiddleware's body for a non-Pro user: HTTP 403, code upgrade_required.
	upgradeRequiredJSON = `{"success":false,"code":"upgrade_required","error":"Upgrade required","feature":"terminal_position_ai","plan":"free","requiredPlan":"pro"}`
)

func TestLookupShareFactsIsAProReadTool(t *testing.T) {
	f := newTerminalFake()
	backendURL := f.server(t).URL
	if terminalTools(t, connect(t, map[string]bool{"market:read": true}, backendURL, nil))["lookup_share_facts"] != nil {
		t.Error("lookup_share_facts exposed without planning:read")
	}
	tool := terminalTools(t, connect(t, map[string]bool{"planning:read": true}, backendURL, nil))["lookup_share_facts"]
	if tool == nil {
		t.Fatal("lookup_share_facts not exposed with planning:read")
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || slices.Contains(tools.WriteToolNames(), "lookup_share_facts") {
		t.Error("lookup_share_facts must be a ReadOnlyHint tool outside WriteToolNames()")
	}
	for _, want := range []string{"Norviq Pro", "saves nothing"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description is missing %q", want)
		}
	}
	assertTerminalNotice(t, tool)
}

func TestLookupShareFactsReturnsASuggestionAndWritesNothing(t *testing.T) {
	f := newTerminalFake()
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "lookup_share_facts", map[string]any{"ticker": " amzn"})
	if isErr {
		t.Fatalf("lookup_share_facts failed: %s", text)
	}
	if len(f.calls) != 1 || f.calls[0].Method+" "+f.calls[0].Path != shareFactsPath || f.calls[0].Body["ticker"] != "AMZN" {
		t.Fatalf("calls = %+v, want one share-facts POST for AMZN", f.calls)
	}
	if f.writes() != 0 {
		t.Error("lookup_share_facts wrote a terminal position")
	}
	for _, want := range []string{`"sharesOutstanding": 10600000000`, "https://ir.aboutamazon.com/quarterly-results", `"saved": false`} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %s:\n%s", want, text)
		}
	}
}

func TestLookupShareFactsExplainsTheProUpgrade(t *testing.T) {
	f := newTerminalFake()
	f.fail[shareFactsPath] = terminalFailure{http.StatusForbidden, upgradeRequiredJSON}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "lookup_share_facts", map[string]any{"ticker": "AMZN"})
	if !isErr || !strings.Contains(text, "lookup_share_facts needs Norviq Pro") {
		t.Errorf("got %q (error=%v), want the Pro upgrade message", text, isErr)
	}
}

func TestLookupShareFactsDoesNotCallAMissingScopeAnUpgrade(t *testing.T) {
	f := newTerminalFake()
	f.fail[shareFactsPath] = terminalFailure{http.StatusForbidden, `{"error":true,"reason":"insufficient_scope: 'planning:read' required"}`}
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

	text, isErr := callPilotTool(t, cs, "lookup_share_facts", map[string]any{"ticker": "AMZN"})
	if !isErr {
		t.Fatal("a 403 must be an error")
	}
	if strings.Contains(text, "lookup_share_facts needs Norviq Pro") {
		t.Errorf("a missing scope was reported as a Pro upgrade: %q", text)
	}
}

func TestLookupShareFactsSaysWhenTheAILookupCannotAnswer(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusServiceUnavailable, "unavailable right now"},
		{http.StatusUnprocessableEntity, "could not find usable, sourced numbers"},
	}
	for _, tc := range cases {
		f := newTerminalFake()
		f.fail[shareFactsPath] = terminalFailure{tc.status, `{"error":true,"reason":"AI lookup unavailable"}`}
		cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)

		text, isErr := callPilotTool(t, cs, "lookup_share_facts", map[string]any{"ticker": "AMZN"})
		if !isErr || !strings.Contains(text, tc.want) || !strings.Contains(text, "Do not guess them") {
			t.Errorf("status %d: got %q (error=%v), want %q and a do-not-guess instruction", tc.status, text, isErr, tc.want)
		}
	}
}

func TestLookupShareFactsRejectsANonTicker(t *testing.T) {
	f := newTerminalFake()
	cs := connect(t, map[string]bool{"planning:read": true}, f.server(t).URL, nil)
	// "AMAZON" would be a syntactically valid ticker; a name with a space is not.
	text, isErr := callPilotTool(t, cs, "lookup_share_facts", map[string]any{"ticker": "Amazon Inc"})
	if !isErr || !strings.Contains(text, "not a valid ticker") {
		t.Errorf("got %q (error=%v), want a not-a-valid-ticker error", text, isErr)
	}
	if len(f.calls) != 0 {
		t.Errorf("an invalid ticker reached the backend: %+v", f.calls)
	}
}
