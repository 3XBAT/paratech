package thread

import (
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

// maxOpenCalls bounds the calls of one step the thread waits for (the Hub returns at most MaxCallsPerStep).
const maxOpenCalls = values.MaxReplyCalls

// OpenCall is a call of the last step that waits for its result. The Hub accepts an echo of calls only when the
// results of exactly those calls follow it, so every call is answered (by the Driver, by the case for card tools and
// refusals, or as not done) before the next step.
type OpenCall struct {
	ID       string
	Tool     values.Tool // empty for a name outside the catalog
	PlanStep int         // the approved step it executes; -1 none
	Admitted bool        // Admit let it run
	Answered bool
	Result   values.ToolResult
}

// Created is an ID that a create_subject or create_folder call made while executing approved plan step Step. A
// deferred plan argument binds only to such an ID from an earlier step (ADR-0021, TASK-115 follow-up).
type Created struct {
	Ref  values.Ref
	Step int
}

// Expect records the calls of the step that just ended; their results are collected with Answer and taken with
// TakeResults. The previous set must have been taken. Call IDs must be well formed and distinct; a tool name outside
// the catalog is kept as unknown.
func (th *Thread) Expect(calls []values.RawCall, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.stepOpen || len(t.open) > 0 {
		return fmt.Errorf("%w: calls expected while a step is open or results are pending", ErrInvalidTransition)
	}
	if len(calls) == 0 || len(calls) > maxOpenCalls || now.IsZero() {
		return fmt.Errorf("%w: a step has 1..%d calls", ErrInvalidThread, maxOpenCalls)
	}
	open := make([]OpenCall, 0, len(calls))
	for _, c := range calls {
		id := c.CallID
		if values.CheckCallID(id) != nil || slices.ContainsFunc(open, func(o OpenCall) bool { return o.ID == id }) {
			return fmt.Errorf("%w: call ids must be distinct and well formed", ErrInvalidThread)
		}
		tool, _ := values.ParseTool(c.Tool)
		open = append(open, OpenCall{ID: id, Tool: tool, PlanStep: -1})
	}
	t.open = open
	t.updatedAt, th.updatedAt = now, now
	return nil
}

func (t *Turn) openCall(id string) *OpenCall {
	for i := range t.open {
		if t.open[i].ID == id {
			return &t.open[i]
		}
	}
	return nil
}

// Answer records the result of an open call. created is the ID a create_subject or create_folder call made (zero
// otherwise); it becomes known to the provenance check and, for an approved plan step, bindable to deferred
// arguments of later steps.
func (th *Thread) Answer(r values.ToolResult, created values.Ref, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	oc := t.openCall(r.CallID)
	if oc == nil || oc.Answered {
		return fmt.Errorf("%w: no open call %q", ErrUnknownCall, r.CallID)
	}
	if !created.IsZero() {
		kind, ok := values.CreatedKind(oc.Tool)
		if !ok || !oc.Admitted || r.IsError || created.Kind() != kind {
			return fmt.Errorf("%w: only a successful create call creates an id of its kind", ErrInvalidThread)
		}
		if err := th.Observe(created); err != nil {
			return err
		}
		if oc.PlanStep >= 0 {
			t.created = append(t.created, Created{Ref: created, Step: oc.PlanStep})
		}
	}
	oc.Answered, oc.Result = true, r
	t.updatedAt, th.updatedAt = now, now
	return nil
}

// Abandon answers every open call that has no result yet with a refusal (cancelled, interrupted).
func (th *Thread) Abandon(why values.Refusal, now time.Time) {
	t := th.current
	if t == nil {
		return
	}
	for i := range t.open {
		if !t.open[i].Answered {
			t.open[i].Answered, t.open[i].Result = true, values.RefusalResult(t.open[i].ID, why)
			t.updatedAt, th.updatedAt = now, now
		}
	}
}

// TakeResults returns the results of the open calls in call order once every call is answered, and clears the set.
// It returns false while a call waits or when there is no set.
func (th *Thread) TakeResults(now time.Time) ([]values.ToolResult, bool) {
	t := th.current
	if t == nil || len(t.open) == 0 {
		return nil, false
	}
	out := make([]values.ToolResult, 0, len(t.open))
	for _, c := range t.open {
		if !c.Answered {
			return nil, false
		}
		out = append(out, c.Result)
	}
	t.open = nil
	t.updatedAt, th.updatedAt = now, now
	return out, true
}

// deferredBound reports whether the call fills in every deferred argument with an ID created in this request by an
// approved step that comes before step (so the create ran first). A missing deferred argument is not bound.
func (t *Turn) deferredBound(s values.PlanStep, step int, call values.Call) bool {
	for _, name := range s.Deferred() {
		ref, ok := call.Args().ID(name)
		if !ok {
			return false
		}
		bound := slices.ContainsFunc(t.created, func(c Created) bool { return c.Ref == ref && c.Step < step })
		if !bound {
			return false
		}
	}
	return true
}

func validOpen(open []OpenCall, approved int) bool {
	if len(open) > maxOpenCalls {
		return false
	}
	ids := map[string]bool{}
	for _, c := range open {
		if c.ID == "" || ids[c.ID] || c.PlanStep < -1 || c.PlanStep >= approved || (c.Tool != "" && !c.Tool.Valid()) {
			return false
		}
		ids[c.ID] = true
		if c.Answered != (c.Result.CallID != "") || (c.Answered && (c.Result.CallID != c.ID || c.Result.Validate() != nil)) {
			return false
		}
	}
	return true
}

func validCreated(created []Created, approved int) bool {
	for _, c := range created {
		kind := c.Ref.Kind()
		if (kind != values.KindSubject && kind != values.KindFolder) || c.Step < 0 || c.Step >= approved {
			return false
		}
	}
	return true
}
