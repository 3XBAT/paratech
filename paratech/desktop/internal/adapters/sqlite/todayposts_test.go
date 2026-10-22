package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

func newTestPost(t *testing.T, rec shared.RecordingID, e post.Event, at time.Time) *post.Post {
	t.Helper()
	p, err := post.New(rec, e, "t1", at)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodayPostSaveGetUpdate(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	repo := NewTodayPostRepository(e.db)
	rec := e.rec.ID()

	p := newTestPost(t, rec, post.EventSummary, asgT0)
	if err := repo.Save(ctx, p); err != nil || p.Version() != 1 {
		t.Fatalf("Save new = %v, version %d", err, p.Version())
	}
	got, err := repo.Get(ctx, rec)
	if err != nil || !reflect.DeepEqual(got.State(), p.State()) {
		t.Fatalf("Get = %+v, %v; want %+v", got, err, p.State())
	}

	if _, err := got.Record(post.EventHomework, "t1", asgT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil || got.Version() != 2 {
		t.Fatalf("Save update = %v, version %d", err, got.Version())
	}
	again, err := repo.Get(ctx, rec)
	if err != nil || !again.HomeworkChecked() || !again.SummaryReady() || again.Version() != 2 {
		t.Fatalf("after update: %+v, %v", again, err)
	}
}

// The recording is the dedup key in the database too: a second post of it is a conflict, never a second row.
func TestTodayPostConflicts(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	repo := NewTodayPostRepository(e.db)
	rec := e.rec.ID()
	if err := repo.Save(ctx, newTestPost(t, rec, post.EventSummary, asgT0)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		p    func() *post.Post
		want error
	}{
		{"second post of the recording", func() *post.Post { return newTestPost(t, rec, post.EventHomework, asgT0) }, shared.ErrVersionConflict},
		{"stale version", func() *post.Post {
			p := newTestPost(t, rec, post.EventSummary, asgT0)
			p.Persisted(7)
			return p
		}, shared.ErrVersionConflict},
		{"unknown recording", func() *post.Post { return newTestPost(t, "r-missing", post.EventSummary, asgT0) }, shared.ErrConflict},
	}
	for _, tc := range cases {
		if err := repo.Save(ctx, tc.p()); !errors.Is(err, tc.want) {
			t.Errorf("%s: Save = %v, want %v", tc.name, err, tc.want)
		}
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM today_posts"); n != 1 {
		t.Fatalf("%d posts, want 1", n)
	}
}

func TestTodayPostListAndCascade(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	repo := NewTodayPostRepository(e.db)
	rec := e.rec.ID()
	if _, err := repo.Get(ctx, rec); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get before save = %v, want ErrNotFound", err)
	}
	p := newTestPost(t, rec, post.EventSummary, asgT0)
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		since time.Time
		want  int
	}{{asgT0.Add(-time.Hour), 1}, {asgT0, 1}, {asgT0.Add(time.Millisecond), 0}} {
		list, err := repo.ListUpdatedSince(ctx, tc.since, 10)
		if err != nil || len(list) != tc.want {
			t.Fatalf("ListUpdatedSince(%v) = %d, %v; want %d", tc.since, len(list), err, tc.want)
		}
	}
	if _, err := repo.ListUpdatedSince(ctx, asgT0, 0); err == nil {
		t.Fatal("limit 0 accepted")
	}

	// The post goes with its recording (ADR-0018 deletion).
	mustExec(t, e.db, "DELETE FROM transcripts")
	mustExec(t, e.db, "DELETE FROM recordings")
	if _, err := repo.Get(ctx, rec); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after the recording was deleted = %v, want ErrNotFound", err)
	}
}

func TestProposalListByStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewProposalRepository(openTest(t, nil))
	for _, p := range []*proposal.Proposal{
		newProposal(t, "p1", propEvent, proposal.StatusValidated),
		newProposal(t, "p2", propEmail, proposal.StatusValidated),
		newProposal(t, "p3", propEvent, proposal.StatusRejected),
		newProposal(t, "p4", propExport, proposal.StatusExecuted),
	} {
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	ids := func(list []*proposal.Proposal) []proposal.ID {
		var out []proposal.ID
		for _, p := range list {
			out = append(out, p.ID())
		}
		return out
	}
	// Same creation time: the id breaks the tie.
	list, err := repo.ListByStatus(ctx, proposal.StatusValidated, 10)
	if err != nil || !reflect.DeepEqual(ids(list), []proposal.ID{"p1", "p2"}) {
		t.Fatalf("validated = %v, %v", ids(list), err)
	}
	if list, err = repo.ListByStatus(ctx, proposal.StatusValidated, 1); err != nil || len(list) != 1 {
		t.Fatalf("limit 1 = %v, %v", ids(list), err)
	}
	if list, err = repo.ListByStatus(ctx, proposal.StatusConfirmed, 10); err != nil || len(list) != 0 {
		t.Fatalf("confirmed = %v, %v", ids(list), err)
	}
	if _, err := repo.ListByStatus(ctx, proposal.StatusValidated, 0); err == nil {
		t.Fatal("limit 0 accepted")
	}
}
