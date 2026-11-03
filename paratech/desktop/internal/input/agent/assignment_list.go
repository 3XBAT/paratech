package agent

import (
	"context"
	"slices"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/values"
)

// AssignmentStates lists the assignments by «Открытые / Выполненные / Все» (cases/homeworkflow, ADR-0033 §9). The
// Assignments of the Driver may implement it (homeworkflow.Service does); without it list_assignments of catalog v10
// lists the open ones only.
type AssignmentStates interface {
	ListAssignmentsByState(ctx context.Context, state homeworkflow.ListState) ([]homeworkflow.AssignmentView, error)
}

// listsByState reports whether list_assignments answers in the form of catalog v10 (rows with done): once the catalog
// is served (TASK-326), or for a call with state, which only catalog v10 parses. Catalog v9 stays byte for byte.
func listsByState(call values.Call) bool {
	return values.CatalogVersion >= values.CatalogWave10 || call.Args().Has("state")
}

// listAssignments10 is list_assignments of catalog v10: open by default, done or all by state; the rows carry done.
// The planner still sees IDs, dates and counters only.
func (t tools) listAssignments10(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	state, _ := call.Args().Enum("state")
	var (
		views []homeworkflow.AssignmentView
		err   error
	)
	if byState, ok := t.assignments.(AssignmentStates); ok {
		views, err = byState.ListAssignmentsByState(ctx, homeworkflow.ListState(state))
	} else if state == "" || state == values.StateOpen {
		views, err = t.assignments.ListAssignments(ctx)
	} else {
		err = errUnavailable
	}
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	rows, err := t.assignmentRows(ctx, views, values.AssignmentQueryOf(call.Args(), scope))
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	return values.AssignmentsResult10(call.ID(), rows)
}

// assignmentRows are the rows of list_assignments that pass q, with their latest draft version.
func (t tools) assignmentRows(ctx context.Context, views []homeworkflow.AssignmentView, q values.AssignmentQuery) ([]values.AssignmentRow10, error) {
	// The subject of an assignment is a free string; only the library subject it names is shown, never the string.
	var names []string
	for _, v := range views {
		if v.Subject != "" && !slices.Contains(names, v.Subject) {
			names = append(names, v.Subject)
		}
	}
	subjects, err := t.library.ResolveSubjects(ctx, names)
	if err != nil {
		return nil, err
	}

	now := t.clock.Now()
	today := dateOf(now, now.Location())
	var rows []values.AssignmentRow10
	for _, v := range views {
		id, ok := refOfID(values.KindAssignment, string(v.ID))
		if !ok {
			continue
		}
		row := values.AssignmentRow{ID: id}
		if !v.Due.IsZero() {
			row.Due = dateOf(v.Due, now.Location())
		}
		row.Status = values.AssignmentStatusOf(row.Due, today)
		row.Lesson, _ = refOfID(values.KindLesson, string(v.LessonID))
		if m, ok := subjects[v.Subject]; ok && v.Subject != "" {
			row.Subject, _ = refOfID(values.KindSubject, string(m.ID))
		}
		if !q.Match(row) {
			continue
		}
		if row.Version, err = t.latestVersion(ctx, v.ID); err != nil {
			return nil, err
		}
		rows = append(rows, values.AssignmentRow10{AssignmentRow: row, Done: v.Done})
	}
	return rows, nil
}
