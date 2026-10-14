package modelcatalog

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// ModelRepository stores the installed STT models in the local database (PRD section 8: name, version, size,
// capabilities; several models at once). Implemented by the sqlite adapter.
type ModelRepository interface {
	// Get loads an installed model (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.SttModelID) (*sttmodel.Model, error)

	// List returns all installed models (PRD section 8: display installed models).
	List(ctx context.Context) ([]*sttmodel.Model, error)

	// Save inserts or updates an installed model (PRD section 8). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// Another installed model with the same name and version yields shared.ErrDuplicate (also errors.Is
	// shared.ErrConflict), which is a different cause from a stale version.
	Save(ctx context.Context, m *sttmodel.Model) error

	// Delete removes the model record (PRD section 8: model removal). Whether removal is allowed is decided by the
	// model aggregate and ModelPins before this is called. The pin check and this Delete must run inside the same
	// Tx.Do (a write transaction), otherwise a job enqueued between the check and the delete could pin a model that
	// is being removed (PRD section 14 case 11).
	//
	// Default model (ADR-0004): settings.default_stt_model_id references the model with ON DELETE RESTRICT, so the
	// database never clears the default behind the settings aggregate's back. If the model is the default, the use
	// case first clears the default through the settings aggregate and SettingsRepository.Save, as its own step in its
	// own Tx.Do (ARCH-23: one aggregate per transaction), and then runs the pin check and this Delete. The steps are
	// repeatable. If the user selects the model as default again in between, the database rejects the delete; the
	// adapter maps that foreign key violation to an error wrapping shared.ErrConflict, and the use case clears the
	// default again or reports that the model is in use. Rows of transcripts that reference the model keep their
	// copied model name and version and lose only the reference (ON DELETE SET NULL).
	Delete(ctx context.Context, id shared.SttModelID) error
}

// ModelPins answers whether an unfinished job pins a model, so that removal can be blocked
// (PRD section 8 diagram, section 14 case 11). Implemented by the sqlite adapter. The adapter holds no business rule
// about which job statuses count as unfinished: the use case passes job.ActiveStatuses() from the domain.
type ModelPins interface {
	// IsPinnedByJobInStatuses reports whether any job in one of the given statuses pins the model (PRD section 14
	// case 11: a job pins the model version, so removing a pinned model is unsafe). The caller passes
	// job.ActiveStatuses(). Call it inside the same Tx.Do as ModelRepository.Delete.
	IsPinnedByJobInStatuses(ctx context.Context, id shared.SttModelID, statuses []job.Status) (bool, error)
}

// SettingsRepository stores the settings aggregate; choosing the default model changes only this aggregate
// (PRD section 8, AC-07, ARCH-23). Implemented by the sqlite adapter.
type SettingsRepository interface {
	// Load returns the current settings, defaults applied (PRD section 15).
	Load(ctx context.Context) (*settings.Settings, error)

	// Save persists the settings (PRD section 8: select the active default model). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// The row is a singleton: Load without a stored row returns the defaults with Version() == 0 and the first Save
	// inserts it; two concurrent first saves resolve as one INSERT and one shared.ErrVersionConflict.
	Save(ctx context.Context, s *settings.Settings) error
}

// ArtifactStore is the access of this use case to model files. All file access goes through the Storage Manager
// (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact, for example to compute its SHA-256; the caller closes it. A missing
	// artifact yields shared.ErrNotFound (PRD section 8.1).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// Write stores the content atomically (a temporary file and a rename), for example an imported model file
	// (PRD sections 5.2 and 8.1).
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error

	// Size returns the size in bytes of an existing artifact, for example a partial download to resume. A missing
	// artifact yields shared.ErrNotFound (PRD section 8: resume support).
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)

	// Delete removes a file or a whole directory tree. Deleting what is already gone is not an error
	// (PRD section 8: model removal).
	Delete(ctx context.Context, ref shared.ArtifactRef) error

	// Append adds content to the end of a file, creating it, and flushes it; the bytes written before a failure
	// stay, so a partial download can resume (PRD section 8: resume support). It returns how many bytes it wrote,
	// also on error.
	Append(ctx context.Context, ref shared.ArtifactRef, content io.Reader) (int64, error)

	// Rename moves a file to a reference that does not exist yet, for example a verified download to its installed
	// name (PRD section 8.1). An existing target yields shared.ErrConflict.
	Rename(ctx context.Context, from, to shared.ArtifactRef) error
}

// ModelSource fetches a catalog model file over the network (PRD section 8: download started by the application,
// with resume support). Implemented by the httpdownload adapter. It only transfers bytes: the use case decides with
// the catalog entry what a reply means (values.CatalogEntry.PlanResume).
type ModelSource interface {
	// Fetch requests src from byte offset (0: the whole file) and returns how the server answered and the body,
	// which the caller closes (PRD sections 8 and 8.1). The request and every redirect go only to https hosts of
	// allow, otherwise the error wraps values.ErrHostNotAllowed (PRD section 13.2: no arbitrary outbound URLs). A
	// RangeNotSatisfiable reply has an empty body.
	Fetch(ctx context.Context, src values.SourceURL, offset int64, allow values.HostAllowlist) (values.RangeReply, io.ReadCloser, error)
}

// DiskSpace reports the free disk space where model files are stored (PRD section 8: warn about required disk space
// before download). Implemented by the filestore adapter.
type DiskSpace interface {
	// FreeBytes returns the bytes available to this user on the disk of the data directory (PRD section 8).
	FreeBytes(ctx context.Context) (int64, error)
}

// CatalogSource supplies the catalog manifest of downloadable models as raw bytes (PRD section 8.1). The use case
// parses and validates it with values.ParseCatalog, so the source is not trusted. Implementations read an embedded or
// locally stored manifest; fetching a manifest over the network is not part of this port.
type CatalogSource interface {
	// Manifest returns the JSON manifest (PRD section 8.1: name, version, size, SHA-256, source URL, quantization,
	// capabilities and recommended hardware of each model).
	Manifest(ctx context.Context) ([]byte, error)
}

// Clock supplies the current time (ARCH-24; PRD section 8). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 8: installation time of a model).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 8). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewSttModelID returns a fresh model identifier (PRD section 8).
	NewSttModelID() shared.SttModelID
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
	// When Do returns an error, aggregates saved inside it must be reloaded before reuse: Save records the new
	// version on the instance (Persisted) before the outermost commit, which a rollback does not undo (ADR-0004(d)).
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
