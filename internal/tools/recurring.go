package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recurringArgs struct {
	Title            string  `json:"title" jsonschema:"recurring expense title"`
	Amount           float64 `json:"amount" jsonschema:"positive recurring amount"`
	Pillar           string  `json:"pillar" jsonschema:"fundamentals, futureYou, or fun"`
	CategoryID       string  `json:"category_id,omitempty" jsonschema:"optional category UUID"`
	Frequency        string  `json:"frequency" jsonschema:"weekly or monthly"`
	SplitMode        string  `json:"split_mode" jsonschema:"personal or shared"`
	UserSharePercent float64 `json:"user_share_percent" jsonschema:"user share from 0 to 100"`
}

func registerRecurring(s *mcp.Server, client *api.Client, principal *auth.Principal) {
	if principal.Scopes["expenses:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_recurring_expenses",
			Description: "List the user's recurring expense templates.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.ServerSession, _ *mcp.CallToolParamsFor[struct{}]) (*mcp.CallToolResult, error) {
			items, err := client.ListRecurringExpenses(ctx)
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(items, "", "  ")
			return textResult(string(out), false), nil
		})
	}

	if !principal.Scopes["expenses:write"] {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_recurring_expense",
		Description: "Propose adding a recurring expense template. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[recurringArgs]) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(req.Arguments.Title) == "" || req.Arguments.Amount <= 0 {
			return textResult("A title and positive amount are required.", true), nil
		}
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Add recurring expense %q for %.2f %s?", req.Arguments.Title, req.Arguments.Amount, req.Arguments.Frequency))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Recurring expense creation was not confirmed.", false), nil
		}
		created, err := client.CreateRecurringExpense(ctx, recurringRequest(req.Arguments), idempotencyKey(principal.UserID, req.Arguments))
		if err != nil {
			return fail(err), nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created recurring expense:\n"+string(out), false), nil
	})

	type updateArgs struct {
		ID string `json:"id" jsonschema:"recurring template UUID"`
		recurringArgs
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_recurring_expense",
		Description: "Propose replacing a recurring expense template. Requires MCP form confirmation.",
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[updateArgs]) (*mcp.CallToolResult, error) {
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Replace recurring expense %q with the proposed values?", req.Arguments.ID))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Recurring expense update was not confirmed.", false), nil
		}
		updated, err := client.UpdateRecurringExpense(ctx, req.Arguments.ID, recurringRequest(req.Arguments.recurringArgs))
		if err != nil {
			return fail(err), nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated recurring expense:\n"+string(out), false), nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"recurring template UUID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_recurring_expense",
		Description: "Propose deleting a recurring expense template. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[deleteArgs]) (*mcp.CallToolResult, error) {
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Permanently delete recurring expense %q?", req.Arguments.ID))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Recurring expense deletion was not confirmed.", false), nil
		}
		if err := client.DeleteRecurringExpense(ctx, req.Arguments.ID); err != nil {
			return fail(err), nil
		}
		return textResult("Deleted recurring expense "+req.Arguments.ID+".", false), nil
	})
}

func recurringRequest(args recurringArgs) api.RecurringExpenseRequest {
	var categoryID *string
	if value := strings.TrimSpace(args.CategoryID); value != "" {
		categoryID = &value
	}
	return api.RecurringExpenseRequest{
		Title:            args.Title,
		Amount:           args.Amount,
		Pillar:           args.Pillar,
		CategoryID:       categoryID,
		Frequency:        args.Frequency,
		SplitMode:        args.SplitMode,
		UserSharePercent: args.UserSharePercent,
	}
}
