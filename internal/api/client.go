// Package api is a focused HTTP client for the norviq-backend REST surface the
// MCP tools call. It intentionally covers only the expenses and reports
// endpoints rather than generating the full 10k-line OpenAPI client — the tool
// surface is small and stable, and a hand-rolled client keeps the service lean.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client talks to norviq-backend as a specific user by forwarding that user's
// bearer token on every request.
type Client struct {
	baseURL string
	bearer  string
	http    *http.Client
}

func NewClient(baseURL, bearer string) *Client {
	return &Client{
		baseURL: baseURL,
		bearer:  bearer,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError carries the backend HTTP status so callers can map billing/limit
// responses to friendly messages.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("backend responded %d: %s", e.Status, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("X-Norviq-MCP", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(payload)}
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// MARK: - Expenses

type Expense struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Amount     float64 `json:"amount"`
	Pillar     string  `json:"pillar"`
	OccurredOn string  `json:"occurredOn"`
	CategoryID *string `json:"categoryId,omitempty"`
}

type CreateExpenseRequest struct {
	Title      string  `json:"title"`
	Amount     float64 `json:"amount"`
	Pillar     string  `json:"pillar"`
	OccurredOn string  `json:"occurredOn"`
	CategoryID *string `json:"categoryId,omitempty"`
}

func (c *Client) ListExpenses(ctx context.Context, from, to string, limit int) ([]Expense, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []Expense
	if err := c.do(ctx, http.MethodGet, "/v1/expenses", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateExpense(ctx context.Context, req CreateExpenseRequest, idempotencyKey string) (*Expense, error) {
	var out Expense
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/expenses", req, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateExpense(ctx context.Context, id string, req map[string]any) (*Expense, error) {
	var out Expense
	if err := c.do(ctx, http.MethodPatch, "/v1/expenses/"+url.PathEscape(id), nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteExpense(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/expenses/"+url.PathEscape(id), nil, nil, nil)
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListCategories(ctx context.Context) ([]Category, error) {
	var out []Category
	if err := c.do(ctx, http.MethodGet, "/v1/expenses/categories", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MARK: - Reports

// GetReport returns the raw JSON of a report endpoint so the model can present
// whatever fields the backend provides without the MCP service re-modelling them.
func (c *Client) GetReport(ctx context.Context, kind, from, to string) (json.RawMessage, error) {
	path := map[string]string{
		"overview":    "/v1/reports/overview",
		"suggestions": "/v1/reports/suggestions",
		"monthly":     "/v1/reports/expenses",
		"yearly":      "/v1/reports/expenses",
	}[kind]
	if path == "" {
		return nil, fmt.Errorf("unknown report kind %q", kind)
	}
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// doWithIdempotency adds an Idempotency-Key header so retried mutations (LLMs
// retry) don't double-write. The backend's IdempotencyMiddleware replays the
// cached response for a repeated key.
func (c *Client) doWithIdempotency(ctx context.Context, method, path string, body, out any, key string) error {
	u := c.baseURL + path
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("X-Norviq-MCP", "1")
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(payload)}
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
