package job

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

// The expected pairs are copied from the diagram of PRD section 7.2, independently of the table under test.
func TestTransitionMatrix(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusQueued, StatusRunning}:              true,
		{StatusRunning, StatusCompleted}:           true,
		{StatusRunning, StatusRetryWait}:           true,
		{StatusRetryWait, StatusQueued}:            true,
		{StatusRunning, StatusFailed}:              true,
		{StatusRunning, StatusWaitingForNetwork}:   true,
		{StatusWaitingForNetwork, StatusQueued}:    true,
		{StatusRunning, StatusWaitingForQuota}:     true,
		{StatusWaitingForQuota, StatusQueued}:      true,
		{StatusQueued, StatusWaitingForPower}:      true,
		{StatusRunning, StatusWaitingForPower}:     true,
		{StatusWaitingForPower, StatusQueued}:      true,
		{StatusQueued, StatusCancelled}:            true,
		{StatusRunning, StatusCancelled}:           true,
		{StatusRetryWait, StatusCancelled}:         true,
		{StatusWaitingForNetwork, StatusCancelled}: true,
		{StatusWaitingForQuota, StatusCancelled}:   true,
		{StatusWaitingForPower, StatusCancelled}:   true,
		{StatusRunning, StatusQueued}:              true,
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
	if CanTransition("BOGUS", StatusQueued) || CanTransition(StatusQueued, "BOGUS") {
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

func TestTerminalIsDerivedFromTable(t *testing.T) {
	for _, s := range Statuses() {
		if got, want := s.IsTerminal(), len(transitions[s]) == 0; got != want {
			t.Errorf("%s: IsTerminal = %v, table says %v", s, got, want)
		}
	}
	for _, s := range []Status{StatusCompleted, StatusFailed, StatusCancelled} {
		if !s.IsTerminal() {
			t.Errorf("%s must be terminal", s)
		}
	}
}

func newJob(t *testing.T) *Job {
	t.Helper()
	j, err := New(Spec{ID: "j1", Type: "stt", Key: "stt:r1", MaxAttempts: 3}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// inStatus builds a job that reached status by legal moves.
func inStatus(t *testing.T, s Status) *Job {
	t.Helper()
	j := newJob(t)
	steps := map[Status]func() error{
		StatusQueued:            func() error { return nil },
		StatusRunning:           func() error { return j.Claim(t0) },
		StatusCompleted:         func() error { return firstErr(j.Claim(t0), j.Complete(t0)) },
		StatusRetryWait:         func() error { return firstErr(j.Claim(t0), j.Retry(t0, t0.Add(time.Minute), "x")) },
		StatusCancelled:         func() error { return j.Cancel(t0) },
		StatusFailed:            func() error { return firstErr(j.Claim(t0), j.Fail(t0, "x")) },
		StatusWaitingForNetwork: func() error { return firstErr(j.Claim(t0), j.WaitFor(s, t0)) },
		StatusWaitingForQuota:   func() error { return firstErr(j.Claim(t0), j.WaitFor(s, t0)) },
		StatusWaitingForPower:   func() error { return j.WaitFor(s, t0) },
	}
	if err := steps[s](); err != nil {
		t.Fatalf("reaching %s: %v", s, err)
	}
	if j.Status() != s {
		t.Fatalf("reached %s, want %s", j.Status(), s)
	}
	return j
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// Every method applied in every status: it succeeds exactly where the table allows the target, and a refused call
// leaves the job unchanged.
func TestMethodsFollowTheTable(t *testing.T) {
	now := t0.Add(time.Hour)
	methods := []struct {
		name string
		to   Status
		call func(j *Job) error
	}{
		{"Claim", StatusRunning, func(j *Job) error { return j.Claim(now) }},
		{"Complete", StatusCompleted, func(j *Job) error { return j.Complete(now) }},
		{"Fail", StatusFailed, func(j *Job) error { return j.Fail(now, "boom") }},
		{"Cancel", StatusCancelled, func(j *Job) error { return j.Cancel(now) }},
		{"Retry", StatusRetryWait, func(j *Job) error { return j.Retry(now, now.Add(time.Minute), "net") }},
		{"WaitForNetwork", StatusWaitingForNetwork, func(j *Job) error { return j.WaitFor(StatusWaitingForNetwork, now) }},
		{"WaitForQuota", StatusWaitingForQuota, func(j *Job) error { return j.WaitFor(StatusWaitingForQuota, now) }},
		{"WaitForPower", StatusWaitingForPower, func(j *Job) error { return j.WaitFor(StatusWaitingForPower, now) }},
		{"Requeue", StatusQueued, func(j *Job) error { return j.Requeue(now) }},
	}
	for _, m := range methods {
		for _, from := range Statuses() {
			t.Run(m.name+" from "+from.String(), func(t *testing.T) {
				j := inStatus(t, from)
				before := *j
				err := m.call(j)
				if CanTransition(from, m.to) {
					if err != nil {
						t.Fatalf("allowed move failed: %v", err)
					}
					if j.Status() != m.to {
						t.Fatalf("status = %s, want %s", j.Status(), m.to)
					}
					if !j.UpdatedAt().Equal(now) {
						t.Fatalf("updatedAt = %v", j.UpdatedAt())
					}
					return
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("error = %v, want ErrInvalidTransition", err)
				}
				if *j != before {
					t.Fatal("a refused transition changed the job")
				}
			})
		}
	}
}

func TestCompletedCannotRun(t *testing.T) {
	j := inStatus(t, StatusCompleted)
	if err := j.Claim(t0.Add(time.Hour)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("COMPLETED to RUNNING: %v", err)
	}
}

func TestTerminalStatesStayFinished(t *testing.T) {
	for _, s := range []Status{StatusCompleted, StatusFailed, StatusCancelled} {
		j := inStatus(t, s)
		if !j.FinishedAt().Equal(t0) {
			t.Errorf("%s: finishedAt = %v", s, j.FinishedAt())
		}
		for _, call := range []func() error{
			func() error { return j.Requeue(t0) }, func() error { return j.Cancel(t0) },
			func() error { return j.ForceRun(t0) }, func() error { return j.WaitFor(StatusWaitingForPower, t0) },
		} {
			if err := call(); !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s: terminal job accepted a change: %v", s, err)
			}
		}
	}
}

func TestNewJob(t *testing.T) {
	good := Spec{ID: "j1", Type: "stt", Key: "k", MaxAttempts: 2}
	with := func(f func(*Spec)) Spec { s := good; f(&s); return s }
	cases := []struct {
		name    string
		spec    Spec
		now     time.Time
		invalid bool
	}{
		{"minimal", good, t0, false},
		{"full", with(func(s *Spec) {
			s.Payload = `{"recording":"r1"}`
			s.PinnedModelID = "m1"
			s.RunAt = t0.Add(time.Minute)
			s.RunNow = true
		}), t0, false},
		{"no id", with(func(s *Spec) { s.ID = "" }), t0, true},
		{"bad id", with(func(s *Spec) { s.ID = "a/b" }), t0, true},
		{"no type", with(func(s *Spec) { s.Type = "" }), t0, true},
		{"type with space", with(func(s *Spec) { s.Type = "a b" }), t0, true},
		{"no key", with(func(s *Spec) { s.Key = "" }), t0, true},
		{"key with newline", with(func(s *Spec) { s.Key = "a\nb" }), t0, true},
		{"key too long", with(func(s *Spec) { s.Key = strings.Repeat("k", MaxKeyBytes+1) }), t0, true},
		{"payload not json", with(func(s *Spec) { s.Payload = "{" }), t0, true},
		{"payload too big", with(func(s *Spec) { s.Payload = `"` + strings.Repeat("x", MaxPayloadBytes) + `"` }), t0, true},
		{"bad model id", with(func(s *Spec) { s.PinnedModelID = "a/b" }), t0, true},
		{"zero attempts", with(func(s *Spec) { s.MaxAttempts = 0 }), t0, true},
		{"no time", good, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j, err := New(tc.spec, tc.now)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidJob) || j != nil {
					t.Fatalf("New = %v, %v; want ErrInvalidJob", j, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if j.Status() != StatusQueued || j.Version() != 0 || j.Attempts() != 0 || !j.CreatedAt().Equal(tc.now) {
				t.Fatalf("unexpected new job %+v", j)
			}
			wantRun := tc.spec.RunAt
			if wantRun.IsZero() {
				wantRun = tc.now
			}
			if !j.NextRunAt().Equal(wantRun) || j.RunNow() != tc.spec.RunNow {
				t.Fatalf("nextRunAt %v runNow %v", j.NextRunAt(), j.RunNow())
			}
			if tc.spec.Payload == "" && j.Payload() != "{}" {
				t.Fatalf("payload = %q", j.Payload())
			}
		})
	}
}

func TestClaim(t *testing.T) {
	t.Run("not due", func(t *testing.T) {
		j, _ := New(Spec{ID: "j", Type: "x", Key: "k", MaxAttempts: 1, RunAt: t0.Add(time.Minute)}, t0)
		if err := j.Claim(t0); !errors.Is(err, ErrNotDue) || j.Status() != StatusQueued || j.Attempts() != 0 {
			t.Fatalf("err = %v, status %s, attempts %d", err, j.Status(), j.Attempts())
		}
		if err := j.Claim(t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("counts the attempt and sets start", func(t *testing.T) {
		j := newJob(t)
		if err := j.Claim(t0.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if j.Attempts() != 1 || !j.StartedAt().Equal(t0.Add(time.Second)) || j.FinishedAt() != (time.Time{}) {
			t.Fatalf("attempts %d started %v", j.Attempts(), j.StartedAt())
		}
		if s := j.Snapshot(); s.Attempt() != 1 || s.Status() != StatusRunning {
			t.Fatalf("snapshot %+v", s)
		}
	})
	t.Run("run now is spent by the claim", func(t *testing.T) {
		j, _ := New(Spec{ID: "j", Type: "x", Key: "k", MaxAttempts: 1, RunNow: true}, t0)
		if err := j.Claim(t0); err != nil || j.RunNow() {
			t.Fatalf("err %v runNow %v", err, j.RunNow())
		}
	})
	t.Run("no attempts left", func(t *testing.T) {
		j, err := Restore(State{Version: 1, ID: "j", Type: "x", Status: StatusQueued, Key: "k", Payload: "{}", Attempts: 2, MaxAttempts: 2,
			NextRunAt: t0, CreatedAt: t0, UpdatedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Claim(t0); !errors.Is(err, ErrAttemptsExhausted) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRetryAndExhaustion(t *testing.T) {
	j := newJob(t) // 3 attempts
	for i := 1; i <= 3; i++ {
		if err := j.Claim(t0.Add(time.Duration(i) * time.Hour)); err != nil {
			t.Fatalf("claim #%d: %v", i, err)
		}
		now := t0.Add(time.Duration(i)*time.Hour + time.Minute)
		if err := j.Retry(now, now.Add(time.Minute), "network reset"); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			if j.Status() != StatusRetryWait || !j.NextRunAt().Equal(now.Add(time.Minute)) || j.LastError() != "network reset" {
				t.Fatalf("after retry #%d: %s %v %q", i, j.Status(), j.NextRunAt(), j.LastError())
			}
			if err := j.Requeue(now); err != nil || j.Status() != StatusQueued || !j.NextRunAt().Equal(now.Add(time.Minute)) {
				t.Fatalf("requeue: %v %s %v", err, j.Status(), j.NextRunAt())
			}
		}
	}
	if j.Status() != StatusFailed || j.FinishedAt().IsZero() {
		t.Fatalf("exhausted job is %s", j.Status())
	}
}

func TestRetryClampsPastRunAt(t *testing.T) {
	j := inStatus(t, StatusRunning)
	now := t0.Add(time.Hour)
	if err := j.Retry(now, t0, "x"); err != nil || !j.NextRunAt().Equal(now) {
		t.Fatalf("err %v nextRunAt %v", err, j.NextRunAt())
	}
}

func TestWaitForGivesAttemptBack(t *testing.T) {
	j := inStatus(t, StatusRunning)
	if err := j.WaitFor(StatusWaitingForPower, t0); err != nil || j.Attempts() != 0 {
		t.Fatalf("err %v attempts %d", err, j.Attempts())
	}
	if err := j.WaitFor(StatusQueued, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("non-waiting target: %v", err)
	}
	if err := j.WaitFor(StatusCompleted, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("COMPLETED as waiting target: %v", err)
	}
}

func TestWaitForClearsRunNow(t *testing.T) {
	j, _ := New(Spec{ID: "j", Type: "x", Key: "k", MaxAttempts: 2, RunNow: true}, t0)
	if err := j.WaitFor(StatusWaitingForPower, t0); err != nil || j.RunNow() {
		t.Fatalf("err %v runNow %v", err, j.RunNow())
	}
}

func TestCancelFromWaitingStatuses(t *testing.T) {
	for _, s := range []Status{StatusRetryWait, StatusWaitingForNetwork, StatusWaitingForQuota, StatusWaitingForPower} {
		j := inStatus(t, s)
		now := t0.Add(time.Hour)
		if err := j.Cancel(now); err != nil || j.Status() != StatusCancelled || !j.FinishedAt().Equal(now) {
			t.Errorf("%s: err %v status %s finished %v", s, err, j.Status(), j.FinishedAt())
		}
	}
}

func TestRequeueDueAtOnce(t *testing.T) {
	for _, s := range []Status{StatusWaitingForNetwork, StatusWaitingForQuota, StatusWaitingForPower} {
		j := inStatus(t, s)
		now := t0.Add(time.Hour)
		if err := j.Requeue(now); err != nil || j.Status() != StatusQueued || !j.NextRunAt().Equal(now) {
			t.Fatalf("%s: err %v status %s next %v", s, err, j.Status(), j.NextRunAt())
		}
	}
}

func TestRequeueStaleRunning(t *testing.T) {
	j := inStatus(t, StatusRunning)
	if err := j.Requeue(t0.Add(time.Hour)); err != nil || j.Status() != StatusQueued || j.Attempts() != 1 {
		t.Fatalf("err %v status %s attempts %d", err, j.Status(), j.Attempts())
	}
	last, _ := New(Spec{ID: "j2", Type: "x", Key: "k2", MaxAttempts: 1}, t0)
	_ = last.Claim(t0)
	if err := last.Requeue(t0); err != nil || last.Status() != StatusFailed || last.FinishedAt().IsZero() {
		t.Fatalf("last attempt: err %v status %s", err, last.Status())
	}
}

func TestForceRun(t *testing.T) {
	now := t0.Add(time.Hour)
	queued := inStatus(t, StatusQueued)
	if err := queued.ForceRun(now); err != nil || !queued.RunNow() || queued.Status() != StatusQueued {
		t.Fatalf("queued: %v %v %s", err, queued.RunNow(), queued.Status())
	}
	waiting := inStatus(t, StatusWaitingForPower)
	if err := waiting.ForceRun(now); err != nil || !waiting.RunNow() || waiting.Status() != StatusQueued || !waiting.NextRunAt().Equal(now) {
		t.Fatalf("waiting: %v %v %s", err, waiting.RunNow(), waiting.Status())
	}
	for _, s := range []Status{StatusRunning, StatusRetryWait, StatusWaitingForQuota, StatusCompleted} {
		if err := inStatus(t, s).ForceRun(now); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestReportProgress(t *testing.T) {
	p, _ := NewProgress(40, "chunk=4")
	j := inStatus(t, StatusRunning)
	if err := j.ReportProgress(p, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := j.Progress(); got != p || j.Snapshot().Checkpoint() != "chunk=4" {
		t.Fatalf("progress %+v", got)
	}
	if err := j.Complete(t0.Add(time.Minute)); err != nil || j.Progress().Percent() != 100 {
		t.Fatalf("complete: %v %d", err, j.Progress().Percent())
	}
	for _, s := range []Status{StatusQueued, StatusCompleted, StatusWaitingForPower} {
		if err := inStatus(t, s).ReportProgress(p, t0); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s accepted progress: %v", s, err)
		}
	}
}

func TestErrorTextIsBounded(t *testing.T) {
	j := inStatus(t, StatusRunning)
	if err := j.Fail(t0, strings.Repeat("é", MaxErrorBytes)+"\xff"); err != nil {
		t.Fatal(err)
	}
	msg := j.LastError()
	if len(msg) > MaxErrorBytes || !strings.HasPrefix(msg, "é") || strings.ContainsRune(msg, 0xFFFD) {
		t.Fatalf("message length %d", len(msg))
	}
}

func TestRestore(t *testing.T) {
	good := State{Version: 2, ID: "j", Type: "stt", Status: StatusRunning, Key: "k", Payload: "{}", Attempts: 1, MaxAttempts: 3,
		NextRunAt: t0, Percent: 10, Checkpoint: "c", CreatedAt: t0, StartedAt: t0, UpdatedAt: t0}
	with := func(f func(*State)) State { s := good; f(&s); return s }
	cases := []struct {
		name    string
		state   State
		invalid bool
	}{
		{"running", good, false},
		{"completed", with(func(s *State) { s.Status = StatusCompleted; s.FinishedAt = t0 }), false},
		{"version zero", with(func(s *State) { s.Version = 0 }), true},
		{"unknown status", with(func(s *State) { s.Status = "BOGUS" }), true},
		{"attempts over max", with(func(s *State) { s.Attempts = 4 }), true},
		{"negative attempts", with(func(s *State) { s.Attempts = -1 }), true},
		{"terminal without finish", with(func(s *State) { s.Status = StatusFailed }), true},
		{"bad percent", with(func(s *State) { s.Percent = 101 }), true},
		{"bad payload", with(func(s *State) { s.Payload = "nope" }), true},
		{"running without start", with(func(s *State) { s.StartedAt = time.Time{} }), true},
		{"queued with finish", with(func(s *State) { s.Status = StatusQueued; s.FinishedAt = t0 }), true},
		{"running with finish", with(func(s *State) { s.FinishedAt = t0 }), true},
		{"retry wait with finish", with(func(s *State) { s.Status = StatusRetryWait; s.FinishedAt = t0 }), true},
		{"queued never started", with(func(s *State) { s.Status = StatusQueued; s.StartedAt = time.Time{}; s.Attempts = 0 }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j, err := Restore(tc.state)
			if tc.invalid {
				if err == nil || j != nil {
					t.Fatalf("Restore = %v, %v; want an error", j, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if j.Version() != tc.state.Version || j.Status() != tc.state.Status || j.Attempts() != tc.state.Attempts ||
				j.Progress().Checkpoint() != tc.state.Checkpoint {
				t.Fatalf("restored %+v", j)
			}
		})
	}
}

func TestSnapshotCarriesRunnerFields(t *testing.T) {
	j, _ := New(Spec{ID: "j", Type: "stt", Key: "k", Payload: `{"a":1}`, PinnedModelID: shared.SttModelID("m1"), MaxAttempts: 2}, t0)
	s := j.Snapshot()
	if s.Payload() != `{"a":1}` || s.PinnedModelID() != "m1" || s.Attempt() != 0 {
		t.Fatalf("snapshot %+v", s)
	}
}
