package filing

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

func newSubject(t *testing.T, id shared.SubjectID, name string) *subject.Subject {
	t.Helper()
	s, err := subject.New(subject.Spec{ID: id, Name: name, Semester: values.SemesterOf(t0)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func folderOf(t *testing.T, s *subject.Subject, sys subject.System) shared.FolderID {
	t.Helper()
	f, ok := s.SystemFolder(sys)
	if !ok {
		t.Fatalf("subject %s has no %s", s.ID(), sys)
	}
	return f.ID()
}

func TestTarget(t *testing.T) {
	math := newSubject(t, "s1", "Математика")
	tests := []struct {
		name   string
		sub    shared.SubjectID
		s      *subject.Subject
		sys    subject.System
		want   shared.FolderID
		wantOK bool
	}{
		{"homework of the subject", "s1", math, subject.SystemHomework, folderOf(t, math, subject.SystemHomework), true},
		{"tickets of the subject", "s1", math, subject.SystemTickets, folderOf(t, math, subject.SystemTickets), true},
		{"no subject", "", math, subject.SystemHomework, "", false},
		{"subject gone", "s1", nil, subject.SystemHomework, "", false},
		{"another subject", "s2", math, subject.SystemHomework, "", false},
		{"no such system folder", "s1", math, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Target(tt.sub, tt.s, tt.sys)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Target = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestNoteFolder(t *testing.T) {
	tests := []struct {
		kind string
		want subject.System
	}{
		{"lecture", subject.SystemLectures},
		{"seminar", subject.SystemSeminars},
		{"practice", subject.SystemSeminars},
		{"lab", subject.SystemLabs},
		{"unknown", subject.SystemLectures},
		{"", subject.SystemLectures},
		{"LAB", subject.SystemLectures}, // codes only: a snapshot keeps them lower-cased
	}
	for _, tt := range tests {
		if got := NoteFolder(tt.kind); got != tt.want {
			t.Errorf("NoteFolder(%q) = %s, want %s", tt.kind, got, tt.want)
		}
	}
}

func TestFiled(t *testing.T) {
	math := newSubject(t, "s1", "Математика")
	phys := newSubject(t, "s2", "Физика")
	mathDZ := folderOf(t, math, subject.SystemHomework)
	type place struct {
		sub    shared.SubjectID
		folder shared.FolderID
	}
	tests := []struct {
		name string
		at   place // "" subject: «Входящие»
		sub  shared.SubjectID
		s    *subject.Subject
		want bool
	}{
		{"in «ДЗ» of its subject", place{"s1", mathDZ}, "s1", math, true},
		{"in another folder of its subject", place{"s1", folderOf(t, math, subject.SystemTickets)}, "s1", math, false},
		{"in «ДЗ» of another subject", place{"s2", folderOf(t, phys, subject.SystemHomework)}, "s1", math, false},
		{"in «Входящие» while its subject has «ДЗ»", place{}, "s1", math, false},
		{"no subject, in «Входящие»", place{}, "", nil, true},
		{"no subject, moved into a subject's «ДЗ»", place{"s1", mathDZ}, "", nil, false},
		{"subject gone, in «Входящие»", place{}, "s3", nil, true},
		{"subject gone, moved into another subject", place{"s1", mathDZ}, "s3", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := material.New(material.Spec{ID: "m1", Digest: material.DigestOf([]byte("x")), Size: 1, Type: material.TypeTXT, Name: "a.txt"}, t0)
			if err != nil {
				t.Fatal(err)
			}
			if tt.at.sub != "" {
				if _, err := m.Place(tt.at.sub, tt.at.folder, t0); err != nil {
					t.Fatal(err)
				}
			}
			if got := Filed(m, tt.sub, tt.s, subject.SystemHomework); got != tt.want {
				t.Fatalf("Filed = %v, want %v", got, tt.want)
			}
		})
	}
}
