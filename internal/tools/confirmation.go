package tools

import (
	"errors"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var writeToolNames = []string{
	"add_expense",
	"update_expense",
	"delete_expense",
	"import_expenses_csv",
	"add_goal",
	"update_goal",
	"delete_goal",
	"create_budget_snapshot",
	"update_budget_snapshot",
	"delete_budget_snapshot",
	"add_budget_item",
	"update_budget_item",
	"delete_budget_item",
	"add_recurring_expense",
	"update_recurring_expense",
	"delete_recurring_expense",
	"upsert_watchlist_items",
	"update_watchlist_item",
	"remove_watchlist_items",
	"create_watchlist_list",
	"delete_watchlist_list",
	"record_trades",
	"update_trade",
	"delete_trade",
	"add_position",
	"sell_position",
	"delete_position",
	"create_price_target",
	"delete_price_target",
	"add_research_note",
	"delete_research_note",
}

// WriteToolNames returns a defensive copy of tools that must never be exposed
// to clients without form elicitation support.
func WriteToolNames() []string {
	return slices.Clone(writeToolNames)
}

// SupportsFormElicitation reports whether the initialized client can render a
// confirmation form. An elicitation capability with neither mode specified
// means form mode per the MCP specification.
func SupportsFormElicitation(session *mcp.ServerSession) bool {
	if session == nil {
		return false
	}
	params := session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false
	}
	capability := params.Capabilities.Elicitation
	return capability.Form != nil || capability.URL == nil
}

// confirmKey is the input-request ID for the confirmation elicitation. One ID
// is enough: a mutation asks exactly one confirmation question.
const confirmKey = "confirm"

// confirmMutation asks the caller to confirm a write before it happens.
//
// Protocol version 2026-07-28 forbids a server from sending elicitation/create
// while it is serving a request (SEP-2322). Instead the handler returns an
// input-required result carrying the elicitation, and the SDK re-invokes the
// handler with the answer in Params.InputResponses. Clients on older protocol
// versions reach the same place: the SDK's server-side middleware fulfills the
// request and reinvokes the handler, so both paths look identical from here.
//
// The return values are (confirmed, pending, err). When pending is non-nil the
// user has not answered yet and the handler must return it unchanged, without
// touching the backend.
func confirmMutation(req *mcp.CallToolRequest, message string) (bool, *mcp.CallToolResult, error) {
	if !SupportsFormElicitation(req.Session) {
		return false, nil, errors.New("this MCP client cannot safely confirm financial changes")
	}

	// Second pass: the client answered, so decide from its response.
	if resp, ok := req.Params.InputResponses[confirmKey]; ok {
		result, ok := resp.(*mcp.ElicitResult)
		if !ok {
			return false, nil, fmt.Errorf("unexpected confirmation response of type %T", resp)
		}
		if result == nil || result.Action != "accept" {
			return false, nil, nil
		}
		confirmed, ok := result.Content["confirm"].(bool)
		return ok && confirmed, nil, nil
	}

	// First pass: ask, and let the SDK bring the answer back to us.
	return false, &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			confirmKey: &mcp.ElicitParams{
				Mode:    "form",
				Message: message,
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confirm": map[string]any{
							"type":        "boolean",
							"title":       "Confirm change",
							"description": "I reviewed this exact change and want Norviq to apply it.",
						},
					},
					"required":             []string{"confirm"},
					"additionalProperties": false,
				},
			},
		},
	}, nil
}
