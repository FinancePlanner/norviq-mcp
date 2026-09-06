package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// watchlistRow is one entry in a batch write.
//
// Batching is deliberate. Every write is confirmed, and confirming a
// seven-symbol watchlist one row at a time is seven prompts for what the user
// thinks of as one action. One tool call carrying all rows means one
// confirmation that shows the whole set before anything is written.
type watchlistRow struct {
	Symbol string `json:"symbol" jsonschema:"ticker symbol, e.g. AVGO"`
	Status string `json:"status,omitempty" jsonschema:"one of: active, researching, waiting, ready, archived"`
	Note   string `json:"note,omitempty" jsonschema:"free-text note, e.g. a buy zone"`
	ListID string `json:"list_id,omitempty" jsonschema:"optional watchlist list id; defaults to the user's default list"`
}

func (r watchlistRow) describe() string {
	parts := []string{strings.ToUpper(strings.TrimSpace(r.Symbol))}
	if r.Status != "" {
		parts = append(parts, r.Status)
	}
	if r.Note != "" {
		parts = append(parts, fmt.Sprintf("%q", r.Note))
	}
	return strings.Join(parts, "  ")
}

// validateStatus rejects unknown values rather than letting them through. The
// backend coerces an unrecognised status to "active", so a typo would silently
// store something other than what the caller asked for.
func validateStatus(status string) error {
	if status == "" || slices.Contains(api.WatchlistStatuses, status) {
		return nil
	}
	return fmt.Errorf(
		"invalid status %q; expected one of: %s",
		status, strings.Join(api.WatchlistStatuses, ", "),
	)
}

func registerWatchlist(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["watchlist:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_watchlist",
			Description: "List the user's watchlist entries with their status and notes.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			items, err := client.ListWatchlist(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(items, "", "  ")
			return textResult(string(out), false), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_watchlist_lists",
			Description: "List the user's named watchlists.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			lists, err := client.ListWatchlistLists(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(lists, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !p.Scopes["watchlist:write"] {
		return
	}

	type upsertArgs struct {
		Items []watchlistRow `json:"items" jsonschema:"watchlist rows to add or update, in one confirmed batch"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "upsert_watchlist_items",
		Description: "Add or update one or more watchlist entries in a single confirmed batch. " +
			"An existing symbol in the same list is updated in place rather than duplicated.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args upsertArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Items) == 0 {
			return textResult("No watchlist rows were supplied.", true), nil, nil
		}
		for _, row := range args.Items {
			if strings.TrimSpace(row.Symbol) == "" {
				return textResult("Every watchlist row needs a symbol.", true), nil, nil
			}
			if err := validateStatus(row.Status); err != nil {
				return textResult(err.Error(), true), nil, nil
			}
		}

		lines := make([]string, 0, len(args.Items))
		for _, row := range args.Items {
			lines = append(lines, "  "+row.describe())
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Write %d watchlist row(s)?\n%s",
			len(args.Items), strings.Join(lines, "\n"),
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Watchlist changes were not confirmed.", false), nil, nil
		}

		// Partial success is reported rather than swallowed: the caller needs to
		// know exactly which rows landed before deciding what to retry.
		written := make([]api.WatchlistItem, 0, len(args.Items))
		var failures []string
		for _, row := range args.Items {
			body := api.WatchlistItemRequest{Symbol: strings.ToUpper(strings.TrimSpace(row.Symbol))}
			if row.Status != "" {
				body.Status = &row.Status
			}
			if row.Note != "" {
				body.Note = &row.Note
			}
			if row.ListID != "" {
				body.WatchlistListID = &row.ListID
			}
			item, err := client.CreateWatchlistItem(ctx, body, idempotencyKey(p.UserID, body))
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", body.Symbol, err.Error()))
				continue
			}
			written = append(written, *item)
		}

		out, _ := json.MarshalIndent(written, "", "  ")
		text := fmt.Sprintf("Wrote %d of %d watchlist row(s):\n%s", len(written), len(args.Items), string(out))
		if len(failures) > 0 {
			text += "\n\nFailed:\n  " + strings.Join(failures, "\n  ")
		}
		return textResult(text, len(written) == 0), nil, nil
	})

	type updateArgs struct {
		ID     string `json:"id" jsonschema:"watchlist entry id"`
		Status string `json:"status,omitempty" jsonschema:"one of: active, researching, waiting, ready, archived"`
		Note   string `json:"note,omitempty" jsonschema:"replacement note"`
		ListID string `json:"list_id,omitempty" jsonschema:"move the entry to this watchlist list"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_watchlist_item",
		Description: "Update the status, note, or list of a single watchlist entry.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		if err := validateStatus(args.Status); err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Update watchlist entry %s?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Watchlist update was not confirmed.", false), nil, nil
		}
		var body api.WatchlistItemUpdateRequest
		if args.Status != "" {
			body.Status = &args.Status
		}
		if args.Note != "" {
			body.Note = &args.Note
		}
		if args.ListID != "" {
			body.WatchlistListID = &args.ListID
		}
		item, err := client.UpdateWatchlistItem(ctx, args.ID, body)
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(item, "", "  ")
		return textResult("Updated watchlist entry:\n"+string(out), false), nil, nil
	})

	type removeArgs struct {
		IDs []string `json:"ids" jsonschema:"watchlist entry ids to remove, in one confirmed batch"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_watchlist_items",
		Description: "Permanently remove one or more watchlist entries.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args removeArgs) (*mcp.CallToolResult, any, error) {
		if len(args.IDs) == 0 {
			return textResult("No watchlist ids were supplied.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Permanently remove %d watchlist entry(ies)?\n  %s",
			len(args.IDs), strings.Join(args.IDs, "\n  "),
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Watchlist removal was not confirmed.", false), nil, nil
		}
		var removed, failures []string
		for _, id := range args.IDs {
			if err := client.DeleteWatchlistItem(ctx, id); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", id, err.Error()))
				continue
			}
			removed = append(removed, id)
		}
		text := fmt.Sprintf("Removed %d of %d entry(ies).", len(removed), len(args.IDs))
		if len(failures) > 0 {
			text += "\n\nFailed:\n  " + strings.Join(failures, "\n  ")
		}
		return textResult(text, len(removed) == 0), nil, nil
	})

	type createListArgs struct {
		Name string `json:"name" jsonschema:"name for the new watchlist"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_watchlist_list",
		Description: "Create a new named watchlist.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createListArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Create watchlist %q?", args.Name))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Watchlist creation was not confirmed.", false), nil, nil
		}
		list, err := client.CreateWatchlistList(ctx, args.Name, idempotencyKey(p.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(list, "", "  ")
		return textResult("Created watchlist:\n"+string(out), false), nil, nil
	})

	type deleteListArgs struct {
		ID string `json:"id" jsonschema:"watchlist list id"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_watchlist_list",
		Description: "Permanently delete a watchlist and its entries.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteListArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Permanently delete watchlist %s and every entry in it?", args.ID,
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Watchlist deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteWatchlistList(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted watchlist "+args.ID+".", false), nil, nil
	})
}
