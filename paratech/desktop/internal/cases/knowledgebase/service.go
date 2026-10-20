package knowledgebase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// Operation names of the log.
const (
	opUnreadable  = "knowledge base: recordings not readable"
	opLesson      = "knowledge base: lesson not read"
	opTranscript  = "knowledge base: transcript not searched"
	opSummary     = "knowledge base: summary not searched"
	opLectureText = "knowledge base: lecture text not searched"
	opCorrectBusy = "knowledge base: recording changed while corrected"
)

// correctAttempts bounds the saves of one correction that lose against another save of the recording.
const correctAttempts = 3

// Service is the knowledge base (TASK-091). Everything is local: no port talks to the network.
type Service struct {
	lessons     LessonStore
	recordings  RecordingRepository
	schedule    LessonReader
	transcripts TranscriptReader
	artifacts   ArtifactReader
	summaries   SummaryReader
	lectures    LectureTextReader
	writer      TextWriter
	clock       Clock
	log         *slog.Logger
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Lessons == nil || d.Recordings == nil || d.Schedule == nil || d.Transcripts == nil || d.Artifacts == nil ||
		d.Summaries == nil || d.LectureTexts == nil || d.Writer == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		lessons: d.Lessons, recordings: d.Recordings, schedule: d.Schedule, transcripts: d.Transcripts,
		artifacts: d.Artifacts, summaries: d.Summaries, lectures: d.LectureTexts, writer: d.Writer, clock: d.Clock, log: log,
	}, nil
}

// Subjects lists the subjects that have a lesson, with how many each has; the group kb.NoSubjectName comes last.
func (s *Service) Subjects(ctx context.Context) ([]kb.Subject, error) {
	all, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	return kb.Subjects(all), nil
}

// Lessons lists the lessons of the subject with key (kb.Subject.Key; "" for kb.NoSubjectName), newest first. A key
// without lessons gives an empty list.
func (s *Service) Lessons(ctx context.Context, key string) ([]kb.Lesson, error) {
	all, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	return kb.LessonsOf(all, key), nil
}

// Lesson returns one lesson of the knowledge base; a recording that is not one is ErrRecordingNotFound.
func (s *Service) Lesson(ctx context.Context, id shared.RecordingID) (kb.Lesson, error) {
	rec, err := s.recording(ctx, id)
	if err != nil {
		return kb.Lesson{}, err
	}
	if !rec.InKnowledgeBase() {
		return kb.Lesson{}, ErrRecordingNotFound
	}
	return s.lessonOf(ctx, rec)
}

// SetLessonInfo stores the subject and kind the user set for a recording (kind "" or a code of
// values.LessonKindCodes); empty ones clear the correction, so the recorded lesson names it again. It returns the
// lesson as the knowledge base now shows it.
func (s *Service) SetLessonInfo(ctx context.Context, id shared.RecordingID, subject, kind string) (kb.Lesson, error) {
	c, err := values.NewLessonCorrection(subject, kind)
	if err != nil {
		return kb.Lesson{}, ErrInvalidLessonInfo
	}
	for attempt := 1; ; attempt++ {
		rec, err := s.recording(ctx, id)
		if err != nil {
			return kb.Lesson{}, err
		}
		changed, err := rec.CorrectLesson(s.clock.Now(), c)
		switch {
		case errors.Is(err, recording.ErrDeleting):
			return kb.Lesson{}, ErrRecordingNotFound
		case errors.Is(err, recording.ErrInvalidTransition):
			return kb.Lesson{}, ErrNotFinished
		case err != nil:
			return kb.Lesson{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		case !rec.InKnowledgeBase():
			// After the refusals above, so a running recording stays ErrNotFinished; rec is not saved.
			return kb.Lesson{}, ErrRecordingNotFound
		}
		if changed {
			err = s.recordings.Save(ctx, rec)
		}
		switch {
		case errors.Is(err, shared.ErrVersionConflict) && attempt < correctAttempts:
			continue
		case errors.Is(err, shared.ErrVersionConflict):
			s.log.Warn(opCorrectBusy, slog.String("recording", string(id)))
			return kb.Lesson{}, ErrBusy
		case err != nil:
			return kb.Lesson{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return s.lessonOf(ctx, rec)
	}
}

// Search finds query in the titles, summaries, lecture texts and transcripts of every lesson, newest lessons first,
// within the caps of kb.Search. A transcript file, summary or lecture text that cannot be read is left out of the
// search, not failed.
func (s *Service) Search(ctx context.Context, query string) (SearchResult, error) {
	search, err := kb.NewSearch(query)
	if err != nil {
		return SearchResult{}, ErrInvalidQuery
	}
	all, err := s.all(ctx)
	if err != nil {
		return SearchResult{}, err
	}

	byID := make(map[shared.RecordingID]kb.Lesson, len(all))
	for _, l := range kb.Newest(all) {
		if search.Full() {
			break
		}
		if err := ctx.Err(); err != nil {
			return SearchResult{}, err
		}
		doc, err := s.document(ctx, l)
		if err != nil {
			return SearchResult{}, err
		}
		search.Scan(doc)
		byID[l.Recording] = l
	}

	out := SearchResult{Hits: []SearchHit{}, Truncated: search.Truncated()}
	for _, h := range search.Hits() {
		out.Hits = append(out.Hits, SearchHit{Hit: h, Lesson: byID[h.Recording]})
	}
	return out, nil
}

// SubjectFileName returns the file name the save dialog proposes for the subject with key: the subject, safe for
// Windows, with ".md". A subject without lessons is ErrSubjectNotFound.
func (s *Service) SubjectFileName(ctx context.Context, key string) (string, error) {
	sub, _, err := s.subject(ctx, key)
	if err != nil {
		return "", err
	}
	return summaryexport.SubjectFileName(sub.Name), nil
}

// ExportSubject writes the whole subject with key to path, chosen by the user in the save dialog, as one Markdown
// file: its lessons oldest first, each with its summary and lecture text (summaryexport.RenderSubject). The file is
// written atomically; on a failure nothing remains under that name.
func (s *Service) ExportSubject(ctx context.Context, key, path string) error {
	if path == "" {
		return ErrInvalidRequest
	}
	sub, lessons, err := s.subject(ctx, key)
	if err != nil {
		return err
	}

	loc := s.clock.Now().Location()
	doc := summaryexport.SubjectDocument{Subject: sub.Name}
	for _, l := range kb.Oldest(lessons) {
		sl := summaryexport.SubjectLesson{Title: l.Title, Kind: l.Kind, Start: l.StartedAt.In(loc), Duration: l.Duration}
		if c, ok, err := s.summary(ctx, l.Ready); err != nil {
			return err
		} else if ok {
			sl.Summary = c
		}
		if sl.Blocks, err = s.lectureText(ctx, l.Ready); err != nil {
			return err
		}
		doc.Lessons = append(doc.Lessons, sl)
	}

	if err := s.writer.WriteFile(ctx, path, []byte(summaryexport.RenderSubject(doc))); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// subject returns the subject with key and its lessons; none is ErrSubjectNotFound.
func (s *Service) subject(ctx context.Context, key string) (kb.Subject, []kb.Lesson, error) {
	all, err := s.all(ctx)
	if err != nil {
		return kb.Subject{}, nil, err
	}
	lessons := kb.LessonsOf(all, key)
	subjects := kb.Subjects(lessons)
	if len(subjects) != 1 {
		return kb.Subject{}, nil, ErrSubjectNotFound
	}
	return subjects[0], lessons, nil
}

// all loads every lesson of the knowledge base. Recordings that cannot be read are left out and logged.
func (s *Service) all(ctx context.Context) ([]kb.Lesson, error) {
	recs, unreadable, err := s.lessons.ListLessons(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(unreadable) > 0 {
		s.log.Warn(opUnreadable, slog.Int("count", len(unreadable)), slog.String("first", string(unreadable[0])))
	}
	ready, err := s.lessons.ListReadiness(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	out := make([]kb.Lesson, 0, len(recs))
	for _, rec := range recs {
		if rec.InKnowledgeBase() {
			out = append(out, kb.NewLesson(s.facts(ctx, rec, ready[rec.ID()])))
		}
	}
	return out, nil
}

// lessonOf is the lesson of one recording.
func (s *Service) lessonOf(ctx context.Context, rec *recording.Recording) (kb.Lesson, error) {
	ready, err := s.lessons.ListReadiness(ctx)
	if err != nil {
		return kb.Lesson{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return kb.NewLesson(s.facts(ctx, rec, ready[rec.ID()])), nil
}

// facts gathers what is known of the lesson of rec. The live lesson is read only when the snapshot lacks the title;
// a failed read only costs the live names.
func (s *Service) facts(ctx context.Context, rec *recording.Recording, ready kb.Readiness) kb.Facts {
	d, _ := rec.Duration()
	f := kb.Facts{Recording: rec.ID(), StartedAt: rec.StartedAt(), Duration: d, Ready: ready}
	snap := rec.LessonSnapshot()
	if !snap.IsZero() {
		f.Snapshot = &kb.Source{Title: snap.Title(), Subject: snap.Subject(), Kind: snap.Kind()}
	}
	if c := rec.LessonCorrection(); !c.IsZero() {
		f.Correction = kb.Source{Subject: c.Subject(), Kind: c.Kind()}
	}
	if rec.LessonID() == "" || snap.Title() != "" {
		return f
	}

	l, err := s.schedule.Get(ctx, rec.LessonID())
	switch {
	case errors.Is(err, shared.ErrNotFound):
	case err != nil:
		s.log.Warn(opLesson, slog.String("recording", string(rec.ID())), slog.Any("cause", err))
	default:
		c := l.Content()
		f.Live = &kb.Source{Title: c.Title, Subject: c.Subject, Kind: c.Kind.String()}
	}
	return f
}

// document gathers the texts of a lesson for the search.
func (s *Service) document(ctx context.Context, l kb.Lesson) (kb.Document, error) {
	doc := kb.Document{Recording: l.Recording, Title: l.Title}
	c, ok, err := s.summary(ctx, l.Ready)
	if err = s.skippable(ctx, err, opSummary, l.Recording); err != nil {
		return kb.Document{}, err
	}
	if ok {
		doc.Summary = append([]string{c.Text()}, c.KeyPoints()...)
	}

	blocks, err := s.lectureText(ctx, l.Ready)
	if err = s.skippable(ctx, err, opLectureText, l.Recording); err != nil {
		return kb.Document{}, err
	}
	for _, b := range blocks {
		doc.Blocks = append(doc.Blocks, kb.Block{Heading: b.Heading(), StartMs: b.StartMs(), Paragraphs: b.Paragraphs()})
	}

	segs, err := s.segments(ctx, l)
	if err != nil {
		return kb.Document{}, err
	}
	for _, seg := range segs {
		doc.Segments = append(doc.Segments, kb.Segment{StartMs: seg.StartMs, Text: seg.Text})
	}
	return doc, nil
}

// skippable logs a read error of one section and returns nil, so one corrupt row does not break the search; a
// cancelled or timed out read is returned.
func (s *Service) skippable(ctx context.Context, err error, op string, rec shared.RecordingID) error {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	s.log.Warn(op, slog.String("recording", string(rec)), slog.Any("cause", err))
	return nil
}

// summary reads the summary of the newest transcript; ok is false when there is none.
func (s *Service) summary(ctx context.Context, r kb.Readiness) (summary.Content, bool, error) {
	if !r.Summary || !r.Transcribed() {
		return summary.Content{}, false, nil
	}
	sum, err := s.summaries.Get(ctx, summary.IDOf(r.Transcript))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return summary.Content{}, false, nil
	case err != nil:
		return summary.Content{}, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return sum.Content(), true, nil
}

// lectureText reads the finished lecture text of the newest transcript; nil when there is none.
func (s *Service) lectureText(ctx context.Context, r kb.Readiness) ([]lecturetext.Block, error) {
	if !r.LectureText || !r.Transcribed() {
		return nil, nil
	}
	l, err := s.lectures.Get(ctx, lecturetext.IDOf(r.Transcript))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return l.Blocks(), nil
}

// segments reads the segments of the newest transcript. A transcript gone meanwhile has none; a document that cannot
// be read is logged and searched as empty, so one broken file does not break the search.
func (s *Service) segments(ctx context.Context, l kb.Lesson) ([]transcript.Segment, error) {
	if !l.Ready.Transcribed() {
		return nil, nil
	}
	t, err := s.transcripts.Get(ctx, l.Ready.Transcript)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	rc, err := s.artifacts.Open(ctx, t.Text())
	if err != nil {
		s.log.Warn(opTranscript, slog.String("recording", string(l.Recording)), slog.Any("cause", err))
		return nil, nil
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, transcript.MaxDocumentBytes+1))
	if err != nil {
		s.log.Warn(opTranscript, slog.String("recording", string(l.Recording)), slog.Any("cause", err))
		return nil, nil
	}
	doc, err := transcript.ParseDocument(data)
	if err != nil {
		s.log.Warn(opTranscript, slog.String("recording", string(l.Recording)), slog.Any("cause", err))
		return nil, nil
	}
	return doc.Segments(), nil
}

// recording loads a recording; a missing one is ErrRecordingNotFound.
func (s *Service) recording(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if err := id.Validate(); err != nil {
		return nil, ErrInvalidRequest
	}
	rec, err := s.recordings.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrRecordingNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return rec, nil
}
