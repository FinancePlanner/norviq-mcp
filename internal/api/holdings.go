package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// AssetCategories mirrors AssetCategory in norviq-shared. The backend rejects an
// unknown value with a 400, so the tool layer validates before sending.
var AssetCategories = []string{
	"stock", "etf", "mutual_fund", "crypto", "cash", "bond", "real_estate", "commodity",
}

type Stock struct {
	ID              string  `json:"id"`
	Symbol          string  `json:"symbol"`
	Shares          float64 `json:"shares"`
	BuyPrice        float64 `json:"buyPrice"`
	BuyDate         string  `json:"buyDate"`
	Notes           *string `json:"notes,omitempty"`
	Category        string  `json:"category"`
	PortfolioListID *string `json:"portfolioListId,omitempty"`
	CreatedAt       string  `json:"createdAt,omitempty"`
}

type StockListPage struct {
	Items      []Stock `json:"items"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

type StockRequest struct {
	Symbol          string  `json:"symbol"`
	Shares          float64 `json:"shares"`
	BuyPrice        float64 `json:"buyPrice"`
	BuyDate         string  `json:"buyDate"`
	Notes           *string `json:"notes,omitempty"`
	Category        string  `json:"category"`
	PortfolioListID *string `json:"portfolioListId,omitempty"`
}

type SellStockRequest struct {
	SharesToSell float64 `json:"sharesToSell"`
	SellPrice    float64 `json:"sellPrice"`
	SellDate     string  `json:"sellDate"`
}

func (c *Client) ListStocks(ctx context.Context, portfolioListID string, limit int) (*StockListPage, error) {
	query := url.Values{}
	if portfolioListID != "" {
		query.Set("portfolioListId", portfolioListID)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var out StockListPage
	if err := c.do(ctx, http.MethodGet, "/v1/stocks", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateStock(ctx context.Context, body StockRequest, idempotencyKey string) (*Stock, error) {
	var out Stock
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/stocks", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateStock(ctx context.Context, id string, body StockRequest) (*Stock, error) {
	var out Stock
	if err := c.do(ctx, http.MethodPut, "/v1/stocks/id/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SellStock records a disposal. It reduces or removes the position, credits cash,
// and (since the manual-trade work) writes a Transaction row so the sale reaches
// realized P&L and tax reports.
func (c *Client) SellStock(ctx context.Context, id string, body SellStockRequest, idempotencyKey string) (*Stock, error) {
	var out Stock
	path := "/v1/stocks/id/" + url.PathEscape(id) + "/sell"
	if err := c.doWithIdempotency(ctx, http.MethodPost, path, body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteStock(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/stocks/id/"+url.PathEscape(id), nil, nil, nil)
}
