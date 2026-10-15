package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Market and insights reads return the backend JSON verbatim so the model sees
// whatever fields the backend provides without the MCP service re-modelling them.

func (c *Client) GetQuote(ctx context.Context, symbol string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/market/quote/"+url.PathEscape(symbol), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SearchSymbols(ctx context.Context, query string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("q", query)
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/market/search", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetPortfolioSummary(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/portfolio/summary", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetInsightsSummary(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/insights/summary", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarketBrief mirrors GET /v1/market/brief (StockPlanShared MarketBriefResponse).
// The brief is shared: the same document for every caller. Enum-like fields
// (slot, tone, direction, kind) are plain strings so a value the backend adds
// later decodes instead of failing the whole call.
type MarketBrief struct {
	// Enabled is false while the backend's market brief feature is off.
	Enabled bool `json:"enabled"`
	// TradingDate is yyyy-MM-dd. Nil with Enabled true means no brief has been
	// generated yet.
	TradingDate *string `json:"tradingDate"`
	// Slot is "morning" (pre-market) or "evening" (market close).
	Slot        *string            `json:"slot"`
	Language    string             `json:"language"`
	Greeting    *string            `json:"greeting"`
	Groups      []MarketBriefGroup `json:"groups"`
	Items       []MarketBriefItem  `json:"items"`
	GeneratedAt *string            `json:"generatedAt"`
	// Degraded is true when the brief was generated with part of its inputs
	// missing.
	Degraded bool `json:"degraded"`
	// Previous points at the brief before this one. The backend works it out
	// per request.
	Previous *MarketBriefRef `json:"previous"`
}

type MarketBriefGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Tone is "up", "down" or "flat".
	Tone string           `json:"tone"`
	Rows []MarketBriefRow `json:"rows"`
}

type MarketBriefRow struct {
	Symbol string `json:"symbol"`
	Flag   string `json:"flag"`
	Name   string `json:"name"`
	// Level is pre-formatted by the backend.
	Level string `json:"level"`
	// ChangePercent is formatted and unsigned; Direction carries the sign.
	ChangePercent string `json:"changePercent"`
	// Direction is "up", "down" or "flat".
	Direction string `json:"direction"`
}

type MarketBriefItem struct {
	// Kind is "highlight", "earnings" or "story".
	Kind      string   `json:"kind"`
	Text      string   `json:"text"`
	Tickers   []string `json:"tickers"`
	SourceURL *string  `json:"sourceUrl"`
}

type MarketBriefRef struct {
	Slot        string `json:"slot"`
	TradingDate string `json:"tradingDate"`
}

// GetMarketBrief fetches the latest brief in lang, or the brief for slot and
// date when both are given. The backend answers 404 when that slot and date
// have no brief, and 400 when slot comes without a yyyy-MM-dd date.
func (c *Client) GetMarketBrief(ctx context.Context, lang, slot, date string) (*MarketBrief, error) {
	q := url.Values{}
	if lang != "" {
		q.Set("lang", lang)
	}
	if slot != "" {
		q.Set("slot", slot)
	}
	if date != "" {
		q.Set("date", date)
	}
	var out MarketBrief
	if err := c.do(ctx, http.MethodGet, "/v1/market/brief", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
