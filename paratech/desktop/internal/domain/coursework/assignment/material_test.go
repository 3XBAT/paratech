package assignment

import (
	"errors"
	"reflect"
	"testing"
)

// An assignment found in a library material (TASK-122) keeps the material as its origin.
func TestMaterialOrigin(t *testing.T) {
	a, err := NewExtracted(Spec{
		ID: "a1", Origin: Origin{Material: "m1", Index: 2}, PromptVersion: 1, Details: details(t, "Задача 5", "", t0),
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := a.Origin()
	if !ok || o != (Origin{Material: "m1", Index: 2}) || a.MaterialID() != "m1" || a.TranscriptID() != "" {
		t.Fatalf("origin = %+v, %v", o, ok)
	}
	if !a.Superseded(2) || a.Superseded(3) {
		t.Fatal("superseded by position, as for a transcript")
	}

	st := a.State()
	st.Version = 1
	back, err := Restore(st)
	if err != nil || !reflect.DeepEqual(back.State(), st) {
		t.Fatalf("Restore = %+v, %v", back, err)
	}
	gone := st
	gone.Material = ""
	if back, err := Restore(gone); err != nil {
		t.Fatal(err)
	} else if _, ok := back.Origin(); ok {
		t.Fatal("an origin whose material was deleted")
	}

	for name, s := range map[string]State{
		"both sources": func() State { b := st; b.Transcript = "t1"; return b }(),
		"bad material": func() State { b := st; b.Material = "a/b"; return b }(),
	} {
		if _, err := Restore(s); !errors.Is(err, ErrInvalidAssignment) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	for name, o := range map[string]Origin{
		"both sources": {Transcript: "t1", Material: "m1"},
		"bad material": {Material: "a/b"},
		"no source":    {},
	} {
		if _, err := NewExtracted(Spec{ID: "a2", Origin: o, PromptVersion: 1, Details: details(t, "x", "", t0)}, t0); !errors.Is(err, ErrInvalidAssignment) {
			t.Errorf("%s: NewExtracted = %v", name, err)
		}
	}
}
