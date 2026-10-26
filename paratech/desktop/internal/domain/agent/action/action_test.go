package action

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

var t0 = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func ref(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func spec(tool values.Tool) Spec {
	return Spec{ID: "act1", Thread: "th1", Turn: 1, CallID: "toolu_1", Tool: tool}
}

func subjectComp(t *testing.T) Compensation {
	return RemoveSubject{Subject: ref(t, values.KindSubject, "s1")}
}

// reach brings a create_subject action to the given status through its methods.
func reach(t *testing.T, s Status) *Action {
	t.Helper()
	if s == StatusProposed {
		a, err := NewProposed(spec(values.ToolProposeSend), ref(t, values.KindProposal, "p1"), t0)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a, err := New(spec(values.ToolCreateSubject), t0)
	if err != nil {
		t.Fatal(err)
	}
	steps := map[Status][]func() error{
		StatusPending:     nil,
		StatusDone:        {func() error { return a.Complete(subjectComp(t), 1, at(1)) }},
		StatusFailed:      {func() error { return a.Fail(at(1)) }},
		StatusUnknown:     {func() error { _, err := a.Recover(at(1)); return err }},
		StatusUndoing:     {func() error { return a.Complete(subjectComp(t), 1, at(1)) }, func() error { return a.BeginUndo(at(2)) }},
		StatusUndone:      {func() error { return a.Complete(subjectComp(t), 1, at(1)) }, func() error { return a.BeginUndo(at(2)) }, func() error { return a.FinishUndo(at(3)) }},
		StatusUndoRefused: {func() error { return a.Complete(subjectComp(t), 1, at(1)) }, func() error { return a.BeginUndo(at(2)) }, func() error { return a.RefuseUndo(UndoChanged, at(3)) }},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			t.Fatalf("reach %s: %v", s, err)
		}
	}
	if a.Status() != s {
		t.Fatalf("reached %s, want %s", a.Status(), s)
	}
	return a
}

func TestTransitionTable(t *testing.T) {
	want := map[Status][]Status{
		StatusPending: {StatusDone, StatusFailed, StatusUnknown},
		StatusDone:    {StatusUndoing},
		StatusUndoing: {StatusUndone, StatusUndoRefused},
	}
	if len(transitions) != len(Statuses()) {
		t.Fatalf("table has %d statuses, Statuses() %d", len(transitions), len(Statuses()))
	}
	for _, from := range Statuses() {
		allowed := map[Status]bool{}
		for _, to := range want[from] {
			allowed[to] = true
		}
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != allowed[to] {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, allowed[to])
			}
		}
		if from.IsTerminal() != (len(want[from]) == 0) {
			t.Errorf("%s.IsTerminal() = %v", from, from.IsTerminal())
		}
	}
	if Status("CANCELLED").Valid() {
		t.Error("unknown status is valid")
	}
}

// TestOperationsFromEveryStatus runs every operation from every status (ARCH-62).
func TestOperationsFromEveryStatus(t *testing.T) {
	ops := map[string]func(t *testing.T, a *Action) error{
		"Complete":   func(t *testing.T, a *Action) error { return a.Complete(subjectComp(t), 1, at(9)) },
		"Fail":       func(_ *testing.T, a *Action) error { return a.Fail(at(9)) },
		"BeginUndo":  func(_ *testing.T, a *Action) error { return a.BeginUndo(at(9)) },
		"StartUndo":  func(_ *testing.T, a *Action) error { return a.StartUndo(at(9)) },
		"FinishUndo": func(_ *testing.T, a *Action) error { return a.FinishUndo(at(9)) },
		"RefuseUndo": func(_ *testing.T, a *Action) error { return a.RefuseUndo(UndoChanged, at(9)) },
	}
	want := map[string]map[Status]Status{
		"Complete":   {StatusPending: StatusDone},
		"Fail":       {StatusPending: StatusFailed},
		"BeginUndo":  {StatusDone: StatusUndoing},
		"StartUndo":  {StatusDone: StatusUndoing, StatusUndoing: StatusUndoing}, // UNDOING since at(2): stale at at(9)
		"FinishUndo": {StatusUndoing: StatusUndone},
		"RefuseUndo": {StatusUndoing: StatusUndoRefused},
	}
	for name, run := range ops {
		for _, from := range Statuses() {
			t.Run(name+"/"+string(from), func(t *testing.T) {
				a := reach(t, from)
				before := a.State()
				err := run(t, a)
				to, ok := want[name][from]
				if !ok {
					if !errors.Is(err, ErrInvalidTransition) && !errors.Is(err, ErrInvalidCompensation) {
						t.Fatalf("error = %v, want a refusal", err)
					}
					if a.State() != before {
						t.Fatal("refused op changed the action")
					}
					return
				}
				if err != nil || a.Status() != to {
					t.Fatalf("status = %s, err = %v; want %s", a.Status(), err, to)
				}
			})
		}
	}
}

func TestRecover(t *testing.T) {
	tests := []struct {
		from  Status
		to    Status
		retry bool
	}{
		{StatusPending, StatusUnknown, false},
		{StatusUndoing, StatusUndoing, true},
		{StatusDone, StatusDone, false},
		{StatusFailed, StatusFailed, false},
		{StatusUnknown, StatusUnknown, false},
		{StatusUndone, StatusUndone, false},
		{StatusUndoRefused, StatusUndoRefused, false},
		{StatusProposed, StatusProposed, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			a := reach(t, tt.from)
			retry, err := a.Recover(at(9))
			if err != nil || retry != tt.retry || a.Status() != tt.to {
				t.Fatalf("Recover = %v, %v; status %s; want %v %s", retry, err, a.Status(), tt.retry, tt.to)
			}
			if a.CanUndo(at(2)) != (tt.to == StatusDone) {
				t.Fatalf("CanUndo = %v in %s", a.CanUndo(at(2)), tt.to)
			}
		})
	}
}

func TestUndoChecksTarget(t *testing.T) {
	a := reach(t, StatusPending)
	if err := a.CheckTarget(1); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("CheckTarget before DONE = %v", err)
	}
	if err := a.Complete(subjectComp(t), 4, at(1)); err != nil {
		t.Fatal(err)
	}
	if err := a.BeginUndo(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckTarget(4); err != nil {
		t.Fatalf("unchanged target = %v", err)
	}
	if err := a.CheckTarget(5); !errors.Is(err, ErrTargetChanged) {
		t.Fatalf("changed target = %v", err)
	}
	if err := a.RefuseUndo(UndoChanged, at(3)); err != nil || a.Status() != StatusUndoRefused {
		t.Fatalf("RefuseUndo = %v", err)
	}
}

func TestCompensationMatchesTool(t *testing.T) {
	s1, f1 := ref(t, values.KindSubject, "s1"), ref(t, values.KindFolder, "f1")
	m1, l1 := ref(t, values.KindMaterial, "m1"), ref(t, values.KindLesson, "l1")
	r1, a1, j1 := ref(t, values.KindRecording, "r1"), ref(t, values.KindAssignment, "a1"), ref(t, values.KindJob, "j1")
	due := time.Date(2026, time.November, 3, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		tool values.Tool
		comp Compensation
		ok   bool
	}{
		{"remove subject", values.ToolCreateSubject, RemoveSubject{Subject: s1}, true},
		{"remove folder", values.ToolCreateFolder, RemoveFolder{Subject: s1, Folder: f1}, true},
		{"return to inbox", values.ToolPlaceUpload, ReturnToInbox{Material: m1}, true},
		{"return to inbox and unlink", values.ToolPlaceUpload, ReturnToInbox{Material: m1, Lesson: l1}, true},
		{"unlink", values.ToolLinkMaterial, UnlinkMaterial{Material: m1, Lesson: l1}, true},
		{"restore lesson info", values.ToolSetLessonInfo, RestoreLessonInfo{Recording: r1, OldSubject: "Физика (лекции)", OldKind: "seminar"}, true},
		{"restore empty lesson info", values.ToolSetLessonInfo, RestoreLessonInfo{Recording: r1}, true},
		{"restore due", values.ToolEditDue, RestoreDue{Assignment: a1, OldDue: due}, true},
		{"restore no due", values.ToolEditDue, RestoreDue{Assignment: a1}, true},
		{"cancel job", values.ToolMakeSummary, CancelJob{Job: j1}, true},
		{"stop turnkey writing", values.ToolWriteWork, StopWork{Work: ref(t, values.KindWork, "w1"), Job: j1}, true},
		{"turnkey writing without its job", values.ToolWriteWork, StopWork{Work: ref(t, values.KindWork, "w1")}, false},
		{"turnkey writing as a plain job", values.ToolWriteWork, CancelJob{Job: j1}, false},
		{"remove added assignment", values.ToolCreateAssignment, RemoveAssignment{Assignment: a1}, true},
		{"remove added assignment without id", values.ToolCreateAssignment, RemoveAssignment{}, false},
		{"remove added assignment of a work", values.ToolCreateAssignment, RemoveAssignment{Assignment: ref(t, values.KindWork, "w1")}, false},
		{"added assignment undone as a due", values.ToolCreateAssignment, RestoreDue{Assignment: a1}, false},

		{"nil", values.ToolCreateSubject, nil, false},
		{"other tool's compensation", values.ToolCreateSubject, CancelJob{Job: j1}, false},
		{"wrong id kind", values.ToolCreateSubject, RemoveSubject{Subject: f1}, false},
		{"zero id", values.ToolPlaceUpload, ReturnToInbox{}, false},
		{"folder without subject", values.ToolCreateFolder, RemoveFolder{Folder: f1}, false},
		{"lesson as material", values.ToolLinkMaterial, UnlinkMaterial{Material: l1, Lesson: l1}, false},
		{"old kind outside enum", values.ToolSetLessonInfo, RestoreLessonInfo{Recording: r1, OldKind: "exam"}, false},
		{"old subject on two lines", values.ToolSetLessonInfo, RestoreLessonInfo{Recording: r1, OldSubject: "a\nb"}, false},
		{"inbox with a recording as lesson", values.ToolPlaceUpload, ReturnToInbox{Material: m1, Lesson: r1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := New(spec(tt.tool), t0)
			if err != nil {
				t.Fatal(err)
			}
			err = a.Complete(tt.comp, 1, at(1))
			if tt.ok != (err == nil) {
				t.Fatalf("Complete error = %v, want ok=%v", err, tt.ok)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalidCompensation) || a.Status() != StatusPending {
					t.Fatalf("refused Complete: %v, status %s", err, a.Status())
				}
				return
			}
			if a.Compensation() != tt.comp {
				t.Fatal("compensation not kept")
			}
		})
	}
	a := reach(t, StatusPending)
	if err := a.Complete(subjectComp(t), 0, at(1)); !errors.Is(err, ErrInvalidCompensation) {
		t.Fatalf("no target version = %v", err)
	}
	for tool := range compensationOf {
		if k, ok := CompensationFor(tool); !ok || k == "" {
			t.Fatalf("CompensationFor(%s)", tool)
		}
	}
}

func TestEveryUToolHasACompensation(t *testing.T) {
	for _, tool := range values.Tools() {
		_, hasComp := CompensationFor(tool)
		_, err := New(spec(tool), t0)
		if hasComp != (err == nil) {
			t.Fatalf("%s: compensation %v, journaled as U: %v", tool, hasComp, err)
		}
	}
}

func TestConstructors(t *testing.T) {
	p1 := ref(t, values.KindProposal, "p1")
	tests := []struct {
		name string
		make func() error
	}{
		{"R tool", func() error { _, err := New(spec(values.ToolSearch), t0); return err }},
		{"C tool as PENDING", func() error { _, err := New(spec(values.ToolProposeSend), t0); return err }},
		{"UI tool", func() error { _, err := New(spec(values.ToolReply), t0); return err }},
		{"unknown tool", func() error { _, err := New(spec("send_email"), t0); return err }},
		{"bad id", func() error { s := spec(values.ToolCreateSubject); s.ID = "a/b"; _, err := New(s, t0); return err }},
		{"bad thread", func() error { s := spec(values.ToolCreateSubject); s.Thread = ""; _, err := New(s, t0); return err }},
		{"no turn", func() error { s := spec(values.ToolCreateSubject); s.Turn = 0; _, err := New(s, t0); return err }},
		{"no call id", func() error { s := spec(values.ToolCreateSubject); s.CallID = ""; _, err := New(s, t0); return err }},
		{"no time", func() error { _, err := New(spec(values.ToolCreateSubject), time.Time{}); return err }},
		{"proposed R tool", func() error { _, err := NewProposed(spec(values.ToolSearch), p1, t0); return err }},
		{"proposed without proposal", func() error { _, err := NewProposed(spec(values.ToolProposeSend), values.Ref{}, t0); return err }},
		{"proposed with a job", func() error {
			_, err := NewProposed(spec(values.ToolProposeSend), ref(t, values.KindJob, "j1"), t0)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.make(); !errors.Is(err, ErrInvalidAction) {
				t.Fatalf("error = %v, want ErrInvalidAction", err)
			}
		})
	}

	// A U call that would replace a ready result is proposed, not run.
	a, err := NewProposed(spec(values.ToolMakeSummary), ref(t, values.KindPreview, "v1"), t0)
	if err != nil || a.Status() != StatusProposed || a.Proposal().ID() != "v1" {
		t.Fatalf("NewProposed(U) = %v", err)
	}
	if got := reach(t, StatusPending).Key(); got != "ag:th1:act:act1" {
		t.Fatalf("Key = %s", got)
	}
}

func TestRestore(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			st := reach(t, s).State()
			st.Version = 2
			a, err := Restore(st)
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if a.State() != st {
				t.Fatalf("round trip: %+v != %+v", a.State(), st)
			}
		})
	}
	bad := []struct {
		name   string
		from   Status
		mutate func(s *State)
	}{
		{"version 0", StatusDone, func(s *State) { s.Version = 0 }},
		{"unknown status", StatusDone, func(s *State) { s.Status = "CANCELLED" }},
		{"done without compensation", StatusDone, func(s *State) { s.Compensation = nil }},
		{"done without target version", StatusDone, func(s *State) { s.TargetVersion = 0 }},
		{"pending with compensation", StatusDone, func(s *State) { s.Status = StatusPending }},
		{"compensation of another tool", StatusDone, func(s *State) { s.Tool = values.ToolEditDue }},
		{"proposal on a U entry", StatusPending, func(s *State) { s.Proposal = ref(t, values.KindProposal, "p1") }},
		{"proposed without proposal", StatusProposed, func(s *State) { s.Proposal = values.Ref{} }},
		{"updated before created", StatusDone, func(s *State) { s.UpdatedAt = t0.Add(-time.Minute) }},
		{"refused without a reason", StatusUndoRefused, func(s *State) { s.Refusal = "" }},
		{"unknown refusal", StatusUndoRefused, func(s *State) { s.Refusal = "tired" }},
		{"refusal on a done entry", StatusDone, func(s *State) { s.Refusal = UndoChanged }},
		{"intent with a target but no version", StatusPending, func(s *State) {
			s.Intent = Intent{Target: ref(t, values.KindSubject, "s1")}
		}},
		{"intent prior of another tool", StatusPending, func(s *State) {
			s.Intent = Intent{Prior: CancelJob{Job: ref(t, values.KindJob, "j1")}}
		}},
	}
	for _, b := range bad {
		t.Run("bad/"+b.name, func(t *testing.T) {
			st := reach(t, b.from).State()
			st.Version = 2
			b.mutate(&st)
			if _, err := Restore(st); err == nil {
				t.Fatal("Restore accepted a broken entry")
			} else if !errors.Is(err, ErrInvalidAction) && !errors.Is(err, ErrInvalidCompensation) {
				t.Fatalf("Restore error = %v", err)
			}
		})
	}
}
