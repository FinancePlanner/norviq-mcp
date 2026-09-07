package api

import (
	"context"
	"net/http"
	"net/url"
)

type ResearchNote struct {
	ID             string   `json:"id"`
	Symbol         string   `json:"symbol"`
	Title          *string  `json:"title,omitempty"`
	Thesis         string   `json:"thesis"`
	Risks          *string  `json:"risks,omitempty"`
	Catalysts      *string  `json:"catalysts,omitempty"`
	ReferenceLinks []string `json:"referenceLinks,omitempty"`
}

type ResearchNoteRequest struct {
	Symbol         string   `json:"symbol"`
	Title          *string  `json:"title,omitempty"`
	Thesis         string   `json:"thesis"`
	Risks          *string  `json:"risks,omitempty"`
	Catalysts      *string  `json:"catalysts,omitempty"`
	ReferenceLinks []string `json:"referenceLinks,omitempty"`
}

func (c *Client) ListResearch(ctx context.Context) ([]ResearchNote, error) {
	out := []ResearchNote{}
	if err := c.do(ctx, http.MethodGet, "/v1/research", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateResearch(ctx context.Context, body ResearchNoteRequest, idempotencyKey string) (*ResearchNote, error) {
	var out ResearchNote
	if err := c.doWithIdempotency(ctx, http.MethodPost, "/v1/research", body, &out, idempotencyKey); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateResearch(ctx context.Context, id string, body ResearchNoteRequest) (*ResearchNote, error) {
	var out ResearchNote
	if err := c.do(ctx, http.MethodPut, "/v1/research/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteResearch(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/research/"+url.PathEscape(id), nil, nil, nil)
}
