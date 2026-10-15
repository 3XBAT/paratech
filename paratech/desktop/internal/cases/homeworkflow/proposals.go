package homeworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, used in error messages.
const (
	opPropose = "homeworkflow: propose action"
	opGet     = "homeworkflow: get proposal"
	opConfirm = "homeworkflow: confirm proposal"
	opDecline = "homeworkflow: decline proposal"
	opExecute = "homeworkflow: execute proposal"
)

// The pipeline of PRD section 13.3 (AC-14, AC-18):
//
//	Propose  untrusted model output -> parsed proposal -> validated against local state -> stored VALIDATED or REJECTED
//	Confirm  explicit user action   -> CONFIRMED
//	Execute  CONFIRMED only         -> facts re-checked (else STALE) -> command -> side-effect port -> EXECUTED
//
// No step calls a side-effect port on model output: Execute needs a confirmation that only the user's Confirm call
// records, and the ports accept only commands built by a confirmed proposal.

// Propose turns model output into a stored proposal. Malformed output (proposal.ErrMalformed) is refused and nothing is
// stored. Well-formed output is validated against trusted local state: the assignment, the configured recipient and the
// export file. The result is VALIDATED, waiting for the user, or REJECTED with a reason. No side effect happens here.
func (s *Service) Propose(ctx context.Context, out values.UntrustedOutput) (ProposalView, error) {
	now := s.clock.Now()
	p, err := proposal.New(s.ids.NewProposalID(), out, now)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}

	facts, err := s.facts(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if err := p.Validate(facts, now); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}

	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.proposals.Save(ctx, p) }); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	return viewOf(p), nil
}

// facts reads the trusted local state a proposal is checked against, only what its kind needs. It runs outside a
// transaction because it reads a file.
func (s *Service) facts(ctx context.Context, p *proposal.Proposal) (proposal.Facts, error) {
	var f proposal.Facts

	switch a, err := s.assignments.Get(ctx, p.AssignmentID()); {
	case err == nil:
		f.AssignmentExists = true
		f.AssignmentTitle = a.Details().Title() // the export heading comes from the stored assignment, not the model
	case !errors.Is(err, shared.ErrNotFound):
		return f, err
	}

	if p.Kind().NeedsRecipient() {
		r, err := s.recipients.HomeworkRecipient(ctx)
		if err != nil {
			return f, err
		}
		f.Recipient = r
	}

	if p.Kind().NeedsAttachment() {
		d, err := s.exportDigest(ctx, p.AssignmentID())
		if err != nil {
			return f, err
		}
		f.Attachment = d
	}
	return f, nil
}

// exportDigest streams the export file of the assignment through SHA-256; a missing file yields the zero digest.
func (s *Service) exportDigest(ctx context.Context, a shared.AssignmentID) (proposal.Digest, error) {
	ref, err := proposal.ExportRef(a)
	if err != nil {
		return proposal.Digest{}, err
	}
	rc, err := s.artifacts.Open(ctx, ref)
	if errors.Is(err, shared.ErrNotFound) {
		return proposal.Digest{}, nil
	}
	if err != nil {
		return proposal.Digest{}, err
	}
	defer func() { _ = rc.Close() }() // read-only: a close error loses nothing

	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return proposal.Digest{}, err
	}
	return proposal.NewDigest(hex.EncodeToString(h.Sum(nil)), n)
}

// Get returns a proposal for the confirmation dialog.
func (s *Service) Get(ctx context.Context, id proposal.ID) (ProposalView, error) {
	p, err := s.proposals.Get(ctx, id)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opGet, err)
	}
	return viewOf(p), nil
}

// Confirm records the explicit user approval of a VALIDATED proposal (AC-14). Only the UI calls it, on a user action;
// nothing derived from model output does.
func (s *Service) Confirm(ctx context.Context, id proposal.ID) (ProposalView, error) {
	return s.change(ctx, opConfirm, id, (*proposal.Proposal).Confirm)
}

// Decline records that the user refused a VALIDATED proposal; it can never be executed afterwards.
func (s *Service) Decline(ctx context.Context, id proposal.ID) (ProposalView, error) {
	return s.change(ctx, opDecline, id, (*proposal.Proposal).Decline)
}

func (s *Service) change(ctx context.Context, op string, id proposal.ID, apply func(*proposal.Proposal, time.Time) error) (ProposalView, error) {
	var view ProposalView
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.proposals.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(p, s.clock.Now()); err != nil {
			return err
		}
		if err := s.proposals.Save(ctx, p); err != nil {
			return err
		}
		view = viewOf(p)
		return nil
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", op, err)
	}
	return view, nil
}

// Execute performs the side effect of a CONFIRMED proposal and marks it EXECUTED. Any other status is refused before
// a port is called (proposal.ErrNotConfirmed). Executing an EXECUTED proposal again does nothing. Right before the
// port, the trusted facts are read again: if what the user confirmed changed, the proposal is stored STALE and
// proposal.ErrStale is returned without calling a port. If the port fails, the proposal stays CONFIRMED and Execute
// may be repeated: the command carries the same idempotency key, so the adapter does not act twice (hard rule 7).
// The port is called outside the transaction (Tx forbids external I/O).
func (s *Service) Execute(ctx context.Context, id proposal.ID) (ProposalView, error) {
	p, err := s.proposals.Get(ctx, id)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opExecute, err)
	}
	if p.Executed() {
		return viewOf(p), nil
	}

	facts, err := s.facts(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opExecute, err)
	}
	if err := p.Recheck(facts, s.clock.Now()); err != nil {
		if errors.Is(err, proposal.ErrStale) {
			// The version check of Save refuses this if a concurrent Execute changed the proposal meanwhile.
			if serr := s.tx.Do(ctx, func(ctx context.Context) error { return s.proposals.Save(ctx, p) }); serr != nil {
				return ProposalView{}, fmt.Errorf("%s: %w", opExecute, errors.Join(err, serr))
			}
		}
		return ProposalView{}, fmt.Errorf("%s: %w", opExecute, err)
	}

	if err := s.perform(ctx, p); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opExecute, err)
	}

	var view ProposalView
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.proposals.Get(ctx, id)
		if err != nil {
			return err
		}
		if !p.Executed() { // a concurrent Execute may have finished first
			if err := p.MarkExecuted(s.clock.Now()); err != nil {
				return err
			}
			if err := s.proposals.Save(ctx, p); err != nil {
				return err
			}
		}
		view = viewOf(p)
		return nil
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opExecute, err)
	}
	return view, nil
}

// perform obtains the command from the proposal (which refuses unless CONFIRMED) and hands it to the port of its kind.
func (s *Service) perform(ctx context.Context, p *proposal.Proposal) error {
	switch p.Kind() {
	case proposal.KindSendHomework:
		cmd, err := p.EmailCommand()
		if err != nil {
			return err
		}
		return s.mailer.SendHomework(ctx, cmd)
	case proposal.KindExportHomework:
		cmd, err := p.ExportCommand()
		if err != nil {
			return err
		}
		return s.exporter.ExportHomework(ctx, cmd)
	case proposal.KindDeadlineEvent:
		cmd, err := p.CalendarCommand()
		if err != nil {
			return err
		}
		return s.calendar.UpsertDeadline(ctx, cmd)
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedKind, p.Kind())
}
