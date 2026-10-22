package post

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Event is a completed post-processing step of a transcript.
type Event string

// Events.
const (
	EventSummary  Event = "summary"  // the summary of the transcript is stored
	EventHomework Event = "homework" // the homework of the transcript was looked for (zero found is fine)
)

// Valid reports whether e is a known event.
func (e Event) Valid() bool { return e == EventSummary || e == EventHomework }

// Post is the post of one recording. Fields change only through its methods (ARCH-14).
type Post struct {
	version    int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	recording  shared.RecordingID
	transcript shared.TranscriptID
	summaryAt  time.Time // zero: no summary yet
	homeworkAt time.Time // zero: the homework was not looked for yet
	createdAt  time.Time
	updatedAt  time.Time
}

// New creates the post of a recording on its first event.
func New(rec shared.RecordingID, e Event, t shared.TranscriptID, now time.Time) (*Post, error) {
	if err := rec.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPost, err)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidPost)
	}
	p := &Post{recording: rec, createdAt: now, updatedAt: now}
	if _, err := p.Record(e, t, now); err != nil {
		return nil, err
	}
	return p, nil
}

// State is the stored form of a post, for Restore.
type State struct {
	Version    int64
	Recording  shared.RecordingID
	Transcript shared.TranscriptID
	SummaryAt  time.Time
	HomeworkAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Restore rebuilds a stored post without side effects and checks its invariants.
func Restore(s State) (*Post, error) {
	bad := func(what string) (*Post, error) { return nil, fmt.Errorf("%w: %s", ErrInvalidPost, what) }
	switch {
	case s.Version < 1:
		return bad("stored version must be at least 1")
	case s.Recording.Validate() != nil:
		return bad("bad recording id")
	case s.Transcript.Validate() != nil:
		return bad("bad transcript id")
	case s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt):
		return bad("inconsistent timestamps")
	case s.SummaryAt.IsZero() && s.HomeworkAt.IsZero():
		return bad("a post without an event")
	case s.SummaryAt.After(s.UpdatedAt) || s.HomeworkAt.After(s.UpdatedAt):
		return bad("an event after the last change")
	}
	return &Post{
		version: s.Version, recording: s.Recording, transcript: s.Transcript, summaryAt: s.SummaryAt,
		homeworkAt: s.HomeworkAt, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt,
	}, nil
}

// State returns the stored form of the post.
func (p *Post) State() State {
	return State{
		Version: p.version, Recording: p.recording, Transcript: p.transcript, SummaryAt: p.summaryAt,
		HomeworkAt: p.homeworkAt, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt,
	}
}

// Persisted records the version the store assigned after a successful Save (ADR-0004).
func (p *Post) Persisted(version int64) { p.version = version }

// Version returns the persisted version.
func (p *Post) Version() int64 { return p.version }

// Recording returns the recording of the post, its identity.
func (p *Post) Recording() shared.RecordingID { return p.recording }

// Transcript returns the transcript whose results the post shows.
func (p *Post) Transcript() shared.TranscriptID { return p.transcript }

// SummaryReady reports whether the summary of the transcript is stored.
func (p *Post) SummaryReady() bool { return !p.summaryAt.IsZero() }

// HomeworkChecked reports whether the homework of the transcript was looked for.
func (p *Post) HomeworkChecked() bool { return !p.homeworkAt.IsZero() }

// CreatedAt returns when the post was created.
func (p *Post) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns when the post last changed.
func (p *Post) UpdatedAt() time.Time { return p.updatedAt }

// Record applies an event of transcript t and reports whether the post changed. The same event again changes nothing
// (the dedup of a repeated completion). An event of another transcript switches the post to it and drops the other
// event, whose result belonged to the old transcript.
func (p *Post) Record(e Event, t shared.TranscriptID, now time.Time) (bool, error) {
	switch {
	case !e.Valid():
		return false, fmt.Errorf("%w: unknown event %q", ErrInvalidPost, string(e))
	case t.Validate() != nil:
		return false, fmt.Errorf("%w: bad transcript id", ErrInvalidPost)
	case now.IsZero():
		return false, fmt.Errorf("%w: no event time", ErrInvalidPost)
	}

	if t != p.transcript {
		p.transcript, p.summaryAt, p.homeworkAt = t, time.Time{}, time.Time{}
	}
	at := &p.summaryAt
	if e == EventHomework {
		at = &p.homeworkAt
	}
	if !at.IsZero() {
		return false, nil
	}
	*at = now
	if now.After(p.updatedAt) {
		p.updatedAt = now
	}
	return true, nil
}

// ShownAt is when the post became worth showing, given how many assignments its transcript has now: the summary, or
// the homework when some was found. Zero means it is not shown (a lesson without speech has neither, TASK-110).
func (p *Post) ShownAt(assignments int) time.Time {
	shown := p.summaryAt
	if assignments > 0 && !p.homeworkAt.IsZero() && (shown.IsZero() || p.homeworkAt.Before(shown)) {
		shown = p.homeworkAt
	}
	return shown
}
