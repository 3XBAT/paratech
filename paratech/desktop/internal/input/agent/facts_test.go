package agent

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/subject"
)

func TestFacts(t *testing.T) {
	k := &fakeKnowledge{lessons: []kb.Lesson{
		{Recording: "r1", Ready: kb.Readiness{Transcript: "t1", Summary: true}},
		{Recording: "r2", Ready: kb.Readiness{Transcript: "t2", LectureText: true}},
	}}
	f, err := NewFacts(k)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		tool values.Tool
		args string
		want thread.Facts
	}{
		{"summary exists", values.ToolMakeSummary, `{"recording":"r1"}`, thread.Facts{ReplacesReady: true}},
		{"no summary yet", values.ToolMakeSummary, `{"recording":"r2"}`, thread.Facts{}},
		{"lecture text exists", values.ToolMakeLectureText, `{"recording":"r2"}`, thread.Facts{ReplacesReady: true}},
		{"no lecture text yet", values.ToolMakeLectureText, `{"recording":"r1"}`, thread.Facts{}},
		{"unknown recording", values.ToolMakeSummary, `{"recording":"r9"}`, thread.Facts{}},
		{"material has no stored results yet", values.ToolMakeSummary, `{"material":"m1"}`, thread.Facts{}},
		// The cost of a paid job is unknown: zero, so the domain asks for a plan (conservative, D8).
		{"paid job without an estimate", values.ToolFindHomework, `{"recording":"r1"}`, thread.Facts{}},
		{"read", values.ToolListSubjects, `{}`, thread.Facts{}},
	}
	for _, tt := range tests {
		got, err := f.Facts(t.Context(), mustCall(t, "c1", tt.tool, tt.args))
		if err != nil || got.ReplacesReady != tt.want.ReplacesReady || got.CostMicroUSD != 0 || got.Triage != "" {
			t.Errorf("%s: %+v, %v", tt.name, got, err)
		}
	}

	k.err = errors.New("database is locked")
	if _, err := f.Facts(t.Context(), mustCall(t, "c1", values.ToolMakeSummary, `{"recording":"r1"}`)); err == nil {
		t.Fatal("a failed read was taken as «nothing to replace»")
	}
}

func TestBaseSubjects(t *testing.T) {
	lib := &fakeLibrary{subjects: []*subject.Subject{
		newSubject(t, "s1", "Физика"), newSubject(t, "s2", injection), newSubject(t, "s3", "Линейная алгебра (2 часть)"),
	}}
	b, err := NewBaseSubjects(lib)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.BaseSubjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID.ID() != "s1" || got[0].Name != "Физика" || got[1].ID.ID() != "s3" {
		t.Fatalf("base subjects = %+v", got)
	}
	today, _ := values.NewDate(2026, 10, 22)
	if _, err := values.NewBase(got, today, "Europe/Moscow"); err != nil {
		t.Fatalf("the base refuses the subjects: %v", err)
	}

	lib.err = errors.New("database is locked")
	if _, err := b.BaseSubjects(t.Context()); err == nil {
		t.Fatal("a failed read gave an empty base")
	}
}
