package tools

import (
	"context"
	"errors"
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

func confirmMutation(ctx context.Context, session *mcp.ServerSession, message string) (bool, error) {
	if !SupportsFormElicitation(session) {
		return false, errors.New("this MCP client cannot safely confirm financial changes")
	}

	result, err := session.Elicit(ctx, &mcp.ElicitParams{
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
	})
	if err != nil {
		return false, err
	}
	if result == nil || result.Action != "accept" {
		return false, nil
	}
	confirmed, ok := result.Content["confirm"].(bool)
	return ok && confirmed, nil
}
