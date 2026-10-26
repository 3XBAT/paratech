package agent

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// opReplyLines is the log name of a reply shown without its list of assignments.
const opReplyLines = "agent: reply list not read"

// replyLines adds to a reply the assignments it lists as the local data has them (ADR-0022, TASK-173): the title, the
// library subject and the deadline. The planner gave only IDs that passed the provenance check; it never sees or
// writes a title, and nothing read here goes back to it. An assignment that is gone (deleted since) is skipped; when
// the assignments cannot be read the reply is shown without the list. No title or text reaches the log.
func (t tools) replyLines(ctx context.Context, card values.ReplyCard) values.ReplyCard {
	if len(card.Assignments) == 0 {
		return card
	}
	views, err := t.assignments.ListAssignments(ctx)
	if err != nil {
		t.log.WarnContext(ctx, opReplyLines, "err", err)
		return card
	}
	listed := values.NewRefSet(card.Assignments...)
	byID := make(map[shared.AssignmentID]int, len(card.Assignments))
	var names []string
	for i, v := range views {
		r, ok := refOfID(values.KindAssignment, string(v.ID))
		if !ok || !listed.Has(r) {
			continue
		}
		byID[v.ID] = i
		if v.Subject != "" && !slices.Contains(names, v.Subject) {
			names = append(names, v.Subject)
		}
	}
	// The subject of an assignment is a free string; only the library subject it names is shown.
	subjects, err := t.library.ResolveSubjects(ctx, names)
	if err != nil {
		t.log.WarnContext(ctx, opReplyLines, "what", "subjects", "err", err)
		subjects = nil
	}

	loc := t.clock.Now().Location()
	lines := make([]values.AssignmentLine, 0, len(byID))
	for _, r := range card.Assignments {
		i, ok := byID[shared.AssignmentID(r.ID())]
		// A line ReplyCard.Validate would refuse is skipped, so one bad title does not drop the whole list.
		if !ok || strings.TrimSpace(views[i].Title) == "" || !utf8.ValidString(views[i].Title) {
			continue
		}
		v := views[i]
		line := values.AssignmentLine{Assignment: r, Title: v.Title}
		if !v.Due.IsZero() {
			line.Due = dateOf(v.Due, loc)
		}
		if m, ok := subjects[v.Subject]; ok && v.Subject != "" {
			line.Subject, _ = refOfID(values.KindSubject, string(m.ID))
		}
		lines = append(lines, line)
	}
	card.Lines = lines
	return card
}
