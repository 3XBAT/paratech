package recording

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

const testCap = 3 * time.Hour

func newRec(t *testing.T) *Recording {
	t.Helper()
	r, err := New(Spec{ID: "r1", LessonID: "lesson-1", MaxDuration: testCap}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustRef(t *testing.T, s string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// inStatus builds a recording that reached s by legal moves.
func inStatus(t *testing.T, s Status) *Recording {
	t.Helper()
	r := newRec(t)
	at := t0.Add(time.Second)
	begin := func() error { return r.Begin(at) }
	steps := map[Status][]func() error{
		StatusStarting:  nil,
		StatusRecording: {begin},
		StatusStopping:  {begin, func() error { return r.Stop(at.Add(time.Minute)) }},
		StatusFinalizing: {
			begin, func() error { return r.Stop(at.Add(time.Minute)) }, func() error { return r.Finalize(at.Add(time.Minute)) },
		},
		StatusCompleted: {
			begin, func() error { return r.Stop(at.Add(time.Minute)) }, func() error { return r.Finalize(at.Add(time.Minute)) },
			func() error { return r.ObserveChunks(2, at.Add(time.Minute)) },
			func() error { return r.Complete(at.Add(2*time.Minute), mustRef(t, "recordings/r1.wav"), time.Minute) },
		},
		StatusFailed:           {func() error { return r.Fail(at, "device not found") }},
		StatusRecoveryRequired: {begin, func() error { return r.RequireRecovery(at.Add(time.Hour), "stale heartbeat") }},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			t.Fatalf("reaching %s: %v", s, err)
		}
	}
	if r.Status() != s {
		t.Fatalf("reached %s, want %s", r.Status(), s)
	}
	return r
}

// The expected pairs are copied from the diagram of PRD section 6.2, independently of the table under test.
func TestTransitionMatrix(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusStarting, StatusRecording}:          true,
		{StatusStarting, StatusFailed}:             true,
		{StatusRecording, StatusStopping}:          true,
		{StatusRecording, StatusFinalizing}:        true,
		{StatusStopping, StatusFinalizing}:         true,
		{StatusFinalizing, StatusCompleted}:        true,
		{StatusFinalizing, StatusFailed}:           true,
		{StatusRecording, StatusRecoveryRequired}:  true,
		{StatusRecoveryRequired, StatusFinalizing}: true,
		{StatusRecoveryRequired, StatusFailed}:     true,
	}
	count := 0
	for _, from := range Statuses() {
		for _, to := range Statuses() {
			want := allowed[[2]Status{from, to}]
			if want {
				count++
			}
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	if count != len(allowed) {
		t.Fatalf("matrix covers %d of %d allowed pairs", count, len(allowed))
	}
	if CanTransition("BOGUS", StatusRecording) || CanTransition(StatusStarting, "BOGUS") {
		t.Fatal("unknown status must not transition")
	}
}

func TestTableKeysAreAllStatuses(t *testing.T) {
	if len(transitions) != len(Statuses()) {
		t.Fatalf("table has %d statuses, Statuses() %d", len(transitions), len(Statuses()))
	}
	for _, s := range Statuses() {
		if !s.Valid() {
			t.Errorf("%s is not in the table", s)
		}
	}
	if Status("BOGUS").Valid() || Status("").Valid() {
		t.Fatal("unknown status is valid")
	}
}

func TestStatusClassification(t *testing.T) {
	cases := []struct {
		status     Status
		terminal   bool
		inProgress bool
	}{
		{StatusStarting, false, true},
		{StatusRecording, false, true},
		{StatusStopping, false, true},
		{StatusFinalizing, false, true},
		{StatusCompleted, true, false},
		{StatusFailed, true, false},
		{StatusRecoveryRequired, false, false},
		{"BOGUS", false, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.terminal {
				t.Errorf("IsTerminal = %v, want %v", got, tc.terminal)
			}
			if tc.status.Valid() && tc.status.IsTerminal() != (len(transitions[tc.status]) == 0) {
				t.Error("IsTerminal disagrees with the table")
			}
			if got := tc.status.InProgress(); got != tc.inProgress {
				t.Errorf("InProgress = %v, want %v", got, tc.inProgress)
			}
		})
	}

	unfinished := UnfinishedStatuses()
	if len(unfinished) != 5 {
		t.Fatalf("UnfinishedStatuses = %v", unfinished)
	}
	for _, s := range unfinished {
		if s.IsTerminal() {
			t.Errorf("%s is terminal", s)
		}
	}
}

// Every method is tried from every status; only the documented source statuses succeed.
func TestMethodsFromEveryStatus(t *testing.T) {
	later := t0.Add(4 * time.Hour) // past the cap
	track := shared.ArtifactRef{}
	methods := []struct {
		name    string
		call    func(r *Recording) error
		from    []Status
		reached Status // status after success; "" means unchanged
	}{
		{"Begin", func(r *Recording) error { return r.Begin(later) }, []Status{StatusStarting}, StatusRecording},
		{"Heartbeat", func(r *Recording) error { return r.Heartbeat(later) }, []Status{StatusRecording}, ""},
		{"Stop", func(r *Recording) error { return r.Stop(later) }, []Status{StatusRecording}, StatusStopping},
		{"ReachLimit", func(r *Recording) error { return r.ReachLimit(later) }, []Status{StatusRecording}, StatusFinalizing},
		{"Interrupt", func(r *Recording) error { return r.Interrupt(later, "device lost") }, []Status{StatusRecording}, StatusFinalizing},
		{"Finalize", func(r *Recording) error { return r.Finalize(later) }, []Status{StatusStopping, StatusRecoveryRequired}, StatusFinalizing},
		{"ObserveChunks", func(r *Recording) error { return r.ObserveChunks(3, later) },
			[]Status{StatusStarting, StatusRecording, StatusStopping, StatusFinalizing, StatusRecoveryRequired}, ""},
		{"Complete", func(r *Recording) error {
			track = mustRef(t, "recordings/r1.wav")
			return r.Complete(later, track, time.Hour)
		}, []Status{StatusFinalizing}, StatusCompleted},
		{"Fail", func(r *Recording) error { return r.Fail(later, "broken") },
			[]Status{StatusStarting, StatusFinalizing, StatusRecoveryRequired}, StatusFailed},
		{"RequireRecovery", func(r *Recording) error { return r.RequireRecovery(later, "stale heartbeat") },
			[]Status{StatusRecording}, StatusRecoveryRequired},
	}
	for _, m := range methods {
		allowed := map[Status]bool{}
		for _, s := range m.from {
			allowed[s] = true
		}
		for _, s := range Statuses() {
			t.Run(m.name+"/"+string(s), func(t *testing.T) {
				r := inStatus(t, s)
				err := m.call(r)
				if !allowed[s] {
					if !errors.Is(err, ErrInvalidTransition) {
						t.Fatalf("error = %v, want ErrInvalidTransition", err)
					}
					if r.Status() != s {
						t.Fatalf("a refused call changed the status to %s", r.Status())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := m.reached
				if want == "" {
					want = s
				}
				if r.Status() != want {
					t.Fatalf("status = %s, want %s", r.Status(), want)
				}
				if !r.UpdatedAt().Equal(later) {
					t.Fatalf("updatedAt = %v", r.UpdatedAt())
				}
			})
		}
	}
}

func TestCompletedCannotRecordAgain(t *testing.T) {
	r := inStatus(t, StatusCompleted)
	if err := r.Begin(t0.Add(5 * time.Hour)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("COMPLETED -> RECORDING error = %v, want ErrInvalidTransition", err)
	}
	if r.Status() != StatusCompleted {
		t.Fatalf("status = %s", r.Status())
	}
}

func TestNew(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		now  time.Time
		ok   bool
	}{
		{"ad-hoc", Spec{ID: "r1", MaxDuration: testCap}, t0, true},
		{"with lesson", Spec{ID: "r1", LessonID: "l1", MaxDuration: time.Minute}, t0, true},
		{"no time", Spec{ID: "r1", MaxDuration: testCap}, time.Time{}, false},
		{"bad id", Spec{ID: "../x", MaxDuration: testCap}, t0, false},
		{"empty id", Spec{MaxDuration: testCap}, t0, false},
		{"bad lesson", Spec{ID: "r1", LessonID: "a b", MaxDuration: testCap}, t0, false},
		{"zero cap", Spec{ID: "r1"}, t0, false},
		{"negative cap", Spec{ID: "r1", MaxDuration: -time.Hour}, t0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(tc.spec, tc.now)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidRecording) {
					t.Fatalf("error = %v, want ErrInvalidRecording", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Status() != StatusStarting || r.Version() != 0 || r.ChunksDir().String() != "recordings/"+string(tc.spec.ID) {
				t.Fatalf("new recording: status %s, version %d, chunks %s", r.Status(), r.Version(), r.ChunksDir())
			}
			if r.LessonID() != tc.spec.LessonID || r.MaxDuration() != tc.spec.MaxDuration || !r.CreatedAt().Equal(t0) {
				t.Fatalf("fields not taken from the spec: %+v", r)
			}
			if _, ok := r.Duration(); ok || !r.Audio().IsZero() || r.Chunks() != 0 || r.Problem() != "" {
				t.Fatal("a new recording has results")
			}
		})
	}
}

func TestLimit(t *testing.T) {
	r := newRec(t)
	begin := t0.Add(10 * time.Second) // the cap counts from the start of the capture, not from the request
	if err := r.Begin(begin); err != nil {
		t.Fatal(err)
	}
	deadline := begin.Add(testCap)
	if !r.Deadline().Equal(deadline) || !r.StartedAt().Equal(begin) || !r.HeartbeatAt().Equal(begin) {
		t.Fatalf("deadline %v, started %v, heartbeat %v", r.Deadline(), r.StartedAt(), r.HeartbeatAt())
	}

	early := deadline.Add(-time.Millisecond)
	if r.LimitReached(early) {
		t.Fatal("limit reached before the deadline")
	}
	if err := r.ReachLimit(early); !errors.Is(err, ErrLimitNotReached) || r.Status() != StatusRecording {
		t.Fatalf("early ReachLimit = %v, status %s", err, r.Status())
	}

	if !r.LimitReached(deadline) {
		t.Fatal("limit not reached at the deadline")
	}
	if err := r.ReachLimit(deadline); err != nil {
		t.Fatal(err)
	}
	if r.Status() != StatusFinalizing || !r.StoppedAt().Equal(deadline) || r.Problem() != "" {
		t.Fatalf("after the limit: %s stopped %v problem %q", r.Status(), r.StoppedAt(), r.Problem())
	}
}

func TestHeartbeatAndStopTimes(t *testing.T) {
	r := inStatus(t, StatusRecording)
	hb := t0.Add(time.Hour)
	if err := r.Heartbeat(hb); err != nil || !r.HeartbeatAt().Equal(hb) {
		t.Fatalf("Heartbeat: %v, at %v", err, r.HeartbeatAt())
	}
	stop := hb.Add(time.Minute)
	if err := r.Stop(stop); err != nil || !r.StoppedAt().Equal(stop) {
		t.Fatalf("Stop: %v, at %v", err, r.StoppedAt())
	}
}

func TestFinishedRecordingKeepsResults(t *testing.T) {
	r := inStatus(t, StatusRecording)
	if err := r.Interrupt(t0.Add(time.Hour), "  output device\nchanged  "); err != nil {
		t.Fatal(err)
	}
	if r.Problem() != "output device changed" {
		t.Fatalf("problem = %q", r.Problem())
	}
	if err := r.ObserveChunks(60, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveChunks(-1, t0.Add(time.Hour)); !errors.Is(err, ErrInvalidRecording) || r.Chunks() != 60 {
		t.Fatalf("negative chunks: %v, chunks %d", err, r.Chunks())
	}
	track := mustRef(t, "recordings/r1.wav")
	bad := []struct {
		name  string
		audio shared.ArtifactRef
		d     time.Duration
	}{
		{"no track", shared.ArtifactRef{}, time.Hour},
		{"other kind", mustRef(t, "exports/r1.wav"), time.Hour},
		{"other recording", mustRef(t, "recordings/r2.wav"), time.Hour},
		{"id prefix only", mustRef(t, "recordings/r10.wav"), time.Hour},
		{"inside the chunk directory", mustRef(t, "recordings/r1/chunk-0001"), time.Hour},
		{"no extension", mustRef(t, "recordings/r1"), time.Hour},
		{"bad extension", mustRef(t, "recordings/r1.w-v"), time.Hour},
		{"nested", mustRef(t, "recordings/r1.wav/x"), time.Hour},
		{"negative duration", track, -time.Second},
		{"zero duration", track, 0},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Complete(t0.Add(time.Hour), tc.audio, tc.d); !errors.Is(err, ErrInvalidRecording) || r.Status() != StatusFinalizing {
				t.Fatalf("Complete(%v, %v) = %v, status %s", tc.audio, tc.d, err, r.Status())
			}
		})
	}
	if err := r.Complete(t0.Add(time.Hour), track, 59*time.Minute); err != nil {
		t.Fatal(err)
	}
	if d, ok := r.Duration(); !ok || d != 59*time.Minute || r.Audio() != track || r.Problem() == "" {
		t.Fatalf("results: %v %v %v %q", d, ok, r.Audio(), r.Problem())
	}
}

func TestProblemText(t *testing.T) {
	r := inStatus(t, StatusStarting)
	for _, empty := range []string{"", "   ", "\n\t"} {
		if err := r.Fail(t0, empty); !errors.Is(err, ErrInvalidRecording) || r.Status() != StatusStarting {
			t.Fatalf("Fail(%q) = %v", empty, err)
		}
	}
	long := strings.Repeat("я", MaxProblemBytes) // two bytes per rune: the cut must respect rune boundaries
	if err := r.Fail(t0, long+"\x00"); err != nil {
		t.Fatal(err)
	}
	if len(r.Problem()) > MaxProblemBytes || !strings.HasPrefix(long, r.Problem()) || r.Problem() == "" {
		t.Fatalf("problem has %d bytes", len(r.Problem()))
	}
}

func TestCheckDeletable(t *testing.T) {
	deletable := map[Status]bool{StatusCompleted: true, StatusFailed: true, StatusRecoveryRequired: true}
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			err := inStatus(t, s).CheckDeletable()
			if deletable[s] != (err == nil) {
				t.Fatalf("CheckDeletable = %v", err)
			}
			if err != nil && !errors.Is(err, ErrNotDeletable) {
				t.Fatalf("error = %v, want ErrNotDeletable", err)
			}
		})
	}
}

func TestTranscribableAudio(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			r := inStatus(t, s)
			audio, ok := r.TranscribableAudio()
			want := s == StatusCompleted
			if ok != want {
				t.Fatalf("TranscribableAudio ok = %v, want %v", ok, want)
			}
			if want && audio != mustRef(t, "recordings/r1.wav") {
				t.Fatalf("TranscribableAudio = %v, want the final track", audio)
			}
			if !want && !audio.IsZero() {
				t.Fatalf("TranscribableAudio = %v, want the zero reference", audio)
			}
		})
	}
}

func TestTrackRef(t *testing.T) {
	r := newRec(t)
	cases := []struct {
		ext  string
		want string
	}{
		{"wav", "recordings/r1.wav"},
		{"flac", "recordings/r1.flac"},
		{"", ""},
		{"w/v", ""},
		{"wav.", ""},
		{"wäv", ""},
		{"verylongext", ""},
	}
	for _, tc := range cases {
		got, err := r.TrackRef(tc.ext)
		if tc.want == "" {
			if err == nil {
				t.Errorf("TrackRef(%q) = %v, want an error", tc.ext, got)
			}
			continue
		}
		if err != nil || got.String() != tc.want {
			t.Errorf("TrackRef(%q) = %v, %v; want %s", tc.ext, got, err, tc.want)
		}
	}
}

func TestPersisted(t *testing.T) {
	r := newRec(t)
	r.Persisted(4)
	if r.Version() != 4 {
		t.Fatalf("Version = %d", r.Version())
	}
}
