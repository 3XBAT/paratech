package homeworkflow

// The use case methods of the homework screen (TASK-045): the assignment list, the user's send proposal and the
// state of a generation.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// assignmentList is the AssignmentList over the proposal fakes: the assignments of the store, ids in reverse order.
type assignmentList struct{ s *store }

func (l assignmentList) ListRecent(_ context.Context, limit int) ([]*assignment.Assignment, error) {
	l.s.mu.Lock()
	ids := make([]shared.AssignmentID, 0, len(l.s.assignments))
	for id, ok := range l.s.assignments {
		if ok {
			ids = append(ids, id)
		}
	}
	l.s.mu.Unlock()
	slices.Sort(ids)
	slices.Reverse(ids)
	out := []*assignment.Assignment{}
	for _, id := range ids[:min(limit, len(ids))] {
		a, err := assignmentRepo(l).Get(context.Background(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// ppAssignmentList is the AssignmentList over the post-processing fakes, in the order of the extraction of t1.
type ppAssignmentList struct{ s *ppStore }

func (l ppAssignmentList) ListRecent(_ context.Context, limit int) ([]*assignment.Assignment, error) {
	out := []*assignment.Assignment{}
	for _, st := range l.s.assignmentsOf("t1") {
		if len(out) == limit {
			break
		}
		a, err := assignment.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// failingList records the limit and fails.
type failingList struct{ limits *[]int }

func (l failingList) ListRecent(_ context.Context, limit int) ([]*assignment.Assignment, error) {
	*l.limits = append(*l.limits, limit)
	return nil, errDiskFull
}

var errDiskFull = errors.New("disk full")

var _ AssignmentList = assignmentList{}

func TestListAssignments(t *testing.T) {
	e := newEditEnv(t)
	got, err := e.svc.ListAssignments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := e.assignmentsOf("t1")
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("ListAssignments = %d items, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v.ID != want[i].ID || v.Title != want[i].Title || v.Description != want[i].Description ||
			v.LessonID != want[i].Lesson || v.Version != want[i].Version {
			t.Errorf("item %d = %+v, want the stored assignment %+v", i, v, want[i])
		}
	}
}

func TestListAssignmentsIsBoundedAndWrapsErrors(t *testing.T) {
	e := newEnv(t)
	var limits []int
	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: assignmentRepo{e.store}, AssignmentList: failingList{&limits},
		Artifacts: e.files, Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Clock: e.clock, IDs: ids{e.store}, Tx: tx{e.store},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListAssignments(context.Background()); !errors.Is(err, errDiskFull) {
		t.Fatalf("err = %v, want the cause wrapped", err)
	}
	if !slices.Equal(limits, []int{MaxListedAssignments}) {
		t.Fatalf("limits = %v, want [%d]", limits, MaxListedAssignments)
	}

	empty, err := newEnv(t).svc.ListAssignments(context.Background())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no assignments: %v, %v, want an empty list", empty, err)
	}
}

// The user's send request takes the Propose path: VALIDATED with the recipient and the export content frozen, or
// REJECTED with a reason. No port of a side effect is called (AC-14).
func TestProposeSend(t *testing.T) {
	const content = "my homework" // withHomework's export
	sum := sha256.Sum256([]byte(content))
	tests := []struct {
		name   string
		setup  func(e *env)
		status proposal.Status
		reason proposal.RejectReason
	}{
		{name: "ready", status: proposal.StatusValidated},
		{name: "no recipient", setup: func(e *env) { e.recipient = "" }, status: proposal.StatusRejected,
			reason: proposal.ReasonNoRecipient},
		{name: "nothing exported", setup: func(e *env) {
			ref, _ := proposal.ExportRef("a1")
			_ = e.files.Delete(context.Background(), ref)
		}, status: proposal.StatusRejected, reason: proposal.ReasonNoArtifact},
		{name: "unknown assignment", setup: func(e *env) { delete(e.assignments, "a1") }, status: proposal.StatusRejected,
			reason: proposal.ReasonUnknownAssignment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t).withHomework(t)
			if tt.setup != nil {
				tt.setup(e)
			}
			v, err := e.svc.ProposeSend(context.Background(), "a1")
			if err != nil {
				t.Fatal(err)
			}
			if v.Kind != proposal.KindSendHomework || v.Status != tt.status || v.Reason != tt.reason {
				t.Fatalf("view = %+v, want %s %q", v, tt.status, tt.reason)
			}
			stored, err := proposalRepo{e.store}.Get(context.Background(), v.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.status == proposal.StatusValidated {
				if v.Recipient != "teacher@school.example" || v.AttachmentName != "homework.md" ||
					v.AttachmentSize != int64(len(content)) {
					t.Errorf("dialog data = %q %q %d", v.Recipient, v.AttachmentName, v.AttachmentSize)
				}
				if d := stored.AttachmentDigest(); d.SHA256() != hex.EncodeToString(sum[:]) ||
					stored.Recipient().Address() != "teacher@school.example" {
					t.Errorf("stored proposal does not pin the recipient and the export: %+v", stored.State())
				}
			} else if v.Recipient != "" || v.AttachmentName != "" || v.AttachmentSize != 0 {
				t.Errorf("a rejected proposal shows nothing pinned: %+v", v)
			}
			if n := e.sideEffects(); n != 0 {
				t.Fatalf("side effects = %d, want 0 before the confirmation", n)
			}
		})
	}
}

func TestProposeSendRefusesAnInvalidID(t *testing.T) {
	e := newEnv(t).withHomework(t)
	for _, id := range []shared.AssignmentID{"", "../a1", `a1","kind":"export_homework`} {
		if _, err := e.svc.ProposeSend(context.Background(), id); err == nil {
			t.Errorf("ProposeSend(%q) succeeded", id)
		}
	}
	if len(e.proposals) != 0 || e.sideEffects() != 0 {
		t.Fatalf("stored %d proposals and %d side effects, want none", len(e.proposals), e.sideEffects())
	}
}

// Only the user's Confirm makes the send executable; the mailer then gets the frozen recipient and content. A file
// changed after the confirmation makes the proposal STALE and nothing is sent.
func TestProposeSendThenConfirm(t *testing.T) {
	ctx := context.Background()
	t.Run("sends the confirmed content", func(t *testing.T) {
		e := newEnv(t).withHomework(t)
		v, err := e.svc.ProposeSend(ctx, "a1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, proposal.ErrNotConfirmed) || e.sideEffects() != 0 {
			t.Fatalf("Execute before Confirm = %v with %d side effects", err, e.sideEffects())
		}
		if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Execute(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		calls := e.mailer.Calls()
		if len(calls) != 1 || calls[0].Recipient().Address() != "teacher@school.example" ||
			calls[0].AttachmentDigest().Size() != v.AttachmentSize {
			t.Fatalf("mailer calls = %+v", calls)
		}
	})
	t.Run("stale after a change", func(t *testing.T) {
		e := newEnv(t).withHomework(t)
		v, err := e.svc.ProposeSend(ctx, "a1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		ref, _ := proposal.ExportRef("a1")
		if err := e.files.Write(ctx, ref, strings.NewReader("changed")); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, proposal.ErrStale) || e.sideEffects() != 0 {
			t.Fatalf("Execute = %v with %d side effects, want ErrStale and none", err, e.sideEffects())
		}
	})
}

// The screen follows a generation job: its status, and for a failed job the fixed error it ended with, also when
// the runner prefixed the text.
func TestGeneration(t *testing.T) {
	ctx := context.Background()

	t.Run("statuses", func(t *testing.T) {
		for _, st := range []job.Status{job.StatusQueued, job.StatusRunning, job.StatusWaitingForNetwork,
			job.StatusWaitingForQuota, job.StatusCompleted, job.StatusCancelled, job.StatusRetryWait} {
			e := newDraftEnv(t)
			res, err := e.svc.Generate(ctx, "a1", "")
			if err != nil {
				t.Fatal(err)
			}
			e.settle(res.JobID, st)
			v, err := e.svc.Generation(ctx, res.JobID)
			if err != nil || v.JobID != res.JobID || v.Status != st || v.Failure != nil {
				t.Errorf("%s: Generation = %+v, %v", st, v, err)
			}
		}
	})

	t.Run("failures", func(t *testing.T) {
		tests := []struct {
			name    string
			lastErr string
			want    error
		}{
			{"draft changed", "permanent: " + ErrDraftChanged.Error(), ErrDraftChanged},
			{"attempts exhausted", "attempts exhausted: " + ErrDraftStorage.Error(), ErrDraftStorage},
			{"invalid answer", ErrDraftInvalidAnswer.Error(), ErrDraftInvalidAnswer},
			{"assignment gone", ErrDraftAssignmentGone.Error(), ErrDraftAssignmentGone},
			{"unknown text", "something else", nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := newDraftEnv(t)
				res, err := e.svc.Generate(ctx, "a1", "")
				if err != nil {
					t.Fatal(err)
				}
				j := e.job(res.JobID)
				if err := j.Claim(e.clock.Now()); err != nil {
					t.Fatal(err)
				}
				if err := j.Fail(e.clock.Now(), tt.lastErr); err != nil {
					t.Fatal(err)
				}
				v, err := e.svc.Generation(ctx, res.JobID)
				if err != nil || v.Status != job.StatusFailed || !errors.Is(v.Failure, tt.want) || (tt.want == nil) != (v.Failure == nil) {
					t.Fatalf("Generation = %+v, %v, want failure %v", v, err, tt.want)
				}
			})
		}
	})

	// The text the runner really stores for a draft that changed under the job.
	t.Run("handler error as stored", func(t *testing.T) {
		e := newDraftEnv(t)
		res, err := e.svc.Generate(ctx, "a1", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.EditDraft(ctx, DraftEdit{Assignment: "a1", Base: 0, Title: "Mine", Body: "Text."}); err != nil {
			t.Fatal(err)
		}
		herr := e.run(res.JobID)
		j := e.job(res.JobID)
		if err := j.Claim(e.clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := j.Fail(e.clock.Now(), herr.Error()); err != nil {
			t.Fatal(err)
		}
		if v, err := e.svc.Generation(ctx, res.JobID); err != nil || !errors.Is(v.Failure, ErrDraftChanged) {
			t.Fatalf("Generation = %+v, %v, want ErrDraftChanged", v, err)
		}
	})

	t.Run("not a draft job", func(t *testing.T) {
		e := newDraftEnv(t)
		other, err := job.New(job.Spec{ID: "jx", Type: SummarizeJobType, Key: "summarize:t1", Payload: `{}`, MaxAttempts: 1},
			e.clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := (ppJobs{e.ppStore}).Enqueue(ctx, other); err != nil {
			t.Fatal(err)
		}
		for _, id := range []shared.JobID{"jx", "missing"} {
			if _, err := e.svc.Generation(ctx, id); !errors.Is(err, shared.ErrNotFound) {
				t.Errorf("Generation(%s) = %v, want ErrNotFound", id, err)
			}
		}
	})
}

// GenerateFrom generates only from the version the student sees.
func TestGenerateFrom(t *testing.T) {
	ctx := context.Background()
	e := newDraftEnv(t)
	e.llm.Answer(values.OperationHomeworkDraft, answer("T", "Model text."))
	if _, err := e.svc.EditDraft(ctx, DraftEdit{Assignment: "a1", Base: 0, Title: "Mine", Body: "Text."}); err != nil {
		t.Fatal(err)
	}
	for _, base := range []int{0, 2, -1} {
		if _, err := e.svc.GenerateFrom(ctx, "a1", base, ""); !errors.Is(err, homeworkdraft.ErrStaleBase) {
			t.Errorf("GenerateFrom(base %d) = %v, want ErrStaleBase", base, err)
		}
	}
	if n := e.jobCount(); n != 0 {
		t.Fatalf("jobs = %d, want none for a stale base", n)
	}
	res, err := e.svc.GenerateFrom(ctx, "a1", 1, "shorter")
	if err != nil || !res.Created || res.Base != 1 {
		t.Fatalf("GenerateFrom(1) = %+v, %v", res, err)
	}
	if again, err := e.svc.Generate(ctx, "a1", "shorter"); err != nil || again.JobID != res.JobID {
		t.Fatalf("Generate with the same input = %+v, %v, want the same job", again, err)
	}
}
