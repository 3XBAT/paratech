package values

import (
	"fmt"
	"slices"
)

// TextState is how far the text of an upload was read, as the planner sees it.
type TextState string

// Text states.
const (
	TextRead       TextState = "read"
	TextEmpty      TextState = "empty"      // read, nothing in it
	TextUnreadable TextState = "unreadable" // damaged or beyond the reader's limits
	TextNotRead    TextState = "not_read"   // a type whose text is not read (PDF)
)

// docKinds are the kinds of a material the reader INSPECT_MATERIAL may name (ADR-0022).
var docKinds = []string{"lecture", "seminar", "lab", "homework", "ticket_list", "syllabus", "other"}

// InspectRow is the result of inspect_upload: the upload and its material ID, how far its text was read and the
// reader's suggestions, each an enum, an ID, a date or a flag (ADR-0022). No text of the file, its name or the
// reader's output is in it. DocKind is "" without an answer.
type InspectRow struct {
	Upload        Ref
	Material      Ref // the same file as a material of the library, for the tools that take one
	Text          TextState
	DocKind       string
	Subject       Ref // a subject of the library the reader named; zero for none
	LessonDate    Date
	HasHomework   bool
	HasTicketList bool
	MentionsEmail bool
}

// InspectResult is the result of inspect_upload and the IDs it shows.
func InspectResult(callID string, r InspectRow) (ToolResult, []Ref, error) {
	if err := errorsOf(wantRef(r.Upload, KindUpload, true), wantRef(r.Material, KindMaterial, true),
		wantRef(r.Subject, KindSubject, false)); err != nil {
		return ToolResult{}, nil, err
	}
	if r.Upload.ID() != r.Material.ID() {
		return ToolResult{}, nil, fmt.Errorf("%w: the upload and its material differ", ErrInvalidResult)
	}
	if !slices.Contains([]TextState{TextRead, TextEmpty, TextUnreadable, TextNotRead}, r.Text) {
		return ToolResult{}, nil, fmt.Errorf("%w: unknown text state", ErrInvalidResult)
	}
	body := map[string]any{"upload": r.Upload.ID(), "material": r.Material.ID(), "text": string(r.Text)}
	refs := []Ref{r.Upload, r.Material}
	switch {
	case r.DocKind == "":
		if !r.Subject.IsZero() || !r.LessonDate.IsZero() || r.HasHomework || r.HasTicketList || r.MentionsEmail {
			return ToolResult{}, nil, fmt.Errorf("%w: suggestions without a document kind", ErrInvalidResult)
		}
	case !slices.Contains(docKinds, r.DocKind) || r.Text != TextRead:
		return ToolResult{}, nil, fmt.Errorf("%w: unknown document kind or no text read", ErrInvalidResult)
	default:
		body["doc_kind"] = r.DocKind
		body["has_homework"], body["has_ticket_list"], body["mentions_email"] = r.HasHomework, r.HasTicketList, r.MentionsEmail
		if !r.Subject.IsZero() {
			body["subject"] = r.Subject.ID()
			refs = append(refs, r.Subject)
		}
		if !r.LessonDate.IsZero() {
			body["lesson_date"] = r.LessonDate.String()
		}
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := res.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return res, refs, nil
}
