package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the proposal repository, used in its error messages.
const (
	opGetProposal   = "sqlite: get proposal"
	opSaveProposal  = "sqlite: save proposal"
	opListProposals = "sqlite: list proposals"
)

// ProposalRepository stores the action proposals of the LLM (the homeworkflow.ProposalRepository port). The status
// machine and the consistency rules stay in proposal.Proposal: the repository only converts rows, and Restore refuses
// a row that does not fit them.
type ProposalRepository struct{ db *DB }

// NewProposalRepository returns the proposal repository of db.
func NewProposalRepository(db *DB) *ProposalRepository { return &ProposalRepository{db: db} }

// Get loads a proposal; a missing one wraps shared.ErrNotFound.
func (r *ProposalRepository) Get(ctx context.Context, id proposal.ID) (*proposal.Proposal, error) {
	row, err := r.db.Queries(ctx).GetProposal(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetProposal, id, MapError(ctx, err))
	}
	p, err := proposalFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetProposal, id, err)
	}
	return p, nil
}

// ListByStatus returns the proposals in status, newest first, at most limit (at least 1); the todayfeed.ProposalList
// port (TASK-125).
func (r *ProposalRepository) ListByStatus(ctx context.Context, status proposal.Status, limit int) ([]*proposal.Proposal, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s: limit %d", opListProposals, limit)
	}
	rows, err := r.db.Queries(ctx).ListProposalsByStatus(ctx, sqlcgen.ListProposalsByStatusParams{
		Status: string(status), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListProposals, MapError(ctx, err))
	}
	out := make([]*proposal.Proposal, 0, len(rows))
	for _, row := range rows {
		p, err := proposalFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opListProposals, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// Save writes the proposal: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id that
// exists already, or a stale version, yields shared.ErrVersionConflict; a stored proposal whose row is gone is never
// inserted again.
func (r *ProposalRepository) Save(ctx context.Context, p *proposal.Proposal) error {
	st := p.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveProposal, st.Version)
	}

	next := st.Version + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertProposal(ctx, sqlcgen.InsertProposalParams{
				ID: string(st.ID), Kind: string(st.Kind), Status: string(st.Status), AssignmentID: string(st.AssignmentID),
				Title: st.Title, Body: st.Body, DueAt: jobNullMs(st.Due), Reason: string(st.Reason), Recipient: st.Recipient,
				AttachmentSha256: st.AttachmentSHA256, AttachmentSize: st.AttachmentSize, AssignmentTitle: st.AssignmentTitle,
				AttachmentFormat: string(st.AttachmentFormat), WorkID: string(st.WorkID), AttachmentName: st.AttachmentName,
				ConfirmedAt: jobNullMs(st.ConfirmedAt), ExecutedAt: jobNullMs(st.ExecutedAt), CreatedAt: jobMs(st.CreatedAt),
				UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateProposal(ctx, sqlcgen.UpdateProposalParams{
			Status: string(st.Status), Reason: string(st.Reason), Recipient: st.Recipient,
			AttachmentSha256: st.AttachmentSHA256, AttachmentSize: st.AttachmentSize, AssignmentTitle: st.AssignmentTitle,
			ConfirmedAt: jobNullMs(st.ConfirmedAt), ExecutedAt: jobNullMs(st.ExecutedAt), UpdatedAt: jobMs(st.UpdatedAt),
			ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveProposal, st.ID, err)
	}

	p.Persisted(next)
	return nil
}

func proposalFromRow(row sqlcgen.Proposal) (*proposal.Proposal, error) {
	p, err := proposal.Restore(proposal.State{
		Version: row.Version, ID: proposal.ID(row.ID), Kind: proposal.Kind(row.Kind), Status: proposal.Status(row.Status),
		AssignmentID: shared.AssignmentID(row.AssignmentID), Title: row.Title, Body: row.Body, Due: jobFromNullMs(row.DueAt),
		Reason: proposal.RejectReason(row.Reason), Recipient: row.Recipient, AttachmentSHA256: row.AttachmentSha256,
		AttachmentSize: row.AttachmentSize, AssignmentTitle: row.AssignmentTitle,
		AttachmentFormat: proposal.Format(row.AttachmentFormat), WorkID: shared.WorkID(row.WorkID),
		AttachmentName: row.AttachmentName, CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
		ConfirmedAt: jobFromNullMs(row.ConfirmedAt), ExecutedAt: jobFromNullMs(row.ExecutedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored proposal: %w", err)
	}
	return p, nil
}
