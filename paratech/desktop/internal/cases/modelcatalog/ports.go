package modelcatalog

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
)

// ModelRepository stores the installed STT models in the local database (PRD section 8: name, version, size,
// capabilities; several models at once). Implemented by the sqlite adapter.
type ModelRepository interface {
	// Get loads an installed model (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.SttModelID) (*sttmodel.Model, error)

	// List returns all installed models (PRD section 8: display installed models).
	List(ctx context.Context) ([]*sttmodel.Model, error)

	// Save inserts or updates an installed model (PRD section 8). It returns an error wrapping shared.ErrConflict if
	// another model with the same name and version exists.
	Save(ctx context.Context, m *sttmodel.Model) error

	// Delete removes the model record (PRD section 8: model removal). Whether removal is allowed is decided by the
	// model aggregate and ModelPins before this is called.
	Delete(ctx context.Context, id shared.SttModelID) error
}

// ModelPins answers whether an unfinished job pins a model, so that removal can be blocked
// (PRD section 8 diagram, section 14 case 11). Implemented by the sqlite adapter.
type ModelPins interface {
	// IsPinnedByActiveJob reports whether any job that is not COMPLETED, FAILED or CANCELLED pins the model
	// (PRD section 14 case 11: a job pins the model version, so removing a pinned model is unsafe).
	IsPinnedByActiveJob(ctx context.Context, id shared.SttModelID) (bool, error)
}

// SettingsRepository stores the settings aggregate; choosing the default model changes only this aggregate
// (PRD section 8, AC-07, ARCH-23). Implemented by the sqlite adapter.
type SettingsRepository interface {
	// Load returns the current settings, defaults applied (PRD section 15).
	Load(ctx context.Context) (*settings.Settings, error)

	// Save persists the settings (PRD section 8: select the active default model).
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

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary is one use case method call and it changes
// one aggregate. Implemented by the sqlite adapter (TASK-005).
type Tx interface {
	// Do runs fn in a database transaction and commits if it returns nil, otherwise it rolls back and returns
	// the error (PRD section 7.3: persistent state transitions). Repositories called with the context passed to
	// fn take part in the transaction; calls with another context do not.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
