package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setTerminalScenarioArgs is the contract's set_terminal_scenario input, field
// for field. Every number is a pointer so "not given" and 0 differ:
// sharesOwned 0 is a real answer (the user owns none, or sold out).
type setTerminalScenarioArgs struct {
	Ticker             string   `json:"ticker" jsonschema:"exchange ticker, e.g. AMZN or BRK.B, not the company name; ask the user if unsure"`
	TerminalShareCount *float64 `json:"terminalShareCount,omitempty" jsonschema:"the user's assumed share count at the terminal date, as a plain number (11 billion is 11000000000)"`
	TerminalMarketCap  *float64 `json:"terminalMarketCap,omitempty" jsonschema:"the user's assumed market cap at the terminal date in the account currency, as a plain number (10 trillion is 10000000000000)"`
	ValueWanted        *float64 `json:"valueWanted,omitempty" jsonschema:"what the user wants the position to be worth at the terminal date, in the account currency"`
	SharesOwned        *float64 `json:"sharesOwned,omitempty" jsonschema:"shares the user owns today; 0 is valid"`
	SharesOutstanding  *float64 `json:"sharesOutstanding,omitempty" jsonschema:"the company's shares outstanding today, for reference"`
	CurrentSharePrice  *float64 `json:"currentSharePrice,omitempty" jsonschema:"today's share price in the account currency; Norviq uses it for capitalAtTodayPrice"`
}

// terminalField is one numeric input, in the order confirmations list them.
type terminalField struct {
	name     string // the contract's argument name
	label    string // what the confirmation calls it
	value    *float64
	money    bool // shown with the account currency
	positive bool // must be > 0; otherwise must be >= 0
	current  func(api.TerminalPosition) *float64
}

func (a setTerminalScenarioArgs) fields() []terminalField {
	return []terminalField{
		{
			name: "terminalShareCount", label: "terminal share count", value: a.TerminalShareCount, positive: true,
			current: func(r api.TerminalPosition) *float64 { return &r.TerminalShareCount },
		},
		{
			name: "terminalMarketCap", label: "terminal market cap", value: a.TerminalMarketCap, money: true, positive: true,
			current: func(r api.TerminalPosition) *float64 { return &r.TerminalMarketCap },
		},
		{
			name: "valueWanted", label: "value wanted", value: a.ValueWanted, money: true,
			current: func(r api.TerminalPosition) *float64 { return &r.ValueWanted },
		},
		{
			name: "sharesOwned", label: "shares owned", value: a.SharesOwned,
			current: func(r api.TerminalPosition) *float64 { return &r.SharesOwned },
		},
		{
			name: "sharesOutstanding", label: "shares outstanding today", value: a.SharesOutstanding, positive: true,
			current: func(r api.TerminalPosition) *float64 { return r.SharesOutstanding },
		},
		{
			name: "currentSharePrice", label: "today's share price", value: a.CurrentSharePrice, money: true, positive: true,
			current: func(r api.TerminalPosition) *float64 { return r.CurrentSharePrice },
		},
	}
}

// validate rejects input before anything is read or asked. The backend stores
// a share count or market cap of 0 so a half-typed web cell still saves, but an
// assistant writing 0 is always a mistake, so MCP refuses it.
func (a setTerminalScenarioArgs) validate() error {
	given := 0
	for _, f := range a.fields() {
		if f.value == nil {
			continue
		}
		given++
		if f.positive && *f.value <= 0 {
			return fmt.Errorf("%s must be greater than 0", f.name)
		}
		if *f.value < 0 {
			return fmt.Errorf("%s must not be negative", f.name)
		}
	}
	if given == 0 {
		return errors.New("nothing to set: give at least one of terminalShareCount, terminalMarketCap, valueWanted, sharesOwned, sharesOutstanding or currentSharePrice")
	}
	return nil
}

func (a setTerminalScenarioArgs) missingForCreate() []string {
	var missing []string
	if a.TerminalShareCount == nil {
		missing = append(missing, "terminalShareCount")
	}
	if a.TerminalMarketCap == nil {
		missing = append(missing, "terminalMarketCap")
	}
	if a.ValueWanted == nil {
		missing = append(missing, "valueWanted")
	}
	return missing
}

// show prints the exact value being written: no rounding, no 10T shorthand.
func (f terminalField) show(v float64, currency string) string {
	text := strconv.FormatFloat(v, 'f', -1, 64)
	if f.money && currency != "" {
		text += " " + currency
	}
	return text
}

const terminalRecalcNote = "Norviq recalculates the terminal share price and shares needed from these. " + terminalDisclaimer

func describeTerminalUpdate(ticker string, row api.TerminalPosition, rowCount int, currency string, args setTerminalScenarioArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Update the terminal scenario for %s", ticker)
	if rowCount > 1 {
		fmt.Fprintf(&b, " (the first of %d %s rows)", rowCount, ticker)
	}
	b.WriteString(":")
	for _, f := range args.fields() {
		if f.value == nil {
			continue
		}
		from := "empty"
		if current := f.current(row); current != nil {
			from = f.show(*current, currency)
		}
		fmt.Fprintf(&b, "\n- %s: %s → %s", f.label, from, f.show(*f.value, currency))
	}
	b.WriteString("\n" + terminalRecalcNote)
	return b.String()
}

func describeTerminalCreate(ticker, currency string, args setTerminalScenarioArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Create a terminal scenario for %s:", ticker)
	for _, f := range args.fields() {
		if f.value == nil {
			continue
		}
		fmt.Fprintf(&b, "\n- %s: %s", f.label, f.show(*f.value, currency))
	}
	b.WriteString("\n" + terminalRecalcNote)
	return b.String()
}

type terminalWriteView struct {
	Action     string               `json:"action"` // "updated" or "created"
	Currency   string               `json:"currency"`
	Position   api.TerminalPosition `json:"position"`
	Disclaimer string               `json:"disclaimer"`
}

func registerSetTerminalScenario(s *mcp.Server, client *api.Client, p *auth.Principal) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_terminal_scenario",
		Description: "Set the user's terminal scenario for one ticker. If the ticker already has a row, this updates the first one " +
			"in the user's sort order and changes only the fields given; otherwise it creates a row, which needs terminalShareCount, " +
			"terminalMarketCap and valueWanted. Every call shows the user the exact values in an MCP confirmation form, and Norviq " +
			"writes nothing unless they confirm. Give numbers as plain amounts in the account currency or plain share counts: " +
			"10 trillion is 10000000000000, not 10T. The answer includes the values Norviq derived." +
			terminalNotice,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args setTerminalScenarioArgs) (*mcp.CallToolResult, any, error) {
		ticker, err := normalizeTicker(args.Ticker)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		if invalid := args.validate(); invalid != nil {
			return textResult(invalid.Error(), true), nil, nil
		}

		list, err := client.ListTerminalPositions(ctx, ticker)
		if err != nil {
			return terminalFail(err), nil, nil
		}
		rows := rowsForTicker(list.Positions, ticker)
		row, exists := firstBySortOrder(rows)

		var message string
		if exists {
			message = describeTerminalUpdate(ticker, row, len(rows), list.Currency, args)
		} else {
			if missing := args.missingForCreate(); len(missing) > 0 {
				return textResult(fmt.Sprintf(
					"%s has no terminal scenario yet, so this would create one, and creating needs %s. Ask the user for them; do not invent them.",
					ticker, strings.Join(missing, ", "),
				), true), nil, nil
			}
			message = describeTerminalCreate(ticker, list.Currency, args)
		}

		confirmed, pending, err := confirmMutation(req, message)
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("The terminal scenario change was not confirmed. Nothing was written.", false), nil, nil
		}

		var written *api.TerminalPosition
		action := "updated"
		if exists {
			written, err = client.UpdateTerminalPosition(ctx, row.ID, api.TerminalPositionUpdateRequest{
				SharesOutstanding:  args.SharesOutstanding,
				TerminalShareCount: args.TerminalShareCount,
				TerminalMarketCap:  args.TerminalMarketCap,
				ValueWanted:        args.ValueWanted,
				SharesOwned:        args.SharesOwned,
				CurrentSharePrice:  args.CurrentSharePrice,
			})
		} else {
			action = "created"
			keyArgs := args
			keyArgs.Ticker = ticker
			written, err = client.CreateTerminalPosition(ctx, api.TerminalPositionCreateRequest{
				Ticker:             ticker,
				SharesOutstanding:  args.SharesOutstanding,
				TerminalShareCount: *args.TerminalShareCount,
				TerminalMarketCap:  *args.TerminalMarketCap,
				ValueWanted:        *args.ValueWanted,
				SharesOwned:        args.SharesOwned,
				CurrentSharePrice:  args.CurrentSharePrice,
			}, terminalCreateKey(p.UserID, keyArgs, time.Now()))
		}
		if err != nil {
			return terminalFail(err), nil, nil
		}
		body, _ := json.MarshalIndent(terminalWriteView{
			Action: action, Currency: list.Currency, Position: *written, Disclaimer: terminalDisclaimer,
		}, "", "  ")
		return textResult(string(body), false), nil, nil
	})
}

// terminalCreateKey buckets the key by 5 minutes: the backend replays a cached
// 2xx for 24h, so a deliberate re-create after a delete must not be replayed.
func terminalCreateKey(userID string, args setTerminalScenarioArgs, now time.Time) string {
	return idempotencyKey(userID, struct {
		Args   setTerminalScenarioArgs
		Bucket int64
	}{args, now.Unix() / 300})
}
