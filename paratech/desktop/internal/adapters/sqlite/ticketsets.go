package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the ticket set repository, used in its error messages.
const (
	opGetTicketSet     = "sqlite: get ticket set"
	opListTicketSets   = "sqlite: list ticket sets"
	opSaveTicketSet    = "sqlite: save ticket set"
	opRestoreTicketSet = "stored ticket set"
)

// TicketSetRepository stores the ticket sets (the tickets.Repository port, TASK-133). The content is kept as canonical
// JSON and read back through ticketset.ParseContent and the aggregate's checks: a stored title passes the same checks
// as a fresh one. Validation stays in the aggregate; the repository only converts rows.
type TicketSetRepository struct{ db *DB }

// NewTicketSetRepository returns the ticket set repository of db.
func NewTicketSetRepository(db *DB) *TicketSetRepository { return &TicketSetRepository{db: db} }

// Get loads a set; a missing one wraps shared.ErrNotFound.
func (r *TicketSetRepository) Get(ctx context.Context, id shared.TicketSetID) (*ticketset.Set, error) {
	row, err := r.db.Queries(ctx).GetTicketSet(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetTicketSet, id, MapError(ctx, err))
	}
	return ticketSetFromRow(row)
}

// ListBySubject returns the newest sets of a subject, at most limit (at least 1), newest first; none is an empty list.
func (r *TicketSetRepository) ListBySubject(ctx context.Context, sub shared.SubjectID, limit int) ([]*ticketset.Set, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s: limit %d, want at least 1", opListTicketSets, limit)
	}
	rows, err := r.db.Queries(ctx).ListTicketSetsBySubject(ctx, sqlcgen.ListTicketSetsBySubjectParams{
		SubjectID: string(sub), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListTicketSets, sub, MapError(ctx, err))
	}
	out := make([]*ticketset.Set, 0, len(rows))
	for _, row := range rows {
		s, err := ticketSetFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func ticketSetFromRow(row sqlcgen.TicketSet) (*ticketset.Set, error) {
	c, err := ticketset.ParseContent(row.Content)
	if err != nil {
		return nil, fmt.Errorf("%s %s: content: %w", opRestoreTicketSet, row.ID, err)
	}
	s, err := ticketset.Restore(ticketset.State{
		Version: row.Version, ID: shared.TicketSetID(row.ID), Subject: shared.SubjectID(row.SubjectID),
		Source: shared.MaterialID(row.MaterialID.String), Status: ticketset.Status(row.Status), Content: c,
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: state: %w", opRestoreTicketSet, row.ID, err)
	}
	return s, nil
}

// Save writes the set: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). A stale version
// wraps shared.ErrVersionConflict.
func (r *TicketSetRepository) Save(ctx context.Context, s *ticketset.Set) error {
	st := s.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveTicketSet, st.Version)
	}
	q := r.db.Queries(ctx)
	if st.Version == 0 {
		err := q.InsertTicketSet(ctx, sqlcgen.InsertTicketSetParams{
			ID: string(st.ID), SubjectID: string(st.Subject), MaterialID: jobNullString(string(st.Source)),
			Status: string(st.Status), Content: st.Content.Encode(), CreatedAt: jobMs(st.CreatedAt),
			UpdatedAt: jobMs(st.UpdatedAt),
		})
		if err := DuplicateAsVersionConflict(MapError(ctx, err)); err != nil {
			return fmt.Errorf("%s %s: %w", opSaveTicketSet, st.ID, err)
		}
	} else {
		n, err := q.UpdateTicketSet(ctx, sqlcgen.UpdateTicketSetParams{
			Status: string(st.Status), Content: st.Content.Encode(), UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID),
			ExpectedVersion: st.Version,
		})
		if err := VersionCAS(ctx, n, err); err != nil {
			return fmt.Errorf("%s %s: %w", opSaveTicketSet, st.ID, err)
		}
	}
	s.Persisted(st.Version + 1)
	return nil
}
