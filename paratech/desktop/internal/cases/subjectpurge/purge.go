package subjectpurge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log and the errors.
const (
	opPurge       = "subjectpurge: purge removed subject"
	opPurged      = "subjectpurge: removed subject purged"
	opKeptInInbox = "subjectpurge: file of a purged subject moved to the inbox: a note of another subject or a work keeps it"
)

// ErrInvalidConfig is returned by New when a required port is missing.
var ErrInvalidConfig = errors.New("subjectpurge: invalid configuration")

// Deps are the ports of the Purger; Log is optional.
type Deps struct {
	Subjects   Subjects
	Notes      Notes
	TicketSets TicketSets
	Materials  Materials
	Blobs      Blobs
	Clock      Clock
	Log        *slog.Logger
}

// Purger erases the subjects removed longer than subject.RestoreWindow (ADR-0027).
type Purger struct {
	// mu runs one purge at a time: the startup cleanup, the daily loop and the library's name check may overlap.
	mu sync.Mutex

	subjects   Subjects
	notes      Notes
	ticketSets TicketSets
	materials  Materials
	blobs      Blobs
	clock      Clock
	log        *slog.Logger
}

// New builds the Purger; every port but Log is required.
func New(d Deps) (*Purger, error) {
	if d.Subjects == nil || d.Notes == nil || d.TicketSets == nil || d.Materials == nil || d.Blobs == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Purger{
		subjects: d.Subjects, notes: d.Notes, ticketSets: d.TicketSets, materials: d.Materials, blobs: d.Blobs,
		clock: d.Clock, log: log,
	}, nil
}

// Purge erases every removed subject whose restore window has ended (subject.PurgeDue) and returns how many were
// erased completely. A failure of one subject does not stop the others; the failures are returned joined, and the
// next run finishes what is left.
func (p *Purger) Purge(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.clock.Now()
	list, err := p.subjects.ListRemoved(ctx)
	if err != nil {
		return 0, fmt.Errorf("%s: list: %w", opPurge, err)
	}
	cutoff := subject.PurgeCutoff(now)

	purged := 0
	var errs []error
	for _, s := range list {
		if err := ctx.Err(); err != nil {
			return purged, errors.Join(append(errs, err)...)
		}
		if !s.PurgeDue(now) {
			continue
		}
		done, err := p.purge(ctx, s.ID(), cutoff)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s %s: %w", opPurge, s.ID(), err))
		case done:
			purged++
			p.log.Info(opPurged, slog.String("subject", string(s.ID())))
		}
	}
	return purged, errors.Join(errs...)
}

// purge erases subject id step by step, the subject row last; done is false when the store no longer had it due (a
// restore that won the race, or gone already).
func (p *Purger) purge(ctx context.Context, id shared.SubjectID, cutoff time.Time) (bool, error) {
	if err := p.purgeNotes(ctx, id, cutoff); err != nil {
		return false, fmt.Errorf("notes: %w", err)
	}
	if err := p.purgeTicketSets(ctx, id, cutoff); err != nil {
		return false, fmt.Errorf("ticket sets: %w", err)
	}
	if err := p.purgeMaterials(ctx, id, cutoff); err != nil {
		return false, fmt.Errorf("files: %w", err)
	}
	done, err := p.subjects.PurgeRemoved(ctx, id, cutoff)
	if err != nil {
		return false, fmt.Errorf("subject: %w", err)
	}
	return done, nil
}

// purgeNotes deletes the notes of sub with their parts, one note per transaction.
func (p *Purger) purgeNotes(ctx context.Context, sub shared.SubjectID, cutoff time.Time) error {
	ids, err := p.notes.ListIDsBySubject(ctx, sub)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := p.notes.PurgeOfRemovedSubject(ctx, id, sub, cutoff); err != nil {
			return fmt.Errorf("note %s: %w", id, err)
		}
	}
	return nil
}

// purgeTicketSets deletes the ticket sets of sub, one per transaction.
func (p *Purger) purgeTicketSets(ctx context.Context, sub shared.SubjectID, cutoff time.Time) error {
	ids, err := p.ticketSets.ListOfSubject(ctx, sub)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := p.ticketSets.PurgeOfRemovedSubject(ctx, id, sub, cutoff); err != nil {
			return fmt.Errorf("set %s: %w", id, err)
		}
	}
	return nil
}

// purgeMaterials deletes or moves out every file filed under sub. A failing file does not stop the others; the
// subject row then stays (its files still hold it) until the next run.
func (p *Purger) purgeMaterials(ctx context.Context, sub shared.SubjectID, cutoff time.Time) error {
	list, err := p.materials.ListFiledUnder(ctx, sub)
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range list {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := p.purgeMaterial(ctx, m, sub, cutoff); err != nil {
			errs = append(errs, fmt.Errorf("material %s: %w", m.ID(), err))
		}
	}
	return errors.Join(errs...)
}

// purgeMaterial erases one file of sub: the stored bytes first (when nothing else uses them), then the row, which the
// store deletes only if it is still filed under sub and no note holds it. A crash or a failed delete in between leaves
// the row, so the next run finds it and deletes the bytes again (a missing file is no error).
//
// A file that a note of another subject holds, or that a work was written from, is kept and moved to «Входящие»
// (ADR-0027 keeps notes of other subjects whole and works out of the purge; works.material_id would cascade).
//
// The subject is re-checked right before the bytes go: a restore after the listing (a clock set back) keeps them.
// One race is left: the same bytes uploaded again while the bytes are deleted can make the import keep the material
// (it moves a hidden one to «Входящие») with its file gone; the store's re-check then keeps the row, and the next
// upload of those bytes writes the file again (library.Service.Import repairs a missing file).
func (p *Purger) purgeMaterial(ctx context.Context, m *material.Material, sub shared.SubjectID, cutoff time.Time) error {
	kept, err := p.materials.KeptElsewhere(ctx, m.ID(), sub)
	if err != nil {
		return err
	}
	if kept {
		return p.moveToInbox(ctx, m)
	}

	used, err := p.materials.DigestInUse(ctx, m.Digest(), m.ID())
	if err != nil {
		return err
	}
	if !used {
		due, err := p.subjects.StillDue(ctx, sub, cutoff)
		if err != nil {
			return err
		}
		if !due {
			return nil
		}
		if err := p.deleteBlobs(ctx, m.Digest()); err != nil {
			return err
		}
	}
	if _, err := p.materials.PurgeFiledUnderRemoved(ctx, m.ID(), sub, cutoff); err != nil {
		return err
	}
	return nil
}

// deleteBlobs deletes the cached text (derived) and then the stored bytes with sha256 d.
func (p *Purger) deleteBlobs(ctx context.Context, d material.Digest) error {
	text, err := material.TextRef(d)
	if err != nil {
		return err
	}
	blob, err := material.BlobRef(d)
	if err != nil {
		return err
	}
	for _, ref := range []shared.ArtifactRef{text, blob} {
		if err := p.blobs.Delete(ctx, ref); err != nil {
			return fmt.Errorf("delete a file: %w", err)
		}
	}
	return nil
}

// moveToInbox moves m to «Входящие» and keeps its status: a removed file is restored only for the move (the aggregate
// moves active files only) and removed again, so it still waits for «Вернуть» there.
func (p *Purger) moveToInbox(ctx context.Context, m *material.Material) error {
	now := p.clock.Now()
	removed := !m.Active()
	if removed {
		if _, err := m.Restore(now); err != nil {
			return err
		}
	}
	if _, err := m.MoveToInbox(now); err != nil {
		return err
	}
	if removed {
		if _, err := m.Remove(now); err != nil {
			return err
		}
	}
	if err := p.materials.Save(ctx, m); err != nil {
		return err
	}
	p.log.Info(opKeptInInbox, slog.String("material", string(m.ID())))
	return nil
}
