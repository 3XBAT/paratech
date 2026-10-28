package todayfeed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNewScreen = "todayfeed: new screen"
	opToday     = "todayfeed: today"
	opThread    = "todayfeed: subject thread"
	opLesson    = "todayfeed: live lesson of a post unread"
	opLabel     = "todayfeed: assignment of a proposal unread"
)

// Screen reads the data of «Сегодня» and the posts of a subject thread. Everything is local: it works offline.
type Screen struct {
	posts       PostRepository
	recordings  RecordingReader
	lessons     LessonReader
	homework    AssignmentReader
	assignments Assignments
	proposals   ProposalList
	subjects    SubjectResolver
	clock       Clock
	log         *slog.Logger
}

// NewScreen checks the ports.
func NewScreen(d ScreenDeps) (*Screen, error) {
	if d.Posts == nil || d.Recordings == nil || d.Lessons == nil || d.Homework == nil || d.Assignments == nil ||
		d.Proposals == nil || d.Subjects == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every port is required", opNewScreen, ErrInvalidConfig)
	}
	s := &Screen{
		posts: d.Posts, recordings: d.Recordings, lessons: d.Lessons, homework: d.Homework, assignments: d.Assignments,
		proposals: d.Proposals, subjects: d.Subjects, clock: d.Clock, log: d.Log,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Today returns the data of «Сегодня» for the student's current day.
func (s *Screen) Today(ctx context.Context) (Today, error) {
	now := s.clock.Now()
	day := today.DayStart(now)
	fail := func(err error) (Today, error) { return Today{}, fmt.Errorf("%s: %w", opToday, err) }

	// Yesterday's posts only feed the chips.
	views, err := s.recent(ctx, day.AddDate(0, 0, -1), now)
	if err != nil {
		return fail(err)
	}
	out := Today{Day: day, Posts: []PostView{}, Done: []DoneItem{}}
	summaryYesterday := false
	for _, v := range views {
		switch today.DaysFrom(now, v.ShownAt) {
		case 0:
			out.Posts = append(out.Posts, v)
			out.Done = append(out.Done, DoneItem{
				Recording: v.Recording, Lesson: v.Lesson, SummaryReady: v.SummaryReady, Homework: v.Homework, At: v.ShownAt,
			})
		case -1:
			summaryYesterday = summaryYesterday || v.SummaryReady
		}
	}

	if out.Lessons, err = s.lessonsOf(ctx, day, views); err != nil {
		return fail(err)
	}
	if out.Waiting, err = s.waiting(ctx, now); err != nil {
		return fail(err)
	}
	deadlines, err := s.assignments.Deadlines(ctx)
	if err != nil {
		return fail(err)
	}
	out.Burning = today.BurningOf(deadlines, now)
	for i := range out.Burning {
		out.Burning[i].Due = local(out.Burning[i].Due, now)
	}

	todaySubject := ""
	for _, v := range out.Posts { // newest first
		if v.SummaryReady {
			todaySubject = v.Lesson.SubjectName
			break
		}
	}
	out.Chips = today.Chips(today.ChipFacts{
		Burning: len(out.Burning), TodaySubject: todaySubject, SummaryYesterday: summaryYesterday,
	})
	return out, nil
}

// Thread returns the posts of the last ThreadHistory in the thread of a library subject, newest first; "" is the
// thread «Без предмета» (lessons whose subject resolves to no library subject).
func (s *Screen) Thread(ctx context.Context, subject shared.SubjectID) ([]PostView, error) {
	if subject != "" {
		if err := subject.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", opThread, err)
		}
	}
	now := s.clock.Now()
	views, err := s.recent(ctx, now.Add(-ThreadHistory), now)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opThread, err)
	}
	out := []PostView{}
	for _, v := range views {
		if v.Lesson.SubjectID == subject {
			out = append(out, v)
		}
	}
	return out, nil
}

// recent returns the views of the posts changed since since that are worth showing, newest shown first.
func (s *Screen) recent(ctx context.Context, since, now time.Time) ([]PostView, error) {
	posts, err := s.posts.ListUpdatedSince(ctx, since, MaxPosts)
	if err != nil {
		return nil, err
	}
	views := make([]PostView, 0, len(posts))
	for _, p := range posts {
		v, ok, err := s.view(ctx, p, now)
		if err != nil {
			return nil, err
		}
		if ok && !v.ShownAt.Before(since) {
			views = append(views, v)
		}
	}
	if err := s.resolve(ctx, views); err != nil {
		return nil, err
	}
	slices.SortFunc(views, func(a, b PostView) int {
		return cmp.Or(b.ShownAt.Compare(a.ShownAt), cmp.Compare(a.Recording, b.Recording))
	})
	return views, nil
}

// view builds the view of a post; ok is false for a post not worth showing or whose recording is gone or being
// deleted.
func (s *Screen) view(ctx context.Context, p *post.Post, now time.Time) (PostView, bool, error) {
	rec, err := s.recordings.Get(ctx, p.Recording())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return PostView{}, false, nil
	case err != nil:
		return PostView{}, false, err
	case rec.Deleting():
		return PostView{}, false, nil
	}
	all, err := s.homework.ListByTranscript(ctx, p.Transcript())
	if err != nil {
		return PostView{}, false, err
	}
	list := make([]*assignment.Assignment, 0, len(all))
	for _, a := range all {
		if !a.Deleted() { // a deleted assignment is hidden everywhere (ADR-0029)
			list = append(list, a)
		}
	}
	shown := p.ShownAt(len(list))
	if shown.IsZero() {
		return PostView{}, false, nil
	}

	facts := today.PostFacts{Summary: p.SummaryReady(), HomeworkChecked: p.HomeworkChecked()}
	for _, a := range list {
		facts.Homework = append(facts.Homework, today.Homework{Assignment: a.ID(), Due: a.Details().Due(), Done: a.Done()})
	}
	card := today.CardOf(facts, now)
	v := PostView{
		Recording: p.Recording(), Transcript: p.Transcript(), Lesson: s.lessonInfo(ctx, rec, now.Location()),
		Headline: card.Headline, SummaryReady: card.Summary, Homework: card.Homework, HomeworkDone: card.Done,
		EarliestDue: local(card.EarliestDue, now),
		Calendar:    make([]CalendarAction, 0, len(card.Calendar)), ShownAt: local(shown, now),
	}
	for _, h := range card.Calendar {
		v.Calendar = append(v.Calendar, CalendarAction{
			ProposalKind: proposal.KindDeadlineEvent, Assignment: h.Assignment, Due: local(h.Due, now),
		})
	}
	return v, true, nil
}

// local is t in the student's zone (the zone of now); the zero time stays zero.
func local(t, now time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(now.Location())
}

// lessonInfo resolves the lesson of a recording as the knowledge base does (kb.NewLesson). The live lesson is read
// only when the snapshot lacks the title; a failed read only costs its names.
func (s *Screen) lessonInfo(ctx context.Context, rec *recording.Recording, loc *time.Location) LessonInfo {
	f := kb.Facts{Recording: rec.ID(), StartedAt: rec.StartedAt()}
	start := rec.StartedAt()
	snap := rec.LessonSnapshot()
	if !snap.IsZero() {
		f.Snapshot = &kb.Source{Title: snap.Title(), Subject: snap.Subject(), Kind: snap.Kind()}
		if !snap.ScheduledStart().IsZero() {
			start = snap.ScheduledStart()
		}
	}
	if c := rec.LessonCorrection(); !c.IsZero() {
		f.Correction = kb.Source{Subject: c.Subject(), Kind: c.Kind()}
	}
	if rec.LessonID() != "" && snap.Title() == "" {
		l, err := s.lessons.Get(ctx, rec.LessonID())
		switch {
		case errors.Is(err, shared.ErrNotFound):
		case err != nil:
			s.log.Warn(opLesson, slog.String("recording", rec.ID().String()), slog.Any("cause", err))
		default:
			c := l.Content()
			f.Live = &kb.Source{Title: c.Title, Subject: c.Subject, Kind: c.Kind.String()}
			if snap.ScheduledStart().IsZero() {
				start = c.Start
			}
		}
	}
	l := kb.NewLesson(f)
	return LessonInfo{
		Lesson: rec.LessonID(), Title: l.Title, Subject: l.Subject, SubjectName: l.Subject, Kind: l.Kind,
		Start: start.In(loc),
	}
}

// resolve sets the library subject of each view (ADR-0024): its id keys the thread, its name is shown.
func (s *Screen) resolve(ctx context.Context, views []PostView) error {
	var names []string
	for _, v := range views {
		if v.Lesson.Subject != "" && !slices.Contains(names, v.Lesson.Subject) {
			names = append(names, v.Lesson.Subject)
		}
	}
	if len(names) == 0 {
		return nil
	}
	lessons := make([]kb.Lesson, len(views))
	for i, v := range views {
		lessons[i] = kb.Lesson{Subject: v.Lesson.Subject}
	}
	matches, err := s.subjects.ResolveSubjects(ctx, names)
	if err != nil {
		return err
	}
	resolved := make(map[string]kb.LibrarySubject, len(matches))
	for name, m := range matches {
		resolved[name] = kb.LibrarySubject{ID: m.ID, Name: m.Name}
	}
	for i, l := range kb.WithLibrary(lessons, resolved) {
		if l.Library.ID != "" {
			views[i].Lesson.SubjectID, views[i].Lesson.SubjectName = l.Library.ID, l.Library.Name
		}
	}
	return nil
}

// lessonsOf returns the lessons of the day starting at day, each with the post of its recording if one is shown.
func (s *Screen) lessonsOf(ctx context.Context, day time.Time, views []PostView) ([]LessonRow, error) {
	list, err := s.lessons.ListStartingBetween(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	out := make([]LessonRow, 0, len(list))
	for _, l := range list {
		c := l.Content()
		row := LessonRow{
			Lesson: l.ID(), Title: c.Title, Subject: c.Subject, Kind: c.Kind.String(), Teacher: c.Teacher,
			Start: c.Start.In(day.Location()), End: c.End.In(day.Location()),
		}
		for i := range views { // newest first: the latest recording of the lesson wins
			if views[i].Lesson.Lesson == l.ID() {
				v := views[i]
				row.Post = &v
				break
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// waiting returns the proposals that wait for the student's «Да». An assignment that is gone leaves its labels empty.
func (s *Screen) waiting(ctx context.Context, now time.Time) ([]WaitingItem, error) {
	list, err := s.proposals.ListByStatus(ctx, proposal.StatusValidated, MaxWaiting)
	if err != nil {
		return nil, err
	}
	out := make([]WaitingItem, 0, len(list))
	for _, p := range list {
		item := WaitingItem{
			Proposal: p.ID(), Kind: p.Kind(), Assignment: p.AssignmentID(), Due: local(p.Due(), now),
			Recipient: p.Recipient().Address(), CreatedAt: local(p.CreatedAt(), now),
		}
		title, subject, err := s.assignments.Label(ctx, p.AssignmentID())
		switch {
		case errors.Is(err, shared.ErrNotFound):
			s.log.Info(opLabel, slog.String("proposal", p.ID().String()))
		case err != nil:
			return nil, err
		default:
			item.Title, item.Subject = title, subject
		}
		out = append(out, item)
	}
	return out, nil
}
