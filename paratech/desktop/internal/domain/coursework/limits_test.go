package coursework

import (
	"testing"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
)

// The aggregates must hold every answer the strict parsers accept, so their limits cannot drift from the prompts.
func TestAggregateLimitsMatchThePrompts(t *testing.T) {
	cases := []struct {
		name      string
		aggregate int
		prompts   int
		exact     bool // false: the aggregate may allow more (the user writes more than the model)
	}{
		{"summary text", summary.MaxTextRunes, prompts.MaxSummaryRunes, true},
		{"summary key points", summary.MaxKeyPoints, prompts.MaxKeyPoints, true},
		{"summary key point", summary.MaxKeyPointRunes, prompts.MaxKeyPointRunes, true},
		{"assignment title", assignment.MaxTitleRunes, prompts.MaxTitleRunes, true},
		{"assignment description", assignment.MaxDescriptionRunes, prompts.MaxDescriptionRunes, false},
		{"draft body in 4-byte runes", proposal.MaxBodyBytes, 4 * prompts.MaxDraftBodyRunes, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.exact && tc.aggregate != tc.prompts {
				t.Fatalf("aggregate limit %d, prompts limit %d: they must be equal", tc.aggregate, tc.prompts)
			}
			if tc.aggregate < tc.prompts {
				t.Fatalf("aggregate limit %d is below the prompts limit %d", tc.aggregate, tc.prompts)
			}
		})
	}
}
