package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SearchResult is the normalized symbol search payload returned by Norviq.
type SearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Currency string `json:"currency"`
	ConID    string `json:"conid"`
}

// TrackedNewsItem mirrors /v1/news/feed.
type TrackedNewsItem struct {
	ID          string `json:"id"`
	Symbol      string `json:"symbol"`
	Headline    string `json:"headline"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	Summary     string `json:"summary"`
	PublishedAt string `json:"publishedAt"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// MarketNewsItem mirrors /v1/market/news and /v1/market/news/general
// (StockPlanShared.StockNews). Source and summary are populated from the
// archive when the provider supplied them.
type MarketNewsItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Date    string `json:"date"`
	Source  string `json:"source,omitempty"`
	Summary string `json:"summary,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
}

func (c *Client) SearchSymbolsList(ctx context.Context, query string) ([]SearchResult, error) {
	q := url.Values{}
	q.Set("q", query)
	var out []SearchResult
	if err := c.do(ctx, http.MethodGet, "/v1/market/search", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetNewsFeed(ctx context.Context, limit int) ([]TrackedNewsItem, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []TrackedNewsItem
	if err := c.do(ctx, http.MethodGet, "/v1/news/feed", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetMarketNews(ctx context.Context, symbol string, limit int) ([]MarketNewsItem, error) {
	q := url.Values{}
	q.Set("symbol", symbol)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []MarketNewsItem
	if err := c.do(ctx, http.MethodGet, "/v1/market/news", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetGeneralMarketNews(ctx context.Context, from, to string, page, limit int) ([]MarketNewsItem, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []MarketNewsItem
	if err := c.do(ctx, http.MethodGet, "/v1/market/news/general", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
