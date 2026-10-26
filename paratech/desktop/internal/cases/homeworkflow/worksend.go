package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"path"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the send of a work.
const (
	opProposeSendWork = "homeworkflow: propose sending a work"
	opConfirmSendWork = "homeworkflow: confirm sending a work"
	opCopyWorkFile    = "homeworkflow: copy the file of a work"
)

// ProposeSendWork is the student's «Отправить преподавателю» on a finished turnkey work (TASK-180); the agent never
// calls it. Only a work of an assignment may be sent: another one yields work.ErrNoAssignment (wrapped) and nothing is
// stored. The proposal (class C, ADR-0021) attaches the work's file in the library (.docx, a presentation .pptx),
// pinned by its stored reference, SHA-256 and size; the recipient comes from the settings and the subject from the
// assignment, as for the homework send. The result is VALIDATED or REJECTED (no recipient, no finished file, the file
// moved out of «ДЗ»); a send of the same file that already waits for «Да» is returned instead of a second one. A
// missing work wraps shared.ErrNotFound. Nothing is sent: only ConfirmSendWork sends.
func (s *Service) ProposeSendWork(ctx context.Context, id shared.WorkID) (ProposalView, error) {
	if s.works == nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, ErrNoWorks)
	}
	if err := id.Validate(); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	w, err := s.works.Work(ctx, id)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	if err := w.Sendable(); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	format := proposal.FormatDocx
	if w.Presentation() {
		format = proposal.FormatPptx
	}

	now := s.clock.Now()
	p, err := proposal.NewWorkSend(s.ids.NewProposalID(), w.Source().Assignment, id, format, now)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	facts, _, err := s.facts(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	if err := p.Validate(facts, now); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	if p.Status() == proposal.StatusValidated && s.waiting != nil {
		// The check is not atomic; two concurrent presses may store two, and each still needs its own «Да».
		list, err := s.waiting.ListByStatus(ctx, proposal.StatusValidated, maxWaitingScan)
		if err != nil {
			return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
		}
		for _, o := range list {
			if o.WaitsAsWorkSend(p) {
				return s.proposalView(ctx, o)
			}
		}
	}

	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.proposals.Save(ctx, p) }); err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	view, err := s.proposalView(ctx, p)
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opProposeSendWork, err)
	}
	return view, nil
}

// ConfirmSendWork is the student's «Да» on the send of a work: Confirm, then Execute, as ConfirmSend. shownSubject and
// shownFormat are what the card showed; another subject or format by now, a changed, replaced, deleted or moved file,
// or another recipient makes the proposal STALE and nothing is sent. Before the send the file is copied under its
// library name into exports, and the email attaches that copy, keyed by the proposal (hard rule 7): a repeat sends no
// second email, and an email whose outcome is unknown is never sent again here. Another kind yields ErrNotASend before
// any change; other statuses the error of proposal.Confirm.
func (s *Service) ConfirmSendWork(ctx context.Context, id proposal.ID, shownSubject string, shownFormat proposal.Format) (ProposalView, error) {
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.proposals.Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Kind() != proposal.KindSendWork {
			return fmt.Errorf("%w: %s", ErrNotASend, p.Kind())
		}
		changed, err := p.ConfirmOnce(s.clock.Now())
		if err != nil || !changed {
			return err
		}
		return s.proposals.Save(ctx, p)
	})
	if err != nil {
		return ProposalView{}, fmt.Errorf("%s: %w", opConfirmSendWork, err)
	}
	return s.execute(ctx, id, &shown{subject: shownSubject, format: shownFormat})
}

// workFile is what the send of a work reads about the work's file now.
type workFile struct {
	digest proposal.Digest    // its content; zero when there is no finished file to send
	ref    shared.ArtifactRef // the stored file the work points at; zero with the digest
	filed  bool               // in «ДЗ», or in «Входящие» where a work without a subject leaves it
	name   string             // its library name
}

// workFileOf reads the finished file of work id in format f. Anything that leaves no file to send (no works wired, no
// work, not done, the material gone or removed, another type, a stored file missing or not its content) is the zero
// workFile, which the proposal rejects or goes stale on; only a failed read is an error.
func (s *Service) workFileOf(ctx context.Context, id shared.WorkID, f proposal.Format) (workFile, error) {
	if s.works == nil {
		return workFile{}, nil
	}
	w, err := s.works.Work(ctx, id)
	if errors.Is(err, shared.ErrNotFound) {
		return workFile{}, nil
	}
	if err != nil {
		return workFile{}, err
	}
	wr, ok := w.Writing()
	if !ok || wr.File == nil || w.Status() != work.StatusDone {
		return workFile{}, nil
	}
	m, err := s.works.Material(ctx, wr.File.Material)
	if errors.Is(err, shared.ErrNotFound) {
		return workFile{}, nil
	}
	if err != nil {
		return workFile{}, err
	}
	ref := wr.File.ArtifactRef()
	stored, err := material.BlobRef(m.Digest())
	if err != nil {
		return workFile{}, err
	}
	if !m.Active() || string(m.Type()) != string(f) || ref != stored {
		return workFile{}, nil
	}

	d, err := s.fileDigest(ctx, ref)
	if err != nil {
		return workFile{}, err
	}
	if d.SHA256() != m.Digest().String() || d.Size() != m.Size() {
		return workFile{}, nil // missing or damaged: not the content the library recorded
	}
	filed, err := s.filed(ctx, m, wr.Snapshot.Subject)
	if err != nil {
		return workFile{}, err
	}
	return workFile{digest: d, ref: ref, filed: filed, name: m.Name()}, nil
}

// filed reports whether m is where the writing of a work of subject sub filed it (library.FileHomework): in «ДЗ» of a
// subject, or in «Входящие» when the work has no subject or its subject is gone.
func (s *Service) filed(ctx context.Context, m *material.Material, sub shared.SubjectID) (bool, error) {
	subjects, err := s.works.Subjects(ctx)
	if err != nil {
		return false, err
	}
	homework := func(id shared.SubjectID) (shared.FolderID, bool) {
		for _, x := range subjects {
			if x.ID() == id {
				f, ok := x.SystemFolder(subject.SystemHomework)
				return f.ID(), ok
			}
		}
		return "", false
	}
	if m.InInbox() {
		_, kept := homework(sub)
		return sub == "" || !kept, nil
	}
	folder, ok := homework(m.Subject())
	return ok && folder == m.Folder(), nil
}

// copyWorkFile writes the copy cmd attaches from the stored file p pinned, unless an earlier try wrote it already. The
// mailer checks the copy against the pinned digest, so a copy that does not match is never sent.
func (s *Service) copyWorkFile(ctx context.Context, p *proposal.Proposal, cmd proposal.EmailCommand) error {
	d, err := s.fileDigest(ctx, cmd.Attachment())
	if err != nil {
		return fmt.Errorf("%s: %w", opCopyWorkFile, err)
	}
	if d == cmd.AttachmentDigest() {
		return nil
	}
	src, err := p.AttachmentSource()
	if err != nil {
		return fmt.Errorf("%s: %w", opCopyWorkFile, err)
	}
	rc, err := s.artifacts.Open(ctx, src)
	if errors.Is(err, shared.ErrNotFound) {
		return fmt.Errorf("%s: %w: the file of the work is gone", opCopyWorkFile, proposal.ErrStale)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", opCopyWorkFile, err)
	}
	defer func() { _ = rc.Close() }() // read-only: a close error loses nothing
	if err := s.artifacts.Write(ctx, cmd.Attachment(), rc); err != nil {
		return fmt.Errorf("%s: %w", opCopyWorkFile, err)
	}
	return nil
}

// workAttachmentName is the name the file of the send of a work goes under (proposal.CopyName of its library name);
// empty when the work or its file is gone.
func (s *Service) workAttachmentName(ctx context.Context, p *proposal.Proposal) (string, error) {
	if s.works == nil {
		return "", nil
	}
	w, err := s.works.Work(ctx, p.WorkID())
	if errors.Is(err, shared.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	wr, ok := w.Writing()
	if !ok || wr.File == nil {
		return "", nil
	}
	m, err := s.works.Material(ctx, wr.File.Material)
	if errors.Is(err, shared.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	ref, err := p.WorkCopyRef(m.Name())
	if err != nil {
		return "", err
	}
	return path.Base(ref.Path()), nil
}
