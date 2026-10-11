package senddelivery

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// EmailAdapter isolates the mail provider (SMTP) from the core (PRD sections 4.1 and 4.3). Implemented by the smtp
// adapter (TASK-044) and by a fake SMTP server in tests. It is called only after the user explicitly confirmed the
// delivery (AC-14); it never receives model output directly (hard rule 3, AC-18).
//
// Credentials: the smtp adapter reads the mail credentials itself, from the same Windows Credential Manager entry
// that CredentialStore writes (the app wires both to the wincred adapter). They never pass through the core and are
// never stored in SQLite (hard rule 4, ARCH-34).
//
// Outcome errors are the sentinels of the delivery package: delivery.ErrOutcomeUnknown, delivery.ErrRejected and
// delivery.ErrTransient. Anything else is treated as transient only if the adapter can prove nothing was handed over.
type EmailAdapter interface {
	// Send delivers the email described by d with the attachment content. The Message-ID of d (generated before
	// sending through IDGenerator.NewMessageID) is used so that the message can be found later (PRD section 10,
	// hard rule 7). The use case saves the delivery as SENDING before calling Send, so a crash during the call leaves
	// SENDING, which startup recovery turns into UNKNOWN. Results: nil means accepted (SENDING to SENT);
	// delivery.ErrTransient means definitely not accepted and the delivery returns SENDING to PENDING for a later
	// attempt; delivery.ErrRejected is permanent (FAILED); an ambiguous result, such as a timeout after the message
	// may have been accepted, is delivery.ErrOutcomeUnknown (UNKNOWN) and must not be retried before Reconcile
	// (PRD section 14 case 7, AC-15).
	Send(ctx context.Context, d *delivery.Delivery, attachment io.Reader) error

	// Reconcile tries to find out whether the message of d was accepted by the provider, by its Message-ID. It is
	// best effort: SMTP cannot look messages up, so the adapter can only answer when the provider offers a way to
	// check, for example IMAP access to the Sent folder, which may require extra configuration. It returns true if
	// the message is known to have been accepted and false if it is known not to have been; if that cannot be
	// determined it returns delivery.ErrOutcomeUnknown (PRD section 10: reconcile before retry, AC-15).
	//
	// Manual resolution is a use case concern, not an adapter one: while Reconcile cannot decide, the delivery stays
	// UNKNOWN and is never retried automatically. The use case offers the user two explicit actions, each applied
	// through a method of the delivery aggregate (TASK-044): "mark as sent" after checking the mailbox
	// (UNKNOWN to SENT), or "send again" accepting the risk of a duplicate (UNKNOWN to PENDING).
	Reconcile(ctx context.Context, d *delivery.Delivery) (accepted bool, err error)
}

// DeliveryRepository stores deliveries in the local database (PRD sections 5.1 and 10: persist the delivery result,
// AC-15). Implemented by the sqlite adapter.
type DeliveryRepository interface {
	// Get loads a delivery (PRD section 10). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.DeliveryID) (*delivery.Delivery, error)

	// GetByIdempotencyKey loads the delivery created under the key, so that repeating the request finds the first
	// one instead of creating another (PRD section 7.3, hard rule 7). It returns an error wrapping
	// shared.ErrNotFound if there is none.
	GetByIdempotencyKey(ctx context.Context, key string) (*delivery.Delivery, error)

	// Save inserts or updates a delivery, including its status and the last error (PRD section 10, AC-15). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// Two different failures are told apart: a stale version yields shared.ErrVersionConflict (reload and decide
	// again), while inserting a second delivery with an existing idempotency key yields shared.ErrDuplicate (load the
	// first one with GetByIdempotencyKey and continue with it). Both satisfy errors.Is(err, shared.ErrConflict); use
	// cases check the narrow sentinel.
	Save(ctx context.Context, d *delivery.Delivery) error

	// ListByStatus returns the deliveries in any of the given statuses, for example the UNKNOWN ones to reconcile or
	// the SENDING ones left by a crash (PRD section 14 case 7, hard rule 7).
	ListByStatus(ctx context.Context, statuses ...delivery.Status) ([]*delivery.Delivery, error)
}

// ArtifactStore is the access of this use case to the exported file that is attached. All file access goes through
// the Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact; the caller closes it. A missing artifact yields shared.ErrNotFound
	// (PRD section 10 step 6: the exported final artifact).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
}

// CredentialStore keeps the mail account credentials in Windows Credential Manager (PRD sections 4.3 and 12, AC-19,
// ARCH-34). Implemented by the wincred adapter (TASK-037) on Windows and by an in-memory store elsewhere.
// Secrets are never written to SQLite, files or logs (hard rule 4).
//
// The use case only writes: it stores or replaces the secret the user typed in the settings, removes it on
// revocation and asks whether one exists, to show the account as configured. It never reads a secret back; the smtp
// adapter reads it (see EmailAdapter). So there is no Get here, and no secret travels from the store into the core.
type CredentialStore interface {
	// Set stores or replaces the secret under name (PRD section 12).
	Set(ctx context.Context, name, secret string) error

	// Exists reports whether a secret is stored under name, without revealing it (PRD section 12).
	Exists(ctx context.Context, name string) (bool, error)

	// Delete removes the secret under name. Removing a missing secret is not an error (PRD section 12: revocation).
	Delete(ctx context.Context, name string) error
}

// JobEnqueuer creates the delivery job after the user's confirmation (PRD section 10 step 8). Implemented by the
// sqlite adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID of the
	// existing job is returned with created == false, so a delivery is never sent twice (PRD section 7.3, hard rule 7).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// Clock supplies the current time (ARCH-24; PRD section 10). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 10: confirmation and sending times).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 10). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewDeliveryID returns a fresh delivery identifier (PRD section 10).
	NewDeliveryID() shared.DeliveryID

	// NewMessageID returns a fresh RFC 5322 Message-ID (for example "<random@host>"), generated before the first
	// send and stored on the delivery, so that an UNKNOWN outcome can be reconciled by it (PRD section 10, hard rule 7).
	// Randomness is a port (ARCH-24).
	NewMessageID() string

	// NewJobID returns a fresh job identifier for the delivery job (PRD section 7.1).
	NewJobID() shared.JobID
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary
// is one use case method call and it changes one aggregate. The two documented exceptions (ADR-0004) are the batch
// import of LessonRepository.ApplyChanges and the deletion of a recording together with its transcripts
// (RecordingTranscripts.DeleteByRecording and RecordingRepository.Delete in one Tx.Do). Implemented by the sqlite
// adapter (TASK-005).
type Tx interface {
	// Do runs fn in one write transaction and commits if fn returns nil, otherwise it rolls back and returns the
	// error (PRD section 7.3: persistent state transitions). The sqlite adapter opens a write transaction up front
	// (BEGIN IMMEDIATE), so a read inside fn cannot be invalidated by another writer before the commit.
	// Repositories called with the context passed to fn take part in the transaction; calls with another context
	// do not. Nesting: a Do called with the context of a running fn joins the outer transaction (no savepoint) and
	// only the outermost Do commits or rolls back. fn performs no external I/O (network, e-mail, calendar, files,
	// subprocesses): it holds the database write lock and only reads and writes local aggregates.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
