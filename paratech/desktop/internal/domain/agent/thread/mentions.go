package thread

import (
	"slices"

	"paratech/desktop/internal/domain/agent/values"
)

// StepMentions are the mentions a step sends and the provenance check accepts like the scope (ADR-0028, TASK-245):
// the IDs of the current request's mentions (Current), then those of the earlier requests whose pairs Window sends on
// the same entries, newest request first and in mention order within a request, each object once, at most
// values.MaxMentions current ones and values.MaxStepMentions in all. A request that is dropped, covered by the summary,
// before HistoryFrom or cut off by the window start gives none, and neither does the summary text. mentions are the
// IDs the current request mentioned; entries carry those of the earlier requests.
func StepMentions(entries []values.HistoryEntry, current int, summary values.HistorySummary, mentions []values.Ref) []values.StepMention {
	var out []values.StepMention
	has := func(r values.Ref) bool {
		return slices.ContainsFunc(out, func(m values.StepMention) bool { return m.Ref == r })
	}
	for _, r := range mentions {
		if len(out) == values.MaxMentions {
			break
		}
		if !r.IsZero() && !has(r) {
			out = append(out, values.StepMention{Ref: r, Current: true})
		}
	}

	pairs := layout(entries, current, summary).pairs
	for i := len(pairs) - 1; i >= 0; i-- {
		for _, r := range pairs[i].mentions {
			if len(out) == values.MaxStepMentions {
				return out
			}
			if !r.IsZero() && !has(r) {
				out = append(out, values.StepMention{Ref: r})
			}
		}
	}
	return out
}
