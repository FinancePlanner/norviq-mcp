package api

import (
	"context"
	"net/http"
	"net/url"
)

type Goal struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type GoalRequest struct {
	Title string `json:"title"`
}

func (c *Client) ListGoals(ctx context.Context) ([]Goal, error) {
	out := []Goal{}
	if err := c.do(ctx, http.MethodGet, "/v1/goals", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateGoal(ctx context.Context, title, idempotencyKey string) (*Goal, error) {
	var out Goal
	if err := c.doWithIdempotency(
		ctx,
		http.MethodPost,
		"/v1/goals",
		GoalRequest{Title: title},
		&out,
		idempotencyKey,
	); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateGoal(ctx context.Context, id, title string) (*Goal, error) {
	var out Goal
	path := "/v1/goals/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, GoalRequest{Title: title}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteGoal(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/goals/"+url.PathEscape(id), nil, nil, nil)
}
