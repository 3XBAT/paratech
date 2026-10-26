package values

import "fmt"

// WriteRow is what an action tool (class U) shows the planner (ADR-0022): the ID it made or placed, the job it started
// and whether it changed anything (false: the state was already so, nothing was journaled).
type WriteRow struct {
	ID      Ref // a subject, folder, material or the assignment create_assignment added; zero for none
	Job     Ref // the job a paid tool started; zero for none
	Changed bool
}

// WriteResult is the result of an action tool and the IDs it shows. An added assignment shows its ID only, never its
// title or note (ADR-0029).
func WriteResult(callID string, w WriteRow) (ToolResult, []Ref, error) {
	switch w.ID.Kind() {
	case "", KindSubject, KindFolder, KindMaterial, KindAssignment:
	default:
		return ToolResult{}, nil, fmt.Errorf("%w: an action shows a subject, folder, material or assignment id", ErrInvalidResult)
	}
	if err := wantRef(w.Job, KindJob, false); err != nil {
		return ToolResult{}, nil, err
	}
	body := map[string]any{"done": true, "changed": w.Changed}
	var refs []Ref
	if !w.ID.IsZero() {
		body["id"] = w.ID.ID()
		refs = append(refs, w.ID)
	}
	if !w.Job.IsZero() {
		body["job"] = w.Job.ID()
		refs = append(refs, w.Job)
	}
	r := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := r.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return r, refs, nil
}
