package values

import (
	"errors"
	"testing"
)

func TestVersionRef(t *testing.T) {
	r, err := VersionRef(3)
	if err != nil || r.Kind() != KindVersion || r.ID() != "v3" {
		t.Fatalf("VersionRef(3) = %v, %v", r, err)
	}
	if n, err := VersionNumber(r); err != nil || n != 3 {
		t.Fatalf("VersionNumber = %d, %v", n, err)
	}
	if _, err := VersionRef(0); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("VersionRef(0) = %v", err)
	}

	for _, bad := range []Ref{
		mustRef(t, KindVersion, "3"), mustRef(t, KindVersion, "v"), mustRef(t, KindVersion, "v0"),
		mustRef(t, KindVersion, "v03"), mustRef(t, KindVersion, "v-1"), mustRef(t, KindVersion, "vx"),
		mustRef(t, KindVersion, "v99999999999999999999"), mustRef(t, KindAssignment, "v3"), {},
	} {
		if _, err := VersionNumber(bad); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("VersionNumber(%v) = %v, want ErrInvalidRef", bad, err)
		}
	}
}

func TestProposalResult(t *testing.T) {
	tests := []struct {
		name   string
		target Ref
		want   string
	}{
		{"proposal", mustRef(t, KindProposal, "p1"), `{"proposal":"p1","proposed":true,"waits_for":"student"}`},
		{"preview", mustRef(t, KindPreview, "rec1"), `{"preview":"rec1","proposed":true,"waits_for":"student"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, seen, err := ProposalResult("c1", tt.target)
			if err != nil || res.ResultJSON != tt.want || res.IsError || len(seen) != 1 || seen[0] != tt.target {
				t.Fatalf("ProposalResult = %+v, %v, %v", res, seen, err)
			}
		})
	}
	for _, bad := range []Ref{{}, mustRef(t, KindAssignment, "a1")} {
		if _, _, err := ProposalResult("c1", bad); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("ProposalResult(%v) = %v", bad, err)
		}
	}
}

func TestAssignmentsResultShowsTheDraftVersion(t *testing.T) {
	v := mustRef(t, KindVersion, "v2")
	rows := []AssignmentRow{{ID: mustRef(t, KindAssignment, "a1"), Status: AssignmentNoDue, Version: v}}
	res, seen, err := AssignmentsResult("c1", rows)
	if err != nil {
		t.Fatal(err)
	}
	if res.ResultJSON != `{"assignments":[{"id":"a1","status":"no_due","version":"v2"}],"count":1,"truncated":false}` {
		t.Fatalf("result = %s", res.ResultJSON)
	}
	if len(seen) != 2 || seen[1] != v {
		t.Fatalf("seen = %v", seen)
	}
	rows[0].Version = mustRef(t, KindJob, "v2")
	if _, _, err := AssignmentsResult("c1", rows); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("a version of another kind = %v", err)
	}
}
