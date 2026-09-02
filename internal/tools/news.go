package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type newsEntry struct {
	Kind        string `json:"kind"`
	Symbol      string `json:"symbol,omitempty"`
	Title       string `json:"title"`
	Source      string `json:"source,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	URL         string `json:"url,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

func registerNews(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if !p.Scopes["market:read"] {
		return
	}

	type newsArgs struct {
		Query        string   `json:"query,omitempty" jsonschema:"company name or ticker to resolve into symbols"`
		Symbol       string   `json:"symbol,omitempty" jsonschema:"single ticker symbol to fetch news for"`
		Symbols      []string `json:"symbols,omitempty" jsonschema:"multiple ticker symbols to fetch news for"`
		Source       string   `json:"source,omitempty" jsonschema:"tracked, market, or general"`
		LookbackDays int      `json:"lookback_days,omitempty" jsonschema:"lookback window in days"`
		MaxResults   int      `json:"max_results,omitempty" jsonschema:"max number of items to return"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_news",
		Description: "Get recent finance news through Norviq. source=tracked (default with no query) returns the user's own feed for held and watched symbols; source=market (default when a query, symbol, or symbols is given) returns headlines per symbol from Norviq's archive; source=general returns broad market headlines. Read-only, bounded by lookback_days and max_results. Use get_quote for prices and get_insights for sentiment, not this.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args newsArgs) (*mcp.CallToolResult, any, error) {
		maxResults := clamp(args.MaxResults, 10, 1, 50)
		lookbackDays := args.LookbackDays
		if lookbackDays <= 0 {
			lookbackDays = 7
		}
		source := strings.ToLower(strings.TrimSpace(args.Source))
		if source == "" {
			if args.Query != "" || args.Symbol != "" || len(args.Symbols) > 0 {
				source = "market"
			} else {
				source = "tracked"
			}
		}

		payload := map[string]any{
			"source":        source,
			"query":         strings.TrimSpace(args.Query),
			"symbol":        strings.ToUpper(strings.TrimSpace(args.Symbol)),
			"lookback_days": lookbackDays,
			"max_results":   maxResults,
		}

		entries := make([]newsEntry, 0, maxResults)
		resolvedSymbols := make([]string, 0, 4)

		switch source {
		case "tracked":
			items, err := client.GetNewsFeed(ctx, maxResults)
			if err != nil {
				return fail(err), nil, nil
			}
			for _, item := range items {
				entries = append(entries, normalizeTrackedNews(item))
			}
		case "market":
			symbols := uniqueSymbols(append(args.Symbols, args.Symbol))
			if len(symbols) == 0 && strings.TrimSpace(args.Query) != "" {
				found, err := client.SearchSymbolsList(ctx, args.Query)
				if err != nil {
					return fail(err), nil, nil
				}
				symbols = prioritizeSearchResults(found, args.Query, 3)
			}
			if len(symbols) == 0 {
				items, err := client.GetNewsFeed(ctx, maxResults)
				if err != nil {
					return fail(err), nil, nil
				}
				payload["source"] = "tracked"
				for _, item := range items {
					entries = append(entries, normalizeTrackedNews(item))
				}
				break
			}
			resolvedSymbols = symbols
			for _, symbol := range symbols {
				items, err := client.GetMarketNews(ctx, symbol, maxResults)
				if err != nil {
					return fail(err), nil, nil
				}
				for _, item := range items {
					entries = append(entries, normalizeMarketNews(symbol, item))
				}
			}
		case "general":
			from, to := lookbackRange(lookbackDays)
			items, err := client.GetGeneralMarketNews(ctx, from, to, 1, maxResults)
			if err != nil {
				return fail(err), nil, nil
			}
			for _, item := range items {
				entries = append(entries, normalizeGeneralNews(item))
			}
		default:
			return fail(fmt.Errorf("unknown news source %q", args.Source)), nil, nil
		}

		entries = filterByLookback(entries, lookbackDays)
		sort.SliceStable(entries, func(i, j int) bool {
			ti, okI := parseNewsTime(entries[i].PublishedAt)
			tj, okJ := parseNewsTime(entries[j].PublishedAt)
			if okI && okJ {
				if !ti.Equal(tj) {
					return ti.After(tj)
				}
			} else if okI != okJ {
				return okI
			}
			if entries[i].Title != entries[j].Title {
				return entries[i].Title < entries[j].Title
			}
			return entries[i].Symbol < entries[j].Symbol
		})
		if len(entries) > maxResults {
			entries = entries[:maxResults]
		}

		payload["resolved_symbols"] = resolvedSymbols
		payload["items"] = entries

		raw, _ := json.Marshal(payload)
		body := "<untrusted_data source=\"news\">\n" + prettyJSON(raw) + "\n</untrusted_data>"
		return textResult(body, false), nil, nil
	})
}

func normalizeTrackedNews(item api.TrackedNewsItem) newsEntry {
	source := strings.TrimSpace(item.Source)
	if source == "" {
		source = "norviq_feed"
	}
	return newsEntry{
		Kind:        "tracked",
		Symbol:      strings.ToUpper(strings.TrimSpace(item.Symbol)),
		Title:       strings.TrimSpace(item.Headline),
		Source:      source,
		PublishedAt: strings.TrimSpace(item.PublishedAt),
		URL:         strings.TrimSpace(item.URL),
		Summary:     strings.TrimSpace(item.Summary),
	}
}

func normalizeMarketNews(symbol string, item api.MarketNewsItem) newsEntry {
	return newsEntry{
		Kind:        "market",
		Symbol:      strings.ToUpper(strings.TrimSpace(symbol)),
		Title:       strings.TrimSpace(item.Title),
		Source:      sourceOr(item.Source, "market_archive"),
		PublishedAt: strings.TrimSpace(item.Date),
		URL:         strings.TrimSpace(item.URL),
		Summary:     strings.TrimSpace(item.Summary),
	}
}

func normalizeGeneralNews(item api.MarketNewsItem) newsEntry {
	return newsEntry{
		Kind:        "general",
		Symbol:      strings.ToUpper(strings.TrimSpace(item.Symbol)),
		Title:       strings.TrimSpace(item.Title),
		Source:      sourceOr(item.Source, "market_news"),
		PublishedAt: strings.TrimSpace(item.Date),
		URL:         strings.TrimSpace(item.URL),
		Summary:     strings.TrimSpace(item.Summary),
	}
}

func sourceOr(value, fallback string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return fallback
}

func uniqueSymbols(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		sym := strings.ToUpper(strings.TrimSpace(value))
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	return out
}

func prioritizeSearchResults(results []api.SearchResult, query string, limit int) []string {
	q := strings.ToUpper(strings.TrimSpace(query))
	exact := make([]string, 0, 1)
	others := make([]string, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		sym := strings.ToUpper(strings.TrimSpace(result.Symbol))
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		if sym == q {
			exact = append(exact, sym)
			continue
		}
		others = append(others, sym)
	}
	out := append(exact, others...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func lookbackRange(days int) (string, string) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days).Format("2006-01-02")
	to := now.Format("2006-01-02")
	return from, to
}

func filterByLookback(entries []newsEntry, days int) []newsEntry {
	if days <= 0 {
		return entries
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	out := make([]newsEntry, 0, len(entries))
	for _, entry := range entries {
		if t, ok := parseNewsTime(entry.PublishedAt); ok && t.Before(cutoff) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func parseNewsTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if unix > 1e12 {
			unix /= 1000
		}
		return time.Unix(unix, 0).UTC(), true
	}
	return time.Time{}, false
}

func clamp(v, def, min, max int) int {
	if v == 0 {
		v = def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
