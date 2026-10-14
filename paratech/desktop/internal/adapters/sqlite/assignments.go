package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the assignment repository, used in its error messages.
const (
	opGetAssignment         = "sqlite: get assignment"
	opGetAssignmentBySource = "sqlite: get assignment by source"
	opListAssignmentsBySrc  = "sqlite: list assignments of a transcript"
	opSaveAssignment        = "sqlite: save assignment"
)

// Values of assignment.State that are NULL in the row.
const (
	assignmentNotExtracted    = -1 // SourceIndex of an assignment that was not extracted
	assignmentNoPromptVersion = 0  // PromptVersion of an assignment that was not extracted
)

// AssignmentRepository stores assignments with their deadlines (the homeworkflow.AssignmentRepository port). Validation
// stays in assignment.Assignment: the repository only converts rows.
type AssignmentRepository struct{ db *DB }

// NewAssignmentRepository returns the assignment repository of db.
func NewAssignmentRepository(db *DB) *AssignmentRepository { return &AssignmentRepository{db: db} }

// Get loads an assignment; a missing one wraps shared.ErrNotFound.
func (r *AssignmentRepository) Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	row, err := r.db.Queries(ctx).GetAssignment(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetAssignment, id, MapError(ctx, err))
	}
	return assignmentFromRow(row)
}

// GetBySource loads the assignment extracted as item index of the extraction of transcript t; a missing one wraps
// shared.ErrNotFound.
func (r *AssignmentRepository) GetBySource(ctx context.Context, t shared.TranscriptID, index int) (*assignment.Assignment, error) {
	row, err := r.db.Queries(ctx).GetAssignmentBySource(ctx, sqlcgen.GetAssignmentBySourceParams{
		TranscriptID: jobNullString(string(t)), SourceIndex: sql.NullInt64{Int64: int64(index), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s/%d: %w", opGetAssignmentBySource, t, index, MapError(ctx, err))
	}
	return assignmentFromRow(row)
}

// ListByTranscript returns the assignments extracted from transcript t, in the order of the extraction answer.
func (r *AssignmentRepository) ListByTranscript(ctx context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error) {
	rows, err := r.db.Queries(ctx).ListAssignmentsByTranscript(ctx, jobNullString(string(t)))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListAssignmentsBySrc, t, MapError(ctx, err))
	}
	out := make([]*assignment.Assignment, 0, len(rows))
	for _, row := range rows {
		a, err := assignmentFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// Save writes the assignment: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id that
// exists already, or a stale version, yields shared.ErrVersionConflict; a second assignment for the same extraction
// item (transcript and source index) yields shared.ErrDuplicate; a lesson or transcript that does not exist yields an
// error wrapping shared.ErrConflict (foreign key).
func (r *AssignmentRepository) Save(ctx context.Context, a *assignment.Assignment) error {
	if a.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveAssignment, a.Version())
	}

	st := a.State()
	next := st.Version + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertAssignment(ctx, sqlcgen.InsertAssignmentParams{
				ID: string(st.ID), LessonID: jobNullString(string(st.Lesson)), TranscriptID: jobNullString(string(st.Transcript)),
				Title: st.Title, Description: st.Description, DueAt: jobNullMs(st.Due), UserEdited: jobBoolInt(st.UserEdited),
				SourceIndex: nullSourceIndex(st.SourceIndex), PromptVersion: nullPromptVersion(st.PromptVersion),
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateAssignment(ctx, sqlcgen.UpdateAssignmentParams{
			Title: st.Title, Description: st.Description, DueAt: jobNullMs(st.Due), UserEdited: jobBoolInt(st.UserEdited),
			PromptVersion: nullPromptVersion(st.PromptVersion), UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID),
			ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveAssignment, st.ID, err)
	}

	a.Persisted(next)
	return nil
}

func nullSourceIndex(i int) sql.NullInt64 {
	if i == assignmentNotExtracted {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(i), Valid: true}
}

func nullPromptVersion(v int) sql.NullInt64 {
	if v == assignmentNoPromptVersion {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

func assignmentFromRow(row sqlcgen.Assignment) (*assignment.Assignment, error) {
	source := assignmentNotExtracted
	if row.SourceIndex.Valid {
		source = int(row.SourceIndex.Int64)
	}
	a, err := assignment.Restore(assignment.State{
		Version: row.Version, ID: shared.AssignmentID(row.ID), Lesson: shared.LessonID(row.LessonID.String),
		Transcript: shared.TranscriptID(row.TranscriptID.String), SourceIndex: source,
		PromptVersion: int(row.PromptVersion.Int64), Title: row.Title, Description: row.Description,
		Due: jobFromNullMs(row.DueAt), UserEdited: row.UserEdited != 0,
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored assignment %s: %w", row.ID, err)
	}
	return a, nil
}
