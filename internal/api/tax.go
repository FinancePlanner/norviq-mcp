package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Tax reads return backend JSON verbatim. The backend remains responsible for
// entitlement filtering, jurisdiction support levels, warnings, and disclaimers.
func (c *Client) GetTaxDashboard(ctx context.Context, jurisdiction string, taxYear int) (json.RawMessage, error) {
	q := taxQuery(jurisdiction, taxYear)
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/tax/dashboard", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetTaxLossCarryforwards(ctx context.Context, jurisdiction string, taxYear int) (json.RawMessage, error) {
	q := taxQuery(jurisdiction, taxYear)
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/tax/loss-carryforwards", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func taxQuery(jurisdiction string, taxYear int) url.Values {
	q := url.Values{}
	if jurisdiction != "" {
		q.Set("jurisdiction", jurisdiction)
	}
	if taxYear > 0 {
		q.Set("taxYear", strconv.Itoa(taxYear))
	}
	return q
}
