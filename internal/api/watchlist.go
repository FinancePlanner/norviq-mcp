package api

import (
	"context"
	"net/http"
	"net/url"
)

// WatchlistStatus values accepted by the backend. Mirrors WatchlistStatus in
// norviq-shared; an unknown value is coerced to "active" server-side, which
// silently loses the caller's intent, so the tool layer validates up front.
var WatchlistStatuses = []string{"active", "researching", "waiting", "ready", "archived"}

type WatchlistItem struct {
	ID              string `json:"id"`
	Symbol          string `json:"symbol"`
	Note            string `json:"note,omitempty"`
	Status          string `json:"status"`
	WatchlistListID string `json:"watchlistListId,omitempty"`
	LastReviewedAt  string `json:"lastReviewedAt,omitempty"`
	NextReviewAt    string `json:"nextReviewAt,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

type WatchlistItemRequest struct {
	Symbol          string  `json:"symbol"`
	Note            *string `json:"note,omitempty"`
	Status          *string `json:"status,omitempty"`
	NextReviewAt    *string `json:"nextReviewAt,omitempty"`
	WatchlistListID *string `json:"watchlistListId,omitempty"`
}

type WatchlistItemUpdateRequest struct {
	Note            *string `json:"note,omitempty"`
	Status          *string `json:"status,omitempty"`
	LastReviewedAt  *string `json:"lastReviewedAt,omitempty"`
	NextReviewAt    *string `json:"nextReviewAt,omitempty"`
	WatchlistListID *string `json:"watchlistListId,omitempty"`
}

type WatchlistList struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

func (c *Client) ListWatchlist(ctx context.Context) ([]WatchlistItem, error) {
	out := []WatchlistItem{}
	if err := c.do(ctx, http.MethodGet, "/v1/watchlist", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWatchlistItem is upsert-shaped server-side: an existing (user, list,
// symbol) is patched and returned 200, otherwise inserted 201.
func (c *Client) CreateWatchlistItem(ctx context.Context, body WatchlistItemRequest, idempotencyKey string) (*WatchlistItem, error) {
	var out WatchlistItem
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/watchlist", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateWatchlistItem(ctx context.Context, id string, body WatchlistItemUpdateRequest) (*WatchlistItem, error) {
	var out WatchlistItem
	path := "/v1/watchlist/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteWatchlistItem(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/watchlist/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) ListWatchlistLists(ctx context.Context) ([]WatchlistList, error) {
	out := []WatchlistList{}
	if err := c.do(ctx, http.MethodGet, "/v1/watchlist/lists", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateWatchlistList(ctx context.Context, name, idempotencyKey string) (*WatchlistList, error) {
	var out WatchlistList
	body := struct {
		Name string `json:"name"`
	}{Name: name}
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/watchlist/lists", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteWatchlistList(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/watchlist/lists/"+url.PathEscape(id), nil, nil, nil)
}
