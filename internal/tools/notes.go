package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTargets(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["targets:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_price_targets",
			Description: "List the user's price targets and alerts.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			targets, err := client.ListTargets(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(targets, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !p.Scopes["targets:write"] {
		return
	}

	type createArgs struct {
		Symbol      string  `json:"symbol" jsonschema:"ticker symbol"`
		Scenario    string  `json:"scenario" jsonschema:"scenario label, e.g. base, bull, bear"`
		TargetPrice float64 `json:"target_price" jsonschema:"target price per share"`
		TargetDate  string  `json:"target_date,omitempty" jsonschema:"date the target is judged against (YYYY-MM-DD), optional"`
		Rationale   string  `json:"rationale,omitempty" jsonschema:"why this target, optional"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_price_target",
		Description: "Create a price target for a symbol.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if args.TargetPrice <= 0 {
			return textResult("target_price must be greater than 0.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Create a %s price target of %g for %s?",
			args.Scenario, args.TargetPrice, strings.ToUpper(args.Symbol),
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Price target was not confirmed.", false), nil, nil
		}
		body := api.TargetRequest{
			Symbol:      strings.ToUpper(strings.TrimSpace(args.Symbol)),
			Scenario:    args.Scenario,
			TargetPrice: args.TargetPrice,
		}
		if args.TargetDate != "" {
			body.TargetDate = &args.TargetDate
		}
		if args.Rationale != "" {
			body.Rationale = &args.Rationale
		}
		created, err := client.CreateTarget(ctx, body, idempotencyKey(p.UserID, body))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created price target:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"price target id"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_price_target",
		Description: "Permanently delete a price target.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, "Permanently delete price target "+args.ID+"?")
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Price target deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteTarget(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted price target "+args.ID+".", false), nil, nil
	})
}

func registerResearch(s *mcp.Server, client *api.Client, p *auth.Principal) {
	if p.Scopes["research:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_research_notes",
			Description: "List the user's research notes.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			notes, err := client.ListResearch(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(notes, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if !p.Scopes["research:write"] {
		return
	}

	type createArgs struct {
		Symbol    string `json:"symbol" jsonschema:"ticker symbol the note is about"`
		Thesis    string `json:"thesis" jsonschema:"the investment thesis"`
		Title     string `json:"title,omitempty" jsonschema:"optional title"`
		Risks     string `json:"risks,omitempty" jsonschema:"optional risks"`
		Catalysts string `json:"catalysts,omitempty" jsonschema:"optional catalysts"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_research_note",
		Description: "Save a research note against a symbol.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Thesis) == "" {
			return textResult("thesis is required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf(
			"Save a research note for %s?", strings.ToUpper(args.Symbol),
		))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Research note was not confirmed.", false), nil, nil
		}
		body := api.ResearchNoteRequest{
			Symbol: strings.ToUpper(strings.TrimSpace(args.Symbol)),
			Thesis: args.Thesis,
		}
		if args.Title != "" {
			body.Title = &args.Title
		}
		if args.Risks != "" {
			body.Risks = &args.Risks
		}
		if args.Catalysts != "" {
			body.Catalysts = &args.Catalysts
		}
		created, err := client.CreateResearch(ctx, body, idempotencyKey(p.UserID, body))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Saved research note:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"research note id"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_research_note",
		Description: "Permanently delete a research note.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, "Permanently delete research note "+args.ID+"?")
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Research note deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteResearch(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted research note "+args.ID+".", false), nil, nil
	})
}
