package homeworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, used in error messages.
const (
	opPropose     = "homeworkflow: propose action"
	opProposeSend = "homeworkflow: propose sending"
	opGet         = "homeworkflow: get proposal"
	opConfirm     = "homeworkflow: confirm proposal"
	opDecline     = "homeworkflow: decline proposal"
	opExecute     = "homeworkflow: execute proposal"
	opConfirmSend = "homeworkflow: confirm sending"
	opConfirmDue  = "homeworkflow: confirm deadline"
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
	return s.propose(ctx, out, "", nil)
}

// propose is Propose; a non-empty format is the attachment the user chose for a send (ProposeSend). A non-nil want is
// the content the send must attach (ProposeEmailOfVersion): a validated send that pins another file is not stored and
// ErrVersionNotExported is returned.
func (s *Service) propose(ctx context.Context, out values.UntrustedOutput, format proposal.Format, want *proposal.Digest) (ProposalView, error) {
	now := s.clock.Now()
	p, err := proposal.New(s.ids.NewProposalID(), out, now)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if format != "" {
		if err := p.ChooseFormat(format); err != nil {
			return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
		}
	}

	facts, err := s.facts(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if err := p.Validate(facts, now); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if want != nil && p.Status() == proposal.StatusValidated && !p.Attaches(*want) {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, ErrVersionNotExported)
	}

	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.proposals.Save(ctx, p) }); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	view, err := s.proposalView(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	return view, nil
}

// proposalView is the view of p with the email subject of a validated send, for the confirmation dialog. ConfirmSend
// carries it back, and Execute sends it only if it is still the subject the core builds (proposal.Facts.Subject).
func (s *Service) proposalView(ctx context.Context, p *proposal.Proposal) (ProposalView, error) {
	v := viewOf(p)
	if p.Kind() != proposal.KindSendHomework || p.Status() != proposal.StatusValidated {
		return v, nil
	}
	var err error
	if v.Subject, err = s.emailSubject(ctx, p.AssignmentID()); err != nil {
		return v, err
	}
	v.DraftVersion, err = s.pinnedVersion(ctx, p)
	return v, err
}

// emailSubject is the subject of the homework email of assignment id: its subject (correction, schedule or hint) and
// its deadline in the student's zone (values.HomeworkEmailSubject).
func (s *Service) emailSubject(ctx context.Context, id shared.AssignmentID) (string, error) {
	a, err := s.assignments.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return s.subjectOf(ctx, a)
}

func (s *Service) subjectOf(ctx context.Context, a *assignment.Assignment) (string, error) {
	l, err := s.lessons.label(ctx, a)
	if err != nil {
		return "", err
	}
	due := a.Details().Due()
	if !due.IsZero() {
		due = due.In(s.clock.Now().Location())
	}
	return values.HomeworkEmailSubject(l.Subject(), due), nil
}

// sendRequest is the canonical send_homework_email object: the only field the wire allows for the kind.
type sendRequest struct {
	Kind         proposal.Kind `json:"kind"`
	AssignmentID string        `json:"assignment_id"`
}

// ProposeSend is the user's request to email the exported homework of assignment a (PRD section 10 step 7) with the
// attachment in format (the text export or the Word export, TASK-094). It takes exactly the Propose path, so the same
// strict parse and the same validation against trusted state apply as to a model proposal: the result is VALIDATED with
// the recipient, the format and the content of that export file frozen, for the confirmation dialog, or REJECTED with
// a reason (unknown assignment, no recipient, nothing exported in that format). An unknown format wraps
// proposal.ErrInvalidProposal. No side effect happens here.
func (s *Service) ProposeSend(ctx context.Context, a shared.AssignmentID, format proposal.Format) (ProposalView, error) {
	if err := a.Validate(); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSend, err)
	}
	if !format.Valid() {
		return ProposalView{}, fmt.Errorf("%s: %w: unknown attachment format %q", opProposeSend, proposal.ErrInvalidProposal, string(format))
	}
	raw, err := json.Marshal(sendRequest{Kind: proposal.KindSendHomework, AssignmentID: a.String()})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSend, err)
	}
	// Wrapped as untrusted on purpose: the request then passes the one parser every proposal passes.
	return s.propose(ctx, values.NewUntrustedOutput(string(raw)), format, nil)
}

// facts reads the trusted local state a proposal is checked against, only what its kind needs. It runs outside a
// transaction because it reads a file.
func (s *Service) facts(ctx context.Context, p *proposal.Proposal) (proposal.Facts, error) {
	var f proposal.Facts

	switch a, err := s.assignments.Get(ctx, p.AssignmentID()); {
	case err == nil:
		f.AssignmentExists = true
		f.AssignmentTitle = a.Details().Title() // the export heading comes from the stored assignment, not the model
		if p.Kind().NeedsSubject() {
			if f.Subject, err = s.subjectOf(ctx, a); err != nil {
				return f, err
			}
		}
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
		d, err := s.exportDigest(ctx, p.AssignmentID(), p.AttachmentFormat())
		if err != nil {
			return f, err
		}
		f.Attachment = d
	}
	return f, nil
}

// exportDigest streams the export file of the assignment in format f through SHA-256; a missing file yields the zero
// digest.
func (s *Service) exportDigest(ctx context.Context, a shared.AssignmentID, f proposal.Format) (proposal.Digest, error) {
	ref, err := proposal.ExportRefOf(a, f)
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
	view, err := s.proposalView(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opGet, err)
	}
	return view, nil
}

// Confirm records the explicit user approval of a VALIDATED proposal (AC-14). Only the UI calls it, on a user action;
// nothing derived from model output does.
func (s *Service) Confirm(ctx context.Context, id proposal.ID) (ProposalView, error) {
	return s.change(ctx, opConfirm, id, (*proposal.Proposal).Confirm)
}

// ConfirmSend is the user's click on the confirmation of a send_homework_email proposal: Confirm, then Execute
// (AC-14). shownSubject is the email subject the dialog showed (ProposalView.Subject); if the subject the core builds
// differs by then (a correction or a new extraction), the proposal goes STALE and nothing is sent (hard rule 3).
// shownFormat is the attachment format the dialog showed (ProposalView.AttachmentFormat); another one than the
// proposal pinned makes it STALE too, so a changed choice is never sent silently (TASK-094).
// Repeating it with the same proposal is safe (hard rule 7): a CONFIRMED proposal whose Execute failed, maybe after
// the delivery was saved, is executed again under the same idempotency key, so no second email is created, and an
// EXECUTED one is returned as it is. Another kind yields ErrNotASend before any change; other statuses yield the
// error of proposal.Confirm (proposal.ErrInvalidTransition).
func (s *Service) ConfirmSend(ctx context.Context, id proposal.ID, shownSubject string, shownFormat proposal.Format) (ProposalView, error) {
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.proposals.Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Kind() != proposal.KindSendHomework {
			return fmt.Errorf("%w: %s", ErrNotASend, p.Kind())
		}
		changed, err := p.ConfirmOnce(s.clock.Now())
		if err != nil || !changed {
			return err // unchanged: confirmed by an earlier click, Execute repeats or returns the result
		}
		return s.proposals.Save(ctx, p)
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opConfirmSend, err)
	}
	return s.execute(ctx, id, &shown{subject: shownSubject, format: shownFormat})
}

// ConfirmDeadline is the user's click on «Да, в календарь» of a create_deadline_event proposal (TASK-127): Confirm,
// then Execute. Like ConfirmSend, repeating it is safe: a CONFIRMED proposal whose Execute failed is executed again
// (the calendar upserts the event of the assignment, so there is never a second one) and an EXECUTED one is returned
// as it is. Another kind yields ErrNotADeadline before any change; other statuses the error of proposal.Confirm.
func (s *Service) ConfirmDeadline(ctx context.Context, id proposal.ID) (ProposalView, error) {
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.proposals.Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Kind() != proposal.KindDeadlineEvent {
			return fmt.Errorf("%w: %s", ErrNotADeadline, p.Kind())
		}
		changed, err := p.ConfirmOnce(s.clock.Now())
		if err != nil || !changed {
			return err
		}
		return s.proposals.Save(ctx, p)
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opConfirmDue, err)
	}
	return s.execute(ctx, id, nil)
}

// shown is what the confirmation dialog of a send showed the user.
type shown struct {
	subject string
	format  proposal.Format
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
// The port is called outside the transaction (Tx forbids external I/O). Execute does not compare the email subject
// with a shown one: the user's path to an email is ConfirmSend.
func (s *Service) Execute(ctx context.Context, id proposal.ID) (ProposalView, error) {
	return s.execute(ctx, id, nil)
}

// execute is Execute; a non-nil seen is what the user confirmed (subject and format), checked by Recheck.
func (s *Service) execute(ctx context.Context, id proposal.ID, seen *shown) (ProposalView, error) {
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
	facts.ShownSubject, facts.ShownFormat = facts.Subject, p.AttachmentFormat()
	if seen != nil {
		facts.ShownSubject, facts.ShownFormat = seen.subject, seen.format
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

	if err := s.perform(ctx, p, facts); err != nil {
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
// The email subject is the one Recheck accepted (f); the lesson of a deadline event comes from trusted local state and
// the cleaned hints (TASK-092).
func (s *Service) perform(ctx context.Context, p *proposal.Proposal, f proposal.Facts) error {
	switch p.Kind() {
	case proposal.KindSendHomework:
		cmd, err := p.EmailCommand()
		if err != nil {
			return err
		}
		if cmd, err = cmd.WithSubject(f.Subject); err != nil {
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
		a, err := s.assignments.Get(ctx, p.AssignmentID())
		if err != nil {
			return err
		}
		l, err := s.lessons.label(ctx, a)
		if err != nil {
			return err
		}
		return s.calendar.UpsertDeadline(ctx, cmd.WithLesson(l))
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedKind, p.Kind())
}
