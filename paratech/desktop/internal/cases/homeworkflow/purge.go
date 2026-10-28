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
	maxPurgeBatch = 200
	// maxPurgeDeliveries bounds the deliveries read for one assignment; one that has more is skipped, since an
	// unresolved one could hide behind the limit.
	maxPurgeDeliveries = 1000
)

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
// removed completely. Each step is its own transaction and idempotent, and the assignment row goes last: a failure or
// a crash in the middle leaves a still deleted (invisible) assignment that the next run finishes. One with an
// unresolved delivery (PENDING, SENDING, UNKNOWN) is skipped and logged, never purged (hard rule 7). A failure of one
// assignment does not stop the others; the failures are returned joined.
func (p *Purger) PurgeDeleted(ctx context.Context) (int, error) {
	now := p.clock.Now()
	cutoff := assignment.PurgeCutoff(now)
	list, err := p.assignments.ListDeletedBefore(ctx, cutoff, maxPurgeBatch)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", opPurge, err)
	}

	purged := 0
	var errs []error
	for _, a := range list {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
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
	return purged, errors.Join(errs...)
}

// purge runs the steps for assignment id; done is false when it was skipped or its row is no longer due.
func (p *Purger) purge(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	files, ok, err := p.filesOf(ctx, id)
	if err != nil || !ok {
		return false, err
	}
	// The files go first: their refs are read from the delivery rows removed below.
	for _, ref := range files {
		if err := p.files.Delete(ctx, ref); err != nil {
			return false, fmt.Errorf("delete a file: %w", err)
		}
	}

	var done bool
	steps := []struct {
		name string
		run  func(ctx context.Context) error
	}{
		{"proposals", func(ctx context.Context) error {
			_, err := p.proposals.DeleteOfAssignment(ctx, id)
			return err
		}},
		{"deliveries", func(ctx context.Context) error {
			_, err := p.deliveries.DeleteFinalOfAssignment(ctx, id)
			return err
		}},
		{"draft", func(ctx context.Context) error { return p.drafts.DeleteOfAssignment(ctx, id) }},
		{"works", func(ctx context.Context) error {
			_, err := p.works.DeleteOfAssignment(ctx, id)
			return err
		}},
		{"assignment", func(ctx context.Context) error {
			var err error
			done, err = p.assignments.PurgeDeleted(ctx, id, cutoff)
			return err
		}},
	}
	for _, s := range steps {
		if err := p.tx.Do(ctx, s.run); err != nil {
			return false, fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return done, nil
}

// filesOf returns the files of assignment id to delete before its rows: the text and Word exports and the attachments
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
