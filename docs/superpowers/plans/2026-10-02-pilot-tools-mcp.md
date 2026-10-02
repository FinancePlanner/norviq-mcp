# MCP Pilot Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the backend's pilot follows to MCP clients as four read-only tools (`list_pilots`, `get_pilot`, `list_pilot_follows`, `get_pilot_follow`), and accept the new `exited` watchlist status.

**Architecture:** A hand-written client file `internal/api/pilots.go` mirrors the backend's Pilots DTOs and GET routes, following the other `internal/api/*.go` files. `internal/tools/pilots.go` registers the four tools behind `portfolio:read`. It turns the backend's flag-off 404 into a clear "not enabled" message: on the list routes any 404 means the flag is off. On the single-item routes a 404 is ambiguous, so the tool checks `GET /v1/pilots`, which 404s only when the flag is off. Watchlist status validation already reads from `api.WatchlistStatuses`, so `exited` goes into that slice and into the two `jsonschema` tags that spell the list out.

**Tech Stack:** Go 1.27, `github.com/modelcontextprotocol/go-sdk` v1.7.0, stdlib `net/http/httptest` for tests, gofumpt, golangci-lint v2.13.0 (with `govet shadow`).

**Spec:** `/Users/fernandocorreiachill/Work/production/apps/norviq/norviq-backend-gates/docs/superpowers/specs/2026-10-01-pilot-follow-design.md` (Phase 5, "Optional MCP"). The API contract is in `/Users/fernandocorreiachill/Work/production/apps/norviq/norviq-backend-gates/Sources/StockPlanBackend/openapi.yaml` (tag `Pilots`, `/v1/pilots`, `/v1/pilot-follows`). The shared DTOs are in `/Users/fernandocorreiachill/Work/production/apps/norviq/norviq-shared/Sources/StockPlanShared/Pilots/PilotDTOs.swift`. The backend controller is `/Users/fernandocorreiachill/Work/production/apps/norviq/norviq-backend-gates/Sources/StockPlanBackend/Pilots/PilotController.swift`.

## Global Constraints

- Work in `/Users/fernandocorreiachill/Work/production/apps/norviq/norviq-mcp-pilots` on branch `feat/pilot-tools`. Do not push.
- The tools are read-only: there are no create, update or delete pilot tools, and nothing sends POST, PATCH or DELETE to `/v1/pilot-follows`. Every pilot tool has `Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}` and is not in `WriteToolNames()`.
- Scope: register the pilot tools only when the token holds `portfolio:read`. The backend guards every pilot GET route with `ScopeRequirementMiddleware(.portfolioRead)` and accepts nothing else. Unlike `get_portfolio_summary`, do **not** also accept legacy `market:read`.
- Every pilot tool description must say three things: the trades are **simulated**, they are disclosed with a **lag**, and **no real money** is invested.
- When the backend returns 404 because `PILOTS_ENABLED` is off, the tool returns a message that starts "Pilot follows are not enabled". It must not return the generic `errmap` "not found in your norviq account" text.
- Naming: only "pilot" / "follow" wording. Never write the name of the third-party copy-trading product the spec mentions: not in code, comments, descriptions, tests or commit messages. Task 2's guard test builds the word by concatenation so that no source file contains it.
- `WatchlistStatus` values, in openapi order: `active, researching, waiting, ready, archived, exited`.
- No new module dependencies. Stdlib only (`regexp`, `errors`, `net/http`).
- Formatting: `go tool gofumpt -w .`. Lint: `golangci-lint run` (pre-commit runs both). With `govet shadow` on, do not redeclare `err` in an inner scope; use distinct names (`eventsErr`, `probeErr`).
- Baseline: on `main` (670d62b), `go test ./...` already fails exactly one test, `TestGetNewsDefaultUsesTrackedFeed`. Its `/v1/news/feed` fixture is dated 2026-09-01, which is now outside the 7-day lookback. This plan does not touch that test. Run targeted tests while you work. In the final full run, that test should be the only failure.

## Review Focus

1. **Flag off vs. missing item on single-item routes.** `GET /v1/pilots/{slug}` and `GET /v1/pilot-follows/{id}` answer 404 for "flag off" and also for "no such pilot/follow". A user with the flag on who mistypes a slug should get "No pilot with slug …", not "not enabled". With the flag off, they should get "not enabled", not "not found". Pinned in Task 3 (`TestGetPilotUnknownSlugIsNotReportedAsDisabled`, `TestGetPilotSaysWhenTheFeatureIsOff`) and Task 4 (the matching follow tests).
2. **A pilot slug or display name passed as `follow_id`.** A model will often pass `nancy-pelosi` to `get_pilot_follow`. The expected result is an immediate error that points to `list_pilot_follows`, with no backend call. Pinned in Task 4 (`TestGetPilotFollowRejectsANonUUID`).
3. **A display name or mixed case passed as `slug`.** "Nancy Pelosi" or "Nancy-Pelosi" should resolve to `nancy-pelosi` (the seed slugs are lowercase and hyphenated). Pinned in Task 3 (`TestGetPilotNormalizesTheSlug`).
4. **A follow with hundreds of events.** The events route returns up to 500 rows. `get_pilot_follow` must not dump all of them into the model's context, so it defaults to the 20 newest and caps at 100. Pinned in Task 4 (`TestGetPilotFollowShowsTheNewestEventsFirstAndCapsThem`).
5. **The events call fails after the follow loads.** The user should still see the follow, with a note that the events could not be loaded, rather than a bare error. Pinned in Task 4 (`TestGetPilotFollowStillAnswersWhenEventsFail`).

`catalog_parity_test.go` needs no change. It only checks write tools against the backend ActionCatalog. Task 4's `TestPortfolioReadExposesExactlyTheFourPilotTools` pins that no pilot tool is a write tool.

---

### Task 1: Accept the `exited` watchlist status

**Files:**
- Modify: `internal/api/watchlist.go:9-12`
- Modify: `internal/tools/watchlist.go:23` (`watchlistRow.Status` tag)
- Modify: `internal/tools/watchlist.go:157` (`updateArgs.Status` tag)
- Test: `internal/tools/tools_test.go` (append after `TestWatchlistRejectsUnknownStatusBeforeWriting`)

**Interfaces:**
- Consumes: nothing new.
- Produces: `api.WatchlistStatuses` = `[]string{"active", "researching", "waiting", "ready", "archived", "exited"}`. `validateStatus` in `internal/tools/watchlist.go` reads this slice and is not edited.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tools/tools_test.go`. The file already imports `context`, `strings`, `testing`, `api` and `mcp`.

```go
func TestWatchlistAcceptsExitedStatus(t *testing.T) {
	// A pilot follow's watchlist feed sets "exited" when the pilot sells. The user
	// must be able to set it as well. Otherwise validateStatus rejects a value the
	// backend accepts.
	backend, seen := fakeBackend(t)
	cs := connect(t, map[string]bool{"watchlist:write": true}, backend.URL, acceptElicit)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "upsert_watchlist_items",
		Arguments: map[string]any{
			"items": []map[string]any{{"symbol": "NVDA", "status": "exited"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("exited was rejected: %s", mustJSON(t, res.Content))
	}
	var posted bool
	for _, entry := range *seen {
		if entry == "POST /v1/watchlist" {
			posted = true
		}
	}
	if !posted {
		t.Error("expected the exited row to reach the backend")
	}
}

func TestWatchlistSchemasListEveryStatus(t *testing.T) {
	// The jsonschema tags spell the status list out by hand, so they can drift from
	// api.WatchlistStatuses. A model only offers the values the schema shows it.
	backend, _ := fakeBackend(t)
	cs := connect(t, map[string]bool{"watchlist:write": true}, backend.URL, acceptElicit)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range res.Tools {
		if tool.Name != "upsert_watchlist_items" && tool.Name != "update_watchlist_item" {
			continue
		}
		checked++
		schema := mustJSON(t, tool.InputSchema)
		for _, status := range api.WatchlistStatuses {
			if !strings.Contains(schema, status) {
				t.Errorf("%s input schema does not list status %q", tool.Name, status)
			}
		}
	}
	if checked != 2 {
		t.Fatalf("expected to check both watchlist write tools, checked %d", checked)
	}
}
```

- [ ] **Step 2: Run the tests to verify the first one fails**

Run: `go test ./internal/tools/ -run 'TestWatchlistAcceptsExitedStatus|TestWatchlistSchemasListEveryStatus' -v`
Expected: `TestWatchlistAcceptsExitedStatus` FAILs with `exited was rejected: ... invalid status \"exited\"`. `TestWatchlistSchemasListEveryStatus` PASSes for now, because the slice does not have `exited` yet.

- [ ] **Step 3: Add `exited` to the status slice**

Replace `internal/api/watchlist.go` lines 9-12 with:

```go
// WatchlistStatus values accepted by the backend. Mirrors WatchlistStatus in
// norviq-shared (5.15.0 added "exited", which a pilot follow's watchlist feed
// sets when the pilot sells). An unknown value is coerced to "active"
// server-side, which silently loses the caller's intent, so the tool layer
// validates up front.
var WatchlistStatuses = []string{"active", "researching", "waiting", "ready", "archived", "exited"}
```

- [ ] **Step 4: Run the tests to verify the schema test now fails**

Run: `go test ./internal/tools/ -run 'TestWatchlistAcceptsExitedStatus|TestWatchlistSchemasListEveryStatus' -v`
Expected: `TestWatchlistAcceptsExitedStatus` PASSes. `TestWatchlistSchemasListEveryStatus` FAILs twice with `input schema does not list status "exited"` (once per tool).

- [ ] **Step 5: Update both jsonschema tags**

In `internal/tools/watchlist.go`, line 23 (inside `type watchlistRow struct`), replace:

```go
	Status string `json:"status,omitempty" jsonschema:"one of: active, researching, waiting, ready, archived"`
```

with:

```go
	Status string `json:"status,omitempty" jsonschema:"one of: active, researching, waiting, ready, archived, exited"`
```

In the same file, line 157 (inside `type updateArgs struct`), make the same replacement:

```go
		Status string `json:"status,omitempty" jsonschema:"one of: active, researching, waiting, ready, archived, exited"`
```

- [ ] **Step 6: Run the watchlist tests**

Run: `go test ./internal/tools/ -run 'TestWatchlist' -v`
Expected: every `TestWatchlist*` test PASSes, including `TestWatchlistRejectsUnknownStatusBeforeWriting`.

- [ ] **Step 7: Commit**

```bash
git add internal/api/watchlist.go internal/tools/watchlist.go internal/tools/tools_test.go
git commit -m "feat(watchlist): accept the exited status"
```

---

### Task 2: Pilot API client and `list_pilots`

**Files:**
- Create: `internal/api/pilots.go`
- Create: `internal/tools/pilots.go`
- Modify: `internal/tools/expenses.go:34-49` (`Register`: add `registerPilots`)
- Test: `internal/tools/pilots_test.go` (create)

**Interfaces:**
- Consumes: `(*api.Client).do`, `api.APIError`, `textResult`, `fail` (`internal/tools/expenses.go`).
- Produces:
  - `api.PilotSummary`, `api.PilotWeight`, `api.PilotDisclosure`, `api.PilotDetail`, `api.PilotFollow`, `api.PilotFollowEvent` (field names below).
  - `func (c *Client) ListPilots(ctx context.Context) ([]PilotSummary, error)`
  - `func (c *Client) GetPilot(ctx context.Context, slug string) (*PilotDetail, error)`
  - `func (c *Client) ListPilotFollows(ctx context.Context) ([]PilotFollow, error)`
  - `func (c *Client) GetPilotFollow(ctx context.Context, id string) (*PilotFollow, error)`
  - `func (c *Client) ListPilotFollowEvents(ctx context.Context, id string) ([]PilotFollowEvent, error)`
  - In package `tools`: `const pilotNotice string`, `const pilotsDisabledMessage string`, `func isNotFound(err error) bool`, `func pilotListFail(err error) *mcp.CallToolResult`, `func registerPilots(s *mcp.Server, client *api.Client, p *auth.Principal)`.
  - In `pilots_test.go`: `pilotBackend`, `pilotSession`, `pilotScopes`, `callPilotTool`, `listPilotTools`, `sawRequest`, `followID`, `followIDNoFeed`, `pilotLagNote`. Tasks 3 and 4 use these.

- [ ] **Step 1: Write the failing tests**

Create `internal/tools/pilots_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/ -run 'TestPilotTools|TestListPilots|TestPilotToolDescriptions|TestNoToolNamesTheThirdPartyBrand' -v`
Expected: `TestPilotToolsNeedPortfolioRead` FAILs with `expected list_pilots to be exposed with portfolio:read`. `TestListPilotsReturnsSummaries` and `TestListPilotsSaysWhenTheFeatureIsOff` FAIL. `callPilotTool` stops with `call list_pilots: ...` because the tool is not registered yet. `TestPilotToolDescriptionsCarryTheDisclaimer` FAILs with `no pilot tools were registered`. `TestNoToolNamesTheThirdPartyBrand` PASSes; it is a guard for later tasks.

- [ ] **Step 3: Write the API client**

Create `internal/api/pilots.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/url"
)

// Pilot types mirror StockPlanShared/Pilots/PilotDTOs.swift (norviq-shared
// 5.15.0) and the Pilots tag in the backend openapi.yaml. Every route here
// answers 404 while the backend's PILOTS_ENABLED flag is off.

type PilotSummary struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	// Kind is "politician" or "fund".
	Kind string `json:"kind"`
	// Chamber is "senate" or "house" for politicians; nil for funds.
	Chamber *string `json:"chamber,omitempty"`
	// UpdatedAt is when the latest book version was computed; nil before the
	// first ingestion.
	UpdatedAt     *string `json:"updatedAt,omitempty"`
	HoldingsCount int     `json:"holdingsCount"`
}

type PilotWeight struct {
	Symbol string `json:"symbol"`
	// Weight is 0...1; the weights in a book sum to 1.
	Weight float64 `json:"weight"`
}

type PilotDisclosure struct {
	Symbol string `json:"symbol"`
	// Side is "buy", "sell", "sell_full" or "hold".
	Side string `json:"side"`
	// Instrument is "stock", "call" or "put".
	Instrument      string   `json:"instrument"`
	TransactionDate *string  `json:"transactionDate,omitempty"`
	DisclosureDate  *string  `json:"disclosureDate,omitempty"`
	AmountMin       *float64 `json:"amountMin,omitempty"`
	AmountMax       *float64 `json:"amountMax,omitempty"`
	// Period is the 13F period such as "2026Q2"; nil for politicians.
	Period *string `json:"period,omitempty"`
}

type PilotDetail struct {
	Pilot   PilotSummary  `json:"pilot"`
	Weights []PilotWeight `json:"weights"`
	// SkippedPuts counts put trades in the window, which are not mirrored
	// because a portfolio cannot go short.
	SkippedPuts       int               `json:"skippedPuts"`
	RecentDisclosures []PilotDisclosure `json:"recentDisclosures"`
	// LagNote is the backend's plain-language reporting-lag and pricing
	// disclaimer for this pilot kind.
	LagNote string `json:"lagNote"`
}

type PilotFollow struct {
	ID    string       `json:"id"`
	Pilot PilotSummary `json:"pilot"`
	// TargetKind is "portfolio" (a hypothetical portfolio) or "watchlist".
	TargetKind      string   `json:"targetKind"`
	PortfolioListID *string  `json:"portfolioListId,omitempty"`
	WatchlistListID *string  `json:"watchlistListId,omitempty"`
	StartingCapital *float64 `json:"startingCapital,omitempty"`
	Currency        string   `json:"currency"`
	// Status is "active" or "paused".
	Status string `json:"status"`
	// AppliedVersion is 0 until the first book version has been applied.
	AppliedVersion int    `json:"appliedVersion"`
	CreatedAt      string `json:"createdAt"`
}

type PilotFollowEvent struct {
	ID          string `json:"id"`
	BookVersion int    `json:"bookVersion"`
	// Kind is "buy", "sell", "watch_added", "watch_exited",
	// "skipped_unpriced" or "skipped_limit".
	Kind     string   `json:"kind"`
	Symbol   string   `json:"symbol"`
	Quantity *float64 `json:"quantity,omitempty"`
	Price    *float64 `json:"price,omitempty"`
	PricedAt string   `json:"pricedAt"`
	Note     *string  `json:"note,omitempty"`
}

func (c *Client) ListPilots(ctx context.Context) ([]PilotSummary, error) {
	out := []PilotSummary{}
	if err := c.do(ctx, http.MethodGet, "/v1/pilots", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetPilot(ctx context.Context, slug string) (*PilotDetail, error) {
	var out PilotDetail
	if err := c.do(ctx, http.MethodGet, "/v1/pilots/"+url.PathEscape(slug), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListPilotFollows(ctx context.Context) ([]PilotFollow, error) {
	out := []PilotFollow{}
	if err := c.do(ctx, http.MethodGet, "/v1/pilot-follows", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetPilotFollow(ctx context.Context, id string) (*PilotFollow, error) {
	var out PilotFollow
	if err := c.do(ctx, http.MethodGet, "/v1/pilot-follows/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPilotFollowEvents returns the follow's simulated trades and watchlist
// changes, newest book version first, at most 500 rows (backend cap).
func (c *Client) ListPilotFollowEvents(ctx context.Context, id string) ([]PilotFollowEvent, error) {
	out := []PilotFollowEvent{}
	path := "/v1/pilot-follows/" + url.PathEscape(id) + "/events"
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Write the tools file with `list_pilots`**

Create `internal/tools/pilots.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pilotNotice is appended to every pilot tool description. A model that reads
// only the tool list must still know, before it says anything about a pilot,
// that the trades are simulated, arrive late, and involve no money.
const pilotNotice = " Pilot trades are SIMULATED: Norviq mirrors a politician's or 13F fund's " +
	"publicly disclosed trades into a hypothetical portfolio or a watchlist feed. " +
	"Disclosures arrive with a lag (up to 45 days for congressional trades, up to 135 days " +
	"for 13F filings), simulated trades are priced when Norviq sees the disclosure, and no " +
	"real money is invested and no orders are placed."

// pilotsDisabledMessage is what every pilot tool says when the backend's
// PILOTS_ENABLED flag is off. The backend answers 404 then, and the generic
// errmap text ("not found in your norviq account") would wrongly suggest the
// user's data is missing.
const pilotsDisabledMessage = "Pilot follows are not enabled on Norviq yet, so there are no " +
	"pilots or follows to show. This is a feature switch on Norviq's side, not a problem " +
	"with the user's account."

func isNotFound(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// pilotListFail maps an error from a pilot list route. Those routes look up no
// single item, so a 404 from them can only mean the feature flag is off.
func pilotListFail(err error) *mcp.CallToolResult {
	if isNotFound(err) {
		return textResult(pilotsDisabledMessage, true)
	}
	return fail(err)
}

func registerPilots(s *mcp.Server, client *api.Client, p *auth.Principal) {
	// PilotController guards every route with portfolio:read alone. Unlike
	// get_portfolio_summary, legacy market:read is not accepted there, so it must
	// not expose these tools here: a tool the backend will 403 is worse than no
	// tool. All pilot tools are read-only by scope decision; follows are created,
	// paused and stopped in the app.
	if !p.Scopes["portfolio:read"] {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_pilots",
		Description: "List the politicians and 13F funds the user can follow in Norviq: slug, " +
			"display name, kind (politician or fund), chamber, number of holdings in the " +
			"current book, and when the book last updated. Use a slug with get_pilot." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		pilots, err := client.ListPilots(ctx)
		if err != nil {
			return pilotListFail(err), nil, nil
		}
		out, _ := json.MarshalIndent(pilots, "", "  ")
		return textResult(string(out), false), nil, nil
	})
}
```

- [ ] **Step 5: Register the pilot tools**

In `internal/tools/expenses.go`, inside `func Register`, add `registerPilots` after `registerResearch` so that lines 34-49 read:

```go
func Register(s *mcp.Server, client *api.Client, p *auth.Principal) {
	registerExpenses(s, client, p)
	registerReports(s, client, p)
	registerCSV(s, client, p)
	registerMarket(s, client, p)
	registerNews(s, client, p)
	registerTax(s, client, p)
	registerPlanning(s, client, p)
	registerBudget(s, client, p)
	registerRecurring(s, client, p)
	registerWatchlist(s, client, p)
	registerTransactions(s, client, p)
	registerHoldings(s, client, p)
	registerTargets(s, client, p)
	registerResearch(s, client, p)
	registerPilots(s, client, p)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tools/ -run 'TestPilotTools|TestListPilots|TestPilotToolDescriptions|TestNoToolNamesTheThirdPartyBrand|TestEveryMutatingToolIsInTheWriteAllowlist' -v`
Expected: all PASS.

- [ ] **Step 7: Format, lint, commit**

```bash
go tool gofumpt -w . && golangci-lint run
git add internal/api/pilots.go internal/tools/pilots.go internal/tools/pilots_test.go internal/tools/expenses.go
git commit -m "feat(pilots): list_pilots read tool behind portfolio:read"
```

---

### Task 3: `get_pilot`

**Files:**
- Modify: `internal/tools/pilots.go` (imports; add `pilotItemFail`, `normalizeSlug`, the `get_pilot` tool)
- Test: `internal/tools/pilots_test.go` (append)

**Interfaces:**
- Consumes: `(*api.Client).GetPilot`, `(*api.Client).ListPilots`, `api.PilotDetail`, `isNotFound`, `pilotNotice`, `pilotsDisabledMessage`, and the test helpers `pilotSession`, `callPilotTool`, `sawRequest`, `pilotScopes`, `pilotLagNote` from Task 2.
- Produces: `func pilotItemFail(ctx context.Context, client *api.Client, err error, missing string) *mcp.CallToolResult` (Task 4 uses it) and `func normalizeSlug(raw string) string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tools/pilots_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/ -run 'TestGetPilot' -v`
Expected: all five FAIL. `callPilotTool` stops with `call get_pilot: ...` because the tool is not registered yet.

- [ ] **Step 3: Implement `get_pilot`**

In `internal/tools/pilots.go`, replace the import block with:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)
```

Add these two functions directly after `pilotListFail`:

```go
// pilotItemFail maps an error from a single-item pilot route. The backend
// answers 404 both for an unknown pilot or follow and for the feature flag being
// off, and the body does not reliably tell them apart. GET /v1/pilots looks up
// no item, so it 404s only when the flag is off; one probe settles it.
func pilotItemFail(ctx context.Context, client *api.Client, err error, missing string) *mcp.CallToolResult {
	if !isNotFound(err) {
		return fail(err)
	}
	if _, probeErr := client.ListPilots(ctx); isNotFound(probeErr) {
		return textResult(pilotsDisabledMessage, true)
	}
	return textResult(missing, true)
}

// normalizeSlug turns "Nancy Pelosi" or " Nancy-Pelosi " into "nancy-pelosi",
// the form the seed slugs use.
func normalizeSlug(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), "-")
}
```

Inside `registerPilots`, after the `list_pilots` `mcp.AddTool(...)` call, add:

```go
	type getPilotArgs struct {
		Slug string `json:"slug" jsonschema:"pilot slug from list_pilots, e.g. nancy-pelosi or berkshire-hathaway"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_pilot",
		Description: "Get one pilot by slug (from list_pilots): the current book weights Norviq " +
			"would mirror, the most recent disclosed trades or filings, how many put trades were " +
			"skipped, and a plain-language note on the reporting lag." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getPilotArgs) (*mcp.CallToolResult, any, error) {
		slug := normalizeSlug(args.Slug)
		if slug == "" {
			return textResult("slug is required, e.g. nancy-pelosi. Call list_pilots to see the available slugs.", true), nil, nil
		}
		detail, err := client.GetPilot(ctx, slug)
		if err != nil {
			missing := fmt.Sprintf("No pilot with slug %q. Call list_pilots to see the available slugs.", slug)
			return pilotItemFail(ctx, client, err, missing), nil, nil
		}
		out, _ := json.MarshalIndent(detail, "", "  ")
		// The backend's lag note leads so it is the first thing the model reads.
		return textResult(detail.LagNote+"\n\n"+string(out), false), nil, nil
	})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/ -run 'TestGetPilot|TestPilotToolDescriptionsCarryTheDisclaimer|TestNoToolNamesTheThirdPartyBrand' -v`
Expected: all PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
go tool gofumpt -w . && golangci-lint run
git add internal/tools/pilots.go internal/tools/pilots_test.go
git commit -m "feat(pilots): get_pilot with flag-off vs unknown-slug distinction"
```

---

### Task 4: `list_pilot_follows`, `get_pilot_follow`, README

**Files:**
- Modify: `internal/tools/pilots.go` (imports; add `uuidPattern`, `pilotFollowView`, and two tools)
- Modify: `README.md:49` (the `portfolio:read` row) and the paragraph after the `get_news` paragraph (line 73)
- Test: `internal/tools/pilots_test.go` (append)

**Interfaces:**
- Consumes: `(*api.Client).ListPilotFollows`, `GetPilotFollow`, `ListPilotFollowEvents`, `api.PilotFollow`, `api.PilotFollowEvent`, `pilotListFail`, `pilotItemFail`, `pilotNotice`, `clamp(v, def, min, max int) int` (`internal/tools/news.go:290`), `tools.WriteToolNames()`, and the Task 2 test helpers plus `followID`, `followIDNoFeed`.
- Produces: `get_pilot_follow` returns JSON shaped as `pilotFollowView`: `{"follow": {...}, "recent_events": [...], "events_shown": n, "events_total": n, "events_error": "..."}` (`events_error` is omitted when empty).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tools/pilots_test.go`. Add `"encoding/json"` and `"github.com/FinancePlanner/norviq-mcp/internal/tools"` to its import block:

```go
func TestListPilotFollowsReturnsTheUsersFollows(t *testing.T) {
	cs, seen := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "list_pilot_follows", nil)
	if isErr {
		t.Fatalf("list_pilot_follows failed: %s", text)
	}
	for _, want := range []string{followID, `"targetKind": "portfolio"`, `"status": "active"`, "nancy-pelosi"} {
		if !strings.Contains(text, want) {
			t.Errorf("list_pilot_follows output missing %s: %s", want, text)
		}
	}
	if !sawRequest(seen, "GET /v1/pilot-follows") {
		t.Errorf("expected GET /v1/pilot-follows, saw %v", *seen)
	}
}

func TestListPilotFollowsSaysWhenTheFeatureIsOff(t *testing.T) {
	cs, _ := pilotSession(t, false, pilotScopes)

	text, isErr := callPilotTool(t, cs, "list_pilot_follows", nil)
	if !isErr || !strings.HasPrefix(text, "Pilot follows are not enabled") {
		t.Errorf("expected the not-enabled message, got (%v) %s", isErr, text)
	}
}

type followViewForTest struct {
	Follow struct {
		ID string `json:"id"`
	} `json:"follow"`
	RecentEvents []struct {
		Symbol string `json:"symbol"`
	} `json:"recent_events"`
	EventsShown int    `json:"events_shown"`
	EventsTotal int    `json:"events_total"`
	EventsError string `json:"events_error"`
}

func decodeFollowView(t *testing.T, text string) followViewForTest {
	t.Helper()
	var view followViewForTest
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("get_pilot_follow did not return JSON: %v\n%s", err, text)
	}
	return view
}

func TestGetPilotFollowShowsTheNewestEventsFirstAndCapsThem(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	// Default: the 20 newest of 30, in the backend's newest-first order.
	text, isErr := callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": followID})
	if isErr {
		t.Fatalf("get_pilot_follow failed: %s", text)
	}
	view := decodeFollowView(t, text)
	if view.Follow.ID != followID {
		t.Errorf("follow id = %q, want %q", view.Follow.ID, followID)
	}
	if view.EventsShown != 20 || len(view.RecentEvents) != 20 || view.EventsTotal != 30 {
		t.Errorf("expected 20 of 30 events, got shown=%d len=%d total=%d",
			view.EventsShown, len(view.RecentEvents), view.EventsTotal)
	}
	if len(view.RecentEvents) > 0 && view.RecentEvents[0].Symbol != "SYM00" {
		t.Errorf("expected the newest event first, got %s", view.RecentEvents[0].Symbol)
	}

	// An explicit limit is honoured.
	text, _ = callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": followID, "event_limit": 5})
	if view = decodeFollowView(t, text); len(view.RecentEvents) != 5 {
		t.Errorf("event_limit 5 returned %d events", len(view.RecentEvents))
	}

	// An oversized limit is capped at 100, so all 30 come back rather than an error.
	text, isErr = callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": followID, "event_limit": 1000})
	if view = decodeFollowView(t, text); isErr || len(view.RecentEvents) != 30 {
		t.Errorf("event_limit 1000 returned (%v) %d events", isErr, len(view.RecentEvents))
	}
}

func TestGetPilotFollowStillAnswersWhenEventsFail(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": followIDNoFeed})
	if isErr {
		t.Fatalf("a failed events call must not hide the follow: %s", text)
	}
	view := decodeFollowView(t, text)
	if view.Follow.ID != followIDNoFeed {
		t.Errorf("follow id = %q, want %q", view.Follow.ID, followIDNoFeed)
	}
	if view.EventsError == "" || len(view.RecentEvents) != 0 {
		t.Errorf("expected an events_error and no events, got error=%q len=%d", view.EventsError, len(view.RecentEvents))
	}
}

func TestGetPilotFollowRejectsANonUUID(t *testing.T) {
	cs, seen := pilotSession(t, true, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": "nancy-pelosi"})
	if !isErr || !strings.Contains(text, "list_pilot_follows") || !strings.Contains(text, "get_pilot") {
		t.Errorf("expected a pointer to list_pilot_follows and get_pilot, got (%v) %s", isErr, text)
	}
	if len(*seen) != 0 {
		t.Errorf("a non-UUID follow id must not reach the backend, saw %v", *seen)
	}
}

func TestGetPilotFollowUnknownIDIsNotReportedAsDisabled(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	const unknown = "11111111-2222-4333-8444-555555555555"
	text, isErr := callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": unknown})
	if !isErr {
		t.Error("expected an unknown follow to be an error")
	}
	if strings.Contains(text, "not enabled") || !strings.Contains(text, "No pilot follow with id "+unknown) {
		t.Errorf("expected a not-found message for the follow, got: %s", text)
	}
}

func TestGetPilotFollowSaysWhenTheFeatureIsOff(t *testing.T) {
	cs, _ := pilotSession(t, false, pilotScopes)

	text, isErr := callPilotTool(t, cs, "get_pilot_follow", map[string]any{"follow_id": followID})
	if !isErr || !strings.HasPrefix(text, "Pilot follows are not enabled") {
		t.Errorf("expected the not-enabled message, got (%v) %s", isErr, text)
	}
}

func TestPortfolioReadExposesExactlyTheFourPilotTools(t *testing.T) {
	cs, _ := pilotSession(t, true, pilotScopes)

	writes := map[string]bool{}
	for _, name := range tools.WriteToolNames() {
		writes[name] = true
	}
	got := map[string]bool{}
	for _, tool := range listPilotTools(t, cs) {
		if !strings.Contains(tool.Name, "pilot") {
			continue
		}
		got[tool.Name] = true
		if writes[tool.Name] {
			t.Errorf("%s is read-only by scope decision but is listed as a write tool", tool.Name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must carry ReadOnlyHint", tool.Name)
		}
	}
	for _, want := range []string{"list_pilots", "get_pilot", "list_pilot_follows", "get_pilot_follow"} {
		if !got[want] {
			t.Errorf("missing pilot tool %s", want)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected exactly 4 pilot tools, got %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/ -run 'TestListPilotFollows|TestGetPilotFollow|TestPortfolioReadExposesExactlyTheFourPilotTools' -v`
Expected: the `list_pilot_follows` and `get_pilot_follow` tests FAIL. `callPilotTool` stops with `call <tool>: ...` because the tools are not registered yet. `TestPortfolioReadExposesExactlyTheFourPilotTools` FAILs with `missing pilot tool list_pilot_follows` and `missing pilot tool get_pilot_follow`.

- [ ] **Step 3: Implement the two follow tools**

In `internal/tools/pilots.go`, replace the import block with the one below. It adds `regexp`, and `errmap`, which the events note uses so that it reads like every other tool's error text:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/errmap"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)
```

Add this after `normalizeSlug`:

```go
// uuidPattern matches a follow id. The backend parses followId as a UUID and
// answers "Follow not found." for anything else. Catching that here lets the
// tool say what was wrong (usually a pilot slug in the wrong tool) without a
// round trip.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// pilotFollowView is get_pilot_follow's answer: the follow and a bounded slice
// of its newest events. events_error is set, rather than failing the whole call,
// when the follow loaded but its events did not.
type pilotFollowView struct {
	Follow       api.PilotFollow        `json:"follow"`
	RecentEvents []api.PilotFollowEvent `json:"recent_events"`
	EventsShown  int                    `json:"events_shown"`
	EventsTotal  int                    `json:"events_total"`
	EventsError  string                 `json:"events_error,omitempty"`
}
```

Inside `registerPilots`, after the `get_pilot` `mcp.AddTool(...)` call, add:

```go
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_pilot_follows",
		Description: "List the user's pilot follows, newest first: which pilot, whether it mirrors " +
			"into a hypothetical portfolio or a watchlist feed, status (active or paused), starting " +
			"capital and currency, and the last applied book version. Use an id with " +
			"get_pilot_follow." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		follows, err := client.ListPilotFollows(ctx)
		if err != nil {
			return pilotListFail(err), nil, nil
		}
		out, _ := json.MarshalIndent(follows, "", "  ")
		return textResult(string(out), false), nil, nil
	})

	type getFollowArgs struct {
		FollowID   string `json:"follow_id" jsonschema:"follow id (a UUID) from list_pilot_follows; not a pilot slug"`
		EventLimit int    `json:"event_limit,omitempty" jsonschema:"how many of the newest events to include, 1-100; defaults to 20"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_pilot_follow",
		Description: "Get one of the user's pilot follows by id (from list_pilot_follows) with its " +
			"newest events: simulated buys and sells in the hypothetical portfolio, watchlist " +
			"symbols added or marked exited, and trades skipped for missing prices or size limits." +
			pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getFollowArgs) (*mcp.CallToolResult, any, error) {
		id := strings.TrimSpace(args.FollowID)
		if !uuidPattern.MatchString(id) {
			return textResult(fmt.Sprintf(
				"%q is not a follow id. Follow ids are UUIDs; call list_pilot_follows to find one. "+
					"To look up a pilot by slug, use get_pilot instead.", args.FollowID,
			), true), nil, nil
		}
		follow, err := client.GetPilotFollow(ctx, id)
		if err != nil {
			missing := fmt.Sprintf("No pilot follow with id %s on this account. Call list_pilot_follows to see the user's follows.", id)
			return pilotItemFail(ctx, client, err, missing), nil, nil
		}

		view := pilotFollowView{Follow: *follow, RecentEvents: []api.PilotFollowEvent{}}
		events, eventsErr := client.ListPilotFollowEvents(ctx, id)
		if eventsErr != nil {
			view.EventsError = "Recent events could not be loaded: " + errmap.Friendly(eventsErr)
		} else {
			limit := clamp(args.EventLimit, 20, 1, 100)
			view.EventsTotal = len(events)
			if len(events) > limit {
				events = events[:limit]
			}
			view.RecentEvents = events
			view.EventsShown = len(events)
		}
		out, _ := json.MarshalIndent(view, "", "  ")
		return textResult(string(out), false), nil, nil
	})
```

- [ ] **Step 4: Run the pilot tests to verify they pass**

Run: `go test ./internal/tools/ -run 'Pilot|TestNoToolNamesTheThirdPartyBrand|TestEveryMutatingToolIsInTheWriteAllowlist|TestEveryWriteToolIsMappedOrDeclaredMCPOnly' -v`
Expected: all PASS.

- [ ] **Step 5: Update the README**

In `README.md`, replace line 49:

```markdown
| `portfolio:read` | `get_portfolio_summary` (also granted by legacy `market:read`) |
```

with:

```markdown
| `portfolio:read` | `get_portfolio_summary` (also granted by legacy `market:read`); `list_pilots`, `get_pilot`, `list_pilot_follows`, `get_pilot_follow` (`portfolio:read` only) |
```

After the `get_news` paragraph (the one starting "`get_news` is the one news tool", line 73), insert a blank line and then:

```markdown
The pilot tools are read-only views of pilot follows: a user follows a curated politician or 13F fund, and Norviq mirrors that pilot's disclosed trades as **simulated** trades into a hypothetical portfolio, or as a watchlist feed that marks sold symbols `exited`. Disclosures arrive with a lag (up to 45 days for congress, 135 for 13F), and no real money is invested. Follows are created, paused and stopped in the app, not over MCP. While the backend's `PILOTS_ENABLED` flag is off, each pilot tool answers "Pilot follows are not enabled…" instead of a not-found error.
```

- [ ] **Step 6: Full verification**

Run: `go tool gofumpt -l .`
Expected: no output.

Run: `golangci-lint run`
Expected: `0 issues.`

Run: `go test ./...`
Expected: everything passes except the pre-existing `TestGetNewsDefaultUsesTrackedFeed` (see Global Constraints). If any other test fails, fix it before you commit.

Run: `grep -rniE 'auto[p]ilot' --include='*.go' --include='*.md' .`
Expected: no output. The bracket keeps this plan from containing the word. Task 2's guard test spells it as `"auto" + "pilot"`, which this pattern does not match.

- [ ] **Step 7: Commit**

```bash
git add internal/tools/pilots.go internal/tools/pilots_test.go README.md
git commit -m "feat(pilots): list_pilot_follows and get_pilot_follow read tools"
```
