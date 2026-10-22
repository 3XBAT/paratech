package todayfeed

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

const (
	summarize = values.SummarizeJobType
	extract   = values.ExtractHomeworkJobType
)

func TestNewPosterNeedsEveryPort(t *testing.T) {
	if _, err := NewPoster(PosterDeps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewPoster without ports: %v, want ErrInvalidConfig", err)
	}
}

// A repeated completion event must not duplicate the post or change it (dedup by the recording).
func TestPosterDedupsRepeatedEvents(t *testing.T) {
	w := newWorld(t)
	w.addRecording("r1", "l1", "Матанализ", at(21, 10, 30), "t1")
	w.summaries["t1"] = true
	p := w.poster()
	ctx := context.Background()

	steps := []struct {
		name    string
		typ     string
		saves   int
		version int64
	}{
		{"summary", summarize, 1, 1},
		{"summary again", summarize, 1, 1},
		{"homework", extract, 2, 2},
		{"homework again", extract, 2, 2},
		{"summary once more", summarize, 2, 2},
	}
	for _, s := range steps {
		w.clock.Advance(time.Minute)
		if err := p.JobCompleted(ctx, snapshot(t, s.typ, "t1")); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if len(w.posts.rows) != 1 || w.posts.saves != s.saves || w.posts.rows["r1"].Version != s.version {
			t.Fatalf("%s: %d posts, %d saves, version %d; want 1, %d, %d", s.name, len(w.posts.rows), w.posts.saves,
				w.posts.rows["r1"].Version, s.saves, s.version)
		}
	}
	st := w.posts.rows["r1"]
	if st.SummaryAt.IsZero() || st.HomeworkAt.IsZero() || st.Transcript != "t1" {
		t.Fatalf("post = %+v", st)
	}
}

// A lesson without speech (TASK-110) has no summary: its event, if it ever came, makes no post.
func TestPosterNoSummaryNoPost(t *testing.T) {
	w := newWorld(t)
	w.addRecording("r1", "l1", "Матанализ", at(21, 10, 30), "t1")
	if err := w.poster().JobCompleted(context.Background(), snapshot(t, summarize, "t1")); err != nil {
		t.Fatal(err)
	}
	if len(w.posts.rows) != 0 {
		t.Fatalf("posts = %v, want none", w.posts.rows)
	}
}

func TestPosterIgnoresOtherJobsAndGoneTranscripts(t *testing.T) {
	w := newWorld(t)
	p := w.poster()
	ctx := context.Background()
	for _, typ := range []string{values.LectureTextJobType, values.HomeworkDraftJobType, "stt"} {
		if err := p.JobCompleted(ctx, snapshot(t, typ, "t1")); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	// The transcript was deleted with its recording before the event.
	if err := p.JobCompleted(ctx, snapshot(t, extract, "t-gone")); err != nil {
		t.Fatalf("gone transcript: %v", err)
	}
	if len(w.posts.rows) != 0 {
		t.Fatalf("posts = %v, want none", w.posts.rows)
	}
}

func TestPosterRefusesABadPayload(t *testing.T) {
	w := newWorld(t)
	j, err := job.New(job.Spec{ID: "j1", Type: job.Type(summarize), Key: "k", Payload: `{"x":1}`, MaxAttempts: 1}, tNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.poster().JobCompleted(context.Background(), j.Snapshot()); err == nil {
		t.Fatal("a payload without a transcript was accepted")
	}
}

// Two events of one recording saved at once: the loser reloads and records again, so both are kept.
func TestPosterRetriesAConcurrentSave(t *testing.T) {
	w := newWorld(t)
	w.addRecording("r1", "l1", "Матанализ", at(21, 10, 30), "t1")
	w.summaries["t1"] = true
	p := w.poster()
	ctx := context.Background()
	w.posts.beforeOp = func() {
		if err := p.Record(ctx, post.EventHomework, "t1"); err != nil {
			t.Error(err)
		}
	}
	if err := p.Record(ctx, post.EventSummary, "t1"); err != nil {
		t.Fatal(err)
	}
	st := w.posts.rows["r1"]
	if st.SummaryAt.IsZero() || st.HomeworkAt.IsZero() || st.Version != 2 {
		t.Fatalf("post = %+v, want both events at version 2", st)
	}
}

func TestPosterPassesStorageErrors(t *testing.T) {
	w := newWorld(t)
	w.addRecording("r1", "l1", "Матанализ", at(21, 10, 30), "t1")
	p, err := NewPoster(PosterDeps{Posts: failingPosts{err: errDisk}, Transcripts: worldTranscripts{w}, Summaries: worldSummaries{w}, Clock: w.clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Record(context.Background(), post.EventHomework, "t1"); !errors.Is(err, errDisk) {
		t.Fatalf("Record = %v, want the storage error", err)
	}
}

type failingPosts struct{ err error }

func (f failingPosts) Get(context.Context, shared.RecordingID) (*post.Post, error) { return nil, f.err }
func (f failingPosts) Save(context.Context, *post.Post) error                      { return f.err }
func (f failingPosts) ListUpdatedSince(context.Context, time.Time, int) ([]*post.Post, error) {
	return nil, f.err
}
