package api

import (
	"context"
	"net/http"
	"net/url"
)

// Terminal position types mirror StockPlanShared/TerminalPositions/
// TerminalPositionsDTOs.swift (norviq-shared 5.21.0), as fixed by the
// cross-repo contract norviq-backend/docs/superpowers/plans/
// 2026-10-09-terminal-contract.md.
//
// The derived values are computed by the backend (the shared TerminalMath) and
// passed through untouched. Nothing in this service computes them. They carry
// no omitempty, so an invalid scenario shows null next to scenarioError rather
// than leaving the model to wonder where they went.

type TerminalPosition struct {
	ID                 string   `json:"id"`
	Ticker             string   `json:"ticker"`
	SharesOutstanding  *float64 `json:"sharesOutstanding"`
	TerminalShareCount float64  `json:"terminalShareCount"`
	TerminalMarketCap  float64  `json:"terminalMarketCap"`
	ValueWanted        float64  `json:"valueWanted"`
	SharesOwned        float64  `json:"sharesOwned"`
	CurrentSharePrice  *float64 `json:"currentSharePrice"`
	Notes              *string  `json:"notes"`
	SortOrder          int      `json:"sortOrder"`
	// Derived by the backend; all nil when ScenarioError is set.
	TerminalSharePrice *float64 `json:"terminalSharePrice"`
	SharesNeeded       *float64 `json:"sharesNeeded"`
	// CapitalAtTodayPrice is also nil when the row has no current price.
	CapitalAtTodayPrice *float64 `json:"capitalAtTodayPrice"`
	Progress            *float64 `json:"progress"`
	SharesStillNeeded   *float64 `json:"sharesStillNeeded"`
	GapValueAtTerminal  *float64 `json:"gapValueAtTerminal"`
	// ScenarioError is a TerminalScenarioError raw value:
	// "share_count_not_positive", "market_cap_not_positive" or "invalid_number".
	ScenarioError *string `json:"scenarioError"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

type TerminalPositionsList struct {
	Currency  string             `json:"currency"`
	Positions []TerminalPosition `json:"positions"`
}

type TerminalPositionCreateRequest struct {
	Ticker             string   `json:"ticker"`
	SharesOutstanding  *float64 `json:"sharesOutstanding,omitempty"`
	TerminalShareCount float64  `json:"terminalShareCount"`
	TerminalMarketCap  float64  `json:"terminalMarketCap"`
	ValueWanted        float64  `json:"valueWanted"`
	SharesOwned        *float64 `json:"sharesOwned,omitempty"`
	CurrentSharePrice  *float64 `json:"currentSharePrice,omitempty"`
}

// TerminalPositionUpdateRequest is a PATCH: only non-nil fields change. A
// pointer to 0 is sent as 0. MCP never sends the contract's ticker, notes or
// clear fields.
type TerminalPositionUpdateRequest struct {
	SharesOutstanding  *float64 `json:"sharesOutstanding,omitempty"`
	TerminalShareCount *float64 `json:"terminalShareCount,omitempty"`
	TerminalMarketCap  *float64 `json:"terminalMarketCap,omitempty"`
	ValueWanted        *float64 `json:"valueWanted,omitempty"`
	SharesOwned        *float64 `json:"sharesOwned,omitempty"`
	CurrentSharePrice  *float64 `json:"currentSharePrice,omitempty"`
}

type ShareFactsRequest struct {
	Ticker string `json:"ticker"`
}

type ShareFactsSuggestion struct {
	Ticker            string   `json:"ticker"`
	SharesOutstanding *float64 `json:"sharesOutstanding"`
	CurrentSharePrice *float64 `json:"currentSharePrice"`
	Currency          *string  `json:"currency"`
	AsOf              *string  `json:"asOf"`
	Sources           []string `json:"sources"`
}

// ListTerminalPositions returns the user's rows in sortOrder. A non-empty
// ticker filters them (the backend matches case-insensitively).
func (c *Client) ListTerminalPositions(ctx context.Context, ticker string) (*TerminalPositionsList, error) {
	q := url.Values{}
	if ticker != "" {
		q.Set("ticker", ticker)
	}
	var out TerminalPositionsList
	if err := c.do(ctx, http.MethodGet, "/v1/terminal-positions", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateTerminalPosition(ctx context.Context, req TerminalPositionCreateRequest, idempotencyKey string) (*TerminalPosition, error) {
	var out TerminalPosition
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/terminal-positions", req, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateTerminalPosition(ctx context.Context, id string, req TerminalPositionUpdateRequest) (*TerminalPosition, error) {
	var out TerminalPosition
	if err := c.do(ctx, http.MethodPatch, "/v1/terminal-positions/"+url.PathEscape(id), nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LookupShareFacts asks the backend's Pro-gated AI lookup for shares
// outstanding and today's price. It only suggests; nothing is stored.
func (c *Client) LookupShareFacts(ctx context.Context, ticker string) (*ShareFactsSuggestion, error) {
	var out ShareFactsSuggestion
	if err := c.do(ctx, http.MethodPost, "/v1/terminal-positions/ai/share-facts", nil, ShareFactsRequest{Ticker: ticker}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
