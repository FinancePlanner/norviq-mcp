package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// marketBriefDisabledMessage is the answer while the backend's market brief
// flag is off. It is not an error: nothing is wrong with the user's account.
const marketBriefDisabledMessage = "Norviq's market brief is not enabled yet, so there is no brief to show. " +
	"This is a feature switch on Norviq's side, not a problem with the user's account."

// marketBriefSchedule tells the model when briefs appear, so an empty answer
// can be explained without guessing.
const marketBriefSchedule = "Norviq writes the morning pre-market brief at 08:15 and the evening " +
	"market-close recap at 22:30, Europe/Lisbon time, on weekdays."

const marketBriefDescription = "Get Norviq's shared AI market brief. Two are written per weekday: the morning " +
	"pre-market brief (08:15 Europe/Lisbon) and the evening market-close recap (22:30 Europe/Lisbon), " +
	"each in English (en) and European Portuguese (pt-PT). The brief is the same for every Norviq user: " +
	"it is not personalised to the user's holdings and it is not financial advice. It has a greeting, " +
	"index groups (European open and US futures in the morning, European and US close in the evening) " +
	"with each index's level and percentage change, and items (highlights, earnings and stories) with " +
	"their tickers and source. With no slot and date it returns the latest brief; pass slot and date together for a " +
	"specific one. `previous` names the brief before this one (slot and tradingDate): pass those back as " +
	"slot and date to step back. Use get_quote for live prices and get_news for headlines, not this."

// normalizeBriefLanguage maps the tool's language argument to the backend's
// lang values. "pt" is accepted because models drop the region.
func normalizeBriefLanguage(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "en":
		return "en", nil
	case "pt", "pt-pt":
		return "pt-PT", nil
	default:
		return "", fmt.Errorf("%q is not a supported brief language: use en (English) or pt-PT (European Portuguese)", raw)
	}
}

// validateBriefSelection checks slot and date before any backend call. The
// backend requires a date with a slot and silently ignores a date without
// one, which would return the latest brief while the model believes it asked
// for a specific day.
func validateBriefSelection(rawSlot, rawDate string) (string, string, error) {
	slot := strings.ToLower(strings.TrimSpace(rawSlot))
	date := strings.TrimSpace(rawDate)
	switch slot {
	case "", "morning", "evening":
	default:
		return "", "", fmt.Errorf("%q is not a brief slot: use morning (pre-market brief) or evening (market-close recap), or leave slot and date empty for the latest brief", rawSlot)
	}
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return "", "", fmt.Errorf("%q is not a valid date: use yyyy-MM-dd, such as 2026-10-15", rawDate)
		}
	}
	switch {
	case slot != "" && date == "":
		return "", "", fmt.Errorf("date is required with slot: pass the trading date as yyyy-MM-dd, or leave slot and date empty for the latest brief")
	case slot == "" && date != "":
		return "", "", fmt.Errorf("slot is required with date: pass morning or evening, or leave slot and date empty for the latest brief")
	}
	return slot, date, nil
}

// marketBriefFail maps a backend error. A 404 for a slot and date means that
// brief does not exist (weekends, holidays, before generation), which is an
// answer, not a failure. A 404 for the latest brief can only mean the route is
// not served.
func marketBriefFail(err error, slot, date, lang string) *mcp.CallToolResult {
	if isNotFound(err) {
		if slot != "" {
			return textResult(fmt.Sprintf(
				"There is no %s brief for %s in %s. Briefs exist for weekdays only, and each one appears after it is written. %s "+
					"Call get_market_brief without slot and date for the latest brief.",
				slot, date, lang, marketBriefSchedule,
			), false)
		}
		return textResult(marketBriefDisabledMessage, false)
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
		reason := strings.TrimSuffix(apiErr.Reason(), ".")
		if reason == "" {
			reason = "the request was malformed"
		}
		return textResult("Norviq rejected the brief request: "+reason+".", true)
	}
	return fail(err)
}

func registerMarketBrief(s *mcp.Server, client *api.Client) {
	type briefArgs struct {
		Language string `json:"language,omitempty" jsonschema:"en (English) or pt-PT (European Portuguese, pt also accepted); defaults to en"`
		Slot     string `json:"slot,omitempty" jsonschema:"morning (pre-market brief) or evening (market-close recap); requires date; omit both for the latest brief"`
		Date     string `json:"date,omitempty" jsonschema:"trading date as yyyy-MM-dd; required with slot and only valid with slot"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_market_brief",
		Description: marketBriefDescription,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args briefArgs) (*mcp.CallToolResult, any, error) {
		lang, err := normalizeBriefLanguage(args.Language)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		slot, date, err := validateBriefSelection(args.Slot, args.Date)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		brief, err := client.GetMarketBrief(ctx, lang, slot, date)
		if err != nil {
			return marketBriefFail(err, slot, date, lang), nil, nil
		}
		if !brief.Enabled {
			return textResult(marketBriefDisabledMessage, false), nil, nil
		}
		if brief.TradingDate == nil || brief.Slot == nil {
			return textResult("No market brief has been generated yet. "+marketBriefSchedule, false), nil, nil
		}
		return textResult(renderMarketBrief(brief, lang), false), nil, nil
	})
}

func briefSlotLabel(slot string) string {
	switch slot {
	case "morning":
		return "morning pre-market brief"
	case "evening":
		return "evening market-close recap"
	default:
		return fmt.Sprintf("%q brief", slot)
	}
}

// signedChange puts the sign back on the backend's unsigned changePercent.
func signedChange(row api.MarketBriefRow) string {
	change := strings.TrimSpace(row.ChangePercent)
	if change == "" || strings.HasPrefix(change, "+") || strings.HasPrefix(change, "-") {
		return change
	}
	switch row.Direction {
	case "up":
		return "+" + change
	case "down":
		return "-" + change
	default:
		return change
	}
}

// sourceHost reduces a source link to its host, which is what a reader needs
// to judge the source. The full URL stays in the structured payload.
func sourceHost(raw *string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(*raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// renderMarketBrief writes the brief as compact text followed by the full
// payload. Everything the model wrote (greeting, item text) and every source
// link is third-party derived, so it sits inside <untrusted_data>; the header,
// degraded note and previous pointer come from Norviq's own fields.
func renderMarketBrief(b *api.MarketBrief, requestedLang string) string {
	lang := b.Language
	if lang == "" {
		lang = requestedLang
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Norviq market brief: %s for %s (%s)", briefSlotLabel(*b.Slot), *b.TradingDate, lang)
	if b.GeneratedAt != nil && *b.GeneratedAt != "" {
		fmt.Fprintf(&out, ", generated %s", *b.GeneratedAt)
	}
	out.WriteString(".\nThis brief is shared: every Norviq user sees the same one. It is an AI-written summary of " +
		"market data and third-party news, not personalised to the user and not financial advice.\n")
	if b.Degraded {
		out.WriteString("Note: this brief is degraded. Some of its inputs were unavailable when it was written, " +
			"so sections may be missing or thinner than usual.\n")
	}
	if b.Previous != nil && b.Previous.Slot != "" && b.Previous.TradingDate != "" {
		fmt.Fprintf(&out, "Previous brief: %s for %s. To read it, call get_market_brief with slot=%q and date=%q",
			briefSlotLabel(b.Previous.Slot), b.Previous.TradingDate, b.Previous.Slot, b.Previous.TradingDate)
		if lang != "en" {
			fmt.Fprintf(&out, " and language=%q", lang)
		}
		out.WriteString(".\n")
	}

	out.WriteString("\n<untrusted_data source=\"market_brief\">\n")
	if b.Greeting != nil && strings.TrimSpace(*b.Greeting) != "" {
		out.WriteString(strings.TrimSpace(*b.Greeting) + "\n\n")
	}
	for _, group := range b.Groups {
		out.WriteString(group.Title)
		if group.Tone != "" {
			fmt.Fprintf(&out, " (%s)", group.Tone)
		}
		out.WriteString("\n")
		for _, row := range group.Rows {
			out.WriteString("- ")
			if row.Flag != "" {
				out.WriteString(row.Flag + " ")
			}
			out.WriteString(row.Name)
			if row.Symbol != "" && row.Symbol != row.Name {
				fmt.Fprintf(&out, " (%s)", row.Symbol)
			}
			fmt.Fprintf(&out, ": %s", row.Level)
			if change := signedChange(row); change != "" {
				fmt.Fprintf(&out, ", %s%%", strings.TrimSuffix(change, "%"))
			}
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	if len(b.Items) > 0 {
		out.WriteString("Items\n")
		for _, item := range b.Items {
			fmt.Fprintf(&out, "- [%s] %s", item.Kind, strings.TrimSpace(item.Text))
			var extra []string
			if len(item.Tickers) > 0 {
				extra = append(extra, "tickers: "+strings.Join(item.Tickers, ", "))
			}
			if host := sourceHost(item.SourceURL); host != "" {
				extra = append(extra, "source: "+host)
			}
			if len(extra) > 0 {
				fmt.Fprintf(&out, " (%s)", strings.Join(extra, "; "))
			}
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	payload, _ := json.MarshalIndent(b, "", "  ")
	out.WriteString("Structured payload:\n")
	out.Write(payload)
	out.WriteString("\n</untrusted_data>")
	return out.String()
}
