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
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNewScreen = "todayfeed: new screen"
	opToday     = "todayfeed: today"
	opThread    = "todayfeed: subject thread"
	opLesson    = "todayfeed: live lesson of a post unread"
	opLabel     = "todayfeed: assignment of a proposal unread"
	opSubjects  = "todayfeed: library subjects of the day unread"
	opCallLinks = "todayfeed: call links of the day unread"
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
	callLinks   SubjectCallLinks
	clock       Clock
	log         *slog.Logger
}

// NewScreen checks the ports.
func NewScreen(d ScreenDeps) (*Screen, error) {
	if d.Posts == nil || d.Recordings == nil || d.Lessons == nil || d.Homework == nil || d.Assignments == nil ||
		d.Proposals == nil || d.Subjects == nil || d.CallLinks == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every port is required", opNewScreen, ErrInvalidConfig)
	}
	s := &Screen{
		posts: d.Posts, recordings: d.Recordings, lessons: d.Lessons, homework: d.Homework, assignments: d.Assignments,
		proposals: d.Proposals, subjects: d.Subjects, callLinks: d.CallLinks, clock: d.Clock, log: d.Log,
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
	list, err := s.lessons.ListStartingBetween(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		return fail(err)
	}
	// The library only adds the subjects and the call links: when it cannot be read the screen still shows the day
	// («Пары дня», «Записать»), with no subject and no link control.
	names := subjectNames(views)
	for _, l := range list {
		names = appendName(names, l.Content().Subject)
	}
	found, err := s.lookup(ctx, names)
	if err != nil {
		s.log.Warn(opSubjects, slog.String("result", "subjects unread"))
		found = nil
	}
	withSubjects(views, found)

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

	out.Lessons = lessonsOf(list, day, views)
	s.lessonSubjects(ctx, out.Lessons, found)
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
	// The thread is keyed by the subject: without the library it cannot be told, so here a failed read fails.
	found, err := s.lookup(ctx, subjectNames(views))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opThread, err)
	}
	withSubjects(views, found)
	out := []PostView{}
	for _, v := range views {
		if v.Lesson.SubjectID == subject {
			out = append(out, v)
		}
	}
	return out, nil
}

// recent returns the views of the posts changed since since that are worth showing, newest shown first, without their
// library subjects (withSubjects sets them).
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

// subjectNames returns the distinct subject strings of the views.
func subjectNames(views []PostView) []string {
	var names []string
	for _, v := range views {
		names = appendName(names, v.Lesson.Subject)
	}
	return names
}

// appendName adds a non-empty name that is not in names yet.
func appendName(names []string, name string) []string {
	if name == "" || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

// lookup tells how each name resolves to the subjects of the library, in one read.
func (s *Screen) lookup(ctx context.Context, names []string) (map[string]subjectmatch.Resolution, error) {
	if len(names) == 0 {
		return map[string]subjectmatch.Resolution{}, nil
	}
	return s.subjects.LookupSubjects(ctx, names)
}

// withSubjects sets the library subject of each view (ADR-0024): its id keys the thread, its name is shown. With no
// lookup (found nil) the views keep the string of their lesson.
func withSubjects(views []PostView, found map[string]subjectmatch.Resolution) {
	if len(views) == 0 || len(found) == 0 {
		return
	}
	resolved := make(map[string]kb.LibrarySubject, len(found))
	for name, r := range found {
		if r.Outcome == subjectmatch.Matched {
			resolved[name] = kb.LibrarySubject{ID: r.Match.ID, Name: r.Match.Name}
		}
	}
	lessons := make([]kb.Lesson, len(views))
	for i, v := range views {
		lessons[i] = kb.Lesson{Subject: v.Lesson.Subject}
	}
	for i, l := range kb.WithLibrary(lessons, resolved) {
		if l.Library.ID != "" {
			views[i].Lesson.SubjectID, views[i].Lesson.SubjectName = l.Library.ID, l.Library.Name
		}
	}
}

// lessonsOf returns the lessons of list in the zone of day, each with the post of its recording if one is shown.
func lessonsOf(list []*lesson.Lesson, day time.Time, views []PostView) []LessonRow {
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
	return out
}

// lessonSubjects sets the subject state of each lesson of the day, its library subject and the call link its kind
// opens (ADR-0032, TASK-364, addendum 2026-11-10). With no lookup (found nil) or a failed read of the links, a lesson that names a
// subject is SubjectUnavailable without SubjectID: a «Добавить ссылку» then could overwrite a link it did not see.
func (s *Screen) lessonSubjects(ctx context.Context, rows []LessonRow, found map[string]subjectmatch.Resolution) {
	var ids []shared.SubjectID
	for i, r := range rows {
		rows[i].SubjectState = subjectState(r.Subject, found)
		if rows[i].SubjectState != SubjectKnown {
			continue
		}
		m := found[r.Subject].Match
		rows[i].SubjectID, rows[i].SubjectName = m.ID, m.Name
		if !slices.Contains(ids, m.ID) {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	links, err := s.callLinks.CallLinkSets(ctx, ids)
	if err != nil {
		// The cause is not logged: a storage error may carry row data, and a link a meeting password.
		s.log.Warn(opCallLinks, slog.String("result", "call links unread"))
		for i := range rows {
			if rows[i].SubjectState == SubjectKnown {
				rows[i].SubjectState, rows[i].SubjectID, rows[i].SubjectName = SubjectUnavailable, "", ""
			}
		}
		return
	}
	for i, r := range rows {
		if r.SubjectID != "" {
			rows[i].CallLink, rows[i].CallLinkKind = links[r.SubjectID].Join(r.Kind)
		}
	}
}

// subjectState is the state of the subject string name in the lookup found; nil found is a library not read.
func subjectState(name string, found map[string]subjectmatch.Resolution) SubjectState {
	if name == "" {
		return SubjectNone
	}
	if found == nil {
		return SubjectUnavailable
	}
	r, ok := found[name]
	switch {
	case !ok:
		return SubjectUnavailable // the resolver left it out: not told, so nothing is offered
	case r.Outcome == subjectmatch.Matched && r.Match.ID != "":
		return SubjectKnown
	case r.Outcome == subjectmatch.Ambiguous:
		return SubjectAmbiguous
	case r.Outcome == subjectmatch.NoKey:
		return SubjectNone
	case r.Outcome == subjectmatch.NoMatch:
		return SubjectUnknown
	}
	return SubjectUnavailable
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
