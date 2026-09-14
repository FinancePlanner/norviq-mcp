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

func registerPlanning(s *mcp.Server, client *api.Client, principal *auth.Principal) {
	if principal.Scopes["expenses:read"] {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_goals",
			Description: "List the user's manual financial goals and their current status.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			goals, err := client.ListGoals(ctx)
			if err != nil {
				return fail(err), nil, nil
			}
			out, _ := json.MarshalIndent(goals, "", "  ")
			return textResult(string(out), false), nil, nil
		})
	}

	if principal.Scopes["planning:read"] {
		registerPlanningEngine(s, client)
	}

	if !principal.Scopes["expenses:write"] {
		return
	}

	type createArgs struct {
		Title string `json:"title" jsonschema:"short title for the financial goal"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_goal",
		Description: "Propose adding a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		title := strings.TrimSpace(args.Title)
		if title == "" {
			return textResult("Goal title is required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Create the financial goal %q?", title))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal creation was not confirmed.", false), nil, nil
		}
		created, err := client.CreateGoal(ctx, title, idempotencyKey(principal.UserID, args))
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(created, "", "  ")
		return textResult("Created goal:\n"+string(out), false), nil, nil
	})

	type updateArgs struct {
		ID    string `json:"id" jsonschema:"id of the goal to update"`
		Title string `json:"title" jsonschema:"new goal title"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_goal",
		Description: "Propose renaming a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		title := strings.TrimSpace(args.Title)
		if strings.TrimSpace(args.ID) == "" || title == "" {
			return textResult("Goal id and title are required.", true), nil, nil
		}
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Rename goal %q to %q?", args.ID, title))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal update was not confirmed.", false), nil, nil
		}
		updated, err := client.UpdateGoal(ctx, args.ID, title)
		if err != nil {
			return fail(err), nil, nil
		}
		out, _ := json.MarshalIndent(updated, "", "  ")
		return textResult("Updated goal:\n"+string(out), false), nil, nil
	})

	type deleteArgs struct {
		ID string `json:"id" jsonschema:"id of the goal to delete"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_goal",
		Description: "Propose deleting a financial goal. The client must show an MCP confirmation form before Norviq writes anything.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptrBool(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		confirmed, pending, err := confirmMutation(req, fmt.Sprintf("Permanently delete goal %q?", args.ID))
		if err != nil {
			return fail(err), nil, nil
		}
		if pending != nil {
			return pending, nil, nil
		}
		if !confirmed {
			return textResult("Goal deletion was not confirmed.", false), nil, nil
		}
		if err := client.DeleteGoal(ctx, args.ID); err != nil {
			return fail(err), nil, nil
		}
		return textResult("Deleted goal "+args.ID+".", false), nil, nil
	})
}

// registerPlanningEngine exposes the two planning calculators. Both are
// read-only: they answer a question, they do not change the user's plan.
//
// The defaults matter here more than on a form. An assistant that has to invent
// a return rate will invent a different one each time, so the zero value of
// every optional field is filled in with the same figure the screens use.
func registerPlanningEngine(s *mcp.Server, client *api.Client) {
	type growthArgs struct {
		InitialAmount             float64 `json:"initialAmount,omitempty" jsonschema:"amount already invested today"`
		MonthlyContribution       float64 `json:"monthlyContribution,omitempty" jsonschema:"amount added every month"`
		Years                     int     `json:"years" jsonschema:"how many years to project, 1 to 100"`
		AnnualReturnPercent       float64 `json:"annualReturnPercent,omitempty" jsonschema:"expected annual return as a percentage, for example 7 for 7 percent; defaults to 7"`
		AnnualInflationPercent    float64 `json:"annualInflationPercent,omitempty" jsonschema:"annual inflation as a percentage; defaults to 2"`
		ContributionGrowthPercent float64 `json:"contributionGrowthPercent,omitempty" jsonschema:"yearly increase in the monthly contribution as a percentage; defaults to 0"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "project_investment_growth",
		Description: "Project what an investment plan becomes: a starting amount plus a monthly contribution, compounded over a number of years. Returns the ending value in nominal and inflation-adjusted terms, what was contributed versus what was growth, and how the answer changes at other return rates. Projections, not advice.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args growthArgs) (*mcp.CallToolResult, any, error) {
		if args.Years <= 0 || args.Years > 100 {
			return fail(fmt.Errorf("years must be between 1 and 100")), nil, nil
		}
		out, err := client.ProjectGrowth(ctx, api.GrowthProjectionRequest{
			Assumptions: api.ProjectionAssumptions{
				InitialAmount:                args.InitialAmount,
				MonthlyContribution:          args.MonthlyContribution,
				AnnualReturnRate:             percentOrDefault(args.AnnualReturnPercent, 7),
				AnnualInflationRate:          percentOrDefault(args.AnnualInflationPercent, 2),
				AnnualContributionGrowthRate: args.ContributionGrowthPercent / 100,
				Years:                        args.Years,
			},
		})
		if err != nil {
			return fail(err), nil, nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return textResult(string(body), false), nil, nil
	})

	type retirementArgs struct {
		CurrentAge             int     `json:"currentAge" jsonschema:"the user's age today"`
		RetirementAge          int     `json:"retirementAge" jsonschema:"the age they want to stop working"`
		LongevityAge           int     `json:"longevityAge,omitempty" jsonschema:"the age the plan should last to; defaults to 90"`
		MonthlyCostOfLife      float64 `json:"monthlyCostOfLife,omitempty" jsonschema:"what a month costs today including housing; omit to use the budget Norviq already has"`
		MonthlyHousing         float64 `json:"monthlyHousing,omitempty" jsonschema:"the housing part of that monthly cost, not an addition to it"`
		HousingEndsAtAge       int     `json:"housingEndsAtAge,omitempty" jsonschema:"age at which housing stops costing anything, for example when a mortgage is paid off"`
		MonthlyOtherIncome     float64 `json:"monthlyOtherIncome,omitempty" jsonschema:"monthly retirement income the portfolio does not have to cover, such as a state pension"`
		InvestedToday          float64 `json:"investedToday,omitempty" jsonschema:"amount already invested; omit to use the user's actual portfolio value"`
		MonthlyContribution    float64 `json:"monthlyContribution,omitempty" jsonschema:"amount added to investments every month"`
		AnnualReturnPercent    float64 `json:"annualReturnPercent,omitempty" jsonschema:"expected annual return as a percentage; defaults to 7"`
		AnnualInflationPercent float64 `json:"annualInflationPercent,omitempty" jsonschema:"annual inflation as a percentage; defaults to 2"`
		WithdrawalRatePercent  float64 `json:"withdrawalRatePercent,omitempty" jsonschema:"safe withdrawal rate as a percentage; defaults to 4"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "check_retirement_readiness",
		Description: "Work out what the user needs to retire, compare it to what their plan will actually have, and report the exact move that closes any gap - saving more each month, retiring later, or spending less. Falls back to the user's real budget and portfolio for any figure not given. Projections, not advice.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args retirementArgs) (*mcp.CallToolResult, any, error) {
		if args.CurrentAge < 18 || args.RetirementAge <= args.CurrentAge {
			return fail(fmt.Errorf("retirementAge must be greater than currentAge, and currentAge at least 18")), nil, nil
		}

		costOfLife := args.MonthlyCostOfLife
		invested := args.InvestedToday
		housing := args.MonthlyHousing

		// Falling back to what Norviq already knows is the whole point of asking
		// through Norviq rather than using any calculator on the internet.
		if costOfLife <= 0 || invested <= 0 {
			if prefill, err := client.PlanningPrefill(ctx); err == nil {
				if costOfLife <= 0 && prefill.MonthlyCostOfLife != nil {
					costOfLife = *prefill.MonthlyCostOfLife
				}
				if housing <= 0 && prefill.MonthlyHousing != nil {
					housing = *prefill.MonthlyHousing
				}
				if invested <= 0 {
					invested = prefill.PortfolioValue
				}
			}
		}
		if costOfLife <= 0 {
			return fail(fmt.Errorf("no cost of life given, and no budget to read one from - ask the user what a month costs them")), nil, nil
		}
		if housing > costOfLife {
			housing = costOfLife
		}

		longevity := args.LongevityAge
		if longevity <= args.RetirementAge {
			longevity = 90
		}
		if longevity <= args.RetirementAge {
			longevity = args.RetirementAge + 1
		}

		var housingEnds *int
		if args.HousingEndsAtAge > 0 {
			age := args.HousingEndsAtAge
			housingEnds = &age
		}

		returnRate := percentOrDefault(args.AnnualReturnPercent, 7)
		inflation := percentOrDefault(args.AnnualInflationPercent, 2)

		out, err := client.PlanRetirement(ctx, api.RetirementPlanningRequest{
			Need: api.RetirementNeedInput{
				CurrentAge:                     args.CurrentAge,
				RetirementAge:                  args.RetirementAge,
				LongevityAge:                   longevity,
				MonthlyCostOfLifeToday:         costOfLife,
				MonthlyHousingToday:            housing,
				HousingEndsAtAge:               housingEnds,
				MonthlyOtherIncomeAtRetirement: args.MonthlyOtherIncome,
				AnnualInflationRate:            inflation,
				WithdrawalRate:                 percentOrDefault(args.WithdrawalRatePercent, 4),
				ExpectedAnnualReturn:           returnRate,
			},
			Plan: api.ProjectionAssumptions{
				InitialAmount:       invested,
				MonthlyContribution: args.MonthlyContribution,
				AnnualReturnRate:    returnRate,
				AnnualInflationRate: inflation,
				Years:               args.RetirementAge - args.CurrentAge,
			},
		})
		if err != nil {
			return fail(err), nil, nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return textResult(string(body), false), nil, nil
	})
}

// percentOrDefault converts a percentage as a person says it (7) into the
// fraction the API wants (0.07), falling back when the caller omitted it.
func percentOrDefault(value, fallback float64) float64 {
	if value == 0 {
		return fallback / 100
	}
	return value / 100
}
