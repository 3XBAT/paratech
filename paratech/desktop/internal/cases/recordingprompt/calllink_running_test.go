package recordingprompt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeRunning is the recording session as RunningRecordings: the ids in runs run.
type fakeRunning struct {
	mu    sync.Mutex
	runs  []shared.RecordingID
	asked []shared.RecordingID
}

func (f *fakeRunning) IsRunning(id shared.RecordingID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	return slices.Contains(f.runs, id)
}

func (f *fakeRunning) set(ids ...shared.RecordingID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = ids
}

// TASK-405: the prompt a recording was started from keeps «Подключиться» while that recording runs, and only it.
func TestCallSubjectWhileTheStartedRecordingRuns(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T, c *callEnv, id prompt.ID) {
		t.Helper()
		if _, err := c.svc.Start(ctx, id, values.WholeComputer()); err != nil {
			t.Fatal(err)
		}
	}
	// Each scenario returns the prompt id the popup page asks for; the fake recording rec-1 runs unless stopped.
	tests := []struct {
		name    string
		run     func(t *testing.T, c *callEnv) prompt.ID
		stopped bool
		want    shared.SubjectID
	}{
		{name: "offered", want: "sub-1", run: func(t *testing.T, c *callEnv) prompt.ID {
			return c.offer("lesson-1").ID
		}},
		{name: "its recording runs", want: "sub-1", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			return v.ID
		}},
		{name: "its recording stopped", stopped: true, run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			return v.ID
		}},
		{name: "another id while it runs", run: func(t *testing.T, c *callEnv) prompt.ID {
			start(t, c, c.offer("lesson-1").ID)
			return "prompt-9"
		}},
		{name: "a newer prompt is on offer", want: "sub-1", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			if next := c.offer("lesson-2"); next.ID == v.ID {
				t.Fatal("the newer prompt reuses the id")
			}
			return v.ID
		}},
		{name: "ad hoc prompt started", run: func(t *testing.T, c *callEnv) prompt.ID {
			v, err := c.svc.OfferAdHoc(ctx)
			if err != nil {
				t.Fatal(err)
			}
			start(t, c, v.ID)
			return v.ID
		}},
		{name: "a later ad hoc start", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			adHoc, err := c.svc.OfferAdHoc(ctx)
			if err != nil {
				t.Fatal(err)
			}
			start(t, c, adHoc.ID) // the fake names this recording rec-1 too: only the latest start counts
			return v.ID
		}},
		{name: "started from the main window", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			if _, err := c.svc.StartManual(ctx, values.WholeComputer()); err != nil {
				t.Fatal(err)
			}
			return v.ID
		}},
		{name: "failed start stays on offer", want: "sub-1", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			c.recordings.set(true, errors.New("device busy"))
			if _, err := c.svc.Start(ctx, v.ID, values.WholeComputer()); !errors.Is(err, ErrStartFailed) {
				t.Fatalf("Start: %v, want ErrStartFailed", err)
			}
			return v.ID
		}},
		{name: "running port not wired", run: func(t *testing.T, c *callEnv) prompt.ID {
			c.svc.running = nil
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			return v.ID
		}},
		{name: "link removed meanwhile", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			c.links.mu.Lock()
			c.links.links = nil
			c.links.mu.Unlock()
			return v.ID
		}},
		{name: "lesson removed meanwhile", run: func(t *testing.T, c *callEnv) prompt.ID {
			v := c.offer("lesson-1")
			start(t, c, v.ID)
			c.lessons.mu.Lock()
			c.lessons.lessons = nil
			c.lessons.mu.Unlock()
			return v.ID
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCallEnv(t, callSubjectName)
			if !tt.stopped {
				c.running.set("rec-1")
			}
			id := tt.run(t, c)

			if got := c.svc.CallSubject(ctx, id); got != tt.want {
				t.Fatalf("CallSubject = %q, want %q", got, tt.want)
			}
			if logs := c.logs.String(); strings.Contains(logs, opCallSubject) || strings.Contains(logs, "FakePwd42") {
				t.Errorf("unexpected log: %q", logs)
			}
		})
	}
}

// The started branch asks about the recording the start returned, and reads the subjects only while it runs.
func TestCallSubjectAfterStartAsksItsRecording(t *testing.T) {
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	v := c.offer("lesson-1")
	if _, err := c.svc.Start(ctx, v.ID, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}

	if got := c.svc.CallSubject(ctx, v.ID); got != "" || len(c.subjects.names) != 0 {
		t.Fatalf("not running: %q, names %v", got, c.subjects.names)
	}
	c.running.set("rec-1")
	if got := c.svc.CallSubject(ctx, v.ID); got != "sub-1" {
		t.Fatalf("running: %q", got)
	}
	c.running.set()
	if got := c.svc.CallSubject(ctx, v.ID); got != "" {
		t.Fatalf("after stop: %q", got)
	}
	if !slices.Equal(c.running.asked, []shared.RecordingID{"rec-1", "rec-1", "rec-1"}) {
		t.Fatalf("asked %v", c.running.asked)
	}
}

// A failure on the started branch is logged like on an offer: one code, no cause.
func TestCallSubjectAfterStartFailureLogged(t *testing.T) {
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	c.running.set("rec-1")
	v := c.offer("lesson-1")
	if _, err := c.svc.Start(ctx, v.ID, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	c.links.mu.Lock()
	c.links.err = errors.New(`read C:\Users\student\db: row "` + testCallLink + `"`)
	c.links.mu.Unlock()

	if got := c.svc.CallSubject(ctx, v.ID); got != "" {
		t.Fatalf("CallSubject = %q, want none", got)
	}
	logs := c.logs.String()
	if strings.Count(logs, opCallSubject) != 1 || !strings.Contains(logs, "call links unread") {
		t.Fatalf("log %q, want one call links failure", logs)
	}
	for _, secret := range []string{"student", "FakePwd42", "meet.example.org"} {
		if strings.Contains(logs, secret) {
			t.Errorf("log carries %q: %q", secret, logs)
		}
	}
}

// CallSubject races with starts and offers without a data race (run with -race).
func TestCallSubjectRacesWithStart(t *testing.T) {
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	c.running.set("rec-1")
	v := c.offer("lesson-1")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			_ = c.svc.CallSubject(ctx, v.ID)
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = c.svc.Start(ctx, v.ID, values.WholeComputer())
		_, _ = c.svc.OfferAdHoc(ctx)
	}()
	wg.Wait()
}
