package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pilotNotice is appended to every pilot tool description. A model that reads
// only the tool list must still know, before it says anything about a pilot,
// that the trades are simulated, arrive late, and involve no money.
const pilotNotice = " Pilot trades are SIMULATED: Norviq mirrors a politician's or 13F fund's " +
	"publicly disclosed trades into a hypothetical portfolio or a watchlist feed. " +
	"Disclosures arrive with a lag (up to 45 days for congressional trades, up to 135 days " +
	"for 13F filings), simulated trades are priced when Norviq sees the disclosure, and no " +
	"real money is invested and no orders are placed."

// pilotsDisabledMessage is what every pilot tool says when the backend's
// PILOTS_ENABLED flag is off. The backend answers 404 then, and the generic
// errmap text ("not found in your norviq account") would wrongly suggest the
// user's data is missing.
const pilotsDisabledMessage = "Pilot follows are not enabled on Norviq yet, so there are no " +
	"pilots or follows to show. This is a feature switch on Norviq's side, not a problem " +
	"with the user's account."

func isNotFound(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// pilotListFail maps an error from a pilot list route. Those routes look up no
// single item, so a 404 from them can only mean the feature flag is off.
func pilotListFail(err error) *mcp.CallToolResult {
	if isNotFound(err) {
		return textResult(pilotsDisabledMessage, true)
	}
	return fail(err)
}

func registerPilots(s *mcp.Server, client *api.Client, p *auth.Principal) {
	// PilotController guards every route with portfolio:read alone. Unlike
	// get_portfolio_summary, legacy market:read is not accepted there, so it must
	// not expose these tools here: a tool the backend will 403 is worse than no
	// tool. All pilot tools are read-only by scope decision; follows are created,
	// paused and stopped in the app.
	if !p.Scopes["portfolio:read"] {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_pilots",
		Description: "List the politicians and 13F funds the user can follow in Norviq: slug, " +
			"display name, kind (politician or fund), chamber, number of holdings in the " +
			"current book, and when the book last updated. Use a slug with get_pilot." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		pilots, err := client.ListPilots(ctx)
		if err != nil {
			return pilotListFail(err), nil, nil
		}
		out, _ := json.MarshalIndent(pilots, "", "  ")
		return textResult(string(out), false), nil, nil
	})
}
