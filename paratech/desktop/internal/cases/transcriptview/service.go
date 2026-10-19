package transcriptview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
)

// Titles and file names (Russian UI).
const (
	adHocTitle      = "Запись %s"      // the start date and time
	adHocFileName   = "Расшифровка %s" // the start date and time
	lessonFileName  = "%s %s"          // the lesson title and the date
	titleTimeLayout = "02.01.2006, 15:04"
	fileDateLayout  = "2006-01-02"
	fileTimeLayout  = "2006-01-02 15-04" // no ":" in Windows file names
)

// sttJobType is the job type of the recognition (the jobs context keeps types open, ARCH-11).
const sttJobType = job.Type(values.STTJobType)

// Service is the transcript viewer (TASK-079): it shows the transcript of a recording, the stored one or the parts
// recognised so far while the recognition runs, renders it for the clipboard, saves it to a file the user chose and
// opens the recording for playback. Everything is local: no port talks to the network, so the transcript is there
// when the summary or the homework wait for the internet or the Hub failed.
type Service struct {
	recordings  RecordingReader
	lessons     LessonReader
	transcripts TranscriptFinder
	jobs        JobFinder
	live        LiveParts
	artifacts   ArtifactStore
	writer      TextWriter
	clock       Clock
	log         *slog.Logger

	// docs keeps the last parsed documents, so paging a long transcript does not parse it again for every page.
	mu   sync.Mutex
	docs []cachedDoc
}

type cachedDoc struct {
	id      shared.TranscriptID
	version int64
	segs    []transcript.Segment
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Recordings == nil || d.Lessons == nil || d.Transcripts == nil || d.Jobs == nil || d.Live == nil ||
		d.Artifacts == nil || d.Writer == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		recordings: d.Recordings, lessons: d.Lessons, transcripts: d.Transcripts, jobs: d.Jobs, live: d.Live,
		artifacts: d.Artifacts, writer: d.Writer, clock: d.Clock, log: log,
	}, nil
}

// Status returns the head of the viewer: title, state of the recognition and how many segments there are.
func (s *Service) Status(ctx context.Context, id shared.RecordingID) (Status, error) {
	rec, err := s.recording(ctx, id)
	if err != nil {
		return Status{}, err
	}
	_, playable := rec.TranscribableAudio()
	st := Status{Recording: id, Title: s.title(ctx, rec), StartedAt: rec.StartedAt(), Playable: playable}

	segs, final, err := s.finalSegments(ctx, id)
	switch {
	case err != nil:
		return Status{}, err
	case final:
		st.State, st.Final, st.Segments = StateDone, true, len(segs)
		return st, nil
	}

	st.State, st.Percent, err = s.recognition(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if st.Segments, err = s.live.CountParts(ctx, id); err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return st, nil
}

// Segments returns at most limit segments from index from (limit is capped at MaxPage): of the stored transcript if
// there is one, otherwise the live parts.
func (s *Service) Segments(ctx context.Context, id shared.RecordingID, from, limit int) (Page, error) {
	if err := id.Validate(); err != nil || from < 0 || limit < 1 {
		return Page{}, ErrInvalidRequest
	}
	limit = min(limit, MaxPage)

	segs, final, err := s.finalSegments(ctx, id)
	if err != nil {
		return Page{}, err
	}
	if final {
		start := min(from, len(segs))
		end := min(start+limit, len(segs))
		return Page{From: from, Total: len(segs), Final: true, Segments: append([]transcript.Segment(nil), segs[start:end]...)}, nil
	}

	total, err := s.live.CountParts(ctx, id)
	if err != nil {
		return Page{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	parts, err := s.live.ListParts(ctx, id, from, limit)
	if err != nil {
		return Page{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	out := make([]transcript.Segment, len(parts))
	for i, p := range parts {
		out[i] = p.Segment()
	}
	return Page{From: from, Total: total, Segments: out}, nil
}

// Text renders the whole transcript as plain text for the clipboard, with or without timestamps.
func (s *Service) Text(ctx context.Context, id shared.RecordingID, withTimestamps bool) (string, error) {
	segs, err := s.all(ctx, id)
	if err != nil {
		return "", err
	}
	text, err := transcript.Render("", segs, transcript.FormatText, withTimestamps)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return text, nil
}

// SuggestedName returns the file name the save dialog proposes: the lesson title and date, or "Расшифровка" with the
// date and time of an ad-hoc recording, safe for Windows, with the extension of f.
func (s *Service) SuggestedName(ctx context.Context, id shared.RecordingID, f transcript.ExportFormat) (string, error) {
	if err := f.Validate(); err != nil {
		return "", ErrInvalidRequest
	}
	rec, err := s.recording(ctx, id)
	if err != nil {
		return "", err
	}
	at := rec.StartedAt().In(s.clock.Now().Location())
	if title := s.lessonTitle(ctx, rec); title != "" {
		return transcript.ExportFileName(fmt.Sprintf(lessonFileName, title, at.Format(fileDateLayout)), f), nil
	}
	return transcript.ExportFileName(fmt.Sprintf(adHocFileName, at.Format(fileTimeLayout)), f), nil
}

// Export writes the transcript to path, chosen by the user in the save dialog, in format f with the title as its
// first line. The file is written atomically; on a failure nothing remains under that name.
func (s *Service) Export(ctx context.Context, id shared.RecordingID, f transcript.ExportFormat, withTimestamps bool, path string) error {
	if err := f.Validate(); err != nil || path == "" {
		return ErrInvalidRequest
	}
	rec, err := s.recording(ctx, id)
	if err != nil {
		return err
	}
	segs, err := s.all(ctx, id)
	if err != nil {
		return err
	}
	text, err := transcript.Render(s.title(ctx, rec), segs, f, withTimestamps)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := s.writer.WriteFile(ctx, path, []byte(text)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// OpenAudio opens the final track of a completed recording for playback; the caller closes the reader.
func (s *Service) OpenAudio(ctx context.Context, id shared.RecordingID) (Audio, error) {
	rec, err := s.recording(ctx, id)
	if err != nil {
		return Audio{}, err
	}
	ref, ok := rec.TranscribableAudio()
	if !ok {
		return Audio{}, ErrNoAudio
	}
	r, size, err := s.artifacts.OpenSeekable(ctx, ref)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return Audio{}, fmt.Errorf("%w: %w", ErrNoAudio, err)
	case err != nil:
		return Audio{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return Audio{Reader: r, Size: size}, nil
}

// recording loads the recording; a missing one is ErrRecordingNotFound.
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

// title is the lesson title or "Запись <date>, <time>" in the clock's location.
func (s *Service) title(ctx context.Context, rec *recording.Recording) string {
	if t := s.lessonTitle(ctx, rec); t != "" {
		return t
	}
	return fmt.Sprintf(adHocTitle, rec.StartedAt().In(s.clock.Now().Location()).Format(titleTimeLayout))
}

// lessonTitle is the title the recording kept at its start (TASK-088), else the title of its lesson in the schedule;
// "" for an ad-hoc recording or a lesson that is gone. A failed read only costs the title.
func (s *Service) lessonTitle(ctx context.Context, rec *recording.Recording) string {
	if t := rec.LessonSnapshot().Title(); t != "" {
		return t
	}
	if rec.LessonID() == "" {
		return ""
	}
	l, err := s.lessons.Get(ctx, rec.LessonID())
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			s.log.Warn("transcript viewer: lesson not read", slog.String("recording", string(rec.ID())), slog.Any("cause", err))
		}
		return ""
	}
	return l.Content().Title
}

// recognition derives the state of the recognition from the newest STT job of the recording.
func (s *Service) recognition(ctx context.Context, id shared.RecordingID) (State, int, error) {
	j, err := s.jobs.LatestByKeyPrefix(ctx, sttJobType, values.STTJobKeyPrefix(id))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return StateNone, 0, nil
	case err != nil:
		return "", 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	switch j.Status() {
	case job.StatusRunning:
		return StateRecognising, j.Progress().Percent(), nil
	case job.StatusFailed, job.StatusCancelled:
		return StateFailed, 0, nil
	case job.StatusCompleted:
		// Completed without a transcript: it was removed since, nothing is being recognised.
		return StateNone, 0, nil
	}
	return StateWaiting, j.Progress().Percent(), nil
}

// all returns every segment: of the stored transcript, otherwise the live parts; none at all is ErrNoTranscript.
func (s *Service) all(ctx context.Context, id shared.RecordingID) ([]transcript.Segment, error) {
	segs, final, err := s.finalSegments(ctx, id)
	if err != nil || final {
		return segs, err
	}

	var out []transcript.Segment
	for from := 0; from < transcript.MaxSegments; from += MaxPage {
		parts, err := s.live.ListParts(ctx, id, from, MaxPage)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		for _, p := range parts {
			out = append(out, p.Segment())
		}
		if len(parts) < MaxPage {
			break
		}
	}
	if len(out) == 0 {
		return nil, ErrNoTranscript
	}
	return out, nil
}

// finalSegments returns the segments of the stored transcript and true, or false when there is none yet.
func (s *Service) finalSegments(ctx context.Context, id shared.RecordingID) ([]transcript.Segment, bool, error) {
	t, err := s.transcripts.LatestByRecording(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if segs, ok := s.cached(t); ok {
		return segs, true, nil
	}
	segs, err := s.readDocument(ctx, t.Text())
	if err != nil {
		return nil, false, err
	}
	s.remember(t, segs)
	return segs, true, nil
}

func (s *Service) readDocument(ctx context.Context, ref shared.ArtifactRef) ([]transcript.Segment, error) {
	rc, err := s.artifacts.Open(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, transcript.MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	doc, err := transcript.ParseDocument(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return doc.Segments(), nil
}

// cached returns the parsed segments of t if they are kept; a transcript is keyed by id and version, so a future
// edit (a new version) is read again.
func (s *Service) cached(t *transcript.Transcript) ([]transcript.Segment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.docs {
		if d.id == t.ID() && d.version == t.Version() {
			// Most recently used last.
			s.docs = append(append(s.docs[:i:i], s.docs[i+1:]...), d)
			return d.segs, true
		}
	}
	return nil, false
}

func (s *Service) remember(t *transcript.Transcript, segs []transcript.Segment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.docs {
		if d.id == t.ID() {
			s.docs = append(s.docs[:i:i], s.docs[i+1:]...)
			break
		}
	}
	if len(s.docs) >= cachedDocuments {
		s.docs = s.docs[1:]
	}
	s.docs = append(s.docs, cachedDoc{id: t.ID(), version: t.Version(), segs: segs})
}
