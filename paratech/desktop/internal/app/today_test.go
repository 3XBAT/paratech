package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/input/wails"
)

// stubHandler is a post-processing handler with a fixed outcome.
type stubHandler struct{ err error }

func (h stubHandler) Handle(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
	return h.err
}

// spyPoster records the jobs it is told about.
type spyPoster struct {
	calls int
	err   error
}

func (p *spyPoster) JobCompleted(context.Context, job.Snapshot) error {
	p.calls++
	return p.err
}

// The post is recorded only after a successful attempt, and its failure never fails the job.
func TestTodayPostsHook(t *testing.T) {
	boom, postErr := errors.New("llm down"), errors.New("disk full")
	cases := []struct {
		name      string
		handler   error
		poster    error
		wantErr   error
		wantPosts int
	}{
		{"attempt succeeded", nil, nil, nil, 1},
		{"attempt failed: no post", boom, nil, boom, 0},
		{"post failed: the job still succeeds", nil, postErr, nil, 1},
	}
	j, err := job.New(job.Spec{ID: "j1", Type: homeworkflow.SummarizeJobType, Key: "k", MaxAttempts: 1}, bridgeT0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &spyPoster{err: tc.poster}
			h := todayPosts{Handler: stubHandler{err: tc.handler}, poster: spy, log: slog.New(slog.DiscardHandler)}
			if err := h.Handle(context.Background(), j.Snapshot(), nil); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Handle = %v, want %v", err, tc.wantErr)
			}
			if spy.calls != tc.wantPosts {
				t.Fatalf("poster called %d times, want %d", spy.calls, tc.wantPosts)
			}
		})
	}
}

// The wired core binds «Сегодня»: an empty day reads without an error.
func TestTodayScreenIsWired(t *testing.T) {
	e, _ := testEnv(t)
	c, _, _, _ := buildPromptCore(t, e)
	if c.ui.Today == nil {
		t.Fatal("the core has no today screen")
	}
	got, err := wails.NewTodayService(c.ui.Today, slog.New(slog.DiscardHandler)).Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 0 || len(got.Chips) == 0 {
		t.Fatalf("today = %+v, want no posts and the hint chips", got)
	}
}
