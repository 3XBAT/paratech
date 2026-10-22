package todayfeed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNewPoster = "todayfeed: new poster"
	opRecord    = "todayfeed: record a post"
	opNoSummary = "todayfeed: no summary stored, no post"
	opNoSource  = "todayfeed: transcript gone, no post"
	opStale     = "todayfeed: event of an older transcript, ignored"
)

// Poster records the posts after lessons from the completed post-processing jobs.
type Poster struct {
	posts       PostRepository
	transcripts TranscriptReader
	summaries   SummaryReader
	clock       Clock
	log         *slog.Logger
}

// NewPoster checks the ports.
func NewPoster(d PosterDeps) (*Poster, error) {
	if d.Posts == nil || d.Transcripts == nil || d.Summaries == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every port is required", opNewPoster, ErrInvalidConfig)
	}
	p := &Poster{posts: d.Posts, transcripts: d.Transcripts, summaries: d.Summaries, clock: d.Clock, log: d.Log}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	return p, nil
}

// eventOf is the post event of a post-processing job type; the lecture text and other types make none.
func eventOf(t job.Type) (post.Event, bool) {
	switch t {
	case job.Type(values.SummarizeJobType):
		return post.EventSummary, true
	case job.Type(values.ExtractHomeworkJobType):
		return post.EventHomework, true
	}
	return "", false
}

// JobCompleted records the post event of a post-processing job whose attempt succeeded; other job types are
// ignored. The runner may report a job twice (a retry after a lost save): the second call changes nothing.
func (p *Poster) JobCompleted(ctx context.Context, snap job.Snapshot) error {
	e, ok := eventOf(snap.Type())
	if !ok {
		return nil
	}
	req, err := values.ParsePostProcessPayload(snap.Payload())
	if err != nil {
		return fmt.Errorf("%s: %w", opRecord, err)
	}
	return p.Record(ctx, e, req.Transcript())
}

// Record applies event e of transcript t to the post of its recording, creating the post on the first event. A
// summary event counts only when the summary is stored, so a lesson without speech (TASK-110) never gets a post
// about its summary. An event of a transcript that is not the newest of its recording is ignored. Concurrent saves are reloaded and recorded again; the same event twice changes nothing.
func (p *Poster) Record(ctx context.Context, e post.Event, t shared.TranscriptID) error {
	if e == post.EventSummary {
		_, err := p.summaries.Get(ctx, summary.IDOf(t))
		switch {
		case errors.Is(err, shared.ErrNotFound):
			p.log.Info(opNoSummary, slog.String("transcript", t.String()))
			return nil
		case err != nil:
			return fmt.Errorf("%s: %w", opRecord, err)
		}
	}
	tr, err := p.transcripts.Get(ctx, t)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		p.log.Info(opNoSource, slog.String("transcript", t.String()))
		return nil
	case err != nil:
		return fmt.Errorf("%s: %w", opRecord, err)
	}

	// A late job of a transcript replaced by a rerun must not switch the post back to the old results.
	rec := tr.RecordingID()
	latest, err := p.transcripts.LatestByRecording(ctx, rec)
	if err != nil {
		return fmt.Errorf("%s: %w", opRecord, err)
	}
	if latest.ID() != t {
		p.log.Info(opStale, slog.String("transcript", t.String()), slog.String("latest", latest.ID().String()))
		return nil
	}

	var last error
	for range saveAttempts {
		now := p.clock.Now()
		cur, err := p.posts.Get(ctx, rec)
		switch {
		case errors.Is(err, shared.ErrNotFound):
			if cur, err = post.New(rec, e, t, now); err != nil {
				return fmt.Errorf("%s: %w", opRecord, err)
			}
		case err != nil:
			return fmt.Errorf("%s: %w", opRecord, err)
		default:
			changed, err := cur.Record(e, t, now)
			if err != nil {
				return fmt.Errorf("%s: %w", opRecord, err)
			}
			if !changed {
				return nil
			}
		}

		// A conflict is another event of the same recording saved meanwhile: reload and record again.
		if last = p.posts.Save(ctx, cur); last == nil || !errors.Is(last, shared.ErrVersionConflict) {
			break
		}
	}
	if last != nil {
		return fmt.Errorf("%s: %w", opRecord, last)
	}
	return nil
}
