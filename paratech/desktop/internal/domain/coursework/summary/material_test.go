package summary

import (
	"errors"
	"reflect"
	"testing"
)

// The summary of a library material (TASK-122) has an id of its own kind and no transcript.
func TestMaterialSummary(t *testing.T) {
	s, err := New(Spec{Material: "m1", PromptVersion: 1, Content: content(t, "Слайды о производной.")}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != IDOfMaterial("m1") || s.ID() == IDOf("m1") || s.MaterialID() != "m1" || s.TranscriptID() != "" {
		t.Fatalf("summary %+v", s.State())
	}

	st := s.State()
	st.Version = 1
	back, err := Restore(st)
	if err != nil || !reflect.DeepEqual(back.State(), st) {
		t.Fatalf("Restore = %+v, %v", back, err)
	}
	// The material was deleted: the id stays, the link is gone.
	gone := st
	gone.Material = ""
	if _, err := Restore(gone); err != nil {
		t.Fatalf("Restore without the material = %v", err)
	}

	bad := map[string]State{
		"both sources":     func() State { b := st; b.Transcript = "t1"; return b }(),
		"other material":   func() State { b := st; b.Material = "m2"; return b }(),
		"material as a tx": func() State { b := st; b.ID = IDOf("m1"); return b }(),
	}
	for name, b := range bad {
		if _, err := Restore(b); !errors.Is(err, ErrInvalidSummary) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	for name, spec := range map[string]Spec{
		"both sources": {Transcript: "t1", Material: "m1", PromptVersion: 1, Content: content(t, "x")},
		"bad material": {Material: "a/b", PromptVersion: 1, Content: content(t, "x")},
		"no source":    {PromptVersion: 1, Content: content(t, "x")},
	} {
		if _, err := New(spec, t0); !errors.Is(err, ErrInvalidSummary) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
}
