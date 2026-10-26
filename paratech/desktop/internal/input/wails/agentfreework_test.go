package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeFreeWork is input/agent.FreeWorks: it records the answers and returns what the test set.
type fakeFreeWork struct {
	cards   []values.FreeWorkCard
	answers []string
	out     values.FreeWorkOutcome
	err     error
}

func (f *fakeFreeWork) FreeWorkCard(_ context.Context, c values.FreeWorkCard) (values.FreeWorkOutcome, error) {
	f.cards = append(f.cards, c)
	return f.out, f.err
}

func (f *fakeFreeWork) FreeWork(_ context.Context, c values.FreeWorkCard, accept, dontShowAgain bool) (values.FreeWorkOutcome, error) {
	f.cards = append(f.cards, c)
	f.answers = append(f.answers, fmt.Sprintf("%v/%v", accept, dontShowAgain))
	return f.out, f.err
}

func TestAgentFreeWork(t *testing.T) {
	s, _, _, _ := newAgent(t, readyLink())
	ref := AgentFreeWorkRef{ThreadID: "th1", Turn: 2, CallID: "c1"}
	if _, err := s.FreeWork(context.Background(), ref, true, false); !errors.Is(err, ErrFreeWorkUnavailable) {
		t.Fatalf("unwired = %v", err)
	}

	f := &fakeFreeWork{out: values.FreeWorkOutcome{State: values.FreeWorkDone, Work: "w1"}}
	s.freeWork = f
	got, err := s.FreeWork(context.Background(), ref, true, true)
	if err != nil || got != (AgentFreeWorkState{State: "done", WorkID: "w1"}) {
		t.Fatalf("FreeWork = %+v, %v", got, err)
	}
	if len(f.cards) != 1 || f.cards[0] != (values.FreeWorkCard{Thread: "th1", Turn: 2, CallID: "c1"}) || f.answers[0] != "true/true" {
		t.Fatalf("calls = %+v, %v", f.cards, f.answers)
	}
	if _, err := s.FreeWork(context.Background(), ref, false, false); err != nil || f.answers[1] != "false/false" {
		t.Fatalf("«Не надо» = %v, %v", err, f.answers)
	}
	f.out = values.FreeWorkOutcome{State: values.FreeWorkWaiting}
	if got, err := s.FreeWorkCard(context.Background(), ref); err != nil || got != (AgentFreeWorkState{State: "waiting"}) {
		t.Fatalf("FreeWorkCard = %+v, %v", got, err)
	}

	raw, err := json.Marshal(AgentFreeWorkState{State: "done", WorkID: "w1"})
	if err != nil || string(raw) != `{"state":"done","workId":"w1"}` {
		t.Fatalf("JSON = %s, %v (frontend/src/bindings/agent.ts reads these names)", raw, err)
	}
}

// The page shows the error texts as they are and calls the methods by name (frontend/src/bindings/agent.ts).
func TestFrontendKnowsFreeWork(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/agent.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{ErrFreeWorkNotFound, ErrFreeWorkUnavailable, ErrFreeWorkFailed} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("agent.ts does not list the error %q", e)
		}
	}
	typ := reflect.TypeFor[*AgentService]().Elem()
	if !strings.Contains(string(src), `"`+typ.PkgPath()+"."+typ.Name()+`"`) {
		t.Fatalf("agent.ts does not name %s", typ.Name())
	}
	for _, m := range []string{"FreeWork", "FreeWorkCard"} {
		if _, ok := reflect.TypeFor[*AgentService]().MethodByName(m); !ok || !strings.Contains(string(src), "${service}."+m+"`") {
			t.Errorf("agent.ts does not call AgentService.%s", m)
		}
	}
}

// Errors are fixed Russian texts; the cause stays in the log.
func TestAgentFreeWorkErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("agent: free work: %w: c9", shared.ErrNotFound), ErrFreeWorkNotFound},
		{fmt.Errorf("agent: %w", shared.ErrServiceNotConfigured), ErrFreeWorkUnavailable},
		{errors.New(`open C:\Users\student\db: locked`), ErrFreeWorkFailed},
	}
	for _, tt := range tests {
		s, _, _, logs := newAgent(t, readyLink())
		s.freeWork = &fakeFreeWork{err: tt.err}
		for _, call := range []func() error{
			func() error { _, err := s.FreeWork(context.Background(), AgentFreeWorkRef{}, true, false); return err },
			func() error { _, err := s.FreeWorkCard(context.Background(), AgentFreeWorkRef{}); return err },
		} {
			if err := call(); !errors.Is(err, tt.want) || strings.Contains(err.Error(), "student") {
				t.Fatalf("%v: err = %v, want %v", tt.err, err, tt.want)
			}
		}
		if tt.want == ErrFreeWorkFailed && !strings.Contains(logs.String(), "locked") {
			t.Fatalf("the cause was not logged: %s", logs)
		}
	}
}
