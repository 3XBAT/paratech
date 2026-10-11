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
// delivery (AC-14); it never receives model output directly (hard rule 3, AC-18). Credentials are read by the
// adapter from the credential store, never from SQLite (hard rule 4).
//
// Outcome errors are the sentinels of the delivery package: delivery.ErrOutcomeUnknown, delivery.ErrRejected and
// delivery.ErrTransient. Anything else is treated as transient only if the adapter can prove nothing was handed over.
type EmailAdapter interface {
	// Send delivers the email described by d with the attachment content. The Message-ID of d is used so that
	// the message can be found later (PRD section 10, hard rule 7). An ambiguous result, such as a timeout
	// after the message may have been accepted, is reported as delivery.ErrOutcomeUnknown and must not be retried
	// before Reconcile (PRD section 14 case 7, AC-15).
	Send(ctx context.Context, d *delivery.Delivery, attachment io.Reader) error

	// Reconcile finds out whether the message of d was accepted by the provider. It returns true if the message is
	// known to have been accepted and false if it is known not to have been; if that cannot be determined it returns
	// delivery.ErrOutcomeUnknown (PRD section 10: reconcile before retry, AC-15).
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

	// Save inserts or updates a delivery, including its status and the last error (PRD section 10, AC-15). Inserting
	// a second delivery with an existing idempotency key returns an error wrapping shared.ErrConflict.
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
type CredentialStore interface {
	// Get returns the secret stored under name. It returns an error wrapping shared.ErrNotFound if there is none
	// (PRD section 12).
	Get(ctx context.Context, name string) (secret string, err error)

	// Set stores or replaces the secret under name (PRD section 12).
	Set(ctx context.Context, name, secret string) error

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

	// NewJobID returns a fresh job identifier for the delivery job (PRD section 7.1).
	NewJobID() shared.JobID
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary is one use case method call and it changes
// one aggregate. Implemented by the sqlite adapter (TASK-005).
type Tx interface {
	// Do runs fn in a database transaction and commits if it returns nil, otherwise it rolls back and returns
	// the error (PRD section 7.3: persistent state transitions). Repositories called with the context passed to
	// fn take part in the transaction; calls with another context do not.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
