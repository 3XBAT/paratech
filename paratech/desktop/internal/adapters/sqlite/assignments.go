package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the assignment repository, used in its error messages.
const (
	opGetAssignment          = "sqlite: get assignment"
	opGetAssignmentBySource  = "sqlite: get assignment by source"
	opListAssignmentsBySrc   = "sqlite: list assignments of a transcript"
	opListAssignmentsByMat   = "sqlite: list assignments of a material"
	opListRecentAssignments  = "sqlite: list recent assignments"
	opSaveAssignment         = "sqlite: save assignment"
	opDeleteAssignment       = "sqlite: delete assignment"
	opDeleteUnusedAssignment = "sqlite: delete unused assignment"
	opBusyAssignment         = "sqlite: busy assignment"
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

// GetByMaterialSource loads the assignment extracted as item index of the extraction of library material m
// (TASK-122); a missing one wraps shared.ErrNotFound.
func (r *AssignmentRepository) GetByMaterialSource(ctx context.Context, m shared.MaterialID, index int) (*assignment.Assignment, error) {
	row, err := r.db.Queries(ctx).GetAssignmentByMaterialSource(ctx, sqlcgen.GetAssignmentByMaterialSourceParams{
		MaterialID: jobNullString(string(m)), SourceIndex: sql.NullInt64{Int64: int64(index), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("%s material %s/%d: %w", opGetAssignmentBySource, m, index, MapError(ctx, err))
	}
	return assignmentFromRow(row)
}

// ListByMaterial returns the assignments extracted from library material m, in the order of the extraction answer;
// none is an empty list.
func (r *AssignmentRepository) ListByMaterial(ctx context.Context, m shared.MaterialID) ([]*assignment.Assignment, error) {
	rows, err := r.db.Queries(ctx).ListAssignmentsByMaterial(ctx, jobNullString(string(m)))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListAssignmentsByMat, m, MapError(ctx, err))
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

// ListByTranscript returns the assignments extracted from transcript t, in the order of the extraction answer; none is
// an empty list.
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

// ListRecent returns at most limit assignments, newest first (the homeworkflow.AssignmentList port); none is an empty
// list. limit must be at least 1.
func (r *AssignmentRepository) ListRecent(ctx context.Context, limit int) ([]*assignment.Assignment, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s: limit %d, want at least 1", opListRecentAssignments, limit)
	}
	rows, err := r.db.Queries(ctx).ListRecentAssignments(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListRecentAssignments, MapError(ctx, err))
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
				MaterialID: jobNullString(string(st.Material)),
				Title:      st.Title, Description: st.Description, DueAt: jobNullMs(st.Due), UserEdited: jobBoolInt(st.UserEdited),
				SourceIndex: nullSourceIndex(st.SourceIndex), PromptVersion: nullPromptVersion(st.PromptVersion),
				HintSubject: st.HintSubject, HintTeacher: st.HintTeacher, UserSubject: st.UserSubject, UserTeacher: st.UserTeacher,
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt), DeletedAt: jobNullMs(st.DeletedAt),
			})))
		}
		n, err := q.UpdateAssignment(ctx, sqlcgen.UpdateAssignmentParams{
			Title: st.Title, Description: st.Description, DueAt: jobNullMs(st.Due), UserEdited: jobBoolInt(st.UserEdited),
			PromptVersion: nullPromptVersion(st.PromptVersion), HintSubject: st.HintSubject, HintTeacher: st.HintTeacher,
			UserSubject: st.UserSubject, UserTeacher: st.UserTeacher, DeletedAt: jobNullMs(st.DeletedAt),
			UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveAssignment, st.ID, err)
	}

	a.Persisted(next)
	return nil
}

// Delete removes the stored assignment as a version compare-and-swap (ADR-0004(d)): a row that changed or is gone
// yields shared.ErrVersionConflict, a row a delivery references (ON DELETE RESTRICT) is kept and yields
// assignment.ErrInUse.
func (r *AssignmentRepository) Delete(ctx context.Context, a *assignment.Assignment) error {
	if a.Version() < 1 {
		return fmt.Errorf("%s %s: never stored: %w", opDeleteAssignment, a.ID(), shared.ErrVersionConflict)
	}

	n, err := r.db.Queries(ctx).DeleteAssignment(ctx, sqlcgen.DeleteAssignmentParams{ID: string(a.ID()), ExpectedVersion: a.Version()})
	err = VersionCAS(ctx, n, err)
	if errors.Is(err, shared.ErrConflict) && !errors.Is(err, shared.ErrVersionConflict) {
		// A delete can only break the foreign key of a referencing row.
		return fmt.Errorf("%s %s: %w", opDeleteAssignment, a.ID(), assignment.ErrInUse)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteAssignment, a.ID(), err)
	}
	return nil
}

// freeProposalStatuses are the proposal statuses that leave an assignment free for DeleteUnused: nothing was or will
// be done with it (ADR-0029).
var freeProposalStatuses = []string{string(proposal.StatusDeclined), string(proposal.StatusRejected)}

// activeJobStatuses are the job statuses of a draft generation that is queued, running or waiting and has no draft
// row yet, which also keep an assignment for DeleteUnused.
var activeJobStatuses = func() []string {
	var out []string
	for _, s := range job.ActiveStatuses() {
		out = append(out, string(s))
	}
	return out
}()

// DeleteUnused removes an assignment the student added (ADR-0029): in one transaction, an assignment that a draft, an
// unfinished draft generation, a delivery, a work or a proposal other than a declined or rejected one references is
// kept and yields assignment.ErrInUse, otherwise the delete is the version compare-and-swap of Delete.
func (r *AssignmentRepository) DeleteUnused(ctx context.Context, a *assignment.Assignment) error {
	if a.Version() < 1 {
		return fmt.Errorf("%s %s: never stored: %w", opDeleteUnusedAssignment, a.ID(), shared.ErrVersionConflict)
	}

	free, err := json.Marshal(freeProposalStatuses)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteUnusedAssignment, a.ID(), err)
	}
	active, err := json.Marshal(activeJobStatuses)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteUnusedAssignment, a.ID(), err)
	}

	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		used, err := q.AssignmentInUse(ctx, sqlcgen.AssignmentInUseParams{
			ID: string(a.ID()), FreeStatuses: string(free),
			DraftJobType: cvalues.HomeworkDraftJobType, ActiveJobStatuses: string(active),
		})
		switch {
		case err != nil:
			return MapError(ctx, err)
		case used != 0:
			return assignment.ErrInUse
		}
		n, err := q.DeleteAssignment(ctx, sqlcgen.DeleteAssignmentParams{ID: string(a.ID()), ExpectedVersion: a.Version()})
		err = VersionCAS(ctx, n, err)
		if errors.Is(err, shared.ErrConflict) && !errors.Is(err, shared.ErrVersionConflict) {
			return assignment.ErrInUse // a reference that appeared despite the checks: the foreign key held
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteUnusedAssignment, a.ID(), err)
	}
	return nil
}

// busyDeliveryStatuses are the delivery statuses that keep an assignment from «Удалить» (ADR-0029): a delivery that may
// still go out (PENDING, a scheduled one too) or may have gone out unseen.
var busyDeliveryStatuses = []string{
	string(delivery.StatusPending), string(delivery.StatusSending), string(delivery.StatusUnknown),
}

// busyWorkStatuses are the statuses of a work whose triage or writing is under way.
var busyWorkStatuses = []string{string(work.StatusTriage), string(work.StatusWriting)}

// busyJobTypes are the job types whose payload names an assignment ("assignment_id") and that keep it while active.
var busyJobTypes = []string{cvalues.HomeworkDraftJobType, cvalues.WorkWritingJobType}

// Busy reports whether something in progress uses assignment id, so that «Удалить» is refused (ADR-0029): a delivery
// in PENDING, SENDING or UNKNOWN, a work in triage or writing, or an active draft generation or writing job. Called
// inside Tx.Do it reads in that transaction.
func (r *AssignmentRepository) Busy(ctx context.Context, id shared.AssignmentID) (bool, error) {
	lists := make([]string, 0, 4)
	for _, l := range [][]string{busyDeliveryStatuses, busyWorkStatuses, busyJobTypes, activeJobStatuses} {
		b, err := json.Marshal(l)
		if err != nil {
			return false, fmt.Errorf("%s %s: %w", opBusyAssignment, id, err)
		}
		lists = append(lists, string(b))
	}

	busy, err := r.db.Queries(ctx).AssignmentBusy(ctx, sqlcgen.AssignmentBusyParams{
		ID: string(id), DeliveryStatuses: lists[0], WorkStatuses: lists[1], JobTypes: lists[2], ActiveJobStatuses: lists[3],
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opBusyAssignment, id, MapError(ctx, err))
	}
	return busy != 0, nil
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
		Transcript: shared.TranscriptID(row.TranscriptID.String), Material: shared.MaterialID(row.MaterialID.String),
		SourceIndex:   source,
		PromptVersion: int(row.PromptVersion.Int64), Title: row.Title, Description: row.Description,
		Due: jobFromNullMs(row.DueAt), UserEdited: row.UserEdited != 0,
		HintSubject: row.HintSubject, HintTeacher: row.HintTeacher, UserSubject: row.UserSubject, UserTeacher: row.UserTeacher,
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt), DeletedAt: jobFromNullMs(row.DeletedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored assignment %s: %w", row.ID, err)
	}
	return a, nil
}
