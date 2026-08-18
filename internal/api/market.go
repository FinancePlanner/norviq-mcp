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
