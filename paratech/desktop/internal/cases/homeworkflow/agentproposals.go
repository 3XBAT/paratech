package homeworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the agent's proposals.
const (
	opProposeDeadline = "homeworkflow: propose a deadline"
	opProposeVersion  = "homeworkflow: propose sending a draft version"
	opPinnedVersion   = "homeworkflow: version of a pinned attachment"
)

// maxWaitingScan bounds the waiting proposals looked through for the same deadline or send.
const maxWaitingScan = 200

// deadlineRequest is the canonical create_deadline_event object.
type deadlineRequest struct {
	Kind         proposal.Kind `json:"kind"`
	AssignmentID string        `json:"assignment_id"`
	Title        string        `json:"title"`
	Due          string        `json:"due"`
}

// ProposeDeadline proposes the deadline of assignment a for the calendar (the agent's propose_deadline and «Да, в
// календарь» of «Сегодня», TASK-127). Title and due come from the stored assignment, never from a model answer or a
// file. A proposal of the same deadline that already waits for «Да» is reused; the check is not atomic, so two
// concurrent calls may store two, which is harmless as the calendar upserts one event per assignment. It takes the
// Propose path (strict parse, validation against local state); the result is VALIDATED or REJECTED (due out of
// range). An assignment without a deadline yields ErrNoDue, a missing or deleted one shared.ErrNotFound. No side
// effect happens.
func (s *Service) ProposeDeadline(ctx context.Context, a shared.AssignmentID) (ProposalView, error) {
	if err := a.Validate(); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeDeadline, err)
	}
	asg, err := liveAssignment(ctx, s.assignments, a)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeDeadline, err)
	}
	d := asg.Details()
	if d.Due().IsZero() {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeDeadline, ErrNoDue)
	}
	due := d.Due().UTC()

	if s.waiting != nil {
		list, err := s.waiting.ListByStatus(ctx, proposal.StatusValidated, maxWaitingScan)
		if err != nil {
			return ProposalView{}, fmt.Errorf("%s: %w", opProposeDeadline, err)
		}
		for _, p := range list {
			if p.WaitsAsDeadline(a, d.Title(), due) {
				return s.proposalView(ctx, p)
			}
		}
	}

	raw, err := json.Marshal(deadlineRequest{
		Kind: proposal.KindDeadlineEvent, AssignmentID: a.String(), Title: d.Title(), Due: due.Format(time.RFC3339),
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeDeadline, err)
	}
	// Wrapped as untrusted on purpose, like ProposeSend: one parser for every proposal.
	return s.propose(ctx, values.NewUntrustedOutput(string(raw)), "", nil)
}

// ProposeEmailOfVersion proposes emailing draft version n of assignment a (the agent's propose_send, TASK-127). The
// agent never exports: the student's text export must already hold exactly that version, else ErrVersionNotExported
// and nothing is stored. The recipient comes from the settings and the attachment is pinned by its digest, as for
// ProposeSend; a send of the same content that already waits for «Да» is returned instead of a second one, so a
// replayed agent call never makes two sends. A REJECTED proposal (no recipient, no export) is stored and returned. A
// missing or deleted assignment, a missing draft or version wraps shared.ErrNotFound. No side effect happens.
func (s *Service) ProposeEmailOfVersion(ctx context.Context, a shared.AssignmentID, n int) (ProposalView, error) {
	if s.drafts == nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, ErrNoDrafts)
	}
	if err := a.Validate(); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, err)
	}
	if _, err := liveAssignment(ctx, s.assignments, a); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, err)
	}
	d, err := s.drafts.Get(ctx, a)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, err)
	}
	r, ok := d.Revision(n)
	if !ok {
		return ProposalView{}, fmt.Errorf("%s: draft version %d: %w", opProposeVersion, n, shared.ErrNotFound)
	}
	want, err := digestOf(r)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, err)
	}
	raw, err := json.Marshal(sendRequest{Kind: proposal.KindSendHomework, AssignmentID: a.String()})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeVersion, err)
	}
	return s.propose(ctx, values.NewUntrustedOutput(string(raw)), proposal.FormatText, &want)
}

// waitingSend is the VALIDATED send of p's assignment, format and recipient that pins want, or nil. Without the
// Waiting port every call stores a new proposal.
func (s *Service) waitingSend(ctx context.Context, p *proposal.Proposal, want proposal.Digest) (*proposal.Proposal, error) {
	if s.waiting == nil {
		return nil, nil
	}
	list, err := s.waiting.ListByStatus(ctx, proposal.StatusValidated, maxWaitingScan)
	if err != nil {
		return nil, err
	}
	for _, w := range list {
		if w.AssignmentID() == p.AssignmentID() && w.Attaches(want) && w.AttachmentFormat() == p.AttachmentFormat() &&
			w.Recipient() == p.Recipient() {
			return w, nil
		}
	}
	return nil, nil
}

// pinnedVersion is the draft version whose text export is exactly the attachment p pinned; 0 when none matches, for
// a Word attachment or without the drafts.
func (s *Service) pinnedVersion(ctx context.Context, p *proposal.Proposal) (int, error) {
	if s.drafts == nil || p.AttachmentFormat() != proposal.FormatText || p.AttachmentDigest().IsZero() {
		return 0, nil
	}
	d, err := s.drafts.Get(ctx, p.AssignmentID())
	if errors.Is(err, shared.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", opPinnedVersion, err)
	}
	revs := d.Revisions()
	for i := len(revs) - 1; i >= 0; i-- { // the newest of equal texts
		got, err := digestOf(revs[i])
		if err != nil {
			return 0, fmt.Errorf("%s: %w", opPinnedVersion, err)
		}
		if got == p.AttachmentDigest() {
			return revs[i].Number(), nil
		}
	}
	return 0, nil
}

// digestOf is the digest of the text export of a draft version: Drafts.ExportDraft writes exactly r.Markdown().
func digestOf(r homeworkdraft.Revision) (proposal.Digest, error) {
	text := r.Markdown()
	sum := sha256.Sum256([]byte(text))
	return proposal.NewDigest(hex.EncodeToString(sum[:]), int64(len(text)))
}
