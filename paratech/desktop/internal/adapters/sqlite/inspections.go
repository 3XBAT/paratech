package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the inspection repository, used in its error messages.
const (
	opGetInspection     = "sqlite: get inspection"
	opSaveInspection    = "sqlite: save inspection"
	opRestoreInspection = "stored inspection"
)

// InspectionRepository stores the reader's outcome per library material (the library.InspectionRepository port,
// TASK-122). Validation stays in inspection.Inspection: the repository only converts rows.
type InspectionRepository struct{ db *DB }

// NewInspectionRepository returns the inspection repository of db.
func NewInspectionRepository(db *DB) *InspectionRepository { return &InspectionRepository{db: db} }

// Get loads the inspection of material m; none wraps shared.ErrNotFound.
func (r *InspectionRepository) Get(ctx context.Context, m shared.MaterialID) (*inspection.Inspection, error) {
	row, err := r.db.Queries(ctx).GetInspection(ctx, string(m))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetInspection, m, MapError(ctx, err))
	}
	return inspectionFromRow(row)
}

// Save writes the inspection: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). A row
// that exists already or a stale version wraps shared.ErrVersionConflict; a material that does not exist
// shared.ErrConflict.
func (r *InspectionRepository) Save(ctx context.Context, i *inspection.Inspection) error {
	st := i.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveInspection, st.Version)
	}
	a := st.Answer
	docKind := jobNullString(string(a.DocKind))
	subject := jobNullString(string(a.Subject))
	date := sql.NullString{}
	if !a.LessonDate.IsZero() {
		date = sql.NullString{String: a.LessonDate.Format(time.DateOnly), Valid: true}
	}
	q := r.db.Queries(ctx)
	var err error
	if st.Version == 0 {
		err = DuplicateAsVersionConflict(MapError(ctx, q.InsertInspection(ctx, sqlcgen.InsertInspectionParams{
			MaterialID: string(st.Material), TextSha256: string(st.Text), Status: string(st.Status),
			Attempt: int64(st.Attempt), DocKind: docKind, SubjectID: subject, LessonDate: date,
			HasHomework: jobBoolInt(a.HasHomework), HasTicketList: jobBoolInt(a.HasTicketList),
			MentionsEmail: jobBoolInt(a.MentionsEmail), CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
		})))
	} else {
		n, uerr := q.UpdateInspection(ctx, sqlcgen.UpdateInspectionParams{
			TextSha256: string(st.Text), Status: string(st.Status), Attempt: int64(st.Attempt), DocKind: docKind,
			SubjectID: subject, LessonDate: date, HasHomework: jobBoolInt(a.HasHomework),
			HasTicketList: jobBoolInt(a.HasTicketList), MentionsEmail: jobBoolInt(a.MentionsEmail),
			UpdatedAt: jobMs(st.UpdatedAt), MaterialID: string(st.Material), ExpectedVersion: st.Version,
		})
		err = VersionCAS(ctx, n, uerr)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveInspection, st.Material, err)
	}
	i.Persisted(st.Version + 1)
	return nil
}

func inspectionFromRow(row sqlcgen.MaterialInspection) (*inspection.Inspection, error) {
	a := inspection.Answer{
		DocKind: inspection.DocKind(row.DocKind.String), Subject: shared.SubjectID(row.SubjectID.String),
		HasHomework: row.HasHomework == 1, HasTicketList: row.HasTicketList == 1, MentionsEmail: row.MentionsEmail == 1,
	}
	if row.LessonDate.Valid {
		d, err := time.Parse(time.DateOnly, row.LessonDate.String)
		if err != nil {
			return nil, fmt.Errorf("%s %s: lesson date: %w", opRestoreInspection, row.MaterialID, err)
		}
		a.LessonDate = d
	}
	i, err := inspection.Restore(inspection.State{
		Version: row.Version, Material: shared.MaterialID(row.MaterialID), Text: material.Digest(row.TextSha256),
		Status: inspection.Status(row.Status), Attempt: int(row.Attempt), Answer: a,
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreInspection, row.MaterialID, err)
	}
	return i, nil
}
