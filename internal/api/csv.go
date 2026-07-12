package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ImportExpensesCSV posts raw CSV to the backend import endpoint and returns the
// per-row result. dryRun=true validates without writing.
func (c *Client) ImportExpensesCSV(ctx context.Context, csv string, dryRun bool) (json.RawMessage, error) {
	q := url.Values{}
	if !dryRun {
		q.Set("dry_run", "false")
	}
	u := c.baseURL + "/v1/expenses/import"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader([]byte(csv)))
	if err != nil {
		return nil, fmt.Errorf("build import request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("X-Norviq-MCP", "1")
	req.Header.Set("Content-Type", "text/csv")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call backend: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: string(payload)}
	}
	return json.RawMessage(payload), nil
}

// ExportExpensesCSV returns the CSV text for the given date range.
func (c *Client) ExportExpensesCSV(ctx context.Context, from, to string) (string, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	u := c.baseURL + "/v1/expenses/export.csv"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("build export request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("X-Norviq-MCP", "1")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call backend: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{Status: resp.StatusCode, Body: string(payload)}
	}
	return string(payload), nil
}
