package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentcompact"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeCompactor answers Compact with run and records the chats asked.
type fakeCompactor struct {
	mu    sync.Mutex
	asked []shared.ThreadID
	run   func(started func()) (agentcompact.Result, error)
}

func (f *fakeCompactor) Compact(_ context.Context, id shared.ThreadID, started func()) (agentcompact.Result, error) {
	f.mu.Lock()
	f.asked = append(f.asked, id)
	f.mu.Unlock()
	return f.run(started)
}

func (f *fakeCompactor) calls() []shared.ThreadID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]shared.ThreadID(nil), f.asked...)
}

// compactEnv is a Driver with a compactor, a log and a script whose Ask records how many updates were out by then.
type compactEnv struct {
	d       *Driver
	sink    *chanSink
	turns   *scriptedTurns
	log     *bytes.Buffer // read after Wait
	atAsk   []int
	compact *fakeCompactor
}

func newCompactEnv(t *testing.T, run func(started func()) (agentcompact.Result, error)) *compactEnv {
	t.Helper()
	e := &compactEnv{sink: &chanSink{}, log: &bytes.Buffer{}, compact: &fakeCompactor{run: run}}
	e.turns = (&scriptedTurns{}).then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Готово"}), nil)
	e.turns.beforeAnswer = func() { e.atAsk = append(e.atAsk, len(e.sink.all())) }
	deps := newReads().deps(e.turns, e.sink)
	deps.Compactor = e.compact
	deps.Log = slog.New(slog.NewTextHandler(e.log, nil))
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	e.d = d
	return e
}

func (e *compactEnv) ask(t *testing.T, id shared.ThreadID) []Update {
	t.Helper()
	if err := e.d.Ask(t.Context(), agentturn.AskInput{ThreadID: id, Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	e.d.Wait()
	return e.sink.all()
}

func compacted(fill agentcompact.Fill) func(func()) (agentcompact.Result, error) {
	return func(started func()) (agentcompact.Result, error) {
		started()
		return agentcompact.Result{Thread: th1, Compacted: true, Fill: fill}, nil
	}
}

// checkCompactionUpdates checks the start and the end of a compaction, ups[0] and ups[1], then the request's own
// update, and that the Ask ran only once the end was published.
func checkCompactionUpdates(t *testing.T, e *compactEnv, ups []Update, fill *agentcompact.Fill) {
	t.Helper()
	if len(ups) != 3 {
		t.Fatalf("updates %+v", ups)
	}
	start, end, req := ups[0], ups[1], ups[2]
	if start.Thread != th1 || !start.Compaction || !start.Compacting || start.Fill != nil || start.Turn != 0 ||
		start.Status != "" || len(start.Cards) != 0 || start.Problem != ProblemNone || start.Progress {
		t.Fatalf("start %+v", start)
	}
	if end.Thread != th1 || !end.Compaction || end.Compacting || end.Turn != 0 || end.Status != "" || len(end.Cards) != 0 {
		t.Fatalf("end %+v", end)
	}
	if (end.Fill == nil) != (fill == nil) || (fill != nil && *end.Fill != *fill) {
		t.Fatalf("end fill %+v, want %+v", end.Fill, fill)
	}
	if req.Compaction || req.Compacting || req.Fill != nil || req.Status != thread.StatusDone {
		t.Fatalf("request update %+v", req)
	}
	if len(e.atAsk) != 1 || e.atAsk[0] != 2 {
		t.Fatalf("the Ask ran with %v updates out, want after both compaction updates", e.atAsk)
	}
}

// Before the Ask of a named chat: compacting, then the end with the new fill, then the request as before.
func TestAskCompactsFirst(t *testing.T) {
	fill := agentcompact.Fill{Percent: 40, Level: thread.FillNormal}
	e := newCompactEnv(t, compacted(fill))
	ups := e.ask(t, th1)
	checkCompactionUpdates(t, e, ups, &fill)
	if got := e.compact.calls(); len(got) != 1 || got[0] != th1 {
		t.Fatalf("compacted %v", got)
	}
}

// A failed compaction ends without a fill, is logged by its code only, and the request still runs to its end.
func TestFailedCompactionDoesNotStopTheRequest(t *testing.T) {
	const secret = "СЕКРЕТ-причины"
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"unavailable", fmt.Errorf("agentcompact: compact: %w: %w", agentcompact.ErrUnavailable, errors.New(secret)), "unavailable"},
		{"refused", fmt.Errorf("agentcompact: compact: %w: %w", agentcompact.ErrRefused, errors.New(secret)), "refused"},
		{"quota", fmt.Errorf("x: %w: %s", agentcompact.ErrQuota, secret), "quota"},
		{"changed", fmt.Errorf("x: %w: %s", agentcompact.ErrChanged, secret), "changed"},
		{"store", fmt.Errorf("x: %w: %s", agentcompact.ErrStore, secret), "store"},
		{"unknown", errors.New(secret), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCompactEnv(t, func(started func()) (agentcompact.Result, error) {
				started()
				return agentcompact.Result{}, tt.err
			})
			ups := e.ask(t, th1)
			checkCompactionUpdates(t, e, ups, nil)
			logged := e.log.String()
			if strings.Contains(logged, secret) || !strings.Contains(logged, "code="+tt.code) ||
				strings.Count(logged, opCompact) != 1 {
				t.Fatalf("log = %s", logged)
			}
		})
	}
}

// A compaction that panics after it started still ends for the panel, and the request runs.
func TestCompactionPanicStillEnds(t *testing.T) {
	e := newCompactEnv(t, func(started func()) (agentcompact.Result, error) {
		started()
		panic("СЕКРЕТ-паники")
	})
	ups := e.ask(t, th1)
	checkCompactionUpdates(t, e, ups, nil)
	if logged := e.log.String(); !strings.Contains(logged, "code=panic") || strings.Contains(logged, "СЕКРЕТ") {
		t.Fatalf("log = %s", logged)
	}
}

// No Hub call, no compaction updates: a chat whose request runs (busy before started), a chat with room, and a
// compactor started twice publishes once.
func TestCompactionWithoutHubCall(t *testing.T) {
	for name, run := range map[string]func(func()) (agentcompact.Result, error){
		"busy": func(func()) (agentcompact.Result, error) {
			return agentcompact.Result{}, fmt.Errorf("agentcompact: compact: %w: %w", agentcompact.ErrBusy, thread.ErrTurnInProgress)
		},
		"room": func(func()) (agentcompact.Result, error) {
			return agentcompact.Result{Thread: th1, Fill: agentcompact.Fill{Percent: 30, Level: thread.FillNormal}}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newCompactEnv(t, run)
			ups := e.ask(t, th1)
			if len(ups) != 1 || ups[0].Compaction || ups[0].Status != thread.StatusDone || len(e.compact.calls()) != 1 {
				t.Fatalf("updates %+v, compacted %v", ups, e.compact.calls())
			}
		})
	}
	t.Run("started twice", func(t *testing.T) {
		e := newCompactEnv(t, func(started func()) (agentcompact.Result, error) {
			started()
			started()
			return agentcompact.Result{Thread: th1}, nil
		})
		if ups := e.ask(t, th1); len(ups) != 3 {
			t.Fatalf("updates %+v", ups)
		}
	})
}

// An Ask of the current chat (no id) is not compacted: the Driver cannot name the chat before the Hub call. Only an Ask
// compacts.
func TestOnlyAnAskOfANamedChatCompacts(t *testing.T) {
	e := newCompactEnv(t, compacted(agentcompact.Fill{}))
	if ups := e.ask(t, ""); len(ups) != 1 || ups[0].Compaction {
		t.Fatalf("updates %+v", ups)
	}

	e.turns.then(step(thread.StatusDone, nil), nil)
	if err := e.d.Rerun(t.Context(), agentturn.TurnRef{Thread: th1, Turn: 1}); err != nil {
		t.Fatal(err)
	}
	e.d.Wait()
	if got := e.compact.calls(); len(got) != 0 {
		t.Fatalf("compacted %v", got)
	}
}

// Without a compactor an Ask runs as before.
func TestAskWithoutCompactor(t *testing.T) {
	turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil), nil)
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{ThreadID: th1, Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if ups := sink.all(); len(ups) != 1 || ups[0].Compaction {
		t.Fatalf("updates %+v", ups)
	}
}

// The compaction runs in the operation's goroutine: Ask returns while it still runs.
func TestCompactionDoesNotHoldTheCaller(t *testing.T) {
	release := make(chan struct{})
	e := newCompactEnv(t, func(started func()) (agentcompact.Result, error) {
		started()
		<-release
		return agentcompact.Result{Thread: th1}, nil
	})
	if err := e.d.Ask(t.Context(), agentturn.AskInput{ThreadID: th1, Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	e.d.Wait()
	if ups := e.sink.all(); len(ups) != 3 {
		t.Fatalf("updates %+v", ups)
	}
}
