package subjectview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/services/today"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/library/services/overview"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNew        = "subjectview: new"
	opOverview   = "subjectview: overview"
	opMarkOpened = "subjectview: mark opened"
	opVisits     = "subjectview: last opening unread, nothing marked new"
	opSummary    = "subjectview: summary unread, not marked new"
)

// Service reads the page of a subject and records its openings.
type Service struct {
	subjects    SubjectReader
	materials   MaterialList
	resolver    SubjectResolver
	assignments Assignments
	schedule    LessonReader
	knowledge   KnowledgeBase
	summaries   SummaryReader
	failures    Failures
	visits      Visits
	clock       Clock
	log         *slog.Logger
}

// New checks that every port except the logger is given.
func New(d Deps) (*Service, error) {
	if d.Subjects == nil || d.Materials == nil || d.Resolver == nil || d.Assignments == nil || d.Schedule == nil ||
		d.Knowledge == nil || d.Summaries == nil || d.Failures == nil || d.Visits == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every port except the logger is required", opNew, ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		subjects: d.Subjects, materials: d.Materials, resolver: d.Resolver, assignments: d.Assignments,
		schedule: d.Schedule, knowledge: d.Knowledge, summaries: d.Summaries, failures: d.Failures, visits: d.Visits,
		clock: d.Clock, log: log,
	}, nil
}

// Overview returns the data of the page of subject id. It changes nothing: the «новое» marks are relative to the
// opening MarkOpened recorded last.
func (s *Service) Overview(ctx context.Context, id shared.SubjectID) (Overview, error) {
	sub, err := s.subject(ctx, id)
	if err != nil {
		return Overview{}, err
	}
	fail := func(err error) (Overview, error) {
		return Overview{}, fmt.Errorf("%s: %w: %w", opOverview, ErrUnavailable, err)
	}
	now := s.clock.Now()
	out := Overview{Subject: id, Name: sub.Name().String(), OpenedAt: s.lastOpened(ctx, id)}

	deadlines, err := s.assignments.Deadlines(ctx)
	if err != nil {
		return fail(err)
	}
	day := today.DayStart(now)
	scheduled, err := s.schedule.ListStartingBetween(ctx, day.Add(-ScheduleBack), day.Add(ScheduleAhead))
	if err != nil {
		return fail(err)
	}
	resolved, err := s.resolve(ctx, deadlines, scheduled)
	if err != nil {
		return fail(err)
	}
	out.Deadlines = overview.Nearest(deadlinesOf(deadlines, resolved, id), now)
	out.Schedule = overview.Slots(scheduledOf(scheduled, resolved, id), now.Location())

	if out.Lessons, err = s.lessons(ctx, id, out.OpenedAt, now.Location()); err != nil {
		return fail(err)
	}

	materials, err := s.materials.ListIn(ctx, id)
	if err != nil {
		return fail(err)
	}
	out.Fresh = []FreshMaterial{}
	for _, m := range overview.FreshMaterials(materials, out.OpenedAt) {
		out.Fresh = append(out.Fresh, FreshMaterial{Material: m.ID(), Folder: m.Folder()})
	}
	return out, nil
}

// MarkOpened records that the student opened the page of subject id now; the next Overview marks only what appears
// after it.
func (s *Service) MarkOpened(ctx context.Context, id shared.SubjectID) error {
	if _, err := s.subject(ctx, id); err != nil {
		return err
	}
	if err := s.visits.MarkOpened(ctx, id, s.clock.Now()); err != nil {
		return fmt.Errorf("%s: %w: %w", opMarkOpened, ErrUnavailable, err)
	}
	return nil
}

// subject loads the subject; a malformed id is ErrInvalidRequest, a missing one ErrSubjectNotFound.
func (s *Service) subject(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	sub, err := s.subjects.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrSubjectNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return sub, nil
}

// lastOpened returns the previous opening; a store that cannot be read only costs the marks.
func (s *Service) lastOpened(ctx context.Context, id shared.SubjectID) time.Time {
	at, err := s.visits.LastOpened(ctx, id)
	if err != nil {
		s.log.Warn(opVisits, slog.String("subject", id.String()), slog.Any("cause", err))
		return time.Time{}
	}
	return at
}

// resolve maps the subject strings of the assignments and the scheduled lessons to library subjects in one call.
func (s *Service) resolve(ctx context.Context, ds []today.Deadline, ls []*lesson.Lesson) (map[string]shared.SubjectID, error) {
	var names []string
	add := func(n string) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	for _, d := range ds {
		add(d.Subject)
	}
	for _, l := range ls {
		add(l.Content().Subject)
	}
	out := map[string]shared.SubjectID{}
	if len(names) == 0 {
		return out, nil
	}
	matches, err := s.resolver.ResolveSubjects(ctx, names)
	if err != nil {
		return nil, err
	}
	for name, m := range matches {
		out[name] = m.ID
	}
	return out, nil
}

// deadlinesOf keeps the assignments whose subject string resolves to id.
func deadlinesOf(ds []today.Deadline, resolved map[string]shared.SubjectID, id shared.SubjectID) []overview.Deadline {
	var out []overview.Deadline
	for _, d := range ds {
		if d.Subject != "" && resolved[d.Subject] == id {
			out = append(out, overview.Deadline{Assignment: d.Assignment, Title: d.Title, Due: d.Due})
		}
	}
	return out
}

// scheduledOf keeps the scheduled lessons whose subject string resolves to id.
func scheduledOf(ls []*lesson.Lesson, resolved map[string]shared.SubjectID, id shared.SubjectID) []overview.Scheduled {
	var out []overview.Scheduled
	for _, l := range ls {
		c := l.Content()
		if c.Subject != "" && resolved[c.Subject] == id {
			out = append(out, overview.Scheduled{Kind: c.Kind.String(), Start: c.Start, End: c.End})
		}
	}
	return out
}

// lessons returns the latest recorded lessons of the subject with their status and the «новое» mark of their
// summaries.
func (s *Service) lessons(ctx context.Context, id shared.SubjectID, opened time.Time, loc *time.Location) ([]Lesson, error) {
	list, err := s.knowledge.Lessons(ctx, kb.LibraryKey(id))
	if err != nil {
		return nil, err
	}
	if len(list) > overview.MaxLessons {
		list = list[:overview.MaxLessons]
	}
	failed, err := s.failures.FailedRecordings(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Lesson, 0, len(list))
	for _, l := range list {
		row := Lesson{
			Recording: l.Recording, Title: l.Title, Kind: l.Kind, StartedAt: l.StartedAt.In(loc), Duration: l.Duration,
			Status: overview.StatusOf(overview.Progress{
				Transcribed: l.Ready.Transcribed(), Summary: l.Ready.Summary, Failed: slices.Contains(failed, l.Recording),
			}),
		}
		if row.Status == overview.StatusSummaryReady && !opened.IsZero() {
			if row.SummaryNew, err = s.summaryNew(ctx, l.Ready.Transcript, opened); err != nil {
				return nil, err
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// summaryNew reports whether the summary of transcript t appeared or was redone after opened. A summary that cannot
// be read only costs its mark; a cancelled read is returned.
func (s *Service) summaryNew(ctx context.Context, t shared.TranscriptID, opened time.Time) (bool, error) {
	sum, err := s.summaries.Get(ctx, summary.IDOf(t))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	case err != nil && ctx.Err() != nil:
		return false, ctx.Err()
	case err != nil:
		s.log.Warn(opSummary, slog.String("transcript", t.String()), slog.Any("cause", err))
		return false, nil
	}
	return overview.Fresh(sum.UpdatedAt(), opened), nil
}
