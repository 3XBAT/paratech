package transcript

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// documentExt is the file extension of the transcript document.
const documentExt = ".json"

// Language is the code of the spoken language as the STT engine reports it, for example "ru" or "yue".
// Empty means unknown.
type Language string

// Validate accepts the empty language and codes of 2 to 8 lowercase ASCII letters.
func (l Language) Validate() error {
	if l == "" {
		return nil
	}
	if len(l) < 2 || len(l) > 8 {
		return fmt.Errorf("%w: %q", ErrInvalidLanguage, string(l))
	}
	for _, r := range l {
		if r < 'a' || r > 'z' {
			return fmt.Errorf("%w: %q", ErrInvalidLanguage, string(l))
		}
	}
	return nil
}

// String returns the code.
func (l Language) String() string { return string(l) }

// Transcript is the text result of transcribing one recording with a pinned model version (PRD section 8, AC-08).
// It does not change after it was created.
type Transcript struct {
	version   int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id        shared.TranscriptID
	recording shared.RecordingID // reference by ID only (ARCH-13)

	model        shared.SttModelID // empty after the model was removed (ON DELETE SET NULL)
	modelName    values.ModelName
	modelVersion values.ModelVersion
	language     Language
	text         shared.ArtifactRef
	createdAt    time.Time
}

// Spec is what the creator of a transcript provides to New.
type Spec struct {
	ID           shared.TranscriptID
	RecordingID  shared.RecordingID
	ModelID      shared.SttModelID
	ModelName    values.ModelName
	ModelVersion values.ModelVersion
	Language     Language           // optional
	Text         shared.ArtifactRef // the document below transcripts/
}

// New creates a transcript that was just produced, never stored (Version() == 0). The model id is required.
func New(spec Spec, createdAt time.Time) (*Transcript, error) {
	if err := spec.ModelID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTranscript, err)
	}
	return build(spec, createdAt, 0)
}

// State is the stored form of a transcript, for Restore.
type State struct {
	Spec
	Version   int64
	CreatedAt time.Time
}

// Restore rebuilds a stored transcript without side effects. Unlike New it accepts an empty model id: the model was
// removed and only its copied name and version remain (AC-08).
func Restore(s State) (*Transcript, error) {
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidTranscript, s.Version)
	}
	if s.ModelID != "" {
		if err := s.ModelID.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidTranscript, err)
		}
	}
	return build(s.Spec, s.CreatedAt, s.Version)
}

func build(s Spec, createdAt time.Time, version int64) (*Transcript, error) {
	if err := validate(s, createdAt); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTranscript, err)
	}
	return &Transcript{
		version: version, id: s.ID, recording: s.RecordingID, model: s.ModelID, modelName: s.ModelName,
		modelVersion: s.ModelVersion, language: s.Language, text: s.Text, createdAt: createdAt,
	}, nil
}

func validate(s Spec, createdAt time.Time) error {
	switch {
	case createdAt.IsZero():
		return errors.New("no creation time")
	case s.Text.IsZero() || s.Text.Kind() != shared.ArtifactTranscripts:
		return fmt.Errorf("document %q is not below %s/", s.Text, shared.ArtifactTranscripts)
	}
	for _, err := range []error{
		s.ID.Validate(), s.RecordingID.Validate(), s.ModelName.Validate(), s.ModelVersion.Validate(), s.Language.Validate(),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// DocumentRef returns where the document of the transcript id is stored: transcripts/<id>.json. It is derived from
// the id so that a rerun of the same STT job writes to the same place instead of leaving a second file.
func DocumentRef(id shared.TranscriptID) (shared.ArtifactRef, error) {
	if err := id.Validate(); err != nil {
		return shared.ArtifactRef{}, fmt.Errorf("%w: %w", ErrInvalidTranscript, err)
	}
	return shared.NewArtifactRef(shared.ArtifactTranscripts, string(id)+documentExt)
}

// ID returns the transcript identifier.
func (t *Transcript) ID() shared.TranscriptID { return t.id }

// RecordingID returns the recording this transcript was produced from.
func (t *Transcript) RecordingID() shared.RecordingID { return t.recording }

// ModelID returns the model that produced the transcript, empty if it was removed since.
func (t *Transcript) ModelID() shared.SttModelID { return t.model }

// ModelName returns the name of the model that produced the transcript (AC-08).
func (t *Transcript) ModelName() values.ModelName { return t.modelName }

// ModelVersion returns the version of the model that produced the transcript (AC-08).
func (t *Transcript) ModelVersion() values.ModelVersion { return t.modelVersion }

// Language returns the detected language, empty if unknown.
func (t *Transcript) Language() Language { return t.language }

// Text returns the reference of the transcript document.
func (t *Transcript) Text() shared.ArtifactRef { return t.text }

// CreatedAt returns when the transcript was created.
func (t *Transcript) CreatedAt() time.Time { return t.createdAt }

// Created returns the event that the transcript exists. It is derived from the state, so a use case that finds the
// transcript already stored (a rerun after a crash) can publish it again.
func (t *Transcript) Created() Created {
	return Created{
		Transcript: t.id, Recording: t.recording, ModelName: t.modelName, ModelVersion: t.modelVersion,
		Language: t.language,
	}
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (t *Transcript) Version() int64 { return t.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (t *Transcript) Persisted(version int64) { t.version = version }
