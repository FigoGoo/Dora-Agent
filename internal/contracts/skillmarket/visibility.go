package skillmarket

import (
	"errors"
	"fmt"
)

type CreatorAnalyticsResponse struct {
	UsageCount         int            `json:"usage_count"`
	RevenueHoldAmount  int            `json:"revenue_hold_amount"`
	RefundCount        int            `json:"refund_count"`
	FailureCodeSummary map[string]int `json:"failure_code_summary"`
	ReviewStatus       string         `json:"review_status"`
	ListingStatus      string         `json:"listing_status"`
	SettlementStatus   string         `json:"settlement_status"`
}

type DataVisibilityFixture struct {
	CreatorAPIResponse map[string]any `json:"creator_api_response"`
	ForbiddenFields    []string       `json:"forbidden_fields"`
}

func ValidateCreatorDataVisibility(fixture DataVisibilityFixture) error {
	if fixture.CreatorAPIResponse == nil {
		return errors.New("creator_api_response is required")
	}
	for _, field := range fixture.ForbiddenFields {
		if _, leaked := fixture.CreatorAPIResponse[field]; leaked {
			return fmt.Errorf("creator API leaked forbidden field %q", field)
		}
	}
	for _, field := range []string{"usage_count", "revenue_hold_amount", "refund_count", "failure_code_summary"} {
		if _, ok := fixture.CreatorAPIResponse[field]; !ok {
			return fmt.Errorf("creator API missing safe field %q", field)
		}
	}
	return nil
}
