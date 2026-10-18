package speechcomponent

import (
	"context"
	"io"

	"paratech/desktop/internal/domain/transcription/values"
)

// ArchiveSource fetches the component archive over the network (PRD section 4.3: the whisper.cpp sidecar; section 13.2:
// no arbitrary outbound URLs). Implemented by the httpdownload adapter.
type ArchiveSource interface {
	// Fetch requests src from byte offset (0: the whole file) and returns how the server answered and the body, which
	// the caller closes (PRD section 8). The request and every redirect go only to https hosts of allow, otherwise the
	// error wraps values.ErrHostNotAllowed (PRD section 13.2).
	Fetch(ctx context.Context, src values.SourceURL, offset int64, allow values.HostAllowlist) (values.RangeReply, io.ReadCloser, error)
}

// DiskSpace reports the free space of the disk of the data directory (PRD section 8: warn about required disk space).
// Implemented by the filestore adapter.
type DiskSpace interface {
	// FreeBytes returns the bytes available to the user on that disk (PRD section 8).
	FreeBytes(ctx context.Context) (int64, error)
}

// ComponentFiles is the file access of the component (PRD section 5.2, hard rule 5): the read-only folder of the
// executable and the component folder below the data directory, where the archive is stored and unpacked.
// Implemented by the componentstore adapter.
type ComponentFiles interface {
	// Inspect returns what folder holds under the names of the component files (PRD section 4.3): for each name that
	// exists, whether it is a regular file (no link or reparse point) and, if so, its size and SHA-256. A folder that
	// does not exist has nothing.
	Inspect(ctx context.Context, folder values.ComponentFolder, spec values.SpeechComponent) ([]values.FoundFile, error)

	// SaveArchive stores the downloaded archive, replacing an older one, and returns its size and SHA-256
	// (PRD section 8.1: verify before install).
	SaveArchive(ctx context.Context, content io.Reader) (int64, values.SHA256, error)

	// Unpack takes the component files out of the stored archive into a fresh staging folder and, only when every
	// file is there with the pinned size and SHA-256, makes them the installed folder (values.FolderInstalled),
	// replacing the old one (PRD section 8.1). The archive must again have the pinned size and SHA-256. Nothing is
	// executed. On an error the installed folder is left as it was, or missing when the replacement itself failed.
	Unpack(ctx context.Context, spec values.SpeechComponent) error

	// DiscardArchive removes the stored archive; a missing one is not an error (PRD section 5.2).
	DiscardArchive(ctx context.Context) error
}

// Activator starts speech recognition with the verified files of a folder, at start or after an install
// (PRD section 4.3): it builds the engine and lets the job runner run STT jobs (PRD section 7). Implemented by the app.
type Activator interface {
	// Activate starts speech recognition from folder (PRD section 4.3). It is called only after the folder verified.
	Activate(ctx context.Context, folder values.ComponentFolder) error
}
