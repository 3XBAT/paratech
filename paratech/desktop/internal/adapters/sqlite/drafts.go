package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the draft repository, used in its error messages.
const (
	opGetDraft  = "sqlite: get homework draft"
	opSaveDraft = "sqlite: save homework draft"
)

// DraftRepository stores the draft history of each assignment (the homeworkflow.DraftRepository port). The rules of the
// history stay in homeworkdraft.Draft: the repository only converts rows.
type DraftRepository struct{ db *DB }

// NewDraftRepository returns the draft repository of db.
func NewDraftRepository(db *DB) *DraftRepository { return &DraftRepository{db: db} }

// Get loads the draft of an assignment with all its versions in one statement; none wraps shared.ErrNotFound.
func (r *DraftRepository) Get(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error) {
	rows, err := r.db.Queries(ctx).GetDraft(ctx, string(a))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetDraft, a, MapError(ctx, err))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s %s: %w", opGetDraft, a, shared.ErrNotFound)
	}
	d, err := draftFromRows(a, rows)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetDraft, a, err)
	}
	return d, nil
}

// Save writes the draft: an INSERT of the root and every version for version 0, otherwise a version compare-and-swap
// of the root that also inserts the versions not stored yet (ADR-0004(d)); both in one transaction. A draft of the
// assignment that exists already, or a stale version, yields shared.ErrVersionConflict and nothing is written; an
// unknown assignment yields an error wrapping shared.ErrConflict (foreign key); a job that already produced a version
// of another draft yields shared.ErrDuplicate.
func (r *DraftRepository) Save(ctx context.Context, d *homeworkdraft.Draft) error {
	st := d.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveDraft, st.Version)
	}
	if len(st.Revisions) == 0 {
		// Restore refuses a draft without versions, so storing one would make it unreadable.
		return fmt.Errorf("%s %s: no versions: %w", opSaveDraft, st.Assignment, homeworkdraft.ErrInvalidDraft)
	}

	next := st.Version + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		stored := int64(0)
		if st.Version == 0 {
			if err := DuplicateAsVersionConflict(MapError(ctx, q.InsertDraft(ctx, string(st.Assignment)))); err != nil {
				return err
			}
		} else {
			n, err := q.UpdateDraftVersion(ctx, sqlcgen.UpdateDraftVersionParams{
				AssignmentID: string(st.Assignment), ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, n, err); err != nil {
				return err
			}
			if stored, err = q.LatestDraftRevision(ctx, string(st.Assignment)); err != nil {
				return MapError(ctx, err)
			}
		}
		if stored > int64(len(st.Revisions)) {
			// The loaded version matched, so the stored versions are the ones this instance was loaded with.
			return fmt.Errorf("%w: %d versions stored, the draft has %d", ErrConstraint, stored, len(st.Revisions))
		}
		for _, rv := range st.Revisions[stored:] {
			if err := q.InsertDraftRevision(ctx, revisionParams(st.Assignment, rv)); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveDraft, st.Assignment, err)
	}

	d.Persisted(next)
	return nil
}

func revisionParams(a shared.AssignmentID, rv homeworkdraft.RevisionState) sqlcgen.InsertDraftRevisionParams {
	return sqlcgen.InsertDraftRevisionParams{
		AssignmentID: string(a), Number: int64(rv.Number), Origin: string(rv.Origin), Title: rv.Title, Body: rv.Body,
		Instruction: rv.Instruction, PromptVersion: nullPromptVersion(rv.PromptVersion), JobID: jobNullString(string(rv.Job)),
		CreatedAt: jobMs(rv.CreatedAt),
	}
}

func draftFromRows(a shared.AssignmentID, rows []sqlcgen.GetDraftRow) (*homeworkdraft.Draft, error) {
	st := homeworkdraft.State{
		Version: rows[0].DraftVersion, Assignment: a, Revisions: make([]homeworkdraft.RevisionState, 0, len(rows)),
	}
	for _, row := range rows {
		st.Revisions = append(st.Revisions, homeworkdraft.RevisionState{
			Number: int(row.Number), Origin: homeworkdraft.Origin(row.Origin), Title: row.Title, Body: row.Body,
			Instruction: row.Instruction, PromptVersion: int(promptVersionOf(row.PromptVersion)),
			Job: shared.JobID(row.JobID.String), CreatedAt: jobFromMs(row.CreatedAt),
		})
	}
	d, err := homeworkdraft.Restore(st)
	if err != nil {
		return nil, fmt.Errorf("stored draft: %w", err)
	}
	return d, nil
}

func promptVersionOf(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}
