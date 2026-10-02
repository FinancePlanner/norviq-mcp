package tools_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	followID       = "3f2b8c1e-9a4d-4e7b-8c2a-1d5e6f7a8b9c"
	followIDNoFeed = "7c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f"

	pelosiSummaryJSON = `{"slug":"nancy-pelosi","displayName":"Nancy Pelosi","kind":"politician","chamber":"house","updatedAt":"2026-09-30T06:00:00Z","holdingsCount":2}`
	pilotLagNote      = "Congressional trades are disclosed up to 45 days after they happen. Simulated trades are priced when Norviq sees the disclosure, not on the original trade date. No real money is invested."

	pilotListJSON   = `[` + pelosiSummaryJSON + `,{"slug":"berkshire-hathaway","displayName":"Berkshire Hathaway","kind":"fund","chamber":null,"updatedAt":null,"holdingsCount":0}]`
	pilotDetailJSON = `{"pilot":` + pelosiSummaryJSON + `,"weights":[{"symbol":"AVGO","weight":0.58},{"symbol":"NVDA","weight":0.42}],"skippedPuts":2,"recentDisclosures":[{"symbol":"NVDA","side":"buy","instrument":"call","transactionDate":"2026-08-20","disclosureDate":"2026-09-14","amountMin":1000001,"amountMax":5000000,"period":null}],"lagNote":"` + pilotLagNote + `"}`
)

var pilotScopes = map[string]bool{"portfolio:read": true}

func followJSON(id string) string {
	return `{"id":"` + id + `","pilot":` + pelosiSummaryJSON + `,"targetKind":"portfolio","portfolioListId":"9b8a7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d","watchlistListId":null,"startingCapital":10000,"currency":"USD","status":"active","appliedVersion":3,"createdAt":"2026-10-01T09:00:00Z"}`
}

// eventsJSON returns n events newest first, which is the backend's order:
// event 0 is SYM00 at the highest book version.
func eventsJSON(n int) string {
	rows := make([]string, 0, n)
	for i := range n {
		rows = append(rows, fmt.Sprintf(
			`{"id":"00000000-0000-4000-8000-%012d","bookVersion":%d,"kind":"buy","symbol":"SYM%02d","quantity":1.5,"price":100,"pricedAt":"2026-09-30T06:00:00Z","note":"Simulated buy"}`,
			i, n-i, i,
		))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// pilotBackend fakes the backend's pilot routes. With enabled=false every route
// answers the way PilotController.requireEnabled does: a bare Abort(.notFound),
// which APIErrorMiddleware renders with the generic reason "Not Found".
func pilotBackend(t *testing.T, enabled bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		notFound := func(reason string) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":true,"code":"not_found","reason":"` + reason + `"}`))
		}
		if !enabled {
			notFound("Not Found")
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilots":
			_, _ = w.Write([]byte(pilotListJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilots/nancy-pelosi":
			_, _ = w.Write([]byte(pilotDetailJSON))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/pilots/"):
			notFound("Pilot not found.")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilot-follows":
			_, _ = w.Write([]byte("[" + followJSON(followID) + "]"))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilot-follows/"+followID:
			_, _ = w.Write([]byte(followJSON(followID)))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilot-follows/"+followID+"/events":
			_, _ = w.Write([]byte(eventsJSON(30)))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilot-follows/"+followIDNoFeed:
			_, _ = w.Write([]byte(followJSON(followIDNoFeed)))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pilot-follows/"+followIDNoFeed+"/events":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":true,"code":"internal_server_error","reason":"Internal Server Error"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/pilot-follows/"):
			notFound("Follow not found.")
		default:
			notFound("Not Found")
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func pilotSession(t *testing.T, enabled bool, scopes map[string]bool) (*mcp.ClientSession, *[]string) {
	t.Helper()
	backend, seen := pilotBackend(t, enabled)
	return connect(t, scopes, backend.URL, nil), seen
}

// callPilotTool calls a tool and returns its text content and error flag.
func callPilotTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var text strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func listPilotTools(t *testing.T, cs *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func sawRequest(seen *[]string, want string) bool {
	for _, entry := range *seen {
		if entry == want {
			return true
		}
	}
	return false
}

func TestPilotToolsNeedPortfolioRead(t *testing.T) {
	// Legacy market:read still unlocks get_portfolio_summary, but the backend's
	// pilot routes accept portfolio:read only. A tool the backend would 403 must
	// not be offered.
	cs, _ := pilotSession(t, true, map[string]bool{"market:read": true})
	for _, tool := range listPilotTools(t, cs) {
		if strings.Contains(tool.Name, "pilot") {
			t.Errorf("%s must not be exposed without portfolio:read", tool.Name)
		}
	}

	cs, _ = pilotSession(t, true, pilotScopes)
	var found bool
	for _, tool := range listPilotTools(t, cs) {
		if tool.Name == "list_pilots" {
			found = true
		}
	}
	if !found {
		t.Error("expected list_pilots to be exposed with portfolio:read")
	}
}

func TestListPilotsReturnsSummaries(t *testing.T) {
	cs, seen := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "list_pilots", nil)
	if isErr {
		t.Fatalf("list_pilots failed: %s", text)
	}
	for _, want := range []string{"nancy-pelosi", "Berkshire Hathaway", "holdingsCount"} {
		if !strings.Contains(text, want) {
			t.Errorf("list_pilots output missing %q: %s", want, text)
		}
	}
	if !sawRequest(seen, "GET /v1/pilots") {
		t.Errorf("expected GET /v1/pilots, saw %v", *seen)
	}
}

func TestListPilotsSaysWhenTheFeatureIsOff(t *testing.T) {
	cs, _ := pilotSession(t, false, pilotScopes)

	text, isErr := callPilotTool(t, cs, "list_pilots", nil)
	if !isErr {
		t.Error("expected the flag-off answer to be flagged as an error")
	}
	if !strings.HasPrefix(text, "Pilot follows are not enabled") {
		t.Errorf("expected the not-enabled message, got: %s", text)
	}
	if strings.Contains(text, "not found in your norviq account") {
		t.Errorf("flag-off must not read as a missing item: %s", text)
	}
}

func TestPilotToolDescriptionsCarryTheDisclaimer(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	checked := 0
	for _, tool := range listPilotTools(t, cs) {
		if !strings.Contains(tool.Name, "pilot") {
			continue
		}
		checked++
		desc := strings.ToLower(tool.Description)
		for _, phrase := range []string{"simulated", "lag", "no real money"} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s description must mention %q: %s", tool.Name, phrase, tool.Description)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no pilot tools were registered")
	}
}

func TestNoToolNamesTheThirdPartyBrand(t *testing.T) {
	// The spec forbids naming the third-party copy-trading product. The word is
	// built from parts so that no source file contains it.
	brand := "auto" + "pilot"
	backend, _ := pilotBackend(t, true)
	all := map[string]bool{}
	for _, scope := range []string{
		"watchlist:read", "watchlist:write", "holdings:read", "holdings:write",
		"transactions:read", "transactions:write", "targets:read", "targets:write",
		"research:read", "research:write", "expenses:read", "expenses:write",
		"budget:read", "budget:write", "goals:read", "goals:write", "planning:read",
		"reports:read", "market:read", "portfolio:read", "insights:read", "tax:read",
	} {
		all[scope] = true
	}
	cs := connect(t, all, backend.URL, acceptElicit)

	for _, tool := range listPilotTools(t, cs) {
		if strings.Contains(strings.ToLower(tool.Name+" "+tool.Description), brand) {
			t.Errorf("%s names the third-party brand: %s", tool.Name, tool.Description)
		}
	}
}

func TestGetPilotReturnsWeightsDisclosuresAndLag(t *testing.T) {
	cs, seen := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot", map[string]any{"slug": "nancy-pelosi"})
	if isErr {
		t.Fatalf("get_pilot failed: %s", text)
	}
	// The lag note leads, so a model summarising the answer meets it first.
	if !strings.HasPrefix(text, pilotLagNote) {
		t.Errorf("expected the lag note first, got: %s", text)
	}
	for _, want := range []string{`"AVGO"`, `"weight": 0.58`, `"skippedPuts": 2`, `"instrument": "call"`} {
		if !strings.Contains(text, want) {
			t.Errorf("get_pilot output missing %s: %s", want, text)
		}
	}
	if !sawRequest(seen, "GET /v1/pilots/nancy-pelosi") {
		t.Errorf("expected GET /v1/pilots/nancy-pelosi, saw %v", *seen)
	}
}

func TestGetPilotNormalizesTheSlug(t *testing.T) {
	// Seed slugs are lowercase and hyphenated. A model often passes the display
	// name or keeps its capitals.
	for _, input := range []string{"Nancy Pelosi", " Nancy-Pelosi "} {
		cs, seen := pilotSession(t, true, pilotScopes)
		text, isErr := callPilotTool(t, cs, "get_pilot", map[string]any{"slug": input})
		if isErr {
			t.Errorf("get_pilot(%q) failed: %s", input, text)
		}
		if !sawRequest(seen, "GET /v1/pilots/nancy-pelosi") {
			t.Errorf("get_pilot(%q) did not resolve to nancy-pelosi, saw %v", input, *seen)
		}
	}
}

func TestGetPilotUnknownSlugIsNotReportedAsDisabled(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot", map[string]any{"slug": "no-such-pilot"})
	if !isErr {
		t.Error("expected an unknown slug to be an error")
	}
	if strings.Contains(text, "not enabled") {
		t.Errorf("an unknown slug with the flag on must not read as disabled: %s", text)
	}
	if !strings.Contains(text, `No pilot with slug "no-such-pilot"`) || !strings.Contains(text, "list_pilots") {
		t.Errorf("expected a pointer to list_pilots, got: %s", text)
	}
}

func TestGetPilotSaysWhenTheFeatureIsOff(t *testing.T) {
	cs, _ := pilotSession(t, false, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot", map[string]any{"slug": "nancy-pelosi"})
	if !isErr {
		t.Error("expected the flag-off answer to be flagged as an error")
	}
	if !strings.HasPrefix(text, "Pilot follows are not enabled") {
		t.Errorf("expected the not-enabled message, got: %s", text)
	}
}

func TestGetPilotRejectsAnEmptySlug(t *testing.T) {
	cs, seen := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot", map[string]any{"slug": "   "})
	if !isErr || !strings.Contains(text, "slug is required") {
		t.Errorf("expected a slug-required error, got (%v) %s", isErr, text)
	}
	if len(*seen) != 0 {
		t.Errorf("an empty slug must not reach the backend, saw %v", *seen)
	}
}
