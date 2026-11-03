package values

import (
	"errors"
	"testing"
)

func TestAssignmentsResult10(t *testing.T) {
	row := AssignmentRow{ID: mustRef(t, KindAssignment, "a1"), Subject: mustRef(t, KindSubject, "s1"), Due: day(t, "2026-10-24"),
		Status: AssignmentUpcoming}
	res, seen, err := AssignmentsResult10("c1", []AssignmentRow10{{AssignmentRow: row, Done: true},
		{AssignmentRow: AssignmentRow{ID: mustRef(t, KindAssignment, "a2"), Status: AssignmentNoDue}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"assignments":[{"id":"a1","subject":"s1","due":"2026-10-24","status":"upcoming","done":true},` +
		`{"id":"a2","status":"no_due","done":false}],"count":2,"truncated":false}`
	if res.ResultJSON != want {
		t.Fatalf("result = %s", res.ResultJSON)
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %v", seen)
	}
	if _, _, err := AssignmentsResult10("c1", []AssignmentRow10{{AssignmentRow: AssignmentRow{ID: row.ID, Status: "later"}}}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("bad status = %v", err)
	}

	// Catalog v9 is unchanged: no done field.
	v9, _, err := AssignmentsResult("c1", []AssignmentRow{row})
	if err != nil || v9.ResultJSON != `{"assignments":[{"id":"a1","subject":"s1","due":"2026-10-24","status":"upcoming"}],"count":1,"truncated":false}` {
		t.Fatalf("v9 = %s, %v", v9.ResultJSON, err)
	}
}

func TestMarksResult(t *testing.T) {
	a1, a2, a3 := mustRef(t, KindAssignment, "a1"), mustRef(t, KindAssignment, "a2"), mustRef(t, KindAssignment, "a3")
	res, seen, err := MarksResult("c1", []MarkRow{{ID: a1, Changed: true}, {ID: a2}, {ID: a3, Refusal: RefuseNotFound}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"changed":true,"done":true,"marks":[{"id":"a1","changed":true},{"id":"a2","changed":false},` +
		`{"id":"a3","changed":false,"refused":"not_found"}]}`
	if res.ResultJSON != want {
		t.Fatalf("result = %s", res.ResultJSON)
	}
	if len(seen) != 2 || seen[0] != a1 || seen[1] != a2 {
		t.Fatalf("seen = %v, want the rows not refused", seen)
	}

	bad := []struct {
		name string
		rows []MarkRow
	}{
		{"none", nil},
		{"not an assignment", []MarkRow{{ID: mustRef(t, KindSubject, "s1")}}},
		{"refused and changed", []MarkRow{{ID: a1, Changed: true, Refusal: RefuseFailed}}},
		{"another refusal", []MarkRow{{ID: a1, Refusal: RefuseBudget}}},
	}
	for _, tt := range bad {
		if _, _, err := MarksResult("c1", tt.rows); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("%s: err = %v, want ErrInvalidResult", tt.name, err)
		}
	}
}
