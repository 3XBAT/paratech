package action

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// wave11Samples is one valid compensation of every wave-11 kind, with the tool and the intent it completes under.
func wave11Samples(t *testing.T) []struct {
	tool values.Tool
	in   Intent
	comp Compensation
} {
	m1, s1 := ref(t, values.KindMaterial, "m1"), ref(t, values.KindSubject, "s1")
	back := PlaceBack{Material: m1, Subject: s1, Folder: ref(t, values.KindFolder, "f1")}
	relink := RelinkMaterial{Material: m1, Lesson: ref(t, values.KindLesson, "l1")}
	alias := RemoveAlias{Subject: s1, Alias: "Мат. анализ (лекции)"}
	folder := RecreateFolder{Subject: s1, Folder: ref(t, values.KindFolder, "f2"), Name: "Контрольные"}
	return []struct {
		tool values.Tool
		in   Intent
		comp Compensation
	}{
		{values.ToolMoveToInbox, Intent{Target: m1, Before: 3, Prior: back}, back},
		{values.ToolUnlinkMaterial, Intent{Target: m1, Before: 3, Prior: relink}, relink},
		{values.ToolRemoveMaterial, Intent{Target: m1, Before: 3}, RestoreMaterial{Material: m1}},
		{values.ToolAddAlias, Intent{Target: s1, Before: 2, Prior: alias}, alias},
		{values.ToolRemoveFolder, Intent{Target: s1, Before: 2, Prior: folder}, folder},
		{values.ToolRemoveSubject, Intent{Target: s1, Before: 2}, RestoreSubject{Subject: s1}},
	}
}

// TestEveryUToolOfCatalog11HasACompensation: the journal takes exactly the U tools of catalog 11, each with its kinds,
// and every wave-11 kind has a sample.
func TestEveryUToolOfCatalog11HasACompensation(t *testing.T) {
	for _, tool := range values.ToolsAt(values.CatalogWave11) {
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
	seen := map[CompensationKind]bool{}
	for _, s := range wave11Samples(t) {
		seen[s.comp.Kind()] = true
		if ks := CompensationsFor(s.tool); len(ks) != 1 || ks[0] != s.comp.Kind() {
			t.Fatalf("%s: kinds %v", s.tool, ks)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("%d wave-11 kinds", len(seen))
	}
	for _, tool := range []values.Tool{values.ToolSetTeacher, values.ToolClearTeacher, values.ToolSetProfileField} {
		if _, ok := CompensationFor(tool); ok {
			t.Fatalf("the C tool %s has an Undo", tool)
		}
		if _, err := New(spec(tool), t0); !errors.Is(err, ErrInvalidAction) {
			t.Fatalf("the C tool %s is journaled as U: %v", tool, err)
		}
	}
}

// TestWave11CompleteAndUndo: every kind completes, survives Restore and is undone while the target is at the version
// the action left; another version is «Изменено после».
func TestWave11CompleteAndUndo(t *testing.T) {
	for _, s := range wave11Samples(t) {
		t.Run(string(s.comp.Kind()), func(t *testing.T) {
			a := pendingWith(t, s.tool, s.in)
			if err := a.Complete(s.comp, 5, at(1)); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if a.Compensation() != s.comp || !a.CanUndo(at(1)) || a.TargetRef() != s.in.Target {
				t.Fatalf("compensation not kept, not undoable or target %v", a.TargetRef())
			}
			st := a.State()
			st.Version = 1
			if back, err := Restore(st); err != nil || back.State() != st {
				t.Fatalf("Restore = %v", err)
			}
			for _, tt := range []struct {
				target Target
				apply  bool
				err    error
			}{
				{Target{Version: 5}, true, nil},
				{Target{Version: 6}, false, ErrTargetChanged},
				{Target{Version: 6, Compensated: true}, false, nil},
			} {
				b, _ := Restore(st)
				if err := b.BeginUndo(at(2)); err != nil {
					t.Fatal(err)
				}
				if apply, err := b.PlanUndo(tt.target); apply != tt.apply || !errors.Is(err, tt.err) {
					t.Fatalf("PlanUndo(%+v) = %v, %v", tt.target, apply, err)
				}
			}
		})
	}
}

func TestWave11CompensationRules(t *testing.T) {
	m1, s1, f1 := ref(t, values.KindMaterial, "m1"), ref(t, values.KindSubject, "s1"), ref(t, values.KindFolder, "f1")
	l1, a1 := ref(t, values.KindLesson, "l1"), ref(t, values.KindAssignment, "a1")
	pinned := func(target values.Ref, c Compensation) func() (Intent, Compensation) {
		return func() (Intent, Compensation) { return Intent{Target: target, Before: 3, Prior: c}, c }
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
		{"back to a folder", values.ToolMoveToInbox, pinned(m1, PlaceBack{Material: m1, Subject: s1, Folder: f1}), true},
		{"back without a folder", values.ToolMoveToInbox, pinned(m1, PlaceBack{Material: m1, Subject: s1}), false},
		{"back without a subject", values.ToolMoveToInbox, pinned(m1, PlaceBack{Material: m1, Folder: f1}), false},
		{"back as a lesson", values.ToolMoveToInbox, pinned(m1, PlaceBack{Material: m1, Subject: s1, Folder: l1}), false},
		{"back as the old inbox undo", values.ToolMoveToInbox, plain(ReturnToInbox{Material: m1}), false},
		{"relink", values.ToolUnlinkMaterial, pinned(m1, RelinkMaterial{Material: m1, Lesson: l1}), true},
		{"relink to an assignment", values.ToolUnlinkMaterial, pinned(m1, RelinkMaterial{Material: m1, Lesson: a1}), false},
		{"unlink undone as unlink", values.ToolUnlinkMaterial, plain(UnlinkMaterial{Material: m1, Lesson: l1}), false},
		{"restore a material", values.ToolRemoveMaterial, plain(RestoreMaterial{Material: m1}), true},
		{"restore a subject as a material", values.ToolRemoveMaterial, plain(RestoreMaterial{Material: s1}), false},
		{"remove an alias", values.ToolAddAlias, pinned(s1, RemoveAlias{Subject: s1, Alias: "Матан"}), true},
		{"remove an empty alias", values.ToolAddAlias, pinned(s1, RemoveAlias{Subject: s1, Alias: " "}), false},
		{"remove an alias on two lines", values.ToolAddAlias, pinned(s1, RemoveAlias{Subject: s1, Alias: "a\nb"}), false},
		{"remove a long alias", values.ToolAddAlias, pinned(s1, RemoveAlias{Subject: s1, Alias: strings.Repeat("я", maxPriorNameRunes+1)}), false},
		{"recreate a folder", values.ToolRemoveFolder, pinned(s1, RecreateFolder{Subject: s1, Folder: f1, Name: "Контрольные"}), true},
		{"recreate without a name", values.ToolRemoveFolder, pinned(s1, RecreateFolder{Subject: s1, Folder: f1}), false},
		{"recreate without an id", values.ToolRemoveFolder, pinned(s1, RecreateFolder{Subject: s1, Name: "x"}), false},
		{"recreate as a material", values.ToolRemoveFolder, pinned(s1, RecreateFolder{Subject: m1, Folder: f1, Name: "x"}), false},
		{"folder undone as the create undo", values.ToolRemoveFolder, plain(RemoveFolder{Subject: s1, Folder: f1}), false},
		{"restore a subject", values.ToolRemoveSubject, plain(RestoreSubject{Subject: s1}), true},
		{"subject undone as the create undo", values.ToolRemoveSubject, plain(RemoveSubject{Subject: s1}), false},
		{"restore nothing", values.ToolRemoveSubject, plain(RestoreSubject{}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, c := tt.make()
			s := spec(tt.tool)
			s.Intent = in
			a, err := New(s, t0)
			if err != nil {
				if tt.ok || !errors.Is(err, ErrInvalidCompensation) {
					t.Fatalf("New = %v", err)
				}
				return
			}
			err = a.Complete(c, 5, at(1))
			if tt.ok != (err == nil) {
				t.Fatalf("Complete = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && (!errors.Is(err, ErrInvalidCompensation) || a.Status() != StatusPending) {
				t.Fatalf("refused Complete: %v, status %s", err, a.Status())
			}
		})
	}
}

// TestWave11PriorIsTheStoredOne (hard rule 3): the prior place, link, alias and folder an Undo writes back is the one
// journaled before the action; another one, as a call could suggest, is refused by Complete, Restore and Settle.
func TestWave11PriorIsTheStoredOne(t *testing.T) {
	m1, s1 := ref(t, values.KindMaterial, "m1"), ref(t, values.KindSubject, "s1")
	f1, f2 := ref(t, values.KindFolder, "f1"), ref(t, values.KindFolder, "f2")
	l1, l2 := ref(t, values.KindLesson, "l1"), ref(t, values.KindLesson, "l2")
	tests := []struct {
		name   string
		tool   values.Tool
		target values.Ref
		prior  Compensation
		comp   Compensation
		ok     bool
	}{
		{"stored place", values.ToolMoveToInbox, m1, PlaceBack{m1, s1, f1}, PlaceBack{m1, s1, f1}, true},
		{"another folder", values.ToolMoveToInbox, m1, PlaceBack{m1, s1, f1}, PlaceBack{m1, s1, f2}, false},
		{"no place journaled", values.ToolMoveToInbox, m1, nil, PlaceBack{m1, s1, f1}, false},
		{"another lesson", values.ToolUnlinkMaterial, m1, RelinkMaterial{m1, l1}, RelinkMaterial{m1, l2}, false},
		{"no link journaled", values.ToolUnlinkMaterial, m1, nil, RelinkMaterial{m1, l1}, false},
		{"another alias", values.ToolAddAlias, s1, RemoveAlias{s1, "Матан"}, RemoveAlias{s1, "Физика"}, false},
		{"another folder name", values.ToolRemoveFolder, s1, RecreateFolder{s1, f1, "Контрольные"}, RecreateFolder{s1, f1, "Чужое"}, false},
		{"stored folder", values.ToolRemoveFolder, s1, RecreateFolder{s1, f1, "Контрольные"}, RecreateFolder{s1, f1, "Контрольные"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Intent{Target: tt.target, Before: 3, Prior: tt.prior}
			a := pendingWith(t, tt.tool, in)
			if err := a.Complete(tt.comp, 5, at(1)); tt.ok != (err == nil) {
				t.Fatalf("Complete = %v, want ok=%v", err, tt.ok)
			}
			st := State{Version: 1, ID: "act1", Thread: "th1", Turn: 1, CallID: "toolu_1", Tool: tt.tool, Status: StatusDone,
				Intent: in, Compensation: tt.comp, TargetVersion: 5, CreatedAt: t0, UpdatedAt: at(1)}
			if _, err := Restore(st); tt.ok != (err == nil) {
				t.Fatalf("Restore = %v, want ok=%v", err, tt.ok)
			}
			b := pendingWith(t, tt.tool, in)
			if err := b.Settle(Observation{Known: true, Version: 5, Effect: true, Compensation: tt.comp}, at(2)); err != nil {
				t.Fatal(err)
			}
			if want := map[bool]Status{true: StatusDone, false: StatusUnknown}[tt.ok]; b.Status() != want {
				t.Fatalf("Settle = %s, want %s", b.Status(), want)
			}
		})
	}

	// No wave-11 compensation holds a model value type, and each is comparable like the others.
	model := []reflect.Type{
		reflect.TypeFor[values.Text](), reflect.TypeFor[values.Label](), reflect.TypeFor[values.Query](),
		reflect.TypeFor[values.Name](), reflect.TypeFor[values.Email](), reflect.TypeFor[values.CallLink](),
		reflect.TypeFor[values.ProfileValue](), reflect.TypeFor[values.ChatTitle](),
	}
	for _, s := range wave11Samples(t) {
		typ := reflect.TypeOf(s.comp)
		if !typ.Comparable() {
			t.Errorf("%s is not comparable", s.comp.Kind())
		}
		for i := range typ.NumField() {
			for _, m := range model {
				if typ.Field(i).Type == m {
					t.Errorf("%s.%s is a model value", s.comp.Kind(), typ.Field(i).Name)
				}
			}
		}
	}
}
