package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

const opPurge = "homeworkflow: purge deleted assignment"

// Bounds of one purge run. What is left over goes in the next run (the next start or the next day).
const (
	// purgePage is the page of the keyset listing: a skipped or failing assignment never hides the later ones.
	purgePage = 100
	// maxPurgeExamined bounds the assignments one run looks at.
	maxPurgeExamined = 2000
	// maxPurgeDeliveries bounds the deliveries read for one assignment; one that has more is skipped, since an
	// unresolved one could hide behind the limit.
	maxPurgeDeliveries = 1000
)

// errPurgeKept rolls the purge transaction of one assignment back without an error for the run: it is no longer due
// (restored, or deleted again later, e.g. after the clock was set back) or it is kept for now (logged).
var errPurgeKept = errors.New("assignment kept")

// Purger removes the assignments deleted longer than assignment.RestoreWindow, completely (ADR-0029 addendum 2):
// proposals, the export files and the email attachments, deliveries, the draft with its versions, works, then the
// assignment row. Kept: agent journal lines (IDs only) and a deadline event removal still waiting for the sync.
type Purger struct {
	assignments DeletedAssignments
	proposals   PurgedProposals
	deliveries  PurgedDeliveries
	drafts      PurgedDrafts
	works       PurgedWorks
	files       FileRemover
	clock       Clock
	tx          Tx
	log         *slog.Logger
}

// NewPurger builds the Purger; every port but Log is required.
func NewPurger(d PurgeDeps) (*Purger, error) {
	if d.Assignments == nil || d.Proposals == nil || d.Deliveries == nil || d.Drafts == nil || d.Works == nil ||
		d.Files == nil || d.Clock == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every purge port is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Purger{
		assignments: d.Assignments, proposals: d.Proposals, deliveries: d.Deliveries, drafts: d.Drafts, works: d.Works,
		files: d.Files, clock: d.Clock, tx: d.Tx, log: log,
	}, nil
}

// PurgeDeleted removes every assignment whose restore window ended (assignment.PurgeDue) and returns how many were
// removed completely. The listing goes by keyset pages, so the assignments skipped or failing in this run never hide
// the later ones. One with an unresolved delivery (PENDING, SENDING, UNKNOWN) is skipped and logged, never purged
// (hard rule 7). A failure of one assignment does not stop the others; the failures are returned joined.
func (p *Purger) PurgeDeleted(ctx context.Context) (int, error) {
	now := p.clock.Now()
	cutoff := assignment.PurgeCutoff(now)

	purged := 0
	var errs []error
	var afterAt time.Time
	var afterID shared.AssignmentID
	for examined := 0; examined < maxPurgeExamined; {
		limit := min(purgePage, maxPurgeExamined-examined)
		page, err := p.assignments.ListDeletedBefore(ctx, cutoff, afterAt, afterID, limit)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", opPurge, err))
			break
		}
		for _, a := range page {
			if err := ctx.Err(); err != nil {
				return purged, errors.Join(append(errs, err)...)
			}
			examined++
			afterAt, afterID = a.DeletedAt(), a.ID()
			if !a.PurgeDue(now) {
				continue
			}
			done, err := p.purge(ctx, a.ID(), cutoff)
			switch {
			case err != nil: // the next run tries again
				errs = append(errs, fmt.Errorf("%s %s: %w", opPurge, a.ID(), err))
			case done:
				purged++
			}
		}
		if len(page) < limit {
			break
		}
	}
	return purged, errors.Join(errs...)
}

// purge removes assignment id in one write transaction; done is false when it was kept (no longer due, or skipped).
//
// Order: re-check, read the file refs, the child rows, the assignment row, then the files, all before the commit. A
// restore takes the same write lock, so it either commits before (the re-check, and the guard every delete repeats,
// see it and nothing is touched) or after (the row is gone: not found). The files go after the rows: a delete that
// fails rolls the rows back, and a failed commit or a crash after the files leaves the assignment deleted and due with
// some files missing, which the next run (the startup cleanup comes first) finishes; the refs are read again from the
// delivery rows that the rollback kept.
func (p *Purger) purge(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	err := p.tx.Do(ctx, func(ctx context.Context) error {
		due, err := p.assignments.StillDeletedBefore(ctx, id, cutoff)
		if err != nil {
			return fmt.Errorf("re-check: %w", err)
		}
		if !due {
			return errPurgeKept
		}
		files, ok, err := p.filesOf(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			return errPurgeKept
		}

		if _, err := p.proposals.DeleteOfAssignment(ctx, id, cutoff); err != nil {
			return fmt.Errorf("proposals: %w", err)
		}
		if _, err := p.deliveries.DeleteFinalOfAssignment(ctx, id, cutoff); err != nil {
			return fmt.Errorf("deliveries: %w", err)
		}
		if _, err := p.drafts.DeleteOfAssignment(ctx, id, cutoff); err != nil {
			return fmt.Errorf("draft: %w", err)
		}
		if _, err := p.works.DeleteOfAssignment(ctx, id, cutoff); err != nil {
			return fmt.Errorf("works: %w", err)
		}
		done, err := p.assignments.PurgeDeleted(ctx, id, cutoff)
		if err != nil {
			return fmt.Errorf("assignment: %w", err)
		}
		if !done { // the guard of the row failed after the re-check passed: undo the deletes above
			return errPurgeKept
		}

		for _, ref := range files {
			if err := p.files.Delete(ctx, ref); err != nil {
				return fmt.Errorf("delete a file: %w", err)
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, errPurgeKept):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// filesOf returns the files of assignment id to delete with its rows: the text and Word exports and the attachments
// of its emails (the copies of a work's file). Only files below exports are deleted: a library material stays. ok is
// false, with a log line, when a delivery is unresolved or there are too many to check: the assignment is skipped.
func (p *Purger) filesOf(ctx context.Context, id shared.AssignmentID) (refs []shared.ArtifactRef, ok bool, err error) {
	ds, err := p.deliveries.ListByAssignment(ctx, id, maxPurgeDeliveries)
	if err != nil {
		return nil, false, fmt.Errorf("list deliveries: %w", err)
	}
	if len(ds) >= maxPurgeDeliveries {
		p.log.Warn("deleted assignment kept: too many deliveries to check", slog.String("assignment", id.String()))
		return nil, false, nil
	}

	seen := make(map[shared.ArtifactRef]bool)
	add := func(ref shared.ArtifactRef) {
		if !ref.IsZero() && ref.Kind() == shared.ArtifactExports && !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	for _, d := range ds {
		if d.Status().IsUnresolved() {
			p.log.Warn("deleted assignment kept: an email of it is unresolved",
				slog.String("assignment", id.String()), slog.String("delivery", d.ID().String()),
				slog.String("status", d.Status().String()))
			return nil, false, nil
		}
		add(d.Attachment().Ref())
	}
	for _, f := range []proposal.Format{proposal.FormatText, proposal.FormatDocx} {
		ref, err := proposal.ExportRefOf(id, f)
		if err != nil {
			return nil, false, err
		}
		add(ref)
	}
	return refs, true, nil
}
