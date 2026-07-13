package tools

import (
	"context"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTax(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if !p.Scopes["tax:read"] {
		return
	}

	type taxArgs struct {
		Jurisdiction string `json:"jurisdiction,omitempty" jsonschema:"tax jurisdiction: US, PT, ES, DE, FR, or IT; defaults to US"`
		TaxYear      int    `json:"tax_year,omitempty" jsonschema:"four-digit tax year; defaults to the current year"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_tax_dashboard",
		Description: "Read the user's educational tax estimate dashboard, including support levels, assumptions, warnings, and eligible opportunities. Never present estimates as filing advice.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[taxArgs]) (*mcp.CallToolResult, error) {
		raw, err := client.GetTaxDashboard(ctx, req.Arguments.Jurisdiction, req.Arguments.TaxYear)
		if err != nil {
			return fail(err), nil
		}
		return textResult(prettyJSON(raw), false), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_tax_loss_carryforwards",
		Description: "Read the user's estimated tax-loss carryforward ledger for a jurisdiction and year. Values are advisor workpaper estimates, not filing-ready figures.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[taxArgs]) (*mcp.CallToolResult, error) {
		raw, err := client.GetTaxLossCarryforwards(ctx, req.Arguments.Jurisdiction, req.Arguments.TaxYear)
		if err != nil {
			return fail(err), nil
		}
		return textResult(prettyJSON(raw), false), nil
	})
}
