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
		}, func(ctx context.Context, _ *mcp.ServerSession, _ *mcp.CallToolParamsFor[struct{}]) (*mcp.CallToolResult, error) {
			goals, err := client.ListGoals(ctx)
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(goals, "", "  ")
			return textResult(string(out), false), nil
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
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[createArgs]) (*mcp.CallToolResult, error) {
		title := strings.TrimSpace(req.Arguments.Title)
		if title == "" {
			return textResult("Goal title is required.", true), nil
		}
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Create the financial goal %q?", title))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Goal creation was not confirmed.", false), nil
		}
		created, err := client.CreateGoal(ctx, title, idempotencyKey(principal.UserID, req.Arguments))
		if err != nil {
			return fail(err), nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created goal:\n"+string(out), false), nil
	})

	type updateArgs struct {
		ID    string `json:"id" jsonschema:"id of the goal to update"`
		Title string `json:"title" jsonschema:"new goal title"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_goal",
		Description: "Propose renaming a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[updateArgs]) (*mcp.CallToolResult, error) {
		title := strings.TrimSpace(req.Arguments.Title)
		if strings.TrimSpace(req.Arguments.ID) == "" || title == "" {
			return textResult("Goal id and title are required.", true), nil
		}
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Rename goal %q to %q?", req.Arguments.ID, title))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Goal update was not confirmed.", false), nil
		}
		updated, err := client.UpdateGoal(ctx, req.Arguments.ID, title)
		if err != nil {
			return fail(err), nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated goal:\n"+string(out), false), nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"id of the goal to delete"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_goal",
		Description: "Propose deleting a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[deleteArgs]) (*mcp.CallToolResult, error) {
		confirmed, err := confirmMutation(ctx, session, fmt.Sprintf("Permanently delete goal %q?", req.Arguments.ID))
		if err != nil {
			return fail(err), nil
		}
		if !confirmed {
			return textResult("Goal deletion was not confirmed.", false), nil
		}
		if err := client.DeleteGoal(ctx, req.Arguments.ID); err != nil {
			return fail(err), nil
		}
		return textResult("Deleted goal "+req.Arguments.ID+".", false), nil
	})
}
