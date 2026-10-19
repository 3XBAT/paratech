package activity

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// fakeDraftAssignments names assignments; calls counts the reads.
type fakeDraftAssignments struct {
	names map[shared.AssignmentID][2]string
	err   map[shared.AssignmentID]error
	calls int
}

func (f *fakeDraftAssignments) DraftAssignment(_ context.Context, id shared.AssignmentID) (string, string, error) {
	f.calls++
	if err := f.err[id]; err != nil {
		return "", "", err
	}
	n, ok := f.names[id]
	if !ok {
		return "", "", fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	return n[0], n[1], nil
}

func draftJob(t *testing.T, id string, a shared.AssignmentID) *job.Job {
	t.Helper()
	req, err := values.NewDraftRequest(a, 0, "")
	must(t, err)
	j, err := job.New(job.Spec{
		ID: shared.JobID(id), Type: values.HomeworkDraftJobType, Key: "homework_draft:" + id, Payload: req.Payload(), MaxAttempts: 3,
	}, t0)
	must(t, err)
	return j
}

// TASK-094: a draft row names its assignment and carries its id, so the page opens it.
func TestListJobsNamesTheAssignmentOfADraft(t *testing.T) {
	other, err := job.New(job.Spec{ID: "j-sync", Type: "calendar_sync", Key: "calendar_sync:1", MaxAttempts: 3}, t0)
	must(t, err)
	repo := &fakeJobs{list: []*job.Job{
		draftJob(t, "j1", "a1"), draftJob(t, "j2", "a1"), draftJob(t, "j3", "gone"), draftJob(t, "j4", "broken"), other,
	}}
	names := &fakeDraftAssignments{
		names: map[shared.AssignmentID][2]string{"a1": {"Пределы", "Математический анализ"}, "broken": {"x", "y"}},
		err:   map[shared.AssignmentID]error{"broken": errors.New("database is locked")},
	}
	s, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil)
	must(t, err)
	s.WithDraftAssignments(names)

	got, err := s.ListJobs(context.Background())
	must(t, err)
	want := []struct {
		id             shared.AssignmentID
		title, subject string
	}{
		{"a1", "Пределы", "Математический анализ"}, {"a1", "Пределы", "Математический анализ"}, {"gone", "", ""},
		{"broken", "", ""}, {"", "", ""},
	}
	for i, w := range want {
		if got[i].AssignmentID != w.id || got[i].AssignmentTitle != w.title || got[i].AssignmentSubject != w.subject {
			t.Errorf("row %d = %s %q %q, want %+v", i, got[i].AssignmentID, got[i].AssignmentTitle, got[i].AssignmentSubject, w)
		}
	}
	if names.calls != 3 {
		t.Errorf("reads = %d, want one per assignment", names.calls)
	}

	// Without the port the rows still carry the id.
	plain, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil)
	must(t, err)
	rows, err := plain.ListJobs(context.Background())
	must(t, err)
	if rows[0].AssignmentID != "a1" || rows[0].AssignmentTitle != "" {
		t.Fatalf("row without names = %+v", rows[0])
	}
}
