package agentturn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
)

// recDrafts records the reply drafts of the steps; cut ends the ctx of the Hub call, as a stored «Стоп» does.
type recDrafts struct {
	mu    sync.Mutex
	steps []Step
	texts [][]string
	ends  []bool
	cuts  []context.CancelFunc
}

func (r *recDrafts) Reply(ctx context.Context, st Step) (context.Context, Draft) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	r.steps = append(r.steps, st)
	r.texts = append(r.texts, nil)
	r.cuts = append(r.cuts, cancel)
	return ctx, &recDraft{r: r, i: len(r.steps) - 1}
}

func (r *recDrafts) cut(i int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cuts[i]()
}

type recDraft struct {
	r *recDrafts
	i int
}

func (d *recDraft) Text(s string) {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	d.r.texts[d.i] = append(d.r.texts[d.i], s)
}

func (d *recDraft) End(answered bool) {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	d.r.ends = append(d.r.ends, answered)
}

func (r *recDrafts) got() ([]Step, [][]string, []bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Step(nil), r.steps...), append([][]string(nil), r.texts...), append([]bool(nil), r.ends...)
}

// historyHas reports whether any stored entry of th1 holds s.
func historyHas(e *env, s string) bool {
	for _, h := range e.history.all("th1") {
		if strings.Contains(fmt.Sprintf("%+v", h.Entry), s) {
			return true
		}
	}
	return false
}

// The streamed text of a step is only handed to the draft: the reply card is the validated reply call, the draft text
// is stored nowhere and never sent to the planner (hard rule 3), and the draft ends answered before the step returns.
func TestReplyDraftIsShownOnly(t *testing.T) {
	d := &recDrafts{}
	e := newEnvPorts(t, nil, d)
	e.model.stream(func(_ context.Context, _ values.StepRequest, draft func(string)) (values.StepReply, error) {
		if draft == nil {
			t.Error("the step got no draft")
			return values.StepReply{}, errors.New("no draft")
		}
		draft("черно")
		draft("черновик ответа")
		return reply(raw("r1", "reply", `{"text":"итоговый ответ"}`)), nil
	}).then(raw("r2", "reply", `{"text":"второй"}`))

	st := ask(t, e, "что такое горутина")
	if st.Status != thread.StatusDone || len(st.Cards) != 1 || st.Cards[0].(values.ReplyCard).Text != "итоговый ответ" {
		t.Fatalf("step = %+v", st)
	}
	steps, texts, ends := d.got()
	if len(steps) != 1 || steps[0].Thread != "th1" || steps[0].Turn != 1 || steps[0].Status != thread.StatusPlanning ||
		len(steps[0].Cards) != 0 || len(steps[0].Calls) != 0 {
		t.Fatalf("draft steps = %+v", steps)
	}
	if len(texts[0]) != 2 || texts[0][1] != "черновик ответа" || len(ends) != 1 || !ends[0] {
		t.Fatalf("texts %q, ends %v", texts, ends)
	}
	if historyHas(e, "черновик") {
		t.Fatal("the draft text reached the history")
	}

	ask(t, e, "ещё")
	for _, h := range e.model.last(t).History {
		if strings.Contains(h.AgentReply+h.StudentText, "черновик") {
			t.Fatalf("the draft text went to the planner: %+v", h)
		}
	}
}

// A failed step drops its draft (End false); the stop is the one of the error, as without drafts.
func TestReplyDraftDroppedOnFailure(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason values.StopReason
	}{
		{"lost", backoff.Transient(errors.New("stream reset")), values.StopOffline},
		{"refused", backoff.Permanent(errors.New("provider failed")), values.StopFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &recDrafts{}
			e := newEnvPorts(t, nil, d)
			e.model.stream(func(_ context.Context, _ values.StepRequest, draft func(string)) (values.StepReply, error) {
				draft("половина ответа")
				return values.StepReply{}, tt.err
			})
			st := ask(t, e, "что такое горутина")
			if st.Status != thread.StatusStopped || st.StopReason != tt.reason {
				t.Fatalf("step = %+v", st)
			}
			if _, _, ends := d.got(); len(ends) != 1 || ends[0] {
				t.Fatalf("ends = %v", ends)
			}
			if historyHas(e, "половина") {
				t.Fatal("the draft text reached the history")
			}
		})
	}
}

// An invalid reply (the Hub's answer breaks the reply rules) drops the draft too.
func TestReplyDraftDroppedOnInvalidReply(t *testing.T) {
	d := &recDrafts{}
	e := newEnvPorts(t, nil, d)
	e.model.stream(func(_ context.Context, _ values.StepRequest, draft func(string)) (values.StepReply, error) {
		draft("текст")
		return values.StepReply{}, nil // no calls: Validate fails
	})
	if st := ask(t, e, "вопрос"); st.Status != thread.StatusStopped {
		t.Fatalf("step = %+v", st)
	}
	if _, _, ends := d.got(); len(ends) != 1 || ends[0] {
		t.Fatalf("ends = %v", ends)
	}
}

// «Стоп» during a streamed step: once it is stored the draft's ctx ends the Hub call; the stored stop by the student
// wins without an error, nothing of the step is stored, and «Продолжить» sends the same key.
func TestReplyDraftCutByStop(t *testing.T) {
	d := &recDrafts{}
	e := newEnvPorts(t, nil, d)
	e.model.stream(func(ctx context.Context, req values.StepRequest, draft func(string)) (values.StepReply, error) {
		draft("начало ответа")
		if _, err := e.svc.Stop(context.Background(), TurnRef{Thread: req.Thread, Turn: 1}); err != nil {
			t.Errorf("Stop: %v", err)
		}
		d.cut(0)
		<-ctx.Done()
		return values.StepReply{}, fmt.Errorf("hub: %w", ctx.Err())
	}).then(raw("r1", "reply", `{"text":"ответ"}`))

	st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "что такое горутина"})
	if err != nil || st.Status != thread.StatusStopped || st.StopReason != values.StopUser {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	if _, _, ends := d.got(); len(ends) != 1 || ends[0] {
		t.Fatalf("ends = %v", ends)
	}
	if all := e.history.all("th1"); len(all) != 1 {
		t.Fatalf("history after the cut step = %+v, want the text only", all)
	}

	cont, err := e.svc.Continue(t.Context(), ref(st))
	if err != nil || cont.Status != thread.StatusDone {
		t.Fatalf("Continue = %+v, %v", cont, err)
	}
	reqs := e.model.requests()
	if len(reqs) != 2 || reqs[0].Key != reqs[1].Key {
		t.Fatalf("requests = %+v", reqs)
	}
}

// A step that panics still ends its draft (dropped), and the request stops as before.
func TestReplyDraftEndedOnPanic(t *testing.T) {
	d := &recDrafts{}
	e := newEnvPorts(t, nil, d)
	e.model.stream(func(context.Context, values.StepRequest, func(string)) (values.StepReply, error) {
		panic("broken adapter")
	})
	st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "вопрос"})
	if err == nil || st.Status != thread.StatusStopped {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	if _, _, ends := d.got(); len(ends) != 1 || ends[0] {
		t.Fatalf("ends = %v", ends)
	}
}

// Without Drafts the model gets no draft.
func TestNoDraftsNoCallback(t *testing.T) {
	e := newEnv(t)
	e.model.stream(func(_ context.Context, _ values.StepRequest, draft func(string)) (values.StepReply, error) {
		if draft != nil {
			t.Error("a draft without the Drafts port")
		}
		return reply(raw("r1", "reply", `{"text":"ответ"}`)), nil
	})
	if st := ask(t, e, "вопрос"); st.Status != thread.StatusDone {
		t.Fatalf("step = %+v", st)
	}
}
