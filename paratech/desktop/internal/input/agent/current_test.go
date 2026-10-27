package agent

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// TestDriverCurrent: the current request of a chat comes back as an update with the checks of a published one (a
// broken card is dropped), and nothing runs: no update is published, no step use case operation is called.
func TestDriverCurrent(t *testing.T) {
	scope := subjectScope(t, "s1")
	stopped := step(thread.StatusStopped, nil, values.StoppedCard{Reason: values.StopOffline}, values.QuestionsCard{Round: 3})
	stopped.StopReason, stopped.Scope = values.StopOffline, scope
	tests := []struct {
		name    string
		current func(s *scriptedTurns)
		want    Update
		ok      bool
		err     error
	}{
		{name: "stopped request", current: func(s *scriptedTurns) { s.current = stopped }, ok: true,
			want: Update{Thread: th1, Turn: 1, Scope: scope, Status: thread.StatusStopped, StopReason: values.StopOffline,
				Cards: []values.Card{values.StoppedCard{Reason: values.StopOffline}}}},
		{name: "no current request", current: func(*scriptedTurns) {}},
		{name: "missing chat", current: func(s *scriptedTurns) { s.currentErr = shared.ErrNotFound }, err: shared.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns := &scriptedTurns{}
			tt.current(turns)
			d, sink := newDriver(t, newReads(), turns)

			got, ok, err := d.Current(t.Context(), th1)
			d.Wait()
			if ok != tt.ok || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Current = %v, %v; want %v, %v", ok, err, tt.ok, tt.err)
			}
			if got.Thread != tt.want.Thread || got.Turn != tt.want.Turn || got.Scope != tt.want.Scope ||
				got.Status != tt.want.Status || got.StopReason != tt.want.StopReason || got.Problem != ProblemNone ||
				got.Progress || len(got.Cards) != len(tt.want.Cards) {
				t.Fatalf("update = %+v, want %+v", got, tt.want)
			}
			for i := range got.Cards {
				if got.Cards[i] != tt.want.Cards[i] {
					t.Fatalf("card %d = %+v, want %+v", i, got.Cards[i], tt.want.Cards[i])
				}
			}
			rep := turns.report()
			if ups := sink.all(); len(ups) != 0 || len(rep.submitted) != 0 || len(rep.cancelled) != 0 || len(rep.stopped) != 0 {
				t.Fatalf("Current ran something: updates %+v, report %+v", ups, rep)
			}
			if len(turns.currents) != 1 || turns.currents[0] != th1 {
				t.Fatalf("asked %v", turns.currents)
			}
		})
	}
}
