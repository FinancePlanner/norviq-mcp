package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type BudgetSnapshot struct {
	ID           string             `json:"id"`
	MonthStart   string             `json:"monthStart"`
	NetSalary    float64            `json:"netSalary"`
	TargetShares map[string]float64 `json:"targetShares"`
}

type BudgetSnapshotRequest struct {
	MonthStart   string             `json:"monthStart"`
	NetSalary    float64            `json:"netSalary"`
	TargetShares map[string]float64 `json:"targetShares"`
}

type BudgetItem struct {
	ID               string  `json:"id"`
	SnapshotID       string  `json:"snapshotId"`
	Title            string  `json:"title"`
	PlannedAmount    float64 `json:"plannedAmount"`
	Pillar           string  `json:"pillar"`
	SplitMode        string  `json:"splitMode"`
	UserSharePercent float64 `json:"userSharePercent"`
}

type BudgetItemRequest struct {
	SnapshotID       string  `json:"snapshotId"`
	Title            string  `json:"title"`
	PlannedAmount    float64 `json:"plannedAmount"`
	Pillar           string  `json:"pillar"`
	SplitMode        string  `json:"splitMode"`
	UserSharePercent float64 `json:"userSharePercent"`
}

type RecurringExpense struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Amount           float64 `json:"amount"`
	Pillar           string  `json:"pillar"`
	CategoryID       *string `json:"categoryId,omitempty"`
	Frequency        string  `json:"frequency"`
	SplitMode        string  `json:"splitMode"`
	UserSharePercent float64 `json:"userSharePercent"`
}

type RecurringExpenseRequest struct {
	Title            string  `json:"title"`
	Amount           float64 `json:"amount"`
	Pillar           string  `json:"pillar"`
	CategoryID       *string `json:"categoryId,omitempty"`
	Frequency        string  `json:"frequency"`
	SplitMode        string  `json:"splitMode"`
	UserSharePercent float64 `json:"userSharePercent"`
}

func (c *Client) ListBudgetSnapshots(ctx context.Context, year, month int) ([]BudgetSnapshot, error) {
	query := url.Values{}
	if year > 0 {
		query.Set("year", strconv.Itoa(year))
	}
	if month > 0 {
		query.Set("month", strconv.Itoa(month))
	}
	out := []BudgetSnapshot{}
	if err := c.do(ctx, http.MethodGet, "/v1/budget/snapshots", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateBudgetSnapshot(ctx context.Context, request BudgetSnapshotRequest, key string) (*BudgetSnapshot, error) {
	var out BudgetSnapshot
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/budget/snapshots", request, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateBudgetSnapshot(ctx context.Context, id string, request BudgetSnapshotRequest) (*BudgetSnapshot, error) {
	var out BudgetSnapshot
	if err := c.do(ctx, http.MethodPatch, "/v1/budget/snapshots/"+url.PathEscape(id), nil, request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteBudgetSnapshot(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/budget/snapshots/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) ListBudgetItems(ctx context.Context) ([]BudgetItem, error) {
	out := []BudgetItem{}
	if err := c.do(ctx, http.MethodGet, "/v1/budget/items", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateBudgetItem(ctx context.Context, request BudgetItemRequest, key string) (*BudgetItem, error) {
	var out BudgetItem
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/budget/items", request, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateBudgetItem(ctx context.Context, id string, request BudgetItemRequest) (*BudgetItem, error) {
	var out BudgetItem
	if err := c.do(ctx, http.MethodPatch, "/v1/budget/items/"+url.PathEscape(id), nil, request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteBudgetItem(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/budget/items/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) ListRecurringExpenses(ctx context.Context) ([]RecurringExpense, error) {
	out := []RecurringExpense{}
	if err := c.do(ctx, http.MethodGet, "/v1/expenses/recurring", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateRecurringExpense(ctx context.Context, request RecurringExpenseRequest, key string) (*RecurringExpense, error) {
	var out RecurringExpense
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/expenses/recurring", request, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateRecurringExpense(ctx context.Context, id string, request RecurringExpenseRequest) (*RecurringExpense, error) {
	var out RecurringExpense
	if err := c.do(ctx, http.MethodPatch, "/v1/expenses/recurring/"+url.PathEscape(id), nil, request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRecurringExpense(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/expenses/recurring/"+url.PathEscape(id), nil, nil, nil)
}
