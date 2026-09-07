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

func validateCategory(category string) error {
	if slices.Contains(api.AssetCategories, category) {
		return nil
	}
	return fmt.Errorf(
		"invalid category %q; expected one of: %s",
		category, strings.Join(api.AssetCategories, ", "),
	)
}

func registerHoldings(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["holdings:read"] {
		type listArgs struct {
			PortfolioListID string `json:"portfolio_list_id,omitempty" jsonschema:"only positions in this portfolio list, optional"`
			Limit           int    `json:"limit,omitempty" jsonschema:"max positions to return, optional"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_positions",
			Description: "List the user's stock positions.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
			positions, err := client.ListStocks(ctx, args.PortfolioListID, args.Limit)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(positions, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !p.Scopes["holdings:write"] {
		return
	}

	type addArgs struct {
		Symbol          string  `json:"symbol" jsonschema:"ticker symbol"`
		Shares          float64 `json:"shares" jsonschema:"number of shares, greater than 0"`
		BuyPrice        float64 `json:"buy_price" jsonschema:"price per share paid"`
		BuyDate         string  `json:"buy_date" jsonschema:"date the position was opened (YYYY-MM-DD)"`
		Category        string  `json:"category,omitempty" jsonschema:"one of: stock, etf, mutual_fund, crypto, cash, bond, real_estate, commodity; defaults to stock"`
		Notes           string  `json:"notes,omitempty" jsonschema:"optional note"`
		PortfolioListID string  `json:"portfolio_list_id,omitempty" jsonschema:"portfolio list to add the position to, optional"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "add_position",
		Description: "Record a holding the user already owns. This is record-keeping: " +
			"it does not place a buy order.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
		category := args.Category
		if category == "" {
			category = "stock"
		}
		if err := validateCategory(category); err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		if args.Shares <= 0 || args.BuyPrice <= 0 {
			return textResult("shares and buy_price must both be greater than 0.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Record %g share(s) of %s bought at %g on %s?",
			args.Shares, strings.ToUpper(args.Symbol), args.BuyPrice, args.BuyDate,
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Position was not confirmed.", false), nil, nil
		}
		body := api.StockRequest{
			Symbol:   strings.ToUpper(strings.TrimSpace(args.Symbol)),
			Shares:   args.Shares,
			BuyPrice: args.BuyPrice,
			BuyDate:  args.BuyDate,
			Category: category,
		}
		if args.Notes != "" {
			body.Notes = &args.Notes
		}
		if args.PortfolioListID != "" {
			body.PortfolioListID = &args.PortfolioListID
		}
		created, err := client.CreateStock(ctx, body, idempotencyKey(p.UserID, body))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Recorded position:\n"+string(out), false), nil, nil
	})

	type sellArgs struct {
		ID           string  `json:"id" jsonschema:"position id to sell from"`
		SharesToSell float64 `json:"shares_to_sell" jsonschema:"number of shares sold, greater than 0"`
		SellPrice    float64 `json:"sell_price" jsonschema:"price per share received"`
		SellDate     string  `json:"sell_date" jsonschema:"date the sale executed (YYYY-MM-DD)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "sell_position",
		Description: "Record a sale against an existing position. Reduces the position, credits " +
			"cash, and records the trade. Selling the full quantity removes the position record. " +
			"This is record-keeping: it does not place a sell order.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sellArgs) (*mcp.CallToolResult, any, error) {
		if args.SharesToSell <= 0 || args.SellPrice <= 0 {
			return textResult("shares_to_sell and sell_price must both be greater than 0.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Record a sale of %g share(s) at %g on %s against position %s?\n"+
				"Selling the whole position removes its record. This does not place an order.",
			args.SharesToSell, args.SellPrice, args.SellDate, args.ID,
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Sale was not confirmed.", false), nil, nil
		}
		body := api.SellStockRequest{
			SharesToSell: args.SharesToSell,
			SellPrice:    args.SellPrice,
			SellDate:     args.SellDate,
		}
		sold, err := client.SellStock(ctx, args.ID, body, idempotencyKey(p.UserID, struct {
			ID string
			api.SellStockRequest
		}{args.ID, body}))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(sold, "", "  ")
		return textResult("Recorded sale:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"position id"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_position",
		Description: "Permanently delete a position record. Use sell_position for an actual " +
			"disposal; this erases the record as if it never existed.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Permanently delete position %s? This erases the record rather than recording a sale.",
			args.ID,
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Position deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteStock(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted position "+args.ID+".", false), nil, nil
	})
}
