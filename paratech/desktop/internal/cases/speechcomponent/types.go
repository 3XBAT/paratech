package speechcomponent

import (
	"log/slog"

	"paratech/desktop/internal/domain/transcription/values"
)

// Deps are the ports and the pinned component; all but Log are required.
type Deps struct {
	Spec      values.SpeechComponent
	Source    ArchiveSource
	Space     DiskSpace
	Files     ComponentFiles
	Activator Activator
	Log       *slog.Logger // optional: nil discards
}

// State is what the pages show about speech recognition.
type State string

// States of the component.
const (
	StateReady      State = "ready"      // speech recognition works
	StateMissing    State = "missing"    // no component file was found
	StateDamaged    State = "damaged"    // files were found, but the set does not verify
	StateInstalling State = "installing" // an install runs
	StateFailed     State = "failed"     // the last install or the activation failed; Status.Err says why
)

// Stage is the step of a running install.
type Stage string

// Stages of an install, in order.
const (
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageUnpacking   Stage = "unpacking"
	StageActivating  Stage = "activating"
)

// Status is the state of the component and of the current or last install.
type Status struct {
	State State
	Stage Stage // StateInstalling only

	// Download progress of StateInstalling; TotalBytes is the archive and the separate files.
	DoneBytes  int64
	TotalBytes int64

	Err error // StateFailed only
}
