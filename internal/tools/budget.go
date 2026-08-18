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

type budgetSnapshotArgs struct {
	MonthStart   string             `json:"month_start" jsonschema:"month start date formatted YYYY-MM-DD"`
	NetSalary    float64            `json:"net_salary" jsonschema:"net monthly income"`
	TargetShares map[string]float64 `json:"target_shares" jsonschema:"pillar target shares as decimals"`
}

type budgetItemArgs struct {
	SnapshotID       string  `json:"snapshot_id" jsonschema:"budget snapshot UUID"`
	Title            string  `json:"title" jsonschema:"budget item title"`
	PlannedAmount    float64 `json:"planned_amount" jsonschema:"non-negative planned amount"`
	Pillar           string  `json:"pillar" jsonschema:"pillar key, such as fundamentals, futureYou, or fun"`
	SplitMode        string  `json:"split_mode" jsonschema:"personal or shared"`
	UserSharePercent float64 `json:"user_share_percent" jsonschema:"user share from 0 to 100"`
}

func registerBudget(s *mcp.Server, client *api.Client, principal *auth.Principal) {
	if principal.Scopes["expenses:read"] {
		type listSnapshotsArgs struct {
			Year  int `json:"year,omitempty" jsonschema:"four-digit year"`
			Month int `json:"month,omitempty" jsonschema:"month number 1 through 12"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_dca_capacity",
			Description: "Convert leftover budget and category overspend into fractional units of the user's chosen DCA holding. Informational only — not an order.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := client.GetDcaCapacity(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			return textResult(prettyJSON(raw), false), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_budget_snapshots",
			Description: "List monthly budget snapshots, optionally filtered by year and month.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args listSnapshotsArgs) (*mcp.CallToolResult, any, error) {
			items, err := client.ListBudgetSnapshots(ctx, args.Year, args.Month)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(items, "", "  ")
			return textResult(string(out), false), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_budget_items",
			Description: "List planned budget items across the user's snapshots.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			items, err := client.ListBudgetItems(ctx)
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
		Name:        "create_budget_snapshot",
		Description: "Propose creating a monthly budget snapshot. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args budgetSnapshotArgs) (*mcp.CallToolResult, any, error) {
		if args.NetSalary < 0 || strings.TrimSpace(args.MonthStart) == "" {
			return textResult("Month and non-negative net income are required.", true), nil, nil
		}
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Create the budget for %s with net income %.2f?", args.MonthStart, args.NetSalary))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget creation was not confirmed.", false), nil, nil
		}
		body := api.BudgetSnapshotRequest{MonthStart: args.MonthStart, NetSalary: args.NetSalary, TargetShares: args.TargetShares}
		created, err := client.CreateBudgetSnapshot(ctx, body, idempotencyKey(principal.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created budget:\n"+string(out), false), nil, nil
	})

	type updateSnapshotArgs struct {
		ID string `json:"id" jsonschema:"budget snapshot UUID"`
		budgetSnapshotArgs
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_budget_snapshot",
		Description: "Propose replacing a monthly budget snapshot. Requires MCP form confirmation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateSnapshotArgs) (*mcp.CallToolResult, any, error) {
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Replace budget snapshot %q with the proposed values?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget update was not confirmed.", false), nil, nil
		}
		body := api.BudgetSnapshotRequest{MonthStart: args.MonthStart, NetSalary: args.NetSalary, TargetShares: args.TargetShares}
		updated, err := client.UpdateBudgetSnapshot(ctx, args.ID, body)
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated budget:\n"+string(out), false), nil, nil
	})

	type idArgs struct {
		ID string `json:"id" jsonschema:"resource UUID"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_budget_snapshot",
		Description: "Propose deleting a monthly budget and its items. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args idArgs) (*mcp.CallToolResult, any, error) {
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Permanently delete budget snapshot %q and its items?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteBudgetSnapshot(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted budget snapshot "+args.ID+".", false), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_budget_item",
		Description: "Propose adding a planned budget item. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args budgetItemArgs) (*mcp.CallToolResult, any, error) {
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Add budget item %q for %.2f?", args.Title, args.PlannedAmount))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget item creation was not confirmed.", false), nil, nil
		}
		body := budgetItemRequest(args)
		created, err := client.CreateBudgetItem(ctx, body, idempotencyKey(principal.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created budget item:\n"+string(out), false), nil, nil
	})

	type updateItemArgs struct {
		ID string `json:"id" jsonschema:"budget item UUID"`
		budgetItemArgs
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_budget_item",
		Description: "Propose replacing a planned budget item. Requires MCP form confirmation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateItemArgs) (*mcp.CallToolResult, any, error) {
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Replace budget item %q with the proposed values?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget item update was not confirmed.", false), nil, nil
		}
		updated, err := client.UpdateBudgetItem(ctx, args.ID, budgetItemRequest(args.budgetItemArgs))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated budget item:\n"+string(out), false), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_budget_item",
		Description: "Propose deleting a planned budget item. Requires MCP form confirmation.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args idArgs) (*mcp.CallToolResult, any, error) {
		confirmed, err := confirmMutation(ctx, req.Session, fmt.Sprintf("Permanently delete budget item %q?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if !confirmed {
			return textResult("Budget item deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteBudgetItem(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted budget item "+args.ID+".", false), nil, nil
	})
}

func budgetItemRequest(args budgetItemArgs) api.BudgetItemRequest {
	return api.BudgetItemRequest{
		SnapshotID:       args.SnapshotID,
		Title:            args.Title,
		PlannedAmount:    args.PlannedAmount,
		Pillar:           args.Pillar,
		SplitMode:        args.SplitMode,
		UserSharePercent: args.UserSharePercent,
	}
}
