package tools

import (
	"context"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerCSV adds bulk CSV import/export tools for expenses.
func registerCSV(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["expenses:write"] {
		type importArgs struct {
			CSVContent string `json:"csv_content" jsonschema:"CSV text with header: title,amount,currency,pillar,category,occurred_on[,external_id]"`
			DryRun     *bool  `json:"dry_run,omitempty" jsonschema:"validate without writing; defaults to true"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "import_expenses_csv",
			Description: "Import expenses from CSV. ALWAYS run once with dry_run=true first, show the user the per-row results, then re-call with dry_run=false only after they confirm.",
		}, func(ctx context.Context, session *mcp.ServerSession, req *mcp.CallToolParamsFor[importArgs]) (*mcp.CallToolResult, error) {
			dryRun := true
			if req.Arguments.DryRun != nil {
				dryRun = *req.Arguments.DryRun
			}
			if !dryRun {
				confirmed, err := confirmMutation(ctx, session, "Import the validated CSV rows into your expense history?")
				if err != nil {
					return fail(err), nil
				}
				if !confirmed {
					return textResult("CSV import was not confirmed.", false), nil
				}
			}
			raw, err := client.ImportExpensesCSV(ctx, req.Arguments.CSVContent, dryRun)
			if err != nil {
				return fail(err), nil
			}
			prefix := "Dry run (nothing written):\n"
			if !dryRun {
				prefix = "Import applied:\n"
			}
			return textResult(prefix+prettyJSON(raw), false), nil
		})
	}

	if p.Scopes["expenses:read"] {
		type exportArgs struct {
			From string `json:"from,omitempty" jsonschema:"start date YYYY-MM-DD, optional"`
			To   string `json:"to,omitempty" jsonschema:"end date YYYY-MM-DD, optional"`
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "export_expenses_csv",
			Description: "Export the user's expenses as CSV text for the given date range.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[exportArgs]) (*mcp.CallToolResult, error) {
			csv, err := client.ExportExpensesCSV(ctx, req.Arguments.From, req.Arguments.To)
			if err != nil {
				return fail(err), nil
			}
			const cap = 1 << 20
			if len(csv) > cap {
				return textResult("The export is larger than 1 MB. Please narrow the date range or use the web export at norviqa.io.", false), nil
			}
			return textResult(csv, false), nil
		})
	}
}
