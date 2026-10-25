package agentjournal

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Live (agentturn.Effects): a request whose entries are all FAILED or UNDONE can be repeated; any other entry of the
// request (an action that ran, an unknown outcome, a proposal) blocks the repeat. Entries of other requests or threads
// do not count.
func TestLive(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *Service)
		want  bool
	}{
		{"no entries", func(*testing.T, *Service) {}, false},
		{"done action", func(t *testing.T, s *Service) { done(t, s, begin(t, s, "c1").ID) }, true},
		{"running action", func(t *testing.T, s *Service) { begin(t, s, "c1") }, true},
		{"failed action", func(t *testing.T, s *Service) {
			if _, err := s.Finish(t.Context(), begin(t, s, "c1").ID, Outcome{Failed: true}); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"undone action", func(t *testing.T, s *Service) {
			e := done(t, s, begin(t, s, "c1").ID)
			if got, err := s.Undo(t.Context(), e.ID); err != nil || got.Status != action.StatusUndone {
				t.Fatalf("Undo = %+v, %v", got, err)
			}
		}, false},
		{"undo refused", func(t *testing.T, s *Service) {
			s.comp = &fakeComp{targets: at(2)}
			e := done(t, s, begin(t, s, "c1").ID)
			if got, err := s.Undo(t.Context(), e.ID); err != nil || got.Status != action.StatusUndoRefused {
				t.Fatalf("Undo = %+v, %v", got, err)
			}
		}, true},
		{"open proposal", func(t *testing.T, s *Service) {
			in := BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolProposeSend}
			if _, err := s.Propose(t.Context(), in, ref(t, values.KindProposal, "p1")); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"one undone, one done", func(t *testing.T, s *Service) {
			e := done(t, s, begin(t, s, "c1").ID)
			if _, err := s.Undo(t.Context(), e.ID); err != nil {
				t.Fatal(err)
			}
			done(t, s, begin(t, s, "c2").ID)
		}, true},
		{"effects of another request or thread", func(t *testing.T, s *Service) {
			done(t, s, beginIn(t, s, BeginInput{Thread: "th1", Turn: 2, CallID: "c1", Tool: values.ToolCreateSubject}).ID)
			done(t, s, beginIn(t, s, BeginInput{Thread: "th2", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject}).ID)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newService(t, &fakeComp{targets: at(1)})
			tt.setup(t, s)
			got, err := s.Live(t.Context(), "th1", 1)
			if err != nil || got != tt.want {
				t.Fatalf("Live = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

type failingList struct{ *memActions }

func (failingList) ListByThread(context.Context, shared.ThreadID, int) ([]*action.Action, error) {
	return nil, errors.New("disk")
}

func TestLiveReadFails(t *testing.T) {
	s, repo := newService(t, nil)
	s.actions = failingList{repo}
	if live, err := s.Live(t.Context(), "th1", 1); err == nil || live {
		t.Fatalf("Live with an unreadable journal = %v, %v", live, err)
	}
}
