package tools

import (
	"context"
	"encoding/json"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerReports(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if !p.Scopes["reports:read"] {
		return
	}
	type reportArgs struct {
		Kind string `json:"kind" jsonschema:"one of: overview, monthly, yearly, suggestions"`
		From string `json:"from,omitempty" jsonschema:"start date (YYYY-MM-DD), optional"`
		To   string `json:"to,omitempty" jsonschema:"end date (YYYY-MM-DD), optional"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_spending_report",
		Description: "Get a spending report. kind selects the view: overview, monthly, yearly, or suggestions.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.ServerSession, req *mcp.CallToolParamsFor[reportArgs]) (*mcp.CallToolResult, error) {
		kind := req.Arguments.Kind
		if kind == "" {
			kind = "overview"
		}
		raw, err := client.GetReport(ctx, kind, req.Arguments.From, req.Arguments.To)
		if err != nil {
			return fail(err), nil
		}
		var pretty json.RawMessage = raw
		out, _ := json.MarshalIndent(&pretty, "", "  ")
		return textResult(string(out), false), nil
	})
}
