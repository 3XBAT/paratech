package action

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

func date(t *testing.T, s string) values.Date {
	t.Helper()
	d, err := values.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func marks(t *testing.T, n int) []DoneMark {
	t.Helper()
	out := make([]DoneMark, n)
	for i := range out {
		out[i] = DoneMark{Assignment: ref(t, values.KindAssignment, fmt.Sprintf("a%d", i)), WasDone: i%2 == 0, Version: int64(i + 2)}
	}
	return out
}

func restoreDone(t *testing.T, m []DoneMark) RestoreDone {
	t.Helper()
	c, err := NewRestoreDone(m)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// wave10Samples is one valid compensation of every wave-10 kind, with the tool and the intent it completes under.
func wave10Samples(t *testing.T) []struct {
	tool values.Tool
	in   Intent
	comp Compensation
} {
	a1, n1 := ref(t, values.KindAssignment, "a1"), ref(t, values.KindNote, "n1")
	revert := RevertAssignment{Assignment: a1, Prior: AssignmentFields{Title: "Реферат", Note: "до пары\nстр. 5", SubjectName: "Физика"}}
	reanchor := ReanchorNote{Note: n1, Prior: NoteAnchor{Kind: AnchorLesson, Target: ref(t, values.KindLesson, "l1"), Date: date(t, "2026-11-03")}}
	retitle := RetitleNote{Note: n1, OldTitle: "Лекция 3"}
	setting := RestoreSetting{Setting: values.SettingRecognitionLoad, OldValue: "gentle"}
	return []struct {
		tool values.Tool
		in   Intent
		comp Compensation
	}{
		{values.ToolChangeAssignment, Intent{Target: a1, Before: 4, Prior: revert}, revert},
		{values.ToolChangeAssignment, Intent{Target: a1, Before: 4}, RestoreAssignment{Assignment: a1}},
		{values.ToolChangeAssignment, Intent{}, DeleteAssignment{Assignment: a1}},
		{values.ToolMarkAssignmentsDone, Intent{}, restoreDone(t, marks(t, 3))},
		{values.ToolNote, Intent{Target: n1, Before: 2}, RemoveNotePart{Note: n1, Part: ref(t, values.KindMaterial, "m1")}},
		{values.ToolNote, Intent{}, RemoveNote{Note: n1}},
		{values.ToolNote, Intent{Target: n1, Before: 2, Prior: reanchor}, reanchor},
		{values.ToolNote, Intent{Target: n1, Before: 2, Prior: retitle}, retitle},
		{values.ToolNote, Intent{Target: n1, Before: 2}, RestoreNote{Note: n1}},
		{values.ToolSetSetting, Intent{Prior: setting}, setting},
		{values.ToolRecording, Intent{}, StopRecording{Recording: ref(t, values.KindRecording, "r1")}},
	}
}

func targetVersionOf(c Compensation) int64 {
	if d, ok := c.(RestoreDone); ok {
		return d.ExpectedVersion()
	}
	return 5
}

func TestWave10CompensationKinds(t *testing.T) {
	seen := map[CompensationKind]bool{}
	for _, s := range wave10Samples(t) {
		seen[s.comp.Kind()] = true
	}
	for _, tool := range []values.Tool{values.ToolChangeAssignment, values.ToolMarkAssignmentsDone, values.ToolNote,
		values.ToolSetSetting, values.ToolRecording} {
		for _, k := range CompensationsFor(tool) {
			if !seen[k] {
				t.Errorf("%s: kind %s has no sample", tool, k)
			}
		}
	}
	if len(seen) != 11 {
		t.Fatalf("%d wave-10 kinds", len(seen))
	}
}

// TestWave10CompleteAndUndo: every kind completes, survives Restore and is undone while the target is at the version
// the action left; another version is the refusal «Изменено после».
func TestWave10CompleteAndUndo(t *testing.T) {
	for _, s := range wave10Samples(t) {
		t.Run(string(s.comp.Kind()), func(t *testing.T) {
			v := targetVersionOf(s.comp)
			a := pendingWith(t, s.tool, s.in)
			if err := a.Complete(s.comp, v, at(1)); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if a.Compensation() != s.comp || !a.CanUndo(at(1)) {
				t.Fatal("compensation not kept or not undoable")
			}

			st := a.State()
			st.Version = 1
			back, err := Restore(st)
			if err != nil || back.State() != st {
				t.Fatalf("Restore = %v", err)
			}

			for _, tt := range []struct {
				name   string
				target Target
				apply  bool
				err    error
			}{
				{"expected version", Target{Version: v}, true, nil},
				{"changed since", Target{Version: v + 1}, false, ErrTargetChanged},
				{"already compensated", Target{Version: v + 1, Compensated: true}, false, nil},
			} {
				b, _ := Restore(st)
				if err := b.BeginUndo(at(2)); err != nil {
					t.Fatal(err)
				}
				apply, err := b.PlanUndo(tt.target)
				if apply != tt.apply || !errors.Is(err, tt.err) {
					t.Fatalf("%s: PlanUndo = %v, %v", tt.name, apply, err)
				}
				if err != nil {
					why, _ := UndoRefusalOf(err)
					if err := b.RefuseUndo(why, at(3)); err != nil || b.Refusal() != UndoChanged {
						t.Fatalf("%s: refusal %s, %v", tt.name, b.Refusal(), err)
					}
				}
			}
		})
	}
}

func TestWave10CompensationRules(t *testing.T) {
	a1, n1, m1 := ref(t, values.KindAssignment, "a1"), ref(t, values.KindNote, "n1"), ref(t, values.KindMaterial, "m1")
	l1, r1 := ref(t, values.KindLesson, "l1"), ref(t, values.KindRecording, "r1")
	d1 := date(t, "2026-11-03")
	fields := AssignmentFields{Title: "Реферат"}
	revert := func(f AssignmentFields) (Intent, Compensation) {
		c := RevertAssignment{Assignment: a1, Prior: f}
		return Intent{Target: a1, Before: 4, Prior: c}, c
	}
	reanchor := func(an NoteAnchor) (Intent, Compensation) {
		c := ReanchorNote{Note: n1, Prior: an}
		return Intent{Target: n1, Before: 2, Prior: c}, c
	}
	setting := func(s values.Setting, v string) (Intent, Compensation) {
		c := RestoreSetting{Setting: s, OldValue: v}
		return Intent{Prior: c}, c
	}
	pinned := func(in Intent, c Compensation) func() (Intent, Compensation) {
		return func() (Intent, Compensation) { return in, c }
	}
	plain := func(c Compensation) func() (Intent, Compensation) {
		return func() (Intent, Compensation) { return Intent{}, c }
	}
	tests := []struct {
		name string
		tool values.Tool
		make func() (Intent, Compensation)
		ok   bool
	}{
		{"revert with a due and a subject", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: "Реферат", Due: time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC), SubjectName: "Физика (лекции)"})), true},
		{"revert to an empty title", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: "  "})), false},
		{"revert to a title on two lines", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: "a\nb"})), false},
		{"revert to a long title", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: strings.Repeat("я", maxPriorTitleRunes+1)})), false},
		{"revert to a note with a bell", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: "x", Note: "a\u0007"})), false},
		{"revert to a subject on two lines", values.ToolChangeAssignment, pinned(revert(AssignmentFields{Title: "x", SubjectName: "a\nb"})), false},
		{"revert a material", values.ToolChangeAssignment, func() (Intent, Compensation) {
			c := RevertAssignment{Assignment: m1, Prior: fields}
			return Intent{Target: m1, Before: 4, Prior: c}, c
		}, false},
		{"restore a note as an assignment", values.ToolChangeAssignment, plain(RestoreAssignment{Assignment: n1}), false},
		{"delete nothing", values.ToolChangeAssignment, plain(DeleteAssignment{}), false},
		{"marks as an assignment change", values.ToolChangeAssignment, plain(restoreDone(t, marks(t, 1))), false},
		{"assignment change as marks", values.ToolMarkAssignmentsDone, plain(RestoreAssignment{Assignment: a1}), false},
		{"zero marks", values.ToolMarkAssignmentsDone, plain(RestoreDone{}), false},

		{"part of a note", values.ToolNote, plain(RemoveNotePart{Note: n1, Part: m1}), true},
		{"part that is an upload", values.ToolNote, plain(RemoveNotePart{Note: n1, Part: ref(t, values.KindUpload, "u1")}), false},
		{"remove an assignment as a note", values.ToolNote, plain(RemoveNote{Note: a1}), false},
		{"back to a recording", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorRecording, Target: r1, Date: d1})), true},
		{"back to a day", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorDay, Date: d1})), true},
		{"back to no date", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorUndated})), true},
		{"back to a lesson without its date", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorLesson, Target: l1})), false},
		{"back to a lesson that is a recording", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorLesson, Target: r1, Date: d1})), false},
		{"back to a day with a lesson", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorDay, Target: l1, Date: d1})), false},
		{"back to an undated day", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: AnchorUndated, Date: d1})), false},
		{"back to an unknown anchor", values.ToolNote, pinned(reanchor(NoteAnchor{Kind: "week", Date: d1})), false},
		{"empty old title", values.ToolNote, func() (Intent, Compensation) {
			c := RetitleNote{Note: n1}
			return Intent{Target: n1, Before: 2, Prior: c}, c
		}, false},
		{"long old title", values.ToolNote, func() (Intent, Compensation) {
			c := RetitleNote{Note: n1, OldTitle: strings.Repeat("я", maxNoteTitleRunes+1)}
			return Intent{Target: n1, Before: 2, Prior: c}, c
		}, false},

		{"toggle back on", values.ToolSetSetting, pinned(setting(values.SettingACPowerOnly, "on")), true},
		{"notice back off", values.ToolSetSetting, pinned(setting(values.SettingFreeWorkNotice, "off")), true},
		{"load back to a toggle value", values.ToolSetSetting, pinned(setting(values.SettingRecognitionLoad, "on")), false},
		{"toggle back to a load", values.ToolSetSetting, pinned(setting(values.SettingRecordingPrompts, "max")), false},
		{"unknown setting", values.ToolSetSetting, pinned(setting("hub_url", "on")), false},

		{"stop a lesson", values.ToolRecording, plain(StopRecording{Recording: l1}), false},
		{"recording undone as a job", values.ToolRecording, plain(CancelJob{Job: ref(t, values.KindJob, "j1")}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, c := tt.make()
			s := spec(tt.tool)
			s.Intent = in
			a, err := New(s, t0)
			if err != nil {
				// A broken prior is already refused as the intent.
				if tt.ok || !errors.Is(err, ErrInvalidCompensation) {
					t.Fatalf("New = %v", err)
				}
				return
			}
			err = a.Complete(c, targetVersionOf(c), at(1))
			if tt.ok != (err == nil) {
				t.Fatalf("Complete = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && (!errors.Is(err, ErrInvalidCompensation) || a.Status() != StatusPending) {
				t.Fatalf("refused Complete: %v, status %s", err, a.Status())
			}
		})
	}
}

func TestRestoreDoneList(t *testing.T) {
	full := marks(t, values.MaxMarkAssignments)
	c, err := NewRestoreDone(full)
	if err != nil || len(c.Marks()) != values.MaxMarkAssignments || c.ExpectedVersion() != int64(values.MaxMarkAssignments+1) {
		t.Fatalf("50 marks = %v, %d, expected %d", err, len(c.Marks()), c.ExpectedVersion())
	}
	c.Marks()[0].Version = 99
	if c.Marks()[0].Version == 99 {
		t.Fatal("Marks returns the compensation's own array")
	}

	dup := marks(t, 2)
	dup[1].Assignment = dup[0].Assignment
	unversioned := marks(t, 2)
	unversioned[1].Version = 0
	other := marks(t, 1)
	other[0].Assignment = ref(t, values.KindWork, "w1")
	for name, m := range map[string][]DoneMark{
		"none": nil, "51": marks(t, values.MaxMarkAssignments+1), "twice an assignment": dup,
		"a mark without the version it saved": unversioned, "a work": other,
	} {
		if _, err := NewRestoreDone(m); !errors.Is(err, ErrInvalidCompensation) {
			t.Errorf("%s: NewRestoreDone = %v", name, err)
		}
	}

	// One entry for the whole list; its target version is the highest saved one, nothing else.
	for _, tt := range []struct {
		v  int64
		ok bool
	}{{c.ExpectedVersion(), true}, {c.ExpectedVersion() - 1, false}, {1, false}} {
		a := pendingWith(t, values.ToolMarkAssignmentsDone, Intent{})
		if err := a.Complete(c, tt.v, at(1)); tt.ok != (err == nil) {
			t.Fatalf("Complete at %d = %v", tt.v, err)
		}
	}
	if !targetOf(c).IsZero() {
		t.Fatal("a list has a single target")
	}
}

// TestRecordingStopHasNoUndo: recording{stop} completes without a compensation (ADR-0033 §2, О-7 а); its entry is
// DONE and never undone. No other tool may complete without one.
func TestRecordingStopHasNoUndo(t *testing.T) {
	a := pendingWith(t, values.ToolRecording, Intent{})
	if err := a.Complete(nil, 3, at(1)); err != nil || a.Status() != StatusDone || a.Compensation() != nil {
		t.Fatalf("Complete(stop) = %v, %s", err, a.Status())
	}
	if a.CanUndo(at(2)) {
		t.Fatal("a stop offers «Отменить»")
	}
	if ok, why := a.Undoable(at(2), Target{}); ok || why != NoUndoIrreversible {
		t.Fatalf("Undoable = %v %s", ok, why)
	}
	for name, op := range map[string]func() error{
		"BeginUndo": func() error { return a.BeginUndo(at(2)) },
		"StartUndo": func() error { return a.StartUndo(at(2)) },
	} {
		if err := op(); !errors.Is(err, ErrInvalidTransition) || a.Status() != StatusDone {
			t.Fatalf("%s = %v, %s", name, err, a.Status())
		}
	}
	if _, err := a.PlanUndo(Target{Version: 3}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("PlanUndo = %v", err)
	}
	if !a.TargetRef().IsZero() {
		t.Fatal("a stop has a target to rebase")
	}

	st := a.State()
	st.Version = 1
	if back, err := Restore(st); err != nil || back.State() != st || back.CanUndo(at(2)) {
		t.Fatalf("Restore(stop) = %v", err)
	}
	for _, s := range []Status{StatusUndoing, StatusUndone, StatusUndoRefused} {
		bad := st
		bad.Status = s
		if s == StatusUndoRefused {
			bad.Refusal = UndoChanged
		}
		if _, err := Restore(bad); !errors.Is(err, ErrInvalidAction) {
			t.Fatalf("Restore(%s without a compensation) = %v", s, err)
		}
	}
	if err := pendingWith(t, values.ToolRecording, Intent{}).Complete(nil, 0, at(1)); !errors.Is(err, ErrInvalidCompensation) {
		t.Fatalf("stop without a version = %v", err)
	}

	// Every other U tool of catalog 10 still needs its compensation.
	for _, tool := range values.ToolsAt(values.CatalogWave10) {
		if _, has := CompensationFor(tool); !has || tool == values.ToolRecording {
			continue
		}
		s := spec(tool)
		if kind, creates := createsTarget[tool]; creates {
			s.Intent = Intent{Target: ref(t, kind, "x1")}
		}
		b, err := New(s, t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Complete(nil, 3, at(1)); !errors.Is(err, ErrInvalidCompensation) {
			t.Fatalf("%s completed without a compensation: %v", tool, err)
		}
	}
}

// TestEveryUToolOfCatalog10HasACompensation: the journal takes exactly the U tools of catalog 10, each with its kinds.
func TestEveryUToolOfCatalog10HasACompensation(t *testing.T) {
	for _, tool := range values.ToolsAt(values.CatalogWave10) {
		_, hasComp := CompensationFor(tool)
		s := spec(tool)
		if kind, creates := createsTarget[tool]; creates {
			s.Intent = Intent{Target: ref(t, kind, "x1")}
		}
		_, err := New(s, t0)
		if hasComp != (err == nil) {
			t.Fatalf("%s: compensation %v, journaled as U: %v", tool, hasComp, err)
		}
	}
}

// TestForgedModelDataCannotReachCompensation (hard rule 3; TASK-310/314): what an Undo writes back is the stored
// state journaled before the action or returned by the use case, never a value the model put in the call.
func TestForgedModelDataCannotReachCompensation(t *testing.T) {
	// No compensation holds a model value type: a parsed argument cannot be put in without a conversion.
	model := []reflect.Type{
		reflect.TypeFor[values.Text](), reflect.TypeFor[values.Label](), reflect.TypeFor[values.Query](),
		reflect.TypeFor[values.Name](), reflect.TypeFor[values.Args](), reflect.TypeFor[values.Call](),
		reflect.TypeFor[values.PlanStep](),
	}
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for _, m := range model {
			if typ == m {
				t.Errorf("%s is a model value %s", path, typ)
			}
		}
		switch typ.Kind() {
		case reflect.Struct:
			for i := range typ.NumField() {
				walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		case reflect.Array, reflect.Slice, reflect.Pointer:
			walk(path+"[]", typ.Elem())
		}
	}
	for _, s := range wave10Samples(t) {
		walk(string(s.comp.Kind()), reflect.TypeOf(s.comp))
		if !reflect.TypeOf(s.comp).Comparable() {
			t.Errorf("%s is not comparable", s.comp.Kind())
		}
	}

	// The call says one thing; the store said another before the action. Only the store's state completes.
	call, err := values.ParseCallAt("c1", "change_assignment", `{"assignment":"a1","action":"edit","title":"Чужой заголовок"}`, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	a1, _ := call.Args().ID("assignment")
	forgedTitle, _ := call.Args().Label("title")
	stored := RevertAssignment{Assignment: a1, Prior: AssignmentFields{Title: "Реферат по физике"}}
	forged := RevertAssignment{Assignment: a1, Prior: AssignmentFields{Title: string(forgedTitle)}}

	set, err := values.ParseCallAt("c2", "set_setting", `{"setting":"recognition_load","value":"max"}`, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	forgedValue, _ := set.Args().Enum("value")
	storedSetting := RestoreSetting{Setting: values.SettingRecognitionLoad, OldValue: "gentle"}
	forgedSetting := RestoreSetting{Setting: values.SettingRecognitionLoad, OldValue: forgedValue}

	n1 := ref(t, values.KindNote, "n1")
	tests := []struct {
		name string
		tool values.Tool
		in   Intent
		comp Compensation
		ok   bool
	}{
		{"stored prior", values.ToolChangeAssignment, Intent{Target: a1, Before: 4, Prior: stored}, stored, true},
		{"the call's title as the prior", values.ToolChangeAssignment, Intent{Target: a1, Before: 4, Prior: stored}, forged, false},
		{"no prior journaled", values.ToolChangeAssignment, Intent{Target: a1, Before: 4}, stored, false},
		{"stored setting", values.ToolSetSetting, Intent{Prior: storedSetting}, storedSetting, true},
		{"the call's value as the prior", values.ToolSetSetting, Intent{Prior: storedSetting}, forgedSetting, false},
		{"no setting journaled", values.ToolSetSetting, Intent{}, storedSetting, false},
		{"another title than journaled", values.ToolNote, Intent{Target: n1, Before: 2, Prior: RetitleNote{Note: n1, OldTitle: "Лекция 3"}},
			RetitleNote{Note: n1, OldTitle: "Лекция 4"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := pendingWith(t, tt.tool, tt.in)
			err := a.Complete(tt.comp, 5, at(1))
			if tt.ok != (err == nil) {
				t.Fatalf("Complete = %v, want ok=%v", err, tt.ok)
			}
			// Settle after a crash and Restore of a stored entry hold the same rule.
			st := State{Version: 1, ID: "act1", Thread: "th1", Turn: 1, CallID: "toolu_1", Tool: tt.tool, Status: StatusDone,
				Intent: tt.in, Compensation: tt.comp, TargetVersion: 5, CreatedAt: t0, UpdatedAt: at(1)}
			if _, err := Restore(st); tt.ok != (err == nil) {
				t.Fatalf("Restore = %v, want ok=%v", err, tt.ok)
			}
			b := pendingWith(t, tt.tool, tt.in)
			if err := b.Settle(Observation{Known: true, Version: 5, Effect: true, Compensation: tt.comp}, at(2)); err != nil {
				t.Fatal(err)
			}
			if want := map[bool]Status{true: StatusDone, false: StatusUnknown}[tt.ok]; b.Status() != want {
				t.Fatalf("Settle = %s, want %s", b.Status(), want)
			}
		})
	}
}

func TestWave10TargetRef(t *testing.T) {
	for _, s := range wave10Samples(t) {
		got := targetOf(s.comp)
		switch s.comp.(type) {
		case RestoreDone, RestoreSetting:
			if !got.IsZero() {
				t.Errorf("%s has target %v", s.comp.Kind(), got)
			}
		default:
			if got.IsZero() {
				t.Errorf("%s has no target", s.comp.Kind())
			}
		}
	}
}
