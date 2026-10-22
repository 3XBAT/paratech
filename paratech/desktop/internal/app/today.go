package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/todayfeed"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// postTimeout bounds the recording of a post after its job, which runs detached from the job's context.
const postTimeout = 5 * time.Second

// todayPosts is the jobrunner hook of «Сегодня» (ADR-0021, TASK-125): after a post-processing attempt succeeded, the
// post after the lesson is recorded. A failure of the post never fails the job: the summary and the homework are
// stored already, and a rerun would ask the Hub again.
type todayPosts struct {
	jobrunner.Handler
	poster jobPoster
	log    *slog.Logger
}

// jobPoster records the post of a completed job; todayfeed.Poster.
type jobPoster interface {
	JobCompleted(ctx context.Context, snap job.Snapshot) error
}

// Handle runs the post-processing attempt, then records its post.
func (h todayPosts) Handle(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	if err := h.Handler.Handle(ctx, snap, report); err != nil {
		return err
	}
	// The job is done already: a shutdown cancelling ctx right now must not lose its post.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	if err := h.poster.JobCompleted(pctx, snap); err != nil {
		h.log.Warn("today post not recorded", slog.String("job", snap.ID().String()), slog.Any("cause", err))
	}
	return nil
}

// withTodayPosts wraps the post-processing handler with the post hook of «Сегодня».
func withTodayPosts(post jobrunner.Handler, db *sqlite.DB, e env, log *slog.Logger) (jobrunner.Handler, error) {
	poster, err := todayfeed.NewPoster(todayfeed.PosterDeps{
		Posts: sqlite.NewTodayPostRepository(db), Transcripts: sqlite.NewTranscriptRepository(db),
		Summaries: sqlite.NewSummaryRepository(db), Clock: e.local, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: today posts: %w", err)
	}
	return todayPosts{Handler: post, poster: poster, log: log}, nil
}

// newTodayScreen builds the data of «Сегодня» over the local storage, the homework use case (the assignments as the
// homework screen shows them) and the library (subjects of the threads). Days in the student's zone.
func (c *core) newTodayScreen(e env, lib *library.Service, log *slog.Logger) (*todayfeed.Screen, error) {
	screen, err := todayfeed.NewScreen(todayfeed.ScreenDeps{
		Posts: sqlite.NewTodayPostRepository(c.db), Recordings: sqlite.NewRecordingRepository(c.db),
		Lessons: sqlite.NewLessonRepository(c.db), Homework: sqlite.NewAssignmentRepository(c.db),
		Assignments: todayAssignments{c.proposals}, Proposals: sqlite.NewProposalRepository(c.db), Subjects: lib,
		Clock: e.local, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: today screen: %w", err)
	}
	return screen, nil
}

// todayAssignments is the Assignments port of «Сегодня» over the homework use case; mapping only.
type todayAssignments struct {
	homework *homeworkflow.Service
}

// Deadlines returns the listed assignments with their deadlines.
func (a todayAssignments) Deadlines(ctx context.Context) ([]today.Deadline, error) {
	list, err := a.homework.ListAssignments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]today.Deadline, 0, len(list))
	for _, v := range list {
		out = append(out, today.Deadline{Assignment: v.ID, Title: v.Title, Subject: v.Subject, Due: v.Due})
	}
	return out, nil
}

// Label returns the title and the subject the assignment shows.
func (a todayAssignments) Label(ctx context.Context, id shared.AssignmentID) (string, string, error) {
	v, err := a.homework.Assignment(ctx, id)
	return v.Title, v.Subject, err
}
