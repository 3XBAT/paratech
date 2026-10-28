package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/shared"
)

// TASK-315: one outcome per id in the order given, with the use case's code; only the changed ones in Prior.
func TestMarkDoneCodesPerID(t *testing.T) {
	e := newHWEnv()
	e.proposals.doneCodes = map[shared.AssignmentID]homeworkflow.DoneCode{
		"a2": homeworkflow.DoneUnchanged, "a3": homeworkflow.DoneNotFound, "a4": homeworkflow.DoneDeleted,
		"a5": homeworkflow.DoneConflict, "a6": homeworkflow.DoneFailed,
	}
	got := e.svc.MarkDone(context.Background(), []string{"a1", "a2", "a3", "a4", "a5", "a6"}, true)
	want := DoneResult{
		Outcomes: []DoneOutcomeItem{
			{ID: "a1", Code: "changed", Done: true, Version: 8},
			{ID: "a2", Code: "unchanged", Done: true, Version: 3},
			{ID: "a3", Code: "not_found"},
			{ID: "a4", Code: "deleted"},
			{ID: "a5", Code: "conflict"},
			{ID: "a6", Code: "failed"},
		},
		Prior:     []DonePriorItem{{ID: "a1", Version: 8}},
		PriorDone: false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkDone = %+v\nwant %+v", got, want)
	}
	if !slices.Equal(e.proposals.markedDone, []bool{true}) || len(e.proposals.marked) != 1 || len(e.proposals.marked[0]) != 6 {
		t.Fatalf("use case got %v, %v", e.proposals.marked, e.proposals.markedDone)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "locked") || !strings.Contains(logs, "conflict=1") {
		t.Fatalf("the failed cause and the counts are not logged: %q", logs)
	}

	// «Вернуть в работу»: the prior state to put back is done.
	e = newHWEnv()
	got = e.svc.MarkDone(context.Background(), []string{"a1"}, false)
	if got.Problem != "" || !got.PriorDone || got.Outcomes[0].Done || !slices.Equal(e.proposals.markedDone, []bool{false}) {
		t.Fatalf("MarkDone(not done) = %+v, use case got %v", got, e.proposals.markedDone)
	}
}

// A malformed id is not_found without a call, a repeated id is reported once, and the order is kept.
func TestMarkDoneMalformedAndRepeatedIDs(t *testing.T) {
	e := newHWEnv()
	got := e.svc.MarkDone(context.Background(), []string{"a1", "a b", "a1", "a2", "a b"}, true)
	codes := []string{}
	for _, o := range got.Outcomes {
		codes = append(codes, o.ID+":"+o.Code)
	}
	if !slices.Equal(codes, []string{"a1:changed", "a b:not_found", "a2:changed"}) {
		t.Fatalf("outcomes = %v", codes)
	}
	if !reflect.DeepEqual(e.proposals.marked, [][]shared.AssignmentID{{"a1", "a2"}}) {
		t.Fatalf("use case got %v", e.proposals.marked)
	}
	if len(got.Prior) != 2 {
		t.Fatalf("prior = %+v", got.Prior)
	}

	e = newHWEnv()
	got = e.svc.MarkDone(context.Background(), []string{"", "x\n"}, true)
	if got.Problem != "" || len(got.Outcomes) != 2 || got.Outcomes[1].Code != "not_found" || len(got.Prior) != 0 ||
		len(e.calls.list()) != 0 {
		t.Fatalf("only malformed ids = %+v, calls %v", got, e.calls.list())
	}
}

func TestMarkDoneBounds(t *testing.T) {
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("a%d", i)
		}
		return out
	}
	for _, n := range []int{0, homeworkflow.MaxMarkDone + 1} {
		e := newHWEnv()
		got := e.svc.MarkDone(context.Background(), ids(n), true)
		if got.Problem != doneInvalid || got.Outcomes == nil || len(got.Outcomes) != 0 || len(e.calls.list()) != 0 {
			t.Errorf("MarkDone(%d ids) = %+v, calls %v", n, got, e.calls.list())
		}
	}
	e := newHWEnv()
	if got := e.svc.MarkDone(context.Background(), nil, true); got.Problem != doneInvalid || e.called("MarkDone") {
		t.Errorf("MarkDone(nil) = %+v", got)
	}
	if got := e.svc.MarkDone(context.Background(), ids(homeworkflow.MaxMarkDone), true); got.Problem != "" ||
		len(got.Outcomes) != homeworkflow.MaxMarkDone {
		t.Errorf("MarkDone(%d ids) = %s, %d outcomes", homeworkflow.MaxMarkDone, got.Problem, len(got.Outcomes))
	}
}

func TestMarkDoneUseCaseErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"refused request", fmt.Errorf("mark: %w: 0 assignments", homeworkflow.ErrInvalidRequest), doneInvalid},
		{"storage", errors.New(`open C:\Users\student\db: locked`), doneUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, call := range []func(*HomeworkService) DoneResult{
				func(s *HomeworkService) DoneResult { return s.MarkDone(context.Background(), []string{"a1"}, true) },
				func(s *HomeworkService) DoneResult {
					return s.RestoreDone(context.Background(), false, []DonePriorItem{{ID: "a1", Version: 8}})
				},
			} {
				e := newHWEnv()
				e.proposals.doneErr = tt.err
				got := call(e.svc)
				if got.Problem != tt.want || len(got.Outcomes) != 0 || len(got.Prior) != 0 {
					t.Fatalf("result = %+v, want problem %s", got, tt.want)
				}
				if !strings.Contains(e.logs.String(), "err=") {
					t.Fatalf("the cause is not logged: %q", e.logs.String())
				}
			}
		})
	}
}

// «Отменить» sends back exactly the returned prior; its own prior undoes the undo.
func TestRestoreDoneUndoesTheMark(t *testing.T) {
	e := newHWEnv()
	e.proposals.doneCodes = map[shared.AssignmentID]homeworkflow.DoneCode{"a2": homeworkflow.DoneUnchanged}
	mark := e.svc.MarkDone(context.Background(), []string{"a1", "a2"}, true)

	undo := e.svc.RestoreDone(context.Background(), mark.PriorDone, mark.Prior)
	if want := [][]homeworkflow.DonePrior{{{ID: "a1", Done: false, Version: 8}}}; !reflect.DeepEqual(e.proposals.restored, want) {
		t.Fatalf("RestoreDone got %+v, want %+v", e.proposals.restored, want)
	}
	want := DoneResult{
		Outcomes:  []DoneOutcomeItem{{ID: "a1", Code: "changed", Done: false, Version: 9}},
		Prior:     []DonePriorItem{{ID: "a1", Version: 9}},
		PriorDone: true,
	}
	if !reflect.DeepEqual(undo, want) {
		t.Fatalf("RestoreDone = %+v\nwant %+v", undo, want)
	}

	e.svc.RestoreDone(context.Background(), undo.PriorDone, undo.Prior)
	if got := e.proposals.restored[1]; !reflect.DeepEqual(got, []homeworkflow.DonePrior{{ID: "a1", Done: true, Version: 9}}) {
		t.Fatalf("undo of the undo got %+v", got)
	}
}

// Any version bump meanwhile (also a background reprocess) is conflict: nothing to undo again.
func TestRestoreDoneConflict(t *testing.T) {
	e := newHWEnv()
	e.proposals.doneCodes = map[shared.AssignmentID]homeworkflow.DoneCode{"a1": homeworkflow.DoneConflict}
	got := e.svc.RestoreDone(context.Background(), false, []DonePriorItem{{ID: "a1", Version: 8}, {ID: "a2", Version: 4}})
	if got.Problem != "" || len(got.Outcomes) != 2 || got.Outcomes[0].Code != "conflict" || got.Outcomes[1].Code != "changed" ||
		!reflect.DeepEqual(got.Prior, []DonePriorItem{{ID: "a2", Version: 5}}) {
		t.Fatalf("RestoreDone = %+v", got)
	}
}

func TestRestoreDoneValidates(t *testing.T) {
	e := newHWEnv()
	got := e.svc.RestoreDone(context.Background(), false, []DonePriorItem{
		{ID: "a1", Version: -1}, {ID: "a b", Version: 2}, {ID: "a2", Version: 0}, {ID: "a2", Version: 7},
	})
	codes := []string{}
	for _, o := range got.Outcomes {
		codes = append(codes, o.ID+":"+o.Code)
	}
	if !slices.Equal(codes, []string{"a1:not_found", "a b:not_found", "a2:changed"}) {
		t.Fatalf("outcomes = %v", codes)
	}
	// A negative version would check none: it never reaches the core. The first entry of an id wins.
	if want := [][]homeworkflow.DonePrior{{{ID: "a2", Version: 0}}}; !reflect.DeepEqual(e.proposals.restored, want) {
		t.Fatalf("use case got %+v", e.proposals.restored)
	}

	many := make([]DonePriorItem, homeworkflow.MaxMarkDone+1)
	for i := range many {
		many[i] = DonePriorItem{ID: fmt.Sprintf("a%d", i), Version: 1}
	}
	for _, prior := range [][]DonePriorItem{nil, {}, many} {
		e := newHWEnv()
		if got := e.svc.RestoreDone(context.Background(), true, prior); got.Problem != doneInvalid || len(e.calls.list()) != 0 {
			t.Errorf("RestoreDone(%d entries) = %+v, calls %v", len(prior), got, e.calls.list())
		}
	}
}

// An id the use case left out or a code the page does not know is failed; Prior follows the reported outcomes only.
func TestMarkDoneDistrustsUnexpectedOutcomes(t *testing.T) {
	e := newHWEnv()
	e.proposals.doneOutcomes = func(m homeworkflow.DoneMarks) homeworkflow.DoneMarks {
		m.Outcomes = []homeworkflow.DoneOutcome{{ID: "a1", Code: "archived"}, {ID: "zz", Code: homeworkflow.DoneChanged}}
		m.Prior = append(m.Prior, homeworkflow.DonePrior{ID: "zz", Version: 1})
		return m
	}
	got := e.svc.MarkDone(context.Background(), []string{"a1", "a2"}, true)
	if len(got.Outcomes) != 2 || got.Outcomes[0].Code != "failed" || got.Outcomes[1].Code != "failed" || len(got.Prior) != 0 {
		t.Fatalf("MarkDone = %+v", got)
	}
}

// The page reads lists, never null.
func TestDoneResultJSON(t *testing.T) {
	e := newHWEnv()
	for _, res := range []DoneResult{
		e.svc.MarkDone(context.Background(), nil, true),
		e.svc.MarkDone(context.Background(), []string{"a b"}, true),
	} {
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "null") {
			t.Fatalf("JSON has null: %s", raw)
		}
	}
}

func TestListAssignmentsFilter(t *testing.T) {
	tests := []struct {
		state string
		want  homeworkflow.ListState
	}{
		{"", homeworkflow.ListOpen},
		{"open", homeworkflow.ListOpen},
		{"done", homeworkflow.ListDone},
		{"all", homeworkflow.ListAll},
	}
	for _, tt := range tests {
		e := newHWEnv()
		if _, err := e.svc.ListAssignments(context.Background(), tt.state); err != nil {
			t.Fatalf("ListAssignments(%q) = %v", tt.state, err)
		}
		if !slices.Equal(e.proposals.states, []homeworkflow.ListState{tt.want}) {
			t.Errorf("ListAssignments(%q) asked %v, want %s", tt.state, e.proposals.states, tt.want)
		}
	}

	for _, state := range []string{"Done", "deleted", " open"} {
		e := newHWEnv()
		if _, err := e.svc.ListAssignments(context.Background(), state); !errors.Is(err, ErrHomeworkListState) || len(e.calls.list()) != 0 {
			t.Errorf("ListAssignments(%q) = %v, calls %v", state, err, e.calls.list())
		}
	}
	e := newHWEnv()
	e.proposals.err = fmt.Errorf("list: %w: state", homeworkflow.ErrInvalidRequest)
	if _, err := e.svc.ListAssignments(context.Background(), "all"); !errors.Is(err, ErrHomeworkListState) {
		t.Errorf("refused filter = %v", err)
	}
}

func TestAssignmentItemCarriesTheDoneMark(t *testing.T) {
	done := homeworkflow.AssignmentView{ID: "a1", Title: "Rivers", UpdatedAt: hwT0, Done: true, DoneAt: hwT0}
	e := newHWEnv()
	e.proposals.list = []homeworkflow.AssignmentView{done, {ID: "a2", Title: "Lakes", UpdatedAt: hwT0}}
	e.proposals.one = &done

	list, err := e.svc.ListAssignments(context.Background(), "all")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListAssignments = %+v, %v", list, err)
	}
	if !list[0].Done || list[0].DoneAt != "2026-10-16T09:30:00Z" || list[1].Done || list[1].DoneAt != "" {
		t.Fatalf("list = %+v", list)
	}
	opened, err := e.svc.OpenAssignment(context.Background(), "a1")
	if err != nil || !opened.Assignment.Done || opened.Assignment.DoneAt != "2026-10-16T09:30:00Z" {
		t.Fatalf("OpenAssignment = %+v, %v", opened.Assignment, err)
	}
}
