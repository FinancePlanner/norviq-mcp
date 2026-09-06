package api

import (
	"context"
	"net/http"
	"net/url"
)

type Transaction struct {
	ID           string   `json:"id"`
	AccountID    string   `json:"accountId"`
	InstrumentID string   `json:"instrumentId"`
	Type         string   `json:"type"`
	Quantity     *float64 `json:"quantity,omitempty"`
	Price        *float64 `json:"price,omitempty"`
	Currency     string   `json:"currency"`
	TradeDate    string   `json:"tradeDate"`
	SettleDate   string   `json:"settleDate,omitempty"`
	Fees         *float64 `json:"fees,omitempty"`
}

type CreateTransactionRequest struct {
	Symbol          string   `json:"symbol"`
	Type            string   `json:"type"`
	Quantity        float64  `json:"quantity"`
	Price           float64  `json:"price"`
	Currency        *string  `json:"currency,omitempty"`
	TradeDate       string   `json:"tradeDate"`
	SettleDate      *string  `json:"settleDate,omitempty"`
	Fees            *float64 `json:"fees,omitempty"`
	PortfolioListID *string  `json:"portfolioListId,omitempty"`
}

type UpdateTransactionRequest struct {
	Quantity   *float64 `json:"quantity,omitempty"`
	Price      *float64 `json:"price,omitempty"`
	Currency   *string  `json:"currency,omitempty"`
	TradeDate  *string  `json:"tradeDate,omitempty"`
	SettleDate *string  `json:"settleDate,omitempty"`
	Fees       *float64 `json:"fees,omitempty"`
}

func (c *Client) ListTransactions(ctx context.Context) ([]Transaction, error) {
	out := []Transaction{}
	if err := c.do(ctx, http.MethodGet, "/v1/transactions", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateTransaction(ctx context.Context, body CreateTransactionRequest, idempotencyKey string) (*Transaction, error) {
	var out Transaction
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/transactions", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateTransaction(ctx context.Context, id string, body UpdateTransactionRequest) (*Transaction, error) {
	var out Transaction
	if err := c.do(ctx, http.MethodPatch, "/v1/transactions/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteTransaction(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/transactions/"+url.PathEscape(id), nil, nil, nil)
}
