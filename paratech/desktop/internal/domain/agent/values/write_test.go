package values

import (
	"errors"
	"testing"
)

func TestWriteResult(t *testing.T) {
	mk := func(kind IDKind, id string) Ref {
		r, err := NewRef(kind, id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	s1, j1 := mk(KindSubject, "s1"), mk(KindJob, "j1")
	tests := []struct {
		name string
		row  WriteRow
		json string
		refs int
	}{
		{"created", WriteRow{ID: s1, Changed: true}, `{"changed":true,"done":true,"id":"s1"}`, 1},
		{"job", WriteRow{Job: j1, Changed: true}, `{"changed":true,"done":true,"job":"j1"}`, 1},
		{"nothing to do", WriteRow{}, `{"changed":false,"done":true}`, 0},
		// create_assignment (v6): the ID only, never a title.
		{"added assignment", WriteRow{ID: mk(KindAssignment, "ag-1"), Changed: true}, `{"changed":true,"done":true,"id":"ag-1"}`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, refs, err := WriteResult("c1", tt.row)
			if err != nil || r.ResultJSON != tt.json || len(refs) != tt.refs || r.IsError {
				t.Fatalf("WriteResult = %+v, %v, %v", r, refs, err)
			}
		})
	}
	for _, bad := range []WriteRow{{ID: mk(KindWork, "w1")}, {Job: s1}} {
		if _, _, err := WriteResult("c1", bad); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("WriteResult(%+v) = %v", bad, err)
		}
	}
}
