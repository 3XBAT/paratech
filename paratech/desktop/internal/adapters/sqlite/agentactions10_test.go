package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// The compensations of the wave-10 assignment tools round-trip, each pinned prior with its intent (TASK-320).
func TestAgentActionsWave10AssignmentKinds(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	a1, a2 := agentRef(t, values.KindAssignment, "a1"), agentRef(t, values.KindAssignment, "a2")
	moscow := time.FixedZone("MSK", 3*60*60)

	marks := make([]action.DoneMark, values.MaxMarkAssignments)
	for i := range marks {
		marks[i] = action.DoneMark{Assignment: agentRef(t, values.KindAssignment, fmt.Sprintf("a%d", i)), WasDone: i%2 == 0, Version: int64(i + 2)}
	}
	fifty, err := action.NewRestoreDone(marks)
	if err != nil {
		t.Fatal(err)
	}
	one, err := action.NewRestoreDone([]action.DoneMark{{Assignment: a2, Version: 7}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		tool    values.Tool
		comp    action.Compensation
		pinned  bool // the prior is the intent's
		version int64
	}{
		{"revert, every field", values.ToolChangeAssignment, action.RevertAssignment{Assignment: a1, Prior: action.AssignmentFields{
			Title: "Реферат «Кант»", Note: "Объём 10 стр.\nШрифт 14", Due: time.Date(2026, 11, 10, 20, 0, 0, 0, moscow),
			SubjectName: "Философия",
		}}, true, 3},
		{"revert, zero due and empty note", values.ToolChangeAssignment, action.RevertAssignment{Assignment: a2, Prior: action.AssignmentFields{
			Title: "Эссе",
		}}, true, 4},
		{"restore", values.ToolChangeAssignment, action.RestoreAssignment{Assignment: a1}, false, 5},
		{"delete again", values.ToolChangeAssignment, action.DeleteAssignment{Assignment: a1}, false, 6},
		{"fifty marks", values.ToolMarkAssignmentsDone, fifty, false, fifty.ExpectedVersion()},
		{"one mark", values.ToolMarkAssignmentsDone, one, false, 7},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := shared.ActionID(fmt.Sprintf("w%d", i))
			spec := action.Spec{ID: id, Thread: "th1", Turn: 1, CallID: fmt.Sprintf("c%d", i), Tool: tc.tool}
			if tc.pinned {
				spec.Intent = action.Intent{Target: a1, Before: 2, Prior: tc.comp}
			}
			a, err := action.New(spec, agentAt(i))
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, a); err != nil {
				t.Fatalf("insert: %v", err)
			}
			// The journal completes the entry it reads back, so the pinned prior must survive the codec.
			stored, err := repo.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			comp := tc.comp
			if tc.pinned {
				comp = stored.Intent().Prior
			}
			if err := stored.Complete(comp, tc.version, agentAt(i).Add(time.Second)); err != nil {
				t.Fatalf("Complete after a read: %v", err)
			}
			if err := repo.Save(ctx, stored); err != nil {
				t.Fatalf("update: %v", err)
			}
			got, err := repo.Get(ctx, id)
			if err != nil || !reflect.DeepEqual(got.State(), stored.State()) {
				t.Fatalf("got %+v, %v\nwant %+v", got.State(), err, stored.State())
			}
			if r, ok := tc.comp.(action.RevertAssignment); ok {
				back := got.Compensation().(action.RevertAssignment)
				if !back.Prior.Due.Equal(r.Prior.Due) || back.Prior.Due.Location() != time.UTC && !back.Prior.Due.IsZero() {
					t.Fatalf("due = %v, want %v in UTC", back.Prior.Due, r.Prior.Due)
				}
				if back.Prior.Title != r.Prior.Title || back.Prior.Note != r.Prior.Note || back.Prior.SubjectName != r.Prior.SubjectName {
					t.Fatalf("fields = %+v, want %+v", back.Prior, r.Prior)
				}
			}
			if d, ok := tc.comp.(action.RestoreDone); ok && !reflect.DeepEqual(got.Compensation().(action.RestoreDone).Marks(), d.Marks()) {
				t.Fatalf("marks = %+v", got.Compensation())
			}
		})
	}
}

// The five compensations of note round-trip on real SQLite; the pinned priors (anchor, title) survive the codec so the
// journal completes the entry it reads back (TASK-321).
func TestAgentActionsWave10NoteKinds(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	n1, m1 := agentRef(t, values.KindNote, "n1"), agentRef(t, values.KindMaterial, "m1")
	date := func(s string) values.Date {
		d, err := values.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	cases := []struct {
		name   string
		comp   action.Compensation
		pinned bool
	}{
		{"part of a note that existed", action.RemoveNotePart{Note: n1, Part: m1}, false},
		{"a note the add made", action.RemoveNote{Note: n1}, false},
		{"back to a recording", action.ReanchorNote{Note: n1, Prior: action.NoteAnchor{
			Kind: action.AnchorRecording, Target: agentRef(t, values.KindRecording, "r1"), Date: date("2026-10-12"),
		}}, true},
		{"back to a lesson", action.ReanchorNote{Note: n1, Prior: action.NoteAnchor{
			Kind: action.AnchorLesson, Target: agentRef(t, values.KindLesson, "l1"), Date: date("2026-10-13"),
		}}, true},
		{"back to a day", action.ReanchorNote{Note: n1, Prior: action.NoteAnchor{Kind: action.AnchorDay, Date: date("2026-10-14")}}, true},
		{"back to no date", action.ReanchorNote{Note: n1, Prior: action.NoteAnchor{Kind: action.AnchorUndated}}, true},
		{"old title", action.RetitleNote{Note: n1, OldTitle: "Лекция 3 «Механика»"}, true},
		{"restore", action.RestoreNote{Note: n1}, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := shared.ActionID(fmt.Sprintf("n%d", i))
			spec := action.Spec{ID: id, Thread: "th1", Turn: 1, CallID: fmt.Sprintf("c%d", i), Tool: values.ToolNote}
			if tc.pinned {
				spec.Intent = action.Intent{Target: n1, Before: 2, Prior: tc.comp, Next: tc.comp}
			}
			a, err := action.New(spec, agentAt(i))
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, a); err != nil {
				t.Fatalf("insert: %v", err)
			}
			stored, err := repo.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			comp := tc.comp
			if tc.pinned {
				if stored.Intent().Prior != tc.comp || stored.Intent().Next != tc.comp {
					t.Fatalf("intent = %+v, want prior and next %+v", stored.Intent(), tc.comp)
				}
				comp = stored.Intent().Prior
			}
			if err := stored.Complete(comp, 3, agentAt(i).Add(time.Second)); err != nil {
				t.Fatalf("Complete after a read: %v", err)
			}
			if err := repo.Save(ctx, stored); err != nil {
				t.Fatalf("update: %v", err)
			}
			got, err := repo.Get(ctx, id)
			if err != nil || !reflect.DeepEqual(got.State(), stored.State()) || got.Compensation() != tc.comp {
				t.Fatalf("got %+v, %v\nwant %+v", got.State(), err, stored.State())
			}
		})
	}
}

func TestAgentActionsWave10RefusesBadRows(t *testing.T) {
	bad := []string{
		`{"kind":"reanchor_note","note":"n1"}`,
		`{"kind":"reanchor_note","note":"n1","anchor":{"kind":"day","date":"14.10.2026"}}`,
		`{"kind":"revert_assignment","assignment":"a1"}`,
		`{"kind":"revert_assignment","assignment":"a1","fields":{"title":"x","due":"not a time"}}`,
		`{"kind":"restore_done","marks":[]}`,
		`{"kind":"restore_done","marks":[{"assignment":"a1","was_done":true,"version":0}]}`,
		`{"kind":"no_such_kind"}`,
	}
	for _, s := range bad {
		var w compensationWire
		if err := strictJSON(s, &w); err != nil {
			t.Fatal(err)
		}
		if _, err := compensationOfWire(w); err == nil {
			t.Errorf("%s: decoded", s)
		}
	}
}
