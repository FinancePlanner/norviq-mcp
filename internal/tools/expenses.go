// Package tools registers the MCP tool surface, binding each tool to a
// per-session backend client and principal.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/errmap"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func ptrBool(b bool) *bool { return &b }

func textResult(text string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: isError,
	}
}

func fail(err error) *mcp.CallToolResult {
	return textResult(errmap.Friendly(err), true)
}

// Register wires every tool onto the server, closing over the request's client
// and principal. Tools whose required scope is absent are simply not registered,
// so the client never sees tools it cannot use.
func Register(s *mcp.Server, client *api.Client, p *auth.Principal) {
	registerExpenses(s, client, p)
	registerReports(s, client, p)
	registerCSV(s, client, p)
	registerMarket(s, client, p)
	registerTax(s, client, p)
}

func registerExpenses(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["expenses:read"] {
		type listArgs struct {
			From  string `json:"from,omitempty" jsonschema:"start date (YYYY-MM-DD), optional"`
			To    string `json:"to,omitempty" jsonschema:"end date (YYYY-MM-DD), optional"`
			Limit int    `json:"limit,omitempty" jsonschema:"max rows to return, optional"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_expenses",
			Description: "List the user's expenses, optionally filtered by date range.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[listArgs]) (*mcp.CallToolResult, error) {
			items, err := client.ListExpenses(ctx, req.Arguments.From, req.Arguments.To, req.Arguments.Limit)
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(items, "", "  ")
			return textResult(string(out), false), nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_expense_categories",
			Description: "List the user's expense categories.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.ServerSession, _ *mcp.CallToolParamsFor[struct{}]) (*mcp.CallToolResult, error) {
			cats, err := client.ListCategories(ctx)
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(cats, "", "  ")
			return textResult(string(out), false), nil
		})
	}

	if p.Scopes["expenses:write"] {
		type addArgs struct {
			Title      string  `json:"title" jsonschema:"short description of the expense"`
			Amount     float64 `json:"amount" jsonschema:"amount in the account currency"`
			Pillar     string  `json:"pillar" jsonschema:"budget pillar: fundamentals, lifestyle, or future"`
			OccurredOn string  `json:"occurred_on" jsonschema:"date the expense occurred (YYYY-MM-DD)"`
			CategoryID string  `json:"category_id,omitempty" jsonschema:"optional category id"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "add_expense",
			Description: "Add a new expense to the user's account.",
			Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
		}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[addArgs]) (*mcp.CallToolResult, error) {
			a := req.Arguments
			body := api.CreateExpenseRequest{Title: a.Title, Amount: a.Amount, Pillar: a.Pillar, OccurredOn: a.OccurredOn}
			if a.CategoryID != "" {
				body.CategoryID = &a.CategoryID
			}
			created, err := client.CreateExpense(ctx, body, idempotencyKey(p.UserID, a))
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(created, "", "  ")
			return textResult("Added expense:\n"+string(out), false), nil
		})

		type updateArgs struct {
			ID         string  `json:"id" jsonschema:"id of the expense to update"`
			Title      string  `json:"title,omitempty"`
			Amount     float64 `json:"amount,omitempty"`
			Pillar     string  `json:"pillar,omitempty"`
			OccurredOn string  `json:"occurred_on,omitempty"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "update_expense",
			Description: "Update fields of an existing expense.",
		}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[updateArgs]) (*mcp.CallToolResult, error) {
			a := req.Arguments
			patch := map[string]any{}
			if a.Title != "" {
				patch["title"] = a.Title
			}
			if a.Amount != 0 {
				patch["amount"] = a.Amount
			}
			if a.Pillar != "" {
				patch["pillar"] = a.Pillar
			}
			if a.OccurredOn != "" {
				patch["occurredOn"] = a.OccurredOn
			}
			updated, err := client.UpdateExpense(ctx, a.ID, patch)
			if err != nil {
				return fail(err), nil
			}
			out, _ := json.MarshalIndent(updated, "", "  ")
			return textResult("Updated expense:\n"+string(out), false), nil
		})

		type deleteArgs struct {
			ID      string `json:"id" jsonschema:"id of the expense to delete"`
			Confirm bool   `json:"confirm" jsonschema:"must be true to actually delete"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "delete_expense",
			Description: "Delete an expense. Requires confirm=true; ask the user to confirm before calling with confirm=true.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
		}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[deleteArgs]) (*mcp.CallToolResult, error) {
			if !req.Arguments.Confirm {
				return textResult("Deletion not performed: set confirm=true after the user confirms.", false), nil
			}
			if err := client.DeleteExpense(ctx, req.Arguments.ID); err != nil {
				return fail(err), nil
			}
			return textResult("Deleted expense "+req.Arguments.ID+".", false), nil
		})
	}
}

// idempotencyKey derives a stable key from the user and normalized args so a
// retried add_expense does not double-write within the backend's dedup window.
func idempotencyKey(userID string, args any) string {
	encoded, _ := json.Marshal(args)
	sum := sha256.Sum256([]byte(userID + ":" + string(encoded)))
	return "mcp_" + hex.EncodeToString(sum[:16])
}

var _ = fmt.Sprintf
