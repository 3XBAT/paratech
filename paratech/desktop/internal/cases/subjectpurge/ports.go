package subjectpurge

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Every Purge… method below deletes only while the subject is still removed at or before cutoff
// (subject.PurgeCutoff), checked in the same statement; false means nothing was deleted (gone already, restored, or
// no longer matching). Each call is its own transaction.

// Subjects reads the removed subjects and deletes one at the end. Implemented by the sqlite adapter.
type Subjects interface {
	// ListRemoved returns the removed subjects.
	ListRemoved(ctx context.Context) ([]*subject.Subject, error)

	// PurgeRemoved deletes the subject row with its aliases, folders and teachers. A file or note still under it wraps
	// shared.ErrConflict.
	PurgeRemoved(ctx context.Context, id shared.SubjectID, cutoff time.Time) (bool, error)

	// StillDue reports whether subject id is still removed at or before cutoff: the check right before stored bytes,
	// which no transaction can bring back, are deleted.
	StillDue(ctx context.Context, id shared.SubjectID, cutoff time.Time) (bool, error)
}

// Notes deletes the student's notes of a removed subject with their parts. Implemented by the sqlite adapter.
type Notes interface {
	// ListIDsBySubject returns the notes of sub in any status, also while sub is removed.
	ListIDsBySubject(ctx context.Context, sub shared.SubjectID) ([]shared.NoteID, error)

	// PurgeOfRemovedSubject deletes note id of sub with its parts (the materials stay).
	PurgeOfRemovedSubject(ctx context.Context, id shared.NoteID, sub shared.SubjectID, cutoff time.Time) (bool, error)
}

// TicketSets deletes the ticket sets of a removed subject: they name it without a foreign key and answer from its
// lessons, so they would be left dangling. Implemented by the sqlite adapter.
type TicketSets interface {
	ListOfSubject(ctx context.Context, sub shared.SubjectID) ([]shared.TicketSetID, error)
	PurgeOfRemovedSubject(ctx context.Context, id shared.TicketSetID, sub shared.SubjectID, cutoff time.Time) (bool, error)
}

// Materials reads and deletes the files filed under a removed subject. Implemented by the sqlite adapter.
type Materials interface {
	// ListFiledUnder returns the materials filed in any folder of sub, in any status, also while sub is removed.
	ListFiledUnder(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error)

	// KeptElsewhere reports whether material id must survive the purge of sub: a note (any status) of another subject
	// holds it as a part, or a work was written from it (ADR-0027 keeps works).
	KeptElsewhere(ctx context.Context, id shared.MaterialID, sub shared.SubjectID) (bool, error)

	// DigestInUse reports whether the stored bytes with sha256 d (and their cached text) are still used by anything
	// other than material except: another material, or a send of a work not yet carried out that pinned them.
	DigestInUse(ctx context.Context, d material.Digest, except shared.MaterialID) (bool, error)

	// Save writes a material moved to «Входящие» under its version: a stale one wraps shared.ErrVersionConflict.
	Save(ctx context.Context, m *material.Material) error

	// PurgeFiledUnderRemoved deletes the row of material id while it is still filed under sub, no note holds it as a
	// part and no work was written from it. Its lesson links and inspection go with it; summaries and assignments made
	// from it stay (their material becomes NULL).
	PurgeFiledUnderRemoved(ctx context.Context, id shared.MaterialID, sub shared.SubjectID, cutoff time.Time) (bool, error)
}

// Blobs deletes stored files (hard rule 5). Implemented by the filestore adapter.
type Blobs interface {
	// Delete removes a stored file; a missing one is not an error.
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// Clock supplies the current time (ARCH-24); the app wires the clock of the library, so «Вернуть» and the purge agree
// on the end of the window.
type Clock interface {
	Now() time.Time
}
