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
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			items, err := client.ListRecurringExpenses(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(items, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !principal.Scopes["expenses:write"] {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_recurring_expense",
		Description: "Propose adding a recurring expense template. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args recurringArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Title) == "" || args.Amount <= 0 {
			return textResult("A title and positive amount are required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Add recurring expense %q for %.2f %s?", args.Title, args.Amount, args.Frequency))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Recurring expense creation was not confirmed.", false), nil, nil
		}
		created, err := client.CreateRecurringExpense(ctx, recurringRequest(args), idempotencyKey(principal.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created recurring expense:\n"+string(out), false), nil, nil
	})

	type updateArgs struct {
		ID string `json:"id" jsonschema:"recurring template UUID"`
		recurringArgs
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_recurring_expense",
		Description: "Propose replacing a recurring expense template. Requires MCP form confirmation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Replace recurring expense %q with the proposed values?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Recurring expense update was not confirmed.", false), nil, nil
		}
		updated, err := client.UpdateRecurringExpense(ctx, args.ID, recurringRequest(args.recurringArgs))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated recurring expense:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"recurring template UUID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_recurring_expense",
		Description: "Propose deleting a recurring expense template. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Permanently delete recurring expense %q?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Recurring expense deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteRecurringExpense(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted recurring expense "+args.ID+".", false), nil, nil
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
