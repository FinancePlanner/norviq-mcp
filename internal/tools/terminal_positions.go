package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// terminalDisclaimer is the spec's disclaimer copy, verbatim. Every terminal
// tool answer and confirmation carries it.
const terminalDisclaimer = "Terminal prices are your assumptions, not forecasts. Not financial advice."

// terminalNotice ends every terminal tool description. A model that reads only
// the tool list must still know that this is planning math, where the
// assumptions come from, and that Norviq, not the model, does the arithmetic.
const terminalNotice = " Terminal position sizing is planning math, not advice: Norviq never recommends buying or selling. " +
	"Terminal assumptions (terminal market cap, terminal share count, value wanted) must come from the user or from sources you cite to the user; never invent or estimate them. " +
	"Norviq computes every derived value: terminalSharePrice = terminalMarketCap / terminalShareCount; " +
	"sharesNeeded = valueWanted × terminalShareCount / terminalMarketCap; and from those capitalAtTodayPrice, progress, " +
	"sharesStillNeeded and gapValueAtTerminal. Report the values Norviq returns. Never compute, round or invent them yourself. " +
	"When scenarioError is set the derived values are null: say the scenario is invalid and why, and do not fill them in. " +
	terminalDisclaimer

// tickerPattern is the backend's ticker rule, applied after trimming and
// uppercasing.
var tickerPattern = regexp.MustCompile(`^[A-Z0-9.\-]{1,12}$`)

func normalizeTicker(raw string) (string, error) {
	ticker := strings.ToUpper(strings.TrimSpace(raw))
	if !tickerPattern.MatchString(ticker) {
		return "", fmt.Errorf("%q is not a valid ticker: use the exchange ticker, 1 to 12 letters, digits, dots or hyphens, such as AMZN or BRK.B", raw)
	}
	return ticker, nil
}

// rowsForTicker keeps only the rows for ticker. The backend already filters on
// ?ticker=, but a write picks its target from this list, so a backend that
// ignored the filter must not lead MCP to change another ticker's row.
func rowsForTicker(rows []api.TerminalPosition, ticker string) []api.TerminalPosition {
	var out []api.TerminalPosition
	for _, row := range rows {
		if strings.EqualFold(row.Ticker, ticker) {
			out = append(out, row)
		}
	}
	return out
}

// firstBySortOrder is the contract's "first row for the ticker by sortOrder".
// The backend returns rows sorted, but the choice decides which row a write
// changes, so it does not rely on that.
func firstBySortOrder(rows []api.TerminalPosition) (api.TerminalPosition, bool) {
	if len(rows) == 0 {
		return api.TerminalPosition{}, false
	}
	first := rows[0]
	for _, row := range rows[1:] {
		if row.SortOrder < first.SortOrder {
			first = row
		}
	}
	return first, true
}

// terminalFail maps a backend error from a terminal route. A 422 carries the
// backend's reason. errmap's generic "try again shortly" would invite the model
// to retry values Norviq has already rejected.
func terminalFail(err error) *mcp.CallToolResult {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
		reason := apiErr.Reason()
		if reason == "" {
			reason = "the values were not accepted"
		}
		return textResult("Norviq rejected this: "+strings.TrimSuffix(reason, ".")+
			". Check the values with the user before trying again; do not retry them unchanged.", true)
	}
	return fail(err)
}

// shareFactsUpgradeMessage answers the backend's Pro gate: a 403 whose body
// code is "upgrade_required". The table itself is free, so the message says the
// user can still type the numbers in.
const shareFactsUpgradeMessage = "lookup_share_facts needs Norviq Pro: the AI share-facts lookup is a Pro feature. " +
	"Terminal position sizing itself is free, so the user can still enter shares outstanding and today's price themselves. " +
	"Upgrade at norviq.org."

// shareFactsFail maps the AI lookup's documented failures. It is not errmap's
// job: errmap says "Pro, or a missing permission" for every 403, and calls a
// 503 a transient fault worth retrying.
func shareFactsFail(err error, ticker string) *mcp.CallToolResult {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusForbidden && apiErr.Code() == "upgrade_required":
			return textResult(shareFactsUpgradeMessage, true)
		case apiErr.Status == http.StatusServiceUnavailable:
			return textResult("Norviq's AI lookup is unavailable right now, so there are no sourced numbers for "+ticker+
				". Ask the user for shares outstanding and today's price, or cite a source they can check. Do not guess them.", true)
		case apiErr.Status == http.StatusUnprocessableEntity:
			return textResult("Norviq's AI lookup could not find usable, sourced numbers for "+ticker+
				". Ask the user for shares outstanding and today's price, or cite a source they can check. Do not guess them.", true)
		}
	}
	return terminalFail(err)
}

type shareFactsView struct {
	Suggestion api.ShareFactsSuggestion `json:"suggestion"`
	// Saved is always false: the lookup only suggests.
	Saved bool   `json:"saved"`
	Note  string `json:"note"`
}

func registerShareFacts(s *mcp.Server, client *api.Client) {
	type tickerArgs struct {
		Ticker string `json:"ticker" jsonschema:"stock ticker, e.g. AMZN or BRK.B"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "lookup_share_facts",
		Description: "Norviq Pro. Ask Norviq's AI web lookup for a ticker's shares outstanding and share price today, with the sources " +
			"it used and the as-of date. It returns a suggestion and saves nothing: show the user the numbers and the sources, " +
			"and write them with set_terminal_scenario (sharesOutstanding, currentSharePrice) only if the user agrees. " +
			"It never suggests a terminal market cap, terminal share count or value wanted; those stay the user's assumptions." +
			terminalNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tickerArgs) (*mcp.CallToolResult, any, error) {
		ticker, err := normalizeTicker(args.Ticker)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		facts, err := client.LookupShareFacts(ctx, ticker)
		if err != nil {
			return shareFactsFail(err, ticker), nil, nil
		}
		body, _ := json.MarshalIndent(shareFactsView{
			Suggestion: *facts,
			Saved:      false,
			Note: "Suggestion only; nothing was saved. Show the user these numbers with their sources before writing anything. " +
				terminalDisclaimer,
		}, "", "  ")
		return textResult(string(body), false), nil, nil
	})
}

type terminalPositionsView struct {
	Currency   string                 `json:"currency"`
	Positions  []api.TerminalPosition `json:"positions"`
	Disclaimer string                 `json:"disclaimer"`
}

type terminalPositionView struct {
	Currency string               `json:"currency"`
	Position api.TerminalPosition `json:"position"`
	// ScenariosForTicker counts the user's rows for this ticker. A duplicated row
	// is a scenario variant, and set_terminal_scenario changes only the first.
	ScenariosForTicker int    `json:"scenariosForTicker"`
	Disclaimer         string `json:"disclaimer"`
}

func registerTerminalPositions(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if !p.Scopes["planning:read"] {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_terminal_positions",
		Description: "List the user's terminal position scenarios in their sort order, with the account currency. " +
			"Each row has the user's inputs (ticker, terminal share count, terminal market cap, value wanted, shares owned, " +
			"and optionally shares outstanding and today's share price) and the values Norviq derived from them " +
			"(terminalSharePrice, sharesNeeded, capitalAtTodayPrice, progress, sharesStillNeeded, gapValueAtTerminal)." +
			terminalNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		list, err := client.ListTerminalPositions(ctx, "")
		if err != nil {
			return terminalFail(err), nil, nil
		}
		positions := list.Positions
		if positions == nil {
			positions = []api.TerminalPosition{}
		}
		body, _ := json.MarshalIndent(terminalPositionsView{
			Currency: list.Currency, Positions: positions, Disclaimer: terminalDisclaimer,
		}, "", "  ")
		return textResult(string(body), false), nil, nil
	})

	type tickerArgs struct {
		Ticker string `json:"ticker" jsonschema:"stock ticker, e.g. AMZN or BRK.B"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_terminal_position",
		Description: "Get the user's terminal scenario for one ticker: the first row for that ticker in the user's sort order, " +
			"with the values Norviq derived, and how many scenario rows the ticker has (a duplicated row is a scenario variant). " +
			"Says so plainly when the ticker has no row." +
			terminalNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tickerArgs) (*mcp.CallToolResult, any, error) {
		ticker, err := normalizeTicker(args.Ticker)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		list, err := client.ListTerminalPositions(ctx, ticker)
		if err != nil {
			return terminalFail(err), nil, nil
		}
		rows := rowsForTicker(list.Positions, ticker)
		first, ok := firstBySortOrder(rows)
		if !ok {
			return textResult(fmt.Sprintf(
				"No terminal scenario for %s yet. To add one, ask the user for the terminal market cap, terminal share count and value wanted, then call set_terminal_scenario. %s",
				ticker, terminalDisclaimer,
			), false), nil, nil
		}
		body, _ := json.MarshalIndent(terminalPositionView{
			Currency: list.Currency, Position: first, ScenariosForTicker: len(rows), Disclaimer: terminalDisclaimer,
		}, "", "  ")
		return textResult(string(body), false), nil, nil
	})

	registerShareFacts(s, client)
}
