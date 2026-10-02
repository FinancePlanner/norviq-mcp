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
	"github.com/FinancePlanner/norviq-mcp/internal/errmap"
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

// pilotItemFail maps an error from a single-item pilot route. The backend
// answers 404 both for an unknown pilot or follow and for the feature flag being
// off, and the body does not reliably tell them apart. GET /v1/pilots looks up
// no item, so it 404s only when the flag is off; one probe settles it.
func pilotItemFail(ctx context.Context, client *api.Client, err error, missing string) *mcp.CallToolResult {
	if !isNotFound(err) {
		return fail(err)
	}
	if _, probeErr := client.ListPilots(ctx); isNotFound(probeErr) {
		return textResult(pilotsDisabledMessage, true)
	}
	return textResult(missing, true)
}

// normalizeSlug turns "Nancy Pelosi" or " Nancy-Pelosi " into "nancy-pelosi",
// the form the seed slugs use.
func normalizeSlug(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), "-")
}

// uuidPattern matches a follow id. The backend parses followId as a UUID and
// answers "Follow not found." for anything else. Catching that here lets the
// tool say what was wrong (usually a pilot slug in the wrong tool) without a
// round trip.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// pilotFollowView is get_pilot_follow's answer: the follow and a bounded slice
// of its newest events. events_error is set, rather than failing the whole call,
// when the follow loaded but its events did not.
type pilotFollowView struct {
	Follow       api.PilotFollow        `json:"follow"`
	RecentEvents []api.PilotFollowEvent `json:"recent_events"`
	EventsShown  int                    `json:"events_shown"`
	EventsTotal  int                    `json:"events_total"`
	EventsError  string                 `json:"events_error,omitempty"`
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

	type getPilotArgs struct {
		Slug string `json:"slug" jsonschema:"pilot slug from list_pilots, e.g. nancy-pelosi or berkshire-hathaway"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_pilot",
		Description: "Get one pilot by slug (from list_pilots): the current book weights Norviq " +
			"would mirror, the most recent disclosed trades or filings, how many put trades were " +
			"skipped, and a plain-language note on the reporting lag." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getPilotArgs) (*mcp.CallToolResult, any, error) {
		slug := normalizeSlug(args.Slug)
		if slug == "" {
			return textResult("slug is required, e.g. nancy-pelosi. Call list_pilots to see the available slugs.", true), nil, nil
		}
		detail, err := client.GetPilot(ctx, slug)
		if err != nil {
			missing := fmt.Sprintf("No pilot with slug %q. Call list_pilots to see the available slugs.", slug)
			return pilotItemFail(ctx, client, err, missing), nil, nil
		}
		out, _ := json.MarshalIndent(detail, "", "  ")
		// The backend's lag note leads so it is the first thing the model reads.
		return textResult(detail.LagNote+"\n\n"+string(out), false), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_pilot_follows",
		Description: "List the user's pilot follows, newest first: which pilot, whether it mirrors " +
			"into a hypothetical portfolio or a watchlist feed, status (active or paused), starting " +
			"capital and currency, and the last applied book version. Use an id with " +
			"get_pilot_follow." + pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		follows, err := client.ListPilotFollows(ctx)
		if err != nil {
			return pilotListFail(err), nil, nil
		}
		out, _ := json.MarshalIndent(follows, "", "  ")
		return textResult(string(out), false), nil, nil
	})

	type getFollowArgs struct {
		FollowID   string `json:"follow_id" jsonschema:"follow id (a UUID) from list_pilot_follows; not a pilot slug"`
		EventLimit int    `json:"event_limit,omitempty" jsonschema:"how many of the newest events to include, 1-100; defaults to 20"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_pilot_follow",
		Description: "Get one of the user's pilot follows by id (from list_pilot_follows) with its " +
			"newest events: simulated buys and sells in the hypothetical portfolio, watchlist " +
			"symbols added or marked exited, and trades skipped for missing prices or size limits." +
			pilotNotice,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getFollowArgs) (*mcp.CallToolResult, any, error) {
		id := strings.TrimSpace(args.FollowID)
		if !uuidPattern.MatchString(id) {
			return textResult(fmt.Sprintf(
				"%q is not a follow id. Follow ids are UUIDs; call list_pilot_follows to find one. "+
					"To look up a pilot by slug, use get_pilot instead.", args.FollowID,
			), true), nil, nil
		}
		follow, err := client.GetPilotFollow(ctx, id)
		if err != nil {
			missing := fmt.Sprintf("No pilot follow with id %s on this account. Call list_pilot_follows to see the user's follows.", id)
			return pilotItemFail(ctx, client, err, missing), nil, nil
		}

		view := pilotFollowView{Follow: *follow, RecentEvents: []api.PilotFollowEvent{}}
		events, eventsErr := client.ListPilotFollowEvents(ctx, id)
		if eventsErr != nil {
			view.EventsError = "Recent events could not be loaded: " + errmap.Friendly(eventsErr)
		} else {
			limit := clamp(args.EventLimit, 20, 1, 100)
			view.EventsTotal = len(events)
			if len(events) > limit {
				events = events[:limit]
			}
			view.RecentEvents = events
			view.EventsShown = len(events)
		}
		out, _ := json.MarshalIndent(view, "", "  ")
		return textResult(string(out), false), nil, nil
	})
}
