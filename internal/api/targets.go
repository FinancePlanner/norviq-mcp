package api

import (
	"context"
	"net/http"
	"net/url"
)

type Target struct {
	ID          string  `json:"id"`
	Symbol      string  `json:"symbol"`
	Scenario    string  `json:"scenario"`
	TargetPrice float64 `json:"targetPrice"`
	TargetDate  *string `json:"targetDate,omitempty"`
	Rationale   *string `json:"rationale,omitempty"`
}

type TargetRequest struct {
	Symbol      string  `json:"symbol"`
	Scenario    string  `json:"scenario"`
	TargetPrice float64 `json:"targetPrice"`
	TargetDate  *string `json:"targetDate,omitempty"`
	Rationale   *string `json:"rationale,omitempty"`
}

func (c *Client) ListTargets(ctx context.Context) ([]Target, error) {
	out := []Target{}
	if err := c.do(ctx, http.MethodGet, "/v1/targets", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateTarget(ctx context.Context, body TargetRequest, idempotencyKey string) (*Target, error) {
	var out Target
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/targets", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateTarget(ctx context.Context, id string, body TargetRequest) (*Target, error) {
	var out Target
	if err := c.do(ctx, http.MethodPut, "/v1/targets/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteTarget(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/targets/"+url.PathEscape(id), nil, nil, nil)
}
