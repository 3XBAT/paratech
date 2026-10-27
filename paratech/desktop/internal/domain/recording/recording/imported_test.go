package recording

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

var testSHA = strings.Repeat("ab", 32)

func importSpec() ImportSpec {
	return ImportSpec{
		ID: "imp-1", LessonID: "lesson-1", Lesson: testSnapshot(), StartedAt: t0.Add(-48 * time.Hour),
		Audio: ImportedAudio{Format: FormatWAV, Duration: 90 * time.Minute}, SHA256: testSHA,
	}
}

func newImported(t *testing.T, mod func(*ImportSpec)) *Recording {
	t.Helper()
	spec := importSpec()
	if mod != nil {
		mod(&spec)
	}
	r, err := NewImported(spec, t0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewImported(t *testing.T) {
	r := newImported(t, nil)
	spec := importSpec()

	if r.Status() != StatusCompleted || r.Origin() != OriginImported || r.SourceSHA256() != testSHA {
		t.Fatalf("status %s origin %s digest %q", r.Status(), r.Origin(), r.SourceSHA256())
	}
	if r.Audio() != mustRef(t, "recordings/imp-1.wav") || r.ChunksDir() != mustRef(t, "recordings/imp-1") || r.Chunks() != 0 {
		t.Fatalf("audio %s chunks %s (%d)", r.Audio(), r.ChunksDir(), r.Chunks())
	}
	if !r.StartedAt().Equal(spec.StartedAt) || !r.StoppedAt().Equal(spec.StartedAt.Add(90*time.Minute)) ||
		!r.CreatedAt().Equal(t0) || !r.UpdatedAt().Equal(t0) {
		t.Fatalf("times %v %v %v %v", r.StartedAt(), r.StoppedAt(), r.CreatedAt(), r.UpdatedAt())
	}
	if d, ok := r.Duration(); !ok || d != 90*time.Minute {
		t.Fatalf("Duration = %v, %v", d, ok)
	}
	if r.LessonID() != "lesson-1" || !r.LessonSnapshot().Equal(testSnapshot()) {
		t.Fatalf("lesson %s %+v", r.LessonID(), r.LessonSnapshot())
	}
	if a, ok := r.TranscribableAudio(); !ok || a != r.Audio() {
		t.Fatalf("TranscribableAudio = %s, %v", a, ok)
	}
	if r.Version() != 0 || r.HasSound() != true {
		t.Fatalf("version %d, sound %v", r.Version(), r.HasSound())
	}
}

// A date and a subject without a lesson keep a snapshot of their own; a format without a header duration is unknown.
func TestNewImportedWithoutLessonOrDuration(t *testing.T) {
	snap := values.NewLessonSnapshot(values.LessonSnapshotSpec{Subject: "Физика", Kind: "lecture", Title: "Оптика"})
	r := newImported(t, func(s *ImportSpec) {
		s.LessonID, s.Lesson, s.Audio = "", snap, ImportedAudio{Format: FormatMP3}
	})
	if r.LessonID() != "" || r.LessonSnapshot().Subject() != "Физика" || r.Audio() != mustRef(t, "recordings/imp-1.mp3") {
		t.Fatalf("lesson %q %+v audio %s", r.LessonID(), r.LessonSnapshot(), r.Audio())
	}
	if d, ok := r.Duration(); ok || d != 0 {
		t.Fatalf("Duration = %v, %v; want unknown", d, ok)
	}
	if !r.StoppedAt().IsZero() {
		t.Fatalf("StoppedAt = %v; want none without a duration", r.StoppedAt())
	}
	if _, ok := r.TranscribableAudio(); !ok {
		t.Fatal("an import of unknown duration is transcribed")
	}
}

func TestNewImportedRefuses(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*ImportSpec)
		now  time.Time
	}{
		{"no time", func(*ImportSpec) {}, time.Time{}},
		{"no lecture time", func(s *ImportSpec) { s.StartedAt = time.Time{} }, t0},
		{"lecture in the future", func(s *ImportSpec) { s.StartedAt = t0.Add(time.Minute) }, t0},
		{"bad id", func(s *ImportSpec) { s.ID = "a/b" }, t0},
		{"empty id", func(s *ImportSpec) { s.ID = "" }, t0},
		{"bad lesson id", func(s *ImportSpec) { s.LessonID = "a b" }, t0},
		{"unknown format", func(s *ImportSpec) { s.Audio.Format = "m4a" }, t0},
		{"negative duration", func(s *ImportSpec) { s.Audio.Duration = -time.Second }, t0},
		{"too long", func(s *ImportSpec) { s.Audio.Duration = MaxImportedDuration + time.Second }, t0},
		{"no digest", func(s *ImportSpec) { s.SHA256 = "" }, t0},
		{"upper-case digest", func(s *ImportSpec) { s.SHA256 = strings.ToUpper(testSHA) }, t0},
		{"short digest", func(s *ImportSpec) { s.SHA256 = testSHA[:63] }, t0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := importSpec()
			tc.mod(&spec)
			if _, err := NewImported(spec, tc.now); !errors.Is(err, ErrInvalidRecording) {
				t.Fatalf("NewImported = %v, want ErrInvalidRecording", err)
			}
		})
	}
	future := importSpec()
	future.StartedAt = t0.Add(time.Second)
	if _, err := NewImported(future, t0); !errors.Is(err, ErrFutureLecture) {
		t.Fatalf("a future lecture: %v, want ErrFutureLecture", err)
	}
	// The lecture may start right now.
	spec := importSpec()
	spec.StartedAt = t0
	if _, err := NewImported(spec, t0); err != nil {
		t.Fatalf("a lecture starting now: %v", err)
	}
}

// An imported recording is never captured: every capture and recovery step is refused and changes nothing.
func TestImportedRefusesCaptureTransitions(t *testing.T) {
	audio := mustRef(t, "recordings/imp-1.wav")
	steps := []struct {
		name string
		do   func(r *Recording) error
	}{
		{"Begin", func(r *Recording) error { return r.Begin(t0) }},
		{"Heartbeat", func(r *Recording) error { return r.Heartbeat(t0) }},
		{"Stop", func(r *Recording) error { return r.Stop(t0) }},
		{"ReachLimit", func(r *Recording) error { return r.ReachLimit(t0.Add(24 * time.Hour)) }},
		{"Interrupt", func(r *Recording) error { return r.Interrupt(t0, "device lost") }},
		{"Finalize", func(r *Recording) error { return r.Finalize(t0) }},
		{"ObserveChunks", func(r *Recording) error { return r.ObserveChunks(3, t0) }},
		{"Complete", func(r *Recording) error { return r.Complete(t0, audio, time.Minute) }},
		{"Fail", func(r *Recording) error { return r.Fail(t0, "x") }},
		{"RequireRecovery", func(r *Recording) error { return r.RequireRecovery(t0, "x") }},
		{"MarkInterrupted", func(r *Recording) error { return r.MarkInterrupted(t0, "x") }},
		{"CompleteRecovery", func(r *Recording) error { return r.CompleteRecovery(t0, audio, time.Minute) }},
		{"FailRecovery", func(r *Recording) error { return r.FailRecovery(t0, "x") }},
		{"DeferRecovery", func(r *Recording) error { return r.DeferRecovery(t0, "x") }},
		{"NoteCaptureFallback", func(r *Recording) error { return r.NoteCaptureFallback(t0, muteSwitch) }},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			r := newImported(t, nil)
			before := stateOf(r)
			if err := st.do(r); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("%s = %v, want ErrInvalidTransition", st.name, err)
			}
			if !reflect.DeepEqual(stateOf(r), before) {
				t.Fatalf("%s changed the recording", st.name)
			}
			if r.AwaitsAssembly() {
				t.Fatal("an imported recording awaits an assembly")
			}
		})
	}
}

// Deleting an import is the deletion of ADR-0018: allowed at once, its files are the copy and the (missing) chunk
// directory.
func TestImportedIsDeletable(t *testing.T) {
	r := newImported(t, nil)
	if err := r.MarkDeleting(t0, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.TranscribableAudio(); ok {
		t.Fatal("a marked import is transcribed")
	}
}

func TestRestoreImported(t *testing.T) {
	r := newImported(t, nil)
	r.Persisted(2)
	st := stateOf(r)
	st.Origin, st.SourceSHA256 = r.Origin(), r.SourceSHA256()
	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
	}

	// Without an origin a stored recording is captured, as before imports existed.
	captured := inStatus(t, StatusCompleted)
	captured.Persisted(1)
	if got, err := Restore(stateOf(captured)); err != nil || got.Origin() != OriginCaptured {
		t.Fatalf("Restore without origin = %v, %v", got, err)
	}
}

func TestRestoreRejectsInconsistentOrigin(t *testing.T) {
	imported := func() State {
		r := newImported(t, nil)
		r.Persisted(1)
		st := stateOf(r)
		st.Origin, st.SourceSHA256 = OriginImported, testSHA
		return st
	}
	tests := []struct {
		name string
		mod  func(*State)
	}{
		{"unknown origin", func(s *State) { s.Origin = "uploaded" }},
		{"imported without digest", func(s *State) { s.SourceSHA256 = "" }},
		{"imported with a bad digest", func(s *State) { s.SourceSHA256 = "xyz" }},
		{"imported but failed", func(s *State) {
			s.Status, s.Audio, s.Duration, s.StoppedAt = StatusFailed, shared.ArtifactRef{}, 0, time.Time{}
		}},
		{"captured with a digest", func(s *State) { s.Origin = OriginCaptured }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := imported()
			tc.mod(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidRecording) {
				t.Fatalf("Restore = %v, want ErrInvalidRecording", err)
			}
		})
	}
}

func TestCheckImportSize(t *testing.T) {
	tests := []struct {
		size int64
		want error
	}{
		{-1, ErrEmptyAudio},
		{0, ErrEmptyAudio},
		{1, nil},
		{MaxImportedBytes, nil},
		{MaxImportedBytes + 1, ErrAudioTooLarge},
	}
	for _, tc := range tests {
		if err := CheckImportSize(tc.size); !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
			t.Errorf("CheckImportSize(%d) = %v, want %v", tc.size, err, tc.want)
		}
	}
	if MaxImportedBytes != 2*1024*1024*1024 {
		t.Fatalf("MaxImportedBytes = %d, want 2 GiB", MaxImportedBytes)
	}
}

func TestCheckImportSpace(t *testing.T) {
	const size = 100 << 20
	if err := CheckImportSpace(size, size+ImportSpaceReserve); err != nil {
		t.Fatalf("exactly enough: %v", err)
	}
	if err := CheckImportSpace(size, size+ImportSpaceReserve-1); !errors.Is(err, ErrNoDiskSpace) {
		t.Fatalf("one byte short: %v, want ErrNoDiskSpace", err)
	}
}

func TestOriginValid(t *testing.T) {
	for o, want := range map[Origin]bool{OriginCaptured: true, OriginImported: true, "": false, "other": false} {
		if o.Valid() != want {
			t.Errorf("%q.Valid() = %v", o, !want)
		}
	}
}
