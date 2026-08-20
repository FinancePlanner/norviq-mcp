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

func registerPlanning(s *mcp.Server, client *api.Client, principal *auth.Principal) {
	if principal.Scopes["expenses:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_goals",
			Description: "List the user's manual financial goals and their current status.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			goals, err := client.ListGoals(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(goals, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !principal.Scopes["expenses:write"] {
		return
	}

	type createArgs struct {
		Title string `json:"title" jsonschema:"short title for the financial goal"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_goal",
		Description: "Propose adding a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		title := strings.TrimSpace(args.Title)
		if title == "" {
			return textResult("Goal title is required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Create the financial goal %q?", title))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal creation was not confirmed.", false), nil, nil
		}
		created, err := client.CreateGoal(ctx, title, idempotencyKey(principal.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created goal:\n"+string(out), false), nil, nil
	})

	type updateArgs struct {
		ID    string `json:"id" jsonschema:"id of the goal to update"`
		Title string `json:"title" jsonschema:"new goal title"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_goal",
		Description: "Propose renaming a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		title := strings.TrimSpace(args.Title)
		if strings.TrimSpace(args.ID) == "" || title == "" {
			return textResult("Goal id and title are required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Rename goal %q to %q?", args.ID, title))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal update was not confirmed.", false), nil, nil
		}
		updated, err := client.UpdateGoal(ctx, args.ID, title)
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated goal:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"id of the goal to delete"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_goal",
		Description: "Propose deleting a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Permanently delete goal %q?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteGoal(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted goal "+args.ID+".", false), nil, nil
	})
}
