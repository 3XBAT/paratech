package values

import (
	"fmt"
	"slices"
)

// Results of the wave-10 assignment tools (ADR-0033 §9, §10). Catalog v9 keeps AssignmentsResult and WriteResult
// byte for byte.

// AssignmentRow10 is a row of list_assignments of catalog v10: the v9 row and whether the assignment is done.
type AssignmentRow10 struct {
	AssignmentRow
	Done bool
}

// AssignmentsResult10 is the result of list_assignments of catalog v10 and the IDs it shows: the v9 rows plus done.
func AssignmentsResult10(callID string, rows []AssignmentRow10) (ToolResult, []Ref, error) {
	type item struct {
		ID      string `json:"id"`
		Subject string `json:"subject,omitempty"`
		Lesson  string `json:"lesson,omitempty"`
		Due     string `json:"due,omitempty"`
		Status  string `json:"status"`
		Done    bool   `json:"done"`
		Version string `json:"version,omitempty"`
	}
	items := make([]item, 0, len(rows))
	refs := make([][]Ref, 0, len(rows)) // per row
	for _, r := range rows {
		if err := errorsOf(wantRef(r.ID, KindAssignment, true), wantRef(r.Subject, KindSubject, false),
			wantRef(r.Lesson, KindLesson, false), wantRef(r.Version, KindVersion, false)); err != nil {
			return ToolResult{}, nil, err
		}
		if !slices.Contains([]AssignmentStatus{AssignmentUpcoming, AssignmentOverdue, AssignmentNoDue}, r.Status) {
			return ToolResult{}, nil, fmt.Errorf("%w: unknown assignment status", ErrInvalidResult)
		}
		items = append(items, item{
			ID: r.ID.ID(), Subject: r.Subject.ID(), Lesson: r.Lesson.ID(), Due: r.Due.String(), Status: string(r.Status),
			Done: r.Done, Version: r.Version.ID(),
		})
		refs = append(refs, []Ref{r.ID, r.Subject, r.Lesson, r.Version})
	}
	return listResult(callID, "assignments", items, refs)
}

// MarkRow is the outcome of mark_assignments_done for one assignment: changed, or already so (Changed false), or
// refused with Refusal (not_found, unknown_id for a deleted one, failed).
type MarkRow struct {
	ID      Ref
	Changed bool
	Refusal Refusal
}

// markRefusals are the refusals one row of mark_assignments_done may carry.
var markRefusals = []Refusal{RefuseNotFound, RefuseUnknownID, RefuseFailed}

// MarksResult is the result of mark_assignments_done: one write for the list (ADR-0033 §9), changed when any row
// changed, and the outcome of each assignment by its ID. It shows the IDs of the rows that were not refused.
func MarksResult(callID string, rows []MarkRow) (ToolResult, []Ref, error) {
	type item struct {
		ID      string `json:"id"`
		Changed bool   `json:"changed"`
		Refused string `json:"refused,omitempty"`
	}
	if len(rows) == 0 || len(rows) > MaxMarkAssignments {
		return ToolResult{}, nil, fmt.Errorf("%w: 1..%d marks", ErrInvalidResult, MaxMarkAssignments)
	}
	items := make([]item, 0, len(rows))
	var (
		shown   []Ref
		changed bool
	)
	for _, r := range rows {
		if err := wantRef(r.ID, KindAssignment, true); err != nil {
			return ToolResult{}, nil, err
		}
		if r.Refusal != "" && (r.Changed || !slices.Contains(markRefusals, r.Refusal)) {
			return ToolResult{}, nil, fmt.Errorf("%w: a refused mark changed nothing and has a mark refusal", ErrInvalidResult)
		}
		items = append(items, item{ID: r.ID.ID(), Changed: r.Changed, Refused: string(r.Refusal)})
		changed = changed || r.Changed
		if r.Refusal == "" {
			shown = append(shown, r.ID)
		}
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(map[string]any{"done": true, "changed": changed, "marks": items})}
	if err := res.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return res, shown, nil
}
