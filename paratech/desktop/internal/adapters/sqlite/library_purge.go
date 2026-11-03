package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// The purge of a removed subject past its restore window (TASK-308, ADR-0027; the subjectpurge ports). Each method is
// one statement, so one transaction; every delete re-checks in SQL that the subject is still removed at or before the
// cutoff (subject.PurgeCutoff), so a restore that won the race keeps everything.

// Operation names of the purge.
const (
	opPurgeSubject       = "sqlite: purge removed subject"
	opSubjectStillDue    = "sqlite: check removed subject still due"
	opListFiledMaterials = "sqlite: list materials of a removed subject"
	opMaterialKept       = "sqlite: check material kept by a note or a work"
	opDigestInUse        = "sqlite: check stored file in use"
	opPurgeMaterial      = "sqlite: purge material of a removed subject"
	opListSubjectSets    = "sqlite: list ticket sets of a subject"
	opPurgeTicketSet     = "sqlite: purge ticket set of a removed subject"
)

// PurgeRemoved deletes subject id with its aliases, folders and teachers (CASCADE) if it is still removed at or before
// cutoff; false when no such row is left. A material or note still under it (RESTRICT) wraps shared.ErrConflict.
func (r *SubjectRepository) PurgeRemoved(ctx context.Context, id shared.SubjectID, cutoff time.Time) (bool, error) {
	n, err := r.db.Queries(ctx).PurgeRemovedSubject(ctx, sqlcgen.PurgeRemovedSubjectParams{
		ID: string(id), Cutoff: jobNullMs(cutoff),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opPurgeSubject, id, MapError(ctx, err))
	}
	return n > 0, nil
}

// StillDue reports whether subject id is still removed at or before cutoff.
func (r *SubjectRepository) StillDue(ctx context.Context, id shared.SubjectID, cutoff time.Time) (bool, error) {
	due, err := r.db.Queries(ctx).SubjectStillDue(ctx, sqlcgen.SubjectStillDueParams{
		ID: string(id), Cutoff: jobNullMs(cutoff),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opSubjectStillDue, id, MapError(ctx, err))
	}
	return due != 0, nil
}

// ListFiledUnder returns the materials filed in any folder of sub, in any status, also while sub is removed (the
// readers hide them then), oldest first.
func (r *MaterialRepository) ListFiledUnder(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListMaterialsFiledUnder(ctx, jobNullString(string(sub)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListFiledMaterials, MapError(ctx, err))
	}
	out := make([]*material.Material, 0, len(rows))
	for _, row := range rows {
		m, err := materialWithLessons(ctx, q, row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// KeptElsewhere reports whether material id must survive the purge of sub: a note, in any status, of another subject
// holds it as a part, or a work was written from it.
func (r *MaterialRepository) KeptElsewhere(ctx context.Context, id shared.MaterialID, sub shared.SubjectID) (bool, error) {
	kept, err := r.db.Queries(ctx).MaterialKeptElsewhere(ctx, sqlcgen.MaterialKeptElsewhereParams{
		ID: string(id), SubjectID: string(sub),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opMaterialKept, id, MapError(ctx, err))
	}
	return kept != 0, nil
}

// DigestInUse reports whether the stored bytes with sha256 d are used by anything other than material except: another
// material, or a send of a work not yet carried out that pinned them. Later stores of the same bytes (note revisions,
// TASK-290/291) belong in its query.
func (r *MaterialRepository) DigestInUse(ctx context.Context, d material.Digest, except shared.MaterialID) (bool, error) {
	used, err := r.db.Queries(ctx).MaterialDigestInUse(ctx, sqlcgen.MaterialDigestInUseParams{
		Sha256: string(d), ExceptID: string(except),
	})
	if err != nil {
		return false, fmt.Errorf("%s: %w", opDigestInUse, MapError(ctx, err))
	}
	return used != 0, nil
}

// PurgeFiledUnderRemoved deletes material id while it is still filed under sub, sub is still removed at or before
// cutoff, no note holds it as a part and no work was written from it (works would cascade); false when not. Its lesson
// links and inspection go with it (CASCADE); the summaries and assignments made from it stay with no material (SET
// NULL).
func (r *MaterialRepository) PurgeFiledUnderRemoved(ctx context.Context, id shared.MaterialID, sub shared.SubjectID,
	cutoff time.Time) (bool, error) {
	n, err := r.db.Queries(ctx).PurgeMaterialOfRemovedSubject(ctx, sqlcgen.PurgeMaterialOfRemovedSubjectParams{
		ID: string(id), SubjectID: jobNullString(string(sub)), Cutoff: jobNullMs(cutoff),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opPurgeMaterial, id, MapError(ctx, err))
	}
	return n > 0, nil
}

// SubjectTicketSets deletes the ticket sets of a purged subject (the subjectpurge.TicketSets port): a set names its
// subject without a foreign key, so it would be left dangling.
type SubjectTicketSets struct{ db *DB }

// NewSubjectTicketSets returns the ticket set purge of db.
func NewSubjectTicketSets(db *DB) *SubjectTicketSets { return &SubjectTicketSets{db: db} }

// ListOfSubject returns the ids of the ticket sets of sub, oldest first.
func (r *SubjectTicketSets) ListOfSubject(ctx context.Context, sub shared.SubjectID) ([]shared.TicketSetID, error) {
	ids, err := r.db.Queries(ctx).ListTicketSetIDsOfSubject(ctx, string(sub))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSubjectSets, MapError(ctx, err))
	}
	out := make([]shared.TicketSetID, 0, len(ids))
	for _, id := range ids {
		out = append(out, shared.TicketSetID(id))
	}
	return out, nil
}

// PurgeOfRemovedSubject deletes ticket set id of sub if sub is still removed at or before cutoff; false when not.
func (r *SubjectTicketSets) PurgeOfRemovedSubject(ctx context.Context, id shared.TicketSetID, sub shared.SubjectID,
	cutoff time.Time) (bool, error) {
	n, err := r.db.Queries(ctx).PurgeTicketSetOfRemovedSubject(ctx, sqlcgen.PurgeTicketSetOfRemovedSubjectParams{
		ID: string(id), SubjectID: string(sub), Cutoff: jobNullMs(cutoff),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opPurgeTicketSet, id, MapError(ctx, err))
	}
	return n > 0, nil
}
