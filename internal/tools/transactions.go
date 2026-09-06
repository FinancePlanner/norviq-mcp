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

// tradeRow is one hand-entered trade in a batch.
//
// These are records of trades already executed at a broker. Nothing here places
// an order or moves money — Norviq's broker integration is read-only by
// construction.
type tradeRow struct {
	Symbol     string  `json:"symbol" jsonschema:"ticker symbol, e.g. AVGO"`
	Type       string  `json:"type" jsonschema:"buy or sell"`
	Quantity   float64 `json:"quantity" jsonschema:"number of shares, greater than 0"`
	Price      float64 `json:"price" jsonschema:"price per share, greater than 0"`
	Currency   string  `json:"currency,omitempty" jsonschema:"ISO currency code; defaults to the instrument's currency"`
	TradeDate  string  `json:"trade_date" jsonschema:"date the trade executed (YYYY-MM-DD)"`
	SettleDate string  `json:"settle_date,omitempty" jsonschema:"settlement date (YYYY-MM-DD), optional"`
	Fees       float64 `json:"fees,omitempty" jsonschema:"commission and fees, optional"`
}

func (t tradeRow) describe() string {
	return fmt.Sprintf(
		"  %s %s %g @ %g on %s",
		strings.ToLower(strings.TrimSpace(t.Type)),
		strings.ToUpper(strings.TrimSpace(t.Symbol)),
		t.Quantity, t.Price, t.TradeDate,
	)
}

func (t tradeRow) validate() error {
	if strings.TrimSpace(t.Symbol) == "" {
		return fmt.Errorf("every trade needs a symbol")
	}
	switch strings.ToLower(strings.TrimSpace(t.Type)) {
	case "buy", "sell":
	default:
		return fmt.Errorf("invalid type %q for %s; expected buy or sell", t.Type, t.Symbol)
	}
	if t.Quantity <= 0 {
		return fmt.Errorf("quantity for %s must be greater than 0", t.Symbol)
	}
	if t.Price <= 0 {
		return fmt.Errorf("price for %s must be greater than 0", t.Symbol)
	}
	if strings.TrimSpace(t.TradeDate) == "" {
		return fmt.Errorf("trade_date is required for %s", t.Symbol)
	}
	return nil
}

func registerTransactions(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["transactions:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_transactions",
			Description: "List the user's recorded trades, both hand-entered and broker-imported.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			rows, err := client.ListTransactions(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(rows, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !p.Scopes["transactions:write"] {
		return
	}

	type recordArgs struct {
		Trades []tradeRow `json:"trades" jsonschema:"trades to record, in one confirmed batch"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "record_trades",
		Description: "Record one or more trades that already executed at a broker, in a single " +
			"confirmed batch. This is record-keeping only: it never places an order or moves money.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args recordArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Trades) == 0 {
			return textResult("No trades were supplied.", true), nil, nil
		}
		for _, trade := range args.Trades {
			if err := trade.validate(); err != nil {
				return textResult(err.Error(), true), nil, nil
			}
		}

		lines := make([]string, 0, len(args.Trades))
		for _, trade := range args.Trades {
			lines = append(lines, trade.describe())
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Record %d trade(s)?\n%s\n\nThis records trades you already made. It does not place any order.",
			len(args.Trades), strings.Join(lines, "\n"),
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Trades were not confirmed.", false), nil, nil
		}

		written := make([]api.Transaction, 0, len(args.Trades))
		var failures []string
		for _, trade := range args.Trades {
			body := api.CreateTransactionRequest{
				Symbol:    strings.ToUpper(strings.TrimSpace(trade.Symbol)),
				Type:      strings.ToLower(strings.TrimSpace(trade.Type)),
				Quantity:  trade.Quantity,
				Price:     trade.Price,
				TradeDate: trade.TradeDate,
			}
			if trade.Currency != "" {
				body.Currency = &trade.Currency
			}
			if trade.SettleDate != "" {
				body.SettleDate = &trade.SettleDate
			}
			if trade.Fees != 0 {
				body.Fees = &trade.Fees
			}
			created, err := client.CreateTransaction(ctx, body, idempotencyKey(p.UserID, body))
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", body.Symbol, err.Error()))
				continue
			}
			written = append(written, *created)
		}

		out, _ := json.MarshalIndent(written, "", "  ")
		text := fmt.Sprintf("Recorded %d of %d trade(s):\n%s", len(written), len(args.Trades), string(out))
		if len(failures) > 0 {
			text += "\n\nFailed:\n  " + strings.Join(failures, "\n  ")
		}
		return textResult(text, len(written) == 0), nil, nil
	})

	type updateArgs struct {
		ID        string  `json:"id" jsonschema:"transaction id"`
		Quantity  float64 `json:"quantity,omitempty" jsonschema:"replacement quantity"`
		Price     float64 `json:"price,omitempty" jsonschema:"replacement price per share"`
		TradeDate string  `json:"trade_date,omitempty" jsonschema:"replacement trade date (YYYY-MM-DD)"`
		Fees      float64 `json:"fees,omitempty" jsonschema:"replacement fees"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_trade",
		Description: "Correct a hand-entered trade. Broker-imported trades are read-only; " +
			"edit those at the broker and re-sync instead.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Update trade %s?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Trade update was not confirmed.", false), nil, nil
		}
		var body api.UpdateTransactionRequest
		if args.Quantity > 0 {
			body.Quantity = &args.Quantity
		}
		if args.Price > 0 {
			body.Price = &args.Price
		}
		if args.TradeDate != "" {
			body.TradeDate = &args.TradeDate
		}
		if args.Fees != 0 {
			body.Fees = &args.Fees
		}
		updated, err := client.UpdateTransaction(ctx, args.ID, body)
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated trade:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"transaction id"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_trade",
		Description: "Permanently delete a hand-entered trade record.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Permanently delete trade record %s? This affects realized P&L and tax reports.", args.ID,
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Trade deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteTransaction(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted trade "+args.ID+".", false), nil, nil
	})
}
