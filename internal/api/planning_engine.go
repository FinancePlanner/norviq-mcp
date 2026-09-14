package api

import (
	"context"
	"net/http"
)

// Mirrors the backend's /v1/planning contract. Only the read side is exposed
// over MCP: the two calculators answer questions, they do not change anything.

type ProjectionAssumptions struct {
	InitialAmount                float64 `json:"initialAmount"`
	MonthlyContribution          float64 `json:"monthlyContribution"`
	AnnualReturnRate             float64 `json:"annualReturnRate"`
	AnnualContributionGrowthRate float64 `json:"annualContributionGrowthRate"`
	AnnualInflationRate          float64 `json:"annualInflationRate"`
	Years                        int     `json:"years"`
}

type ProjectionResult struct {
	EndingValueNominal float64 `json:"endingValueNominal"`
	EndingValueReal    float64 `json:"endingValueReal"`
	TotalContributed   float64 `json:"totalContributed"`
	TotalGrowth        float64 `json:"totalGrowth"`
}

type SensitivityPoint struct {
	AnnualReturnRate   float64 `json:"annualReturnRate"`
	EndingValueNominal float64 `json:"endingValueNominal"`
}

type GrowthProjectionRequest struct {
	Assumptions ProjectionAssumptions `json:"assumptions"`
}

type GrowthProjectionResponse struct {
	Result          ProjectionResult   `json:"result"`
	Sensitivity     []SensitivityPoint `json:"sensitivity"`
	AssumptionNotes []string           `json:"assumptionNotes"`
}

type RetirementNeedInput struct {
	CurrentAge                     int     `json:"currentAge"`
	RetirementAge                  int     `json:"retirementAge"`
	LongevityAge                   int     `json:"longevityAge"`
	MonthlyCostOfLifeToday         float64 `json:"monthlyCostOfLifeToday"`
	MonthlyHousingToday            float64 `json:"monthlyHousingToday"`
	HousingEndsAtAge               *int    `json:"housingEndsAtAge,omitempty"`
	MonthlyOtherIncomeAtRetirement float64 `json:"monthlyOtherIncomeAtRetirement"`
	AnnualInflationRate            float64 `json:"annualInflationRate"`
	WithdrawalRate                 float64 `json:"withdrawalRate"`
	ExpectedAnnualReturn           float64 `json:"expectedAnnualReturn"`
}

type RetirementNeed struct {
	AnnualSpendingAtRetirement float64 `json:"annualSpendingAtRetirement"`
	NestEggAtWithdrawalRate    float64 `json:"nestEggAtWithdrawalRate"`
	RunwayYears                int     `json:"runwayYears"`
	EndingBalance              float64 `json:"endingBalance"`
	ShortfallAge               *int    `json:"shortfallAge,omitempty"`
}

type PlanLever struct {
	Gap                           float64  `json:"gap"`
	AdditionalMonthlyContribution *float64 `json:"additionalMonthlyContribution,omitempty"`
	DelayYears                    *int     `json:"delayYears,omitempty"`
	SpendingReductionMonthly      *float64 `json:"spendingReductionMonthly,omitempty"`
}

type RetirementPlanningRequest struct {
	Need RetirementNeedInput   `json:"need"`
	Plan ProjectionAssumptions `json:"plan"`
}

type RetirementPlanningResponse struct {
	Need                           RetirementNeed `json:"need"`
	Lever                          PlanLever      `json:"lever"`
	ProjectedPortfolioAtRetirement float64        `json:"projectedPortfolioAtRetirement"`
	ReadinessProbability           *float64       `json:"readinessProbability,omitempty"`
	AssumptionNotes                []string       `json:"assumptionNotes"`
}

type PlanningPrefill struct {
	Currency               string             `json:"currency"`
	PortfolioValue         float64            `json:"portfolioValue"`
	MonthlyCostOfLife      *float64           `json:"monthlyCostOfLife,omitempty"`
	MonthlyByPillar        map[string]float64 `json:"monthlyByPillar"`
	MonthlyHousing         *float64           `json:"monthlyHousing,omitempty"`
	SuggestedRetirementAge *int               `json:"suggestedRetirementAge,omitempty"`
	HasBudget              bool               `json:"hasBudget"`
	HasPortfolio           bool               `json:"hasPortfolio"`
}

func (c *Client) PlanningPrefill(ctx context.Context) (*PlanningPrefill, error) {
	var out PlanningPrefill
	if err := c.do(ctx, http.MethodGet, "/v1/planning/prefill", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ProjectGrowth(ctx context.Context, in GrowthProjectionRequest) (*GrowthProjectionResponse, error) {
	var out GrowthProjectionResponse
	if err := c.do(ctx, http.MethodPost, "/v1/planning/projection", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PlanRetirement(ctx context.Context, in RetirementPlanningRequest) (*RetirementPlanningResponse, error) {
	var out RetirementPlanningResponse
	if err := c.do(ctx, http.MethodPost, "/v1/planning/retirement", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
