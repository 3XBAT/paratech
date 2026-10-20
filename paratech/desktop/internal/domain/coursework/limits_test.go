package coursework

import (
	"testing"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/lectureparts"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
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
		{"summary sections", summary.MaxSections, prompts.MaxSummarySections, true},
		{"summary section heading", summary.MaxHeadingRunes, prompts.MaxSectionHeadingRunes, true},
		{"summary section text", summary.MaxSectionTextRunes, prompts.MaxSectionTextRunes, true},
		{"summary section points", summary.MaxSectionPoints, prompts.MaxSectionPoints, true},
		{"summary section point", summary.MaxPointRunes, prompts.MaxSectionPointRunes, true},
		{"assignment title", assignment.MaxTitleRunes, prompts.MaxTitleRunes, true},
		{"assignment description", assignment.MaxDescriptionRunes, prompts.MaxDescriptionRunes, false},
		{"draft body in 4-byte runes", proposal.MaxBodyBytes, 4 * prompts.MaxDraftBodyRunes, false},
		{"homework draft title", homeworkdraft.MaxTitleRunes, prompts.MaxTitleRunes, true},
		{"homework draft body", homeworkdraft.MaxBodyRunes, prompts.MaxDraftBodyRunes, true},
		{"lecture text heading", lecturetext.MaxHeadingRunes, prompts.MaxLectureHeadingRunes, true},
		{"lecture text paragraph", lecturetext.MaxParagraphRunes, prompts.MaxLectureParagraphRunes, true},
		{"lecture text paragraphs of a block", lecturetext.MaxParagraphs, prompts.MaxLectureParagraphs, false},
		{"lecture text blocks of a run", lecturetext.MaxBlocks, lecturetext.MaxParts * prompts.MaxLectureBlocks, false},
		{"lecture text parts", lecturetext.MaxParts, lectureparts.MaxParts, true},
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

// A stored draft version and the student's instructions must fit the LLM context of the next generation and an export
// proposal, so that a refinement is never refused for a draft the app itself accepted.
func TestDraftLimitsFitTheContext(t *testing.T) {
	cases := []struct {
		name     string
		value    int
		maxBytes int
	}{
		{"draft body in 4-byte runes, as the previous draft", 4 * homeworkdraft.MaxBodyRunes, contextselection.MaxPreviousDraftBytes},
		{"draft body in 4-byte runes, as an export proposal", 4 * homeworkdraft.MaxBodyRunes, proposal.MaxBodyBytes},
		{"instruction, as the student request", values.MaxInstructionBytes, contextselection.MaxStudentRequestBytes},
	}
	for _, tc := range cases {
		if tc.value > tc.maxBytes {
			t.Errorf("%s: %d bytes, the limit is %d", tc.name, tc.value, tc.maxBytes)
		}
	}
}
