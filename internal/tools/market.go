package tools

import (
	"context"
	"encoding/json"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerMarket adds read-only market and insights tools when the token holds
// the corresponding scopes.
func registerMarket(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["market:read"] {
		type quoteArgs struct {
			Symbol string `json:"symbol" jsonschema:"ticker symbol, e.g. AAPL"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_quote",
			Description: "Get the latest market quote for a stock symbol.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args quoteArgs) (*mcp.CallToolResult, any, error) {
			raw, err := client.GetQuote(ctx, args.Symbol)
			if err != nil {
				return fail(err), nil, nil
			}
			return textResult(prettyJSON(raw), false), nil, nil
		})

		type searchArgs struct {
			Query string `json:"query" jsonschema:"company name or ticker to search for"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "search_symbols",
			Description: "Search for stocks, ETFs, and companies by name or ticker.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
			raw, err := client.SearchSymbols(ctx, args.Query)
			if err != nil {
				return fail(err), nil, nil
			}
			return textResult(prettyJSON(raw), false), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_portfolio_summary",
			Description: "Get the user's portfolio summary: value, weights, and holdings.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := client.GetPortfolioSummary(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			return textResult(prettyJSON(raw), false), nil, nil
		})
	}

	if p.Scopes["insights:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_insights",
			Description: "Get the latest market sentiment insights summary.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := client.GetInsightsSummary(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			// Insights contain third-party (social) text. Wrap as untrusted so the
			// model treats any embedded instructions as data, not commands.
			body := "<untrusted_data source=\"market_insights\">\n" + prettyJSON(raw) + "\n</untrusted_data>"
			return textResult(body, false), nil, nil
		})
	}
}

func prettyJSON(raw json.RawMessage) string {
	var buf json.RawMessage = raw
	out, err := json.MarshalIndent(&buf, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
