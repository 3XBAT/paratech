package recording

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Status is the lifecycle state of a recording (PRD section 6.2). The set of values is mirrored by the
// CHECK constraint of recordings.status in docs/contracts/schema.sql; a test keeps both in sync.
//
// Allowed transitions are the table in transitions.go.
type Status string

// Recording statuses in the order of PRD section 6.2.
const (
	StatusStarting         Status = "STARTING"
	StatusRecording        Status = "RECORDING"
	StatusStopping         Status = "STOPPING"
	StatusFinalizing       Status = "FINALIZING"
	StatusCompleted        Status = "COMPLETED"
	StatusFailed           Status = "FAILED"
	StatusRecoveryRequired Status = "RECOVERY_REQUIRED"
)

// Statuses returns every status, in the order of PRD section 6.2.
func Statuses() []Status {
	return []Status{
		StatusStarting, StatusRecording, StatusStopping, StatusFinalizing,
		StatusCompleted, StatusFailed, StatusRecoveryRequired,
	}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// Limits of free-form fields.
const (
	MaxProblemBytes  = 512
	maxTrackExtBytes = 8
)

// Recording is one capture of system audio of an online class (PRD section 6.2). Fields change only through the
// methods below.
type Recording struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.RecordingID
	lesson  shared.LessonID
	status  Status

	lessonSnapshot values.LessonSnapshot // the lesson as it was at the start (TASK-088); zero without one

	maxDuration time.Duration
	duration    time.Duration // final duration; meaningful only with audio
	chunks      int
	chunksDir   shared.ArtifactRef
	audio       shared.ArtifactRef
	silent      bool // the final track is silent all or almost all the time (TASK-082)
	problem     string

	// The deletion mark (TASK-095): set before any file or row is removed, so a crash in the middle resumes.
	deletingAt          time.Time
	deletingAssignments []shared.AssignmentID // found homework the user chose to delete with the recording

	startedAt   time.Time
	stoppedAt   time.Time
	heartbeatAt time.Time
	createdAt   time.Time
	updatedAt   time.Time
}

// Spec is what the creator of a recording provides to New.
type Spec struct {
	ID          shared.RecordingID
	LessonID    shared.LessonID // optional: empty for an ad-hoc recording
	MaxDuration time.Duration   // hard cap of this recording, see the limits policy
	// Lesson is the snapshot of the lesson at the start (TASK-088); only with LessonID, optional even then.
	Lesson values.LessonSnapshot
}

// New creates a STARTING recording whose chunks go to recordings/<id>.
func New(spec Spec, now time.Time) (*Recording, error) {
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidRecording)
	}
	if err := spec.ID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	dir, err := shared.NewArtifactRef(shared.ArtifactRecordings, string(spec.ID))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	if spec.LessonID == "" && !spec.Lesson.IsZero() {
		return nil, fmt.Errorf("%w: a lesson snapshot without a lesson", ErrInvalidRecording)
	}

	r := &Recording{
		id: spec.ID, lesson: spec.LessonID, lessonSnapshot: spec.Lesson, status: StatusStarting,
		maxDuration: spec.MaxDuration, chunksDir: dir, startedAt: now, createdAt: now, updatedAt: now,
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// State is the stored form of a recording, for Restore.
type State struct {
	Version  int64
	ID       shared.RecordingID
	LessonID shared.LessonID
	// Lesson is the snapshot of the lesson at the start; it outlives the link to the lesson (LessonID may be empty).
	Lesson      values.LessonSnapshot
	Status      Status
	MaxDuration time.Duration
	Duration    time.Duration // final duration; must be zero without Audio
	Chunks      int
	ChunksDir   shared.ArtifactRef
	Audio       shared.ArtifactRef // the final track; set exactly when COMPLETED
	Silent      bool               // the final track is silent; only on a COMPLETED recording
	Problem     string
	// DeletingAt is the deletion mark (zero: not being deleted) and DeletingAssignments the homework deleted with it.
	DeletingAt          time.Time
	DeletingAssignments []shared.AssignmentID
	StartedAt           time.Time
	StoppedAt           time.Time // zero = the capture has not ended
	HeartbeatAt         time.Time // zero = no heartbeat yet
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Restore rebuilds a stored recording without side effects; it only checks that the state is consistent.
func Restore(s State) (*Recording, error) {
	r := &Recording{
		version: s.Version, id: s.ID, lesson: s.LessonID, lessonSnapshot: s.Lesson, status: s.Status,
		maxDuration: s.MaxDuration, duration: s.Duration, chunks: s.Chunks, chunksDir: s.ChunksDir, audio: s.Audio,
		silent: s.Silent, problem: s.Problem, startedAt: s.StartedAt, stoppedAt: s.StoppedAt, heartbeatAt: s.HeartbeatAt,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt,
		deletingAt: s.DeletingAt, deletingAssignments: slices.Clone(s.DeletingAssignments),
	}

	switch {
	case s.Version < 1:
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidRecording, s.Version)
	case !s.Status.Valid():
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidRecording, s.Status)
	case (s.Status == StatusCompleted) != !s.Audio.IsZero():
		return nil, fmt.Errorf("%w: %s recording with audio %q", ErrInvalidRecording, s.Status, s.Audio)
	case s.Audio.IsZero() && s.Duration != 0:
		return nil, fmt.Errorf("%w: duration without a final track", ErrInvalidRecording)
	case s.Silent && s.Status != StatusCompleted:
		return nil, fmt.Errorf("%w: a silent track on a %s recording", ErrInvalidRecording, s.Status)
	case (s.Status == StatusStarting || s.Status == StatusRecording) && !s.StoppedAt.IsZero():
		return nil, fmt.Errorf("%w: %s recording with a stop time", ErrInvalidRecording, s.Status)
	case s.Status == StatusStopping && s.StoppedAt.IsZero():
		return nil, fmt.Errorf("%w: STOPPING recording without a stop time", ErrInvalidRecording)
	case len(s.Problem) > MaxProblemBytes:
		return nil, fmt.Errorf("%w: problem text longer than %d bytes", ErrInvalidRecording, MaxProblemBytes)
	case !s.DeletingAt.IsZero() && s.Status.InProgress():
		return nil, fmt.Errorf("%w: a %s recording marked for deletion", ErrInvalidRecording, s.Status)
	case s.DeletingAt.IsZero() && len(s.DeletingAssignments) > 0:
		return nil, fmt.Errorf("%w: assignments to delete without a deletion mark", ErrInvalidRecording)
	}
	if err := checkDeletedAssignments(s.DeletingAssignments); err != nil {
		return nil, err
	}

	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Recording) validate() error {
	if err := r.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRecording, err)
	}
	if r.lesson != "" {
		if err := r.lesson.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRecording, err)
		}
	}

	switch {
	case r.maxDuration <= 0:
		return fmt.Errorf("%w: hard cap %v", ErrInvalidRecording, r.maxDuration)
	case r.chunks < 0:
		return fmt.Errorf("%w: %d chunks", ErrInvalidRecording, r.chunks)
	case r.duration < 0:
		return fmt.Errorf("%w: duration %v", ErrInvalidRecording, r.duration)
	case r.chunksDir.Kind() != shared.ArtifactRecordings:
		return fmt.Errorf("%w: chunk directory %q", ErrInvalidRecording, r.chunksDir)
	case !r.audio.IsZero() && r.audio.Kind() != shared.ArtifactRecordings:
		return fmt.Errorf("%w: final track %q", ErrInvalidRecording, r.audio)
	case r.startedAt.IsZero() || r.createdAt.IsZero() || r.updatedAt.IsZero():
		return fmt.Errorf("%w: missing time", ErrInvalidRecording)
	}
	return nil
}

// ID returns the recording identifier.
func (r *Recording) ID() shared.RecordingID { return r.id }

// LessonID returns the lesson the recording belongs to, empty for an ad-hoc recording.
func (r *Recording) LessonID() shared.LessonID { return r.lesson }

// LessonSnapshot returns what the recording kept of its lesson at the start (TASK-088); the zero value without a
// lesson and for a recording stored before snapshots existed. It stays when the schedule deletes the lesson.
func (r *Recording) LessonSnapshot() values.LessonSnapshot { return r.lessonSnapshot }

// Status returns the current lifecycle state.
func (r *Recording) Status() Status { return r.status }

// MaxDuration returns the hard cap applied to this recording.
func (r *Recording) MaxDuration() time.Duration { return r.maxDuration }

// Deadline returns when the hard cap is reached: the start of the capture plus the cap.
func (r *Recording) Deadline() time.Time { return r.startedAt.Add(r.maxDuration) }

// LimitReached reports whether the hard cap is reached at now.
func (r *Recording) LimitReached(now time.Time) bool { return !now.Before(r.Deadline()) }

// Duration returns the duration of the final track; ok is false until the recording is COMPLETED.
func (r *Recording) Duration() (d time.Duration, ok bool) { return r.duration, !r.audio.IsZero() }

// Chunks returns the number of chunk files last observed in the chunk directory.
func (r *Recording) Chunks() int { return r.chunks }

// ChunksDir returns the directory of the chunk files.
func (r *Recording) ChunksDir() shared.ArtifactRef { return r.chunksDir }

// Audio returns the final track; the zero reference until COMPLETED.
func (r *Recording) Audio() shared.ArtifactRef { return r.audio }

// Silent reports a COMPLETED recording whose final track is silent all or almost all the time (TASK-082): the chosen
// source played nothing. It is not transcribed.
func (r *Recording) Silent() bool { return r.silent }

// Problem returns a short text about what went wrong, empty if nothing did. A COMPLETED recording keeps the
// reason why its capture ended early and the note of a capture fallback (NoteCaptureFallback).
func (r *Recording) Problem() string { return r.problem }

// StartedAt returns when the capture began (the creation time while STARTING).
func (r *Recording) StartedAt() time.Time { return r.startedAt }

// StoppedAt returns when the capture ended; zero while it runs or if unknown.
func (r *Recording) StoppedAt() time.Time { return r.stoppedAt }

// HeartbeatAt returns the last liveness mark of the capture; zero if none.
func (r *Recording) HeartbeatAt() time.Time { return r.heartbeatAt }

// CreatedAt returns the creation time.
func (r *Recording) CreatedAt() time.Time { return r.createdAt }

// UpdatedAt returns the time of the last change.
func (r *Recording) UpdatedAt() time.Time { return r.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (r *Recording) Version() int64 { return r.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (r *Recording) Persisted(version int64) { r.version = version }

// ValidateTrackExtension checks the extension of a final track: short, ASCII letters and digits only.
func ValidateTrackExtension(ext string) error {
	if ext == "" || len(ext) > maxTrackExtBytes || strings.ContainsFunc(ext, notAlnum) {
		return fmt.Errorf("%w: track extension %q", ErrInvalidRecording, ext)
	}
	return nil
}

// TrackRef returns the reference of the final track recordings/<id>.<ext>.
func (r *Recording) TrackRef(ext string) (shared.ArtifactRef, error) {
	if err := ValidateTrackExtension(ext); err != nil {
		return shared.ArtifactRef{}, err
	}
	return shared.NewArtifactRef(shared.ArtifactRecordings, string(r.id)+"."+ext)
}

// ownsTrack reports whether ref is recordings/<id>.<ext> of this recording with a valid extension.
func (r *Recording) ownsTrack(ref shared.ArtifactRef) bool {
	if ref.Kind() != shared.ArtifactRecordings {
		return false
	}

	ext, ok := strings.CutPrefix(ref.Path(), string(r.id)+".")
	if !ok {
		return false
	}

	want, err := r.TrackRef(ext)
	return err == nil && want == ref
}

func notAlnum(c rune) bool {
	return c > unicode.MaxASCII || !unicode.IsLetter(c) && !unicode.IsDigit(c)
}

// move applies one table transition.
func (r *Recording) move(to Status, now time.Time) error {
	if !CanTransition(r.status, to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, r.status, to)
	}
	r.status = to
	r.updatedAt = now
	return nil
}

// Begin moves a STARTING recording to RECORDING once the capture runs; the hard cap counts from now.
func (r *Recording) Begin(now time.Time) error {
	if err := r.move(StatusRecording, now); err != nil {
		return err
	}
	r.startedAt, r.heartbeatAt = now, now
	return nil
}

// Heartbeat marks a RECORDING as alive; a stale mark after a restart means the app died (PRD section 6.2).
func (r *Recording) Heartbeat(now time.Time) error {
	if r.status != StatusRecording {
		return fmt.Errorf("%w: heartbeat of a %s recording", ErrInvalidTransition, r.status)
	}
	r.heartbeatAt, r.updatedAt = now, now
	return nil
}

// NoteCaptureFallback records on a RECORDING that its capture fell back from process loopback to classic loopback
// (ADR-0002 Decision 3): the fixed note of sw becomes the problem and the recording goes on. The note stays when the
// recording is stopped and completes; an interruption or a lost session puts its own reason first and keeps the note
// after it; a failure replaces it. Noting the same switch again changes nothing.
func (r *Recording) NoteCaptureFallback(now time.Time, sw values.CaptureSwitch) error {
	if r.status != StatusRecording {
		return fmt.Errorf("%w: capture note on a %s recording", ErrInvalidTransition, r.status)
	}
	if !sw.IsFallback() {
		return fmt.Errorf("%w: switch from %q to %q is no fallback", ErrInvalidRecording, sw.From, sw.To)
	}
	text, err := problemText(sw.Note())
	if err != nil {
		return err
	}
	if text != r.problem {
		r.problem, r.updatedAt = text, now
	}
	return nil
}

// withNote is reason followed by the capture note a RECORDING keeps as its problem, bounded; the reason wins.
func (r *Recording) withNote(reason string) string {
	if r.problem == "" || r.problem == reason {
		return reason
	}
	text, err := problemText(reason + "; " + r.problem)
	if err != nil {
		return reason
	}
	return text
}

// Stop moves a RECORDING to STOPPING at the user's request.
func (r *Recording) Stop(now time.Time) error {
	if err := r.move(StatusStopping, now); err != nil {
		return err
	}
	r.stoppedAt = now
	return nil
}

// ReachLimit moves a RECORDING to FINALIZING because the hard cap is reached (AC-04). Before the deadline it fails
// with ErrLimitNotReached.
func (r *Recording) ReachLimit(now time.Time) error {
	if r.status != StatusRecording {
		return fmt.Errorf("%w: limit of a %s recording", ErrInvalidTransition, r.status)
	}
	if !r.LimitReached(now) {
		return fmt.Errorf("%w: deadline %v", ErrLimitNotReached, r.Deadline())
	}
	if err := r.move(StatusFinalizing, now); err != nil {
		return err
	}
	r.stoppedAt = now
	return nil
}

// Interrupt moves a RECORDING to FINALIZING because the capture ended by itself (device lost, disk full): the
// chunks written so far are still assembled, and the problem is kept for the UI.
func (r *Recording) Interrupt(now time.Time, problem string) error {
	text, err := problemText(problem)
	if err != nil {
		return err
	}
	if r.status != StatusRecording {
		return fmt.Errorf("%w: interruption of a %s recording", ErrInvalidTransition, r.status)
	}
	if err := r.move(StatusFinalizing, now); err != nil {
		return err
	}
	r.stoppedAt, r.problem = now, r.withNote(text)
	return nil
}

// Finalize moves a STOPPING recording, or one being recovered after a restart, to FINALIZING. A RECORDING goes
// there only through ReachLimit or Interrupt.
func (r *Recording) Finalize(now time.Time) error {
	if r.status == StatusRecording {
		return fmt.Errorf("%w: a RECORDING is stopped first", ErrInvalidTransition)
	}
	return r.move(StatusFinalizing, now)
}

// ObserveChunks records how many chunk files the chunk directory holds; only an unfinished recording changes.
func (r *Recording) ObserveChunks(n int, now time.Time) error {
	if r.status.IsTerminal() {
		return fmt.Errorf("%w: chunks of a %s recording", ErrInvalidTransition, r.status)
	}
	if n < 0 {
		return fmt.Errorf("%w: %d chunks", ErrInvalidRecording, n)
	}
	r.chunks, r.updatedAt = n, now
	return nil
}

// Complete moves a FINALIZING recording to COMPLETED with its final track and duration. The track must be the
// TrackRef of this recording and the duration positive.
func (r *Recording) Complete(now time.Time, audio shared.ArtifactRef, duration time.Duration) error {
	if !r.ownsTrack(audio) {
		return fmt.Errorf("%w: final track %q of recording %s", ErrInvalidRecording, audio, r.id)
	}
	if duration <= 0 {
		return fmt.Errorf("%w: duration %v", ErrInvalidRecording, duration)
	}
	if err := r.move(StatusCompleted, now); err != nil {
		return err
	}
	r.audio, r.duration = audio, duration
	return nil
}

// CompleteTrack is Complete with what the assembly measured: a silent track (values.AssembledTrack.Silent) marks the
// recording Silent, so it is not transcribed (TASK-082).
func (r *Recording) CompleteTrack(now time.Time, audio shared.ArtifactRef, track values.AssembledTrack) error {
	if err := r.Complete(now, audio, track.Duration); err != nil {
		return err
	}
	r.silent = track.Silent()
	return nil
}

// Fail moves the recording to FAILED: a start that did not succeed, a finalization that did not, or a recovery that
// is impossible. Chunk files are not touched.
func (r *Recording) Fail(now time.Time, problem string) error {
	text, err := problemText(problem)
	if err != nil {
		return err
	}
	if err := r.move(StatusFailed, now); err != nil {
		return err
	}
	r.problem = text
	return nil
}

// RequireRecovery moves a RECORDING whose session died (crash, stale heartbeat) to RECOVERY_REQUIRED.
func (r *Recording) RequireRecovery(now time.Time, problem string) error {
	text, err := problemText(problem)
	if err != nil {
		return err
	}
	if err := r.move(StatusRecoveryRequired, now); err != nil {
		return err
	}
	r.problem = r.withNote(text)
	return nil
}

// MarkInterrupted is the startup decision for an unfinished recording that no session of this process captures or
// finalizes (PRD section 6.2 watchdog, section 14 case 1, AC-05). The app runs as one instance and the recovery runs
// before the first recording of a process, so such a recording was cut off by the end of an earlier process and its
// heartbeat is stale by definition:
//   - STARTING: the capture never ran; the recording is FAILED with problem.
//   - RECORDING: its session died; it is RECOVERY_REQUIRED with problem, and the capture counts as ended at the last
//     heartbeat.
//   - STOPPING, FINALIZING, RECOVERY_REQUIRED: unchanged; AwaitsAssembly holds and the recovery assembles the chunks.
func (r *Recording) MarkInterrupted(now time.Time, problem string) error {
	switch r.status {
	case StatusStarting:
		return r.Fail(now, problem)
	case StatusRecording:
		stopped := r.heartbeatAt
		if stopped.IsZero() {
			stopped = now
		}
		if err := r.RequireRecovery(now, problem); err != nil {
			return err
		}
		r.stoppedAt = stopped
		return nil
	case StatusStopping, StatusFinalizing, StatusRecoveryRequired:
		return nil
	}
	return fmt.Errorf("%w: interruption of a %s recording", ErrInvalidTransition, r.status)
}

// AwaitsAssembly reports whether the recovery after a restart is to assemble the chunks: the session ended while the
// recording was being stopped or finalized, or it is RECOVERY_REQUIRED. A recording marked for deletion never does.
func (r *Recording) AwaitsAssembly() bool {
	if r.Deleting() {
		return false // its files are being deleted: nothing is assembled any more
	}
	switch r.status {
	case StatusStopping, StatusFinalizing, StatusRecoveryRequired:
		return true
	}
	return false
}

func (r *Recording) checkAwaitsAssembly() error {
	if !r.AwaitsAssembly() {
		return fmt.Errorf("%w: recovery of a %s recording", ErrInvalidTransition, r.status)
	}
	return nil
}

// CompleteRecovery finishes a recording that AwaitsAssembly with the track the recovery assembled: it moves through
// FINALIZING to COMPLETED, with the same rules for the track and the duration as Complete. The note of an earlier
// DeferRecovery is dropped and the reason the recording had before it stays (for a former RECORDING, why its session
// ended). Nothing changes on an error.
func (r *Recording) CompleteRecovery(now time.Time, audio shared.ArtifactRef, duration time.Duration) error {
	if err := r.checkAwaitsAssembly(); err != nil {
		return err
	}
	if !r.ownsTrack(audio) {
		return fmt.Errorf("%w: final track %q of recording %s", ErrInvalidRecording, audio, r.id)
	}
	if duration <= 0 {
		return fmt.Errorf("%w: duration %v", ErrInvalidRecording, duration)
	}
	reason := r.reasonBeforeDeferral()
	if r.status != StatusFinalizing {
		if err := r.move(StatusFinalizing, now); err != nil {
			return err
		}
	}
	if err := r.Complete(now, audio, duration); err != nil {
		return err
	}
	r.problem = reason
	return nil
}

// CompleteRecoveryTrack is CompleteRecovery with what the assembly measured: a silent track marks the recording
// Silent, as CompleteTrack does.
func (r *Recording) CompleteRecoveryTrack(now time.Time, audio shared.ArtifactRef, track values.AssembledTrack) error {
	if err := r.CompleteRecovery(now, audio, track.Duration); err != nil {
		return err
	}
	r.silent = track.Silent()
	return nil
}

// FailRecovery moves a recording that AwaitsAssembly to FAILED because its chunks hold no readable audio. A STOPPING
// recording passes through FINALIZING, as the table requires. The chunk files are not touched.
func (r *Recording) FailRecovery(now time.Time, problem string) error {
	if _, err := problemText(problem); err != nil {
		return err
	}
	if err := r.checkAwaitsAssembly(); err != nil {
		return err
	}
	if r.status == StatusStopping {
		if err := r.move(StatusFinalizing, now); err != nil {
			return err
		}
	}
	return r.Fail(now, problem)
}

// pendingMark starts the note of DeferRecovery in the problem text. The schema has no column for the note, so the
// aggregate keeps it in the problem after the earlier reason and finds it again by this mark.
const pendingMark = "assembly pending: "

// DeferRecovery keeps a recording that AwaitsAssembly in its status after an assembly that failed for a reason that
// may pass (a locked file, a busy disk): the next recovery tries again. The problem becomes the earlier reason (without
// the note of an earlier deferral) followed by note; CompleteRecovery restores the reason. A reason near
// MaxProblemBytes leaves no room for the note, and the reason wins.
func (r *Recording) DeferRecovery(now time.Time, note string) error {
	text, err := problemText(note)
	if err != nil {
		return err
	}
	if err := r.checkAwaitsAssembly(); err != nil {
		return err
	}
	text = pendingMark + text
	if reason := r.reasonBeforeDeferral(); reason != "" {
		text = reason + "; " + text
	}
	if text, err = problemText(text); err != nil {
		return err
	}
	r.problem, r.updatedAt = text, now
	return nil
}

// reasonBeforeDeferral returns the problem without the note that DeferRecovery appended.
func (r *Recording) reasonBeforeDeferral() string {
	if strings.HasPrefix(r.problem, pendingMark) {
		return ""
	}
	if reason, _, ok := strings.Cut(r.problem, "; "+pendingMark); ok {
		return reason
	}
	return r.problem
}

// TranscribableAudio returns the final track to transcribe; ok is false unless the recording is COMPLETED with a
// final track that is not silent: only such a recording can be transcribed (a silent one would give an empty text).
// A recording marked for deletion is not transcribed either.
func (r *Recording) TranscribableAudio() (audio shared.ArtifactRef, ok bool) {
	if r.status != StatusCompleted || r.audio.IsZero() || r.silent || r.Deleting() {
		return shared.ArtifactRef{}, false
	}
	return r.audio, true
}

// problemText makes a problem description safe to store and show: valid UTF-8, one line, bounded.
func problemText(s string) (string, error) {
	s = strings.ToValidUTF8(s, "?")
	s = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: empty problem text", ErrInvalidRecording)
	}
	if len(s) <= MaxProblemBytes {
		return s, nil
	}
	cut := MaxProblemBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], nil
}
