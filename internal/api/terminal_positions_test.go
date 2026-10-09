package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
)

// The AMZN worked example from the spec: 10T terminal market cap, 11B terminal
// shares, 1M wanted, 750 owned at $200 → 909.09… per share, 1,100 shares needed.
const amznPositionJSON = `{"id":"11111111-1111-4111-8111-111111111111","ticker":"AMZN","sharesOutstanding":10600000000,"terminalShareCount":11000000000,"terminalMarketCap":10000000000000,"valueWanted":1000000,"sharesOwned":750,"currentSharePrice":200,"sortOrder":0,"terminalSharePrice":909.0909090909091,"sharesNeeded":1100,"capitalAtTodayPrice":220000,"progress":0.6818181818181818,"sharesStillNeeded":350,"gapValueAtTerminal":318181.8181818182,"createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"}`

// Swift's encoder omits nil optionals, so an invalid scenario arrives with no
// derived keys at all, only scenarioError.
const invalidPositionJSON = `{"id":"22222222-2222-4222-8222-222222222222","ticker":"VG","terminalShareCount":0,"terminalMarketCap":8000000000,"valueWanted":500000,"sharesOwned":0,"sortOrder":1,"scenarioError":"share_count_not_positive","createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return api.NewClient(srv.URL, "nvq_pat_test")
}

func TestListTerminalPositionsSendsTheTickerAndDecodesDerivedValues(t *testing.T) {
	var gotTicker, gotAuth string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/terminal-positions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotTicker = r.URL.Query().Get("ticker")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"currency":"USD","positions":[` + amznPositionJSON + `,` + invalidPositionJSON + `]}`))
	})

	list, err := client.ListTerminalPositions(context.Background(), "AMZN")
	if err != nil {
		t.Fatal(err)
	}
	if gotTicker != "AMZN" {
		t.Errorf("ticker query = %q, want AMZN", gotTicker)
	}
	if gotAuth != "Bearer nvq_pat_test" {
		t.Errorf("Authorization = %q, want the user's bearer", gotAuth)
	}
	if list.Currency != "USD" || len(list.Positions) != 2 {
		t.Fatalf("list = %+v, want USD and two positions", list)
	}
	valid := list.Positions[0]
	if valid.TerminalSharePrice == nil || *valid.TerminalSharePrice != 909.0909090909091 {
		t.Errorf("terminalSharePrice = %v, want the backend's 909.0909090909091", valid.TerminalSharePrice)
	}
	if valid.SharesNeeded == nil || *valid.SharesNeeded != 1100 {
		t.Errorf("sharesNeeded = %v, want the backend's 1100", valid.SharesNeeded)
	}
	invalid := list.Positions[1]
	if invalid.TerminalSharePrice != nil || invalid.SharesNeeded != nil || invalid.Progress != nil {
		t.Error("an invalid scenario must decode with nil derived values")
	}
	if invalid.ScenarioError == nil || *invalid.ScenarioError != "share_count_not_positive" {
		t.Errorf("scenarioError = %v, want share_count_not_positive", invalid.ScenarioError)
	}
}

func TestListTerminalPositionsWithoutATickerSendsNoQuery(t *testing.T) {
	rawQuery := "unset"
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"currency":"EUR","positions":[]}`))
	})
	if _, err := client.ListTerminalPositions(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Errorf("query = %q, want none", rawQuery)
	}
}

func TestUpdateTerminalPositionSendsOnlyTheGivenFields(t *testing.T) {
	var gotRequest string
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRequest = r.Method + " " + r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(amznPositionJSON))
	})

	valueWanted := 2000000.0
	sharesOwned := 0.0
	updated, err := client.UpdateTerminalPosition(context.Background(), "11111111-1111-4111-8111-111111111111",
		api.TerminalPositionUpdateRequest{ValueWanted: &valueWanted, SharesOwned: &sharesOwned})
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest != "PATCH /v1/terminal-positions/11111111-1111-4111-8111-111111111111" {
		t.Errorf("request = %q", gotRequest)
	}
	// sharesOwned 0 is a real value (the user owns none) and must be sent.
	if len(body) != 2 || body["valueWanted"] != 2000000.0 || body["sharesOwned"] != 0.0 {
		t.Errorf("PATCH body = %v, want exactly valueWanted 2000000 and sharesOwned 0", body)
	}
	if updated.ID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("decoded id = %q", updated.ID)
	}
}

func TestCreateTerminalPositionSendsAnIdempotencyKey(t *testing.T) {
	var gotKey string
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/terminal-positions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotKey = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(amznPositionJSON))
	})

	created, err := client.CreateTerminalPosition(context.Background(), api.TerminalPositionCreateRequest{
		Ticker: "AMZN", TerminalShareCount: 11000000000, TerminalMarketCap: 10000000000000, ValueWanted: 1000000,
	}, "mcp_test_key")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "mcp_test_key" {
		t.Errorf("Idempotency-Key = %q, want mcp_test_key", gotKey)
	}
	if body["ticker"] != "AMZN" || body["terminalShareCount"] != 11000000000.0 ||
		body["terminalMarketCap"] != 10000000000000.0 || body["valueWanted"] != 1000000.0 {
		t.Errorf("POST body = %v", body)
	}
	for _, absent := range []string{"sharesOutstanding", "sharesOwned", "currentSharePrice"} {
		if _, ok := body[absent]; ok {
			t.Errorf("POST body carries %s although it was not given: %v", absent, body)
		}
	}
	if created.SharesNeeded == nil || *created.SharesNeeded != 1100 {
		t.Errorf("created sharesNeeded = %v, want 1100", created.SharesNeeded)
	}
}

func TestLookupShareFactsPostsTheTicker(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/terminal-positions/ai/share-facts" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"ticker":"AMZN","sharesOutstanding":10600000000,"currentSharePrice":201.5,"currency":"USD","asOf":"2026-10-08","sources":["https://ir.aboutamazon.com/quarterly-results"]}`))
	})

	facts, err := client.LookupShareFacts(context.Background(), "AMZN")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["ticker"] != "AMZN" {
		t.Errorf("body = %v, want only ticker AMZN", body)
	}
	if facts.SharesOutstanding == nil || *facts.SharesOutstanding != 10600000000 || len(facts.Sources) != 1 {
		t.Errorf("facts = %+v", facts)
	}
}

func TestLookupShareFactsKeepsTheUpgradeErrorReadable(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"code":"upgrade_required","error":"Upgrade required","feature":"terminal_position_ai","plan":"free","requiredPlan":"pro"}`))
	})
	_, err := client.LookupShareFacts(context.Background(), "AMZN")
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *api.APIError", err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Code() != "upgrade_required" {
		t.Errorf("status %d code %q, want 403 upgrade_required", apiErr.Status, apiErr.Code())
	}
}

func TestAPIErrorReadsCodeAndReason(t *testing.T) {
	abort := &api.APIError{Status: 422, Body: `{"error":true,"reason":"Ticker must be 1-12 letters, digits, dots or hyphens."}`}
	if abort.Reason() != "Ticker must be 1-12 letters, digits, dots or hyphens." {
		t.Errorf("Reason() = %q", abort.Reason())
	}
	if abort.Code() != "" {
		t.Errorf("Code() = %q, want empty for a Vapor Abort body", abort.Code())
	}
	garbage := &api.APIError{Status: 502, Body: "<html>bad gateway</html>"}
	if garbage.Code() != "" || garbage.Reason() != "" {
		t.Error("a non-JSON body must yield empty code and reason")
	}
}
