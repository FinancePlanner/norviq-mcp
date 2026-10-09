package tools_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var terminalWriteScopes = map[string]bool{"planning:read": true, "planning:write": true}

// confirmAndCapture accepts the confirmation and records what the user was shown.
func confirmAndCapture(prompt *string) func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		if req.Params != nil {
			*prompt = req.Params.Message
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}
}

func (f *terminalFake) callsWith(method string) []terminalCall {
	var out []terminalCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func TestSetTerminalScenarioIsAConfirmedWriteTool(t *testing.T) {
	f := newTerminalFake()
	backendURL := f.server(t).URL
	for _, scopes := range []map[string]bool{{"planning:read": true}, {"planning:write": true}} {
		if terminalTools(t, connect(t, scopes, backendURL, acceptElicit))["set_terminal_scenario"] != nil {
			t.Errorf("set_terminal_scenario exposed with only %v; it reads before it writes, so it needs both planning scopes", scopes)
		}
	}
	tool := terminalTools(t, connect(t, terminalWriteScopes, backendURL, acceptElicit))["set_terminal_scenario"]
	if tool == nil {
		t.Fatal("set_terminal_scenario not exposed with planning:read and planning:write")
	}
	if tool.Annotations == nil || tool.Annotations.ReadOnlyHint ||
		tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		t.Error("set_terminal_scenario must carry DestructiveHint true and no ReadOnlyHint")
	}
	if !slices.Contains(tools.WriteToolNames(), "set_terminal_scenario") {
		t.Error("set_terminal_scenario must be in WriteToolNames() so clients that cannot confirm never see it")
	}
	if !strings.Contains(tool.Description, "confirm") {
		t.Error("the description must say the user confirms first")
	}
	assertTerminalNotice(t, tool)
}

func TestSetTerminalScenarioUpdatesTheFirstRowAfterConfirmation(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowB, "AMZN", 3), terminalRowJSON(terminalRowA, "AMZN", 0)}
	var prompt string
	cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{
		"ticker": "amzn", "terminalMarketCap": 12000000000000,
	})
	if isErr {
		t.Fatalf("set_terminal_scenario failed: %s", text)
	}
	patches := f.callsWith(http.MethodPatch)
	if len(patches) != 1 || patches[0].Path != "/v1/terminal-positions/"+terminalRowA {
		t.Fatalf("patches = %+v, want one PATCH of the sortOrder-0 row %s", patches, terminalRowA)
	}
	if len(patches[0].Body) != 1 || patches[0].Body["terminalMarketCap"] != 12000000000000.0 {
		t.Errorf("PATCH body = %v, want only terminalMarketCap", patches[0].Body)
	}
	if len(f.callsWith(http.MethodPost)) != 0 {
		t.Error("an update must not also create a row")
	}
	for _, want := range []string{
		"Update the terminal scenario for AMZN (the first of 2 AMZN rows):",
		"- terminal market cap: 10000000000000 USD → 12000000000000 USD",
		terminalDisclaimerText,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("confirmation is missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "value wanted") {
		t.Errorf("the confirmation lists a field that is not being written:\n%s", prompt)
	}
	for _, want := range []string{`"action": "updated"`, `"currency": "USD"`, `"terminalSharePrice": 123.45`, `"sharesNeeded": 4321`} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %s:\n%s", want, text)
		}
	}
}

func TestSetTerminalScenarioWritesAZeroSharesOwned(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	var prompt string
	cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{"ticker": "AMZN", "sharesOwned": 0})
	if isErr {
		t.Fatalf("set_terminal_scenario failed: %s", text)
	}
	patches := f.callsWith(http.MethodPatch)
	if len(patches) != 1 {
		t.Fatalf("patches = %+v, want one", patches)
	}
	value, ok := patches[0].Body["sharesOwned"]
	if !ok || value != 0.0 || len(patches[0].Body) != 1 {
		t.Errorf("PATCH body = %v, want exactly sharesOwned 0", patches[0].Body)
	}
	if !strings.Contains(prompt, "- shares owned: 750 → 0") {
		t.Errorf("confirmation should show 750 → 0:\n%s", prompt)
	}
}

func TestSetTerminalScenarioCreatesARowWhenTheTickerHasNone(t *testing.T) {
	f := newTerminalFake()
	var prompt string
	cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{
		"ticker": "tsla", "terminalShareCount": 3500000000, "terminalMarketCap": 5000000000000,
		"valueWanted": 250000, "sharesOwned": 10,
	})
	if isErr {
		t.Fatalf("set_terminal_scenario failed: %s", text)
	}
	creates := f.callsWith(http.MethodPost)
	if len(creates) != 1 || creates[0].Path != "/v1/terminal-positions" {
		t.Fatalf("creates = %+v, want one POST /v1/terminal-positions", creates)
	}
	if !strings.HasPrefix(creates[0].IdempotencyKey, "mcp_") {
		t.Errorf("Idempotency-Key = %q, want an mcp_ key", creates[0].IdempotencyKey)
	}
	want := map[string]any{
		"ticker": "TSLA", "terminalShareCount": 3500000000.0, "terminalMarketCap": 5000000000000.0,
		"valueWanted": 250000.0, "sharesOwned": 10.0,
	}
	if len(creates[0].Body) != len(want) {
		t.Errorf("POST body = %v, want exactly %v", creates[0].Body, want)
	}
	for key, value := range want {
		if creates[0].Body[key] != value {
			t.Errorf("POST body %s = %v, want %v", key, creates[0].Body[key], value)
		}
	}
	if len(f.callsWith(http.MethodPatch)) != 0 {
		t.Error("a create must not patch anything")
	}
	for _, line := range []string{
		"Create a terminal scenario for TSLA:",
		"- terminal share count: 3500000000",
		"- terminal market cap: 5000000000000 USD",
		"- value wanted: 250000 USD",
		"- shares owned: 10",
		terminalDisclaimerText,
	} {
		if !strings.Contains(prompt, line) {
			t.Errorf("confirmation is missing %q:\n%s", line, prompt)
		}
	}
	if !strings.Contains(text, `"action": "created"`) {
		t.Errorf("result should say created:\n%s", text)
	}
}

func TestSetTerminalScenarioWillNotCreateWithoutTheThreeAssumptions(t *testing.T) {
	f := newTerminalFake()
	var prompt string
	cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{"ticker": "TSLA", "valueWanted": 250000})
	if !isErr || !strings.Contains(text, "terminalShareCount, terminalMarketCap") || !strings.Contains(text, "do not invent them") {
		t.Errorf("got %q (error=%v), want an error naming the missing assumptions", text, isErr)
	}
	if prompt != "" {
		t.Errorf("the user was asked to confirm an impossible create: %q", prompt)
	}
	if f.writes() != 0 {
		t.Error("an incomplete create reached the backend")
	}
}

func TestSetTerminalScenarioRejectsImpossibleValuesBeforeAsking(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"nothing to set", map[string]any{"ticker": "AMZN"}, "nothing to set"},
		{"zero market cap", map[string]any{"ticker": "AMZN", "terminalMarketCap": 0}, "terminalMarketCap must be greater than 0"},
		{"negative share count", map[string]any{"ticker": "AMZN", "terminalShareCount": -5}, "terminalShareCount must be greater than 0"},
		{"negative value wanted", map[string]any{"ticker": "AMZN", "valueWanted": -1}, "valueWanted must not be negative"},
		{"negative shares owned", map[string]any{"ticker": "AMZN", "sharesOwned": -2}, "sharesOwned must not be negative"},
		{"zero price", map[string]any{"ticker": "AMZN", "currentSharePrice": 0}, "currentSharePrice must be greater than 0"},
		{"not a ticker", map[string]any{"ticker": "$AMZN", "valueWanted": 1}, "not a valid ticker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTerminalFake()
			var prompt string
			cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))
			text, isErr := callPilotTool(t, cs, "set_terminal_scenario", tc.args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("got %q (error=%v), want %q", text, isErr, tc.want)
			}
			if prompt != "" || len(f.calls) != 0 {
				t.Errorf("rejected input still asked (%q) or called the backend (%+v)", prompt, f.calls)
			}
		})
	}
}

func TestSetTerminalScenarioDeclinedWritesNothing(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	cs := connect(t, terminalWriteScopes, f.server(t).URL, declineElicit)

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{"ticker": "AMZN", "valueWanted": 2000000})
	if isErr || !strings.Contains(text, "not confirmed") {
		t.Errorf("got %q (error=%v), want a soft not-confirmed message", text, isErr)
	}
	if f.writes() != 0 {
		t.Error("a declined change reached the backend")
	}
}

func TestSetTerminalScenarioRefusesAClientThatCannotConfirm(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	// No elicitation handler. server.go strips write tools from such sessions;
	// this harness registers them anyway, so this pins the handler's own guard.
	cs := connect(t, terminalWriteScopes, f.server(t).URL, nil)

	_, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{"ticker": "AMZN", "valueWanted": 2000000})
	if !isErr {
		t.Error("a client without form elicitation must get an error")
	}
	if f.writes() != 0 {
		t.Error("an unconfirmable change reached the backend")
	}
}

func TestSetTerminalScenarioNeverPatchesAnotherTickersRow(t *testing.T) {
	f := newTerminalFake()
	f.ignoreFilter = true // the backend answers every row whatever ?ticker= says
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	var prompt string
	cs := connect(t, terminalWriteScopes, f.server(t).URL, confirmAndCapture(&prompt))

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{
		"ticker": "TSLA", "terminalShareCount": 3500000000, "terminalMarketCap": 5000000000000, "valueWanted": 250000,
	})
	if isErr {
		t.Fatalf("set_terminal_scenario failed: %s", text)
	}
	if len(f.callsWith(http.MethodPatch)) != 0 {
		t.Fatal("MCP patched AMZN's row while setting TSLA")
	}
	if creates := f.callsWith(http.MethodPost); len(creates) != 1 || creates[0].Body["ticker"] != "TSLA" {
		t.Errorf("creates = %+v, want one TSLA create", creates)
	}
}

func TestSetTerminalScenarioExplainsABackendRejection(t *testing.T) {
	f := newTerminalFake()
	f.rows["AMZN"] = []string{terminalRowJSON(terminalRowA, "AMZN", 0)}
	f.fail["PATCH /v1/terminal-positions/"+terminalRowA] = terminalFailure{
		http.StatusUnprocessableEntity, `{"error":true,"reason":"Ticker must be 1-12 letters, digits, dots or hyphens."}`,
	}
	cs := connect(t, terminalWriteScopes, f.server(t).URL, acceptElicit)

	text, isErr := callPilotTool(t, cs, "set_terminal_scenario", map[string]any{"ticker": "AMZN", "valueWanted": 5})
	if !isErr || !strings.Contains(text, "Norviq rejected this: Ticker must be 1-12 letters, digits, dots or hyphens.") {
		t.Errorf("got %q (error=%v), want the backend's reason", text, isErr)
	}
}
