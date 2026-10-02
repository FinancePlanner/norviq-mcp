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
