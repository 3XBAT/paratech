package recording

import (
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Origin tells how a recording came to be (ADR-0027 «Old recordings made elsewhere»). The set of values is mirrored by
// the CHECK constraint of recordings.origin.
type Origin string

// Origins of a recording.
const (
	OriginCaptured Origin = "captured" // system audio captured by this app
	OriginImported Origin = "imported" // an audio file of a lecture recorded elsewhere
)

// Valid reports whether o is a known origin.
func (o Origin) Valid() bool { return o == OriginCaptured || o == OriginImported }

// Limits of an imported audio file (ADR-0027 «Limits», proposed; Open 10 is not answered yet).
const (
	MaxImportedBytes    int64 = 2 << 30 // 2 GiB
	MaxImportedDuration       = 4 * time.Hour
)

// ImportSpec is what the import provides to NewImported.
type ImportSpec struct {
	ID shared.RecordingID
	// LessonID is the past lesson the file is a recording of; empty when the student named a date and a subject.
	LessonID shared.LessonID
	// Lesson is the snapshot of that lesson, or of the subject, kind and title the student named for the date.
	Lesson values.LessonSnapshot
	// StartedAt is when the lecture began: the lesson's start or the date and time the student chose.
	StartedAt time.Time
	Audio     ImportedAudio
	// SHA256 is the lowercase hex digest of the copied bytes: the same bytes are the same recording.
	SHA256 string
}

// NewImported creates the COMPLETED recording of an imported file (ADR-0027): its track is recordings/<id>.<ext>, it
// has no chunks and is never captured, so every capture transition is refused (COMPLETED is terminal). The lecture
// cannot start after now; the duration is the one DetectAudio read from the headers (or Counted from the MP3 frames),
// and an MP3 whose frames are not counted yet is refused.
func NewImported(spec ImportSpec, now time.Time) (*Recording, error) {
	if err := CheckLectureTime(spec.StartedAt, now); err != nil {
		return nil, err
	}
	switch {
	case spec.Audio.Duration < 0 || spec.Audio.Duration > MaxImportedDuration:
		return nil, fmt.Errorf("%w: duration %v", ErrInvalidRecording, spec.Audio.Duration)
	case spec.Audio.counting:
		return nil, fmt.Errorf("%w: the MP3 frames are not counted yet", ErrInvalidRecording)
	}
	if err := checkSHA256(spec.SHA256); err != nil {
		return nil, err
	}
	if err := spec.ID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	dir, err := shared.NewArtifactRef(shared.ArtifactRecordings, string(spec.ID))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	audio, err := ImportedTrackRef(spec.ID, spec.Audio.Format)
	if err != nil {
		return nil, err
	}

	r := &Recording{
		id: spec.ID, lesson: spec.LessonID, lessonSnapshot: spec.Lesson, status: StatusCompleted,
		origin: OriginImported, sourceSHA256: spec.SHA256,
		maxDuration: MaxImportedDuration, duration: spec.Audio.Duration, chunksDir: dir, audio: audio,
		startedAt: spec.StartedAt, createdAt: now, updatedAt: now,
	}
	if spec.Audio.Duration > 0 {
		r.stoppedAt = spec.StartedAt.Add(spec.Audio.Duration)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// CheckLectureTime refuses the time of an imported lecture that is missing or after now (ErrFutureLecture), both with
// ErrInvalidRecording, so that an import is refused before its file is copied.
func CheckLectureTime(startedAt, now time.Time) error {
	switch {
	case now.IsZero():
		return fmt.Errorf("%w: no creation time", ErrInvalidRecording)
	case startedAt.IsZero():
		return fmt.Errorf("%w: no lecture time", ErrInvalidRecording)
	case startedAt.After(now):
		return fmt.Errorf("%w: %w", ErrInvalidRecording, ErrFutureLecture)
	}
	return nil
}

// ImportedTrackRef is the reference of the copy of an imported file: recordings/<id>.<ext of the format>.
func ImportedTrackRef(id shared.RecordingID, f AudioFormat) (shared.ArtifactRef, error) {
	if !f.Valid() {
		return shared.ArtifactRef{}, fmt.Errorf("%w: audio format %q", ErrInvalidRecording, f)
	}
	if err := id.Validate(); err != nil {
		return shared.ArtifactRef{}, fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	return shared.NewArtifactRef(shared.ArtifactRecordings, string(id)+"."+string(f))
}

// Origin returns how the recording came to be; OriginCaptured for every recording stored before imports existed.
func (r *Recording) Origin() Origin { return r.origin }

// SourceSHA256 returns the digest of the imported file; empty for a captured recording.
func (r *Recording) SourceSHA256() string { return r.sourceSHA256 }

// CheckImportSize refuses a file that is empty (ErrEmptyAudio) or larger than MaxImportedBytes (ErrAudioTooLarge).
func CheckImportSize(size int64) error {
	switch {
	case size <= 0:
		return ErrEmptyAudio
	case size > MaxImportedBytes:
		return fmt.Errorf("%w: %d bytes, at most %d", ErrAudioTooLarge, size, MaxImportedBytes)
	}
	return nil
}

// ImportSpaceReserve is the free space left after a copy, for the transcript, the database and Windows.
const ImportSpaceReserve int64 = 256 << 20

// CheckImportSpace refuses a copy of size bytes when less than ImportSpaceReserve would stay free (ErrNoDiskSpace).
func CheckImportSpace(size, free int64) error {
	if free-size < ImportSpaceReserve {
		return fmt.Errorf("%w: %d bytes free, %d needed", ErrNoDiskSpace, free, size+ImportSpaceReserve)
	}
	return nil
}

// checkOrigin is the consistency of the origin in a restored state: an imported recording is COMPLETED and has its
// digest, a captured one has none.
func checkOrigin(s State) error {
	switch {
	case !s.Origin.Valid():
		return fmt.Errorf("%w: unknown origin %q", ErrInvalidRecording, s.Origin)
	case s.Origin == OriginImported && s.Status != StatusCompleted:
		return fmt.Errorf("%w: an imported %s recording", ErrInvalidRecording, s.Status)
	case s.Origin == OriginImported:
		return checkSHA256(s.SourceSHA256)
	case s.SourceSHA256 != "":
		return fmt.Errorf("%w: a captured recording with a file digest", ErrInvalidRecording)
	}
	return nil
}

func checkSHA256(s string) error {
	if len(s) != 64 || strings.ContainsFunc(s, func(c rune) bool { return (c < '0' || c > '9') && (c < 'a' || c > 'f') }) {
		return fmt.Errorf("%w: file digest %q", ErrInvalidRecording, s)
	}
	return nil
}
