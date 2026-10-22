package agent

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// LessonReadiness reads what is stored for the lesson of a recording (cases/knowledgebase).
type LessonReadiness interface {
	Lesson(ctx context.Context, id shared.RecordingID) (kb.Lesson, error)
}

// Facts tells agentturn what the agent domain cannot see in a call before it runs (agentturn.CallFacts). It only
// reports facts; the domain decides what they mean (ARCH-41).
//   - ReplacesReady: make_summary or make_lecture_text of a recording whose summary or lecture text is stored, or
//     make_summary of a material whose summary is stored (TASK-122, with WithMaterials).
//   - Cost: no estimate exists yet, so it stays unknown (zero) and every paid write needs a plan (D8, conservative).
//   - Triage: no triage tool runs yet (TASK-103), so none.
type Facts struct {
	lessons   LessonReadiness
	materials MaterialReadiness // nil: a material's summary is never ready
}

// MaterialReadiness tells whether the summary of a library material is stored (cases/homeworkflow).
type MaterialReadiness interface {
	SummaryReady(ctx context.Context, id shared.MaterialID) (bool, error)
}

// WithMaterials returns Facts that also know whether a material's summary is stored.
func (f *Facts) WithMaterials(m MaterialReadiness) *Facts {
	out := *f
	out.materials = m
	return &out
}

// NewFacts checks the dependency.
func NewFacts(lessons LessonReadiness) (*Facts, error) {
	if lessons == nil {
		return nil, fmt.Errorf("agent: facts: %w: the lesson reader is required", ErrInvalidConfig)
	}
	return &Facts{lessons: lessons}, nil
}

// Facts returns the facts of one call.
func (f *Facts) Facts(ctx context.Context, call values.Call) (thread.Facts, error) {
	tool := call.Tool()
	if tool != values.ToolMakeSummary && tool != values.ToolMakeLectureText {
		return thread.Facts{}, nil
	}
	if m, ok := call.Args().ID("material"); ok {
		if f.materials == nil {
			return thread.Facts{}, nil
		}
		ready, err := f.materials.SummaryReady(ctx, shared.MaterialID(m.ID()))
		if err != nil {
			return thread.Facts{}, err
		}
		return thread.Facts{ReplacesReady: ready}, nil
	}
	rec, ok := call.Args().ID("recording")
	if !ok {
		return thread.Facts{}, nil
	}
	l, err := f.lessons.Lesson(ctx, shared.RecordingID(rec.ID()))
	switch {
	case errors.Is(err, knowledgebase.ErrRecordingNotFound):
		return thread.Facts{}, nil // nothing to replace; the tool itself reports the missing recording
	case err != nil:
		return thread.Facts{}, err
	}
	ready := l.Ready.Summary
	if tool == values.ToolMakeLectureText {
		ready = l.Ready.LectureText
	}
	return thread.Facts{ReplacesReady: ready}, nil
}

// SubjectLister lists the subjects of the library (cases/library).
type SubjectLister interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
}

// BaseSubjects gives agentturn the subjects of the shared base (agentturn.Subjects): their IDs and the names the
// student gave. A subject whose name is not a valid Name (ADR-0022: letters, digits, spaces, ".,-()", at most 80) is
// left out of the base; list_subjects still shows its ID.
type BaseSubjects struct {
	library SubjectLister
}

// NewBaseSubjects checks the dependency.
func NewBaseSubjects(library SubjectLister) (*BaseSubjects, error) {
	if library == nil {
		return nil, fmt.Errorf("agent: base subjects: %w: the library is required", ErrInvalidConfig)
	}
	return &BaseSubjects{library: library}, nil
}

// BaseSubjects returns the subjects, at most values.MaxBaseSubjects.
func (b *BaseSubjects) BaseSubjects(ctx context.Context) ([]values.BaseSubject, error) {
	list, err := b.library.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]values.BaseSubject, 0, min(len(list), values.MaxBaseSubjects))
	for _, s := range list {
		if len(out) == values.MaxBaseSubjects {
			break
		}
		id, ok := refOfID(values.KindSubject, string(s.ID()))
		if !ok {
			continue
		}
		name, err := values.ParseName(s.Name().String())
		if err != nil {
			continue
		}
		out = append(out, values.BaseSubject{ID: id, Name: name})
	}
	return out, nil
}
