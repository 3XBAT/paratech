package knowledgebase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)

// msk is the location of the clock: exported dates use it.
var msk = time.FixedZone("MSK", 3*3600)

var errDisk = errors.New(`open C:\Users\student\db: locked`)

// memRecordings is the recordings table: the lesson store lists it in id order, as is (the use case filters).
type memRecordings struct {
	rows       map[shared.RecordingID]*recording.Recording
	ready      map[shared.RecordingID]kb.Readiness
	unreadable []shared.RecordingID
	listErr    error
	saveErr    error
	conflicts  int // the next saves that lose against another save
	saves      int
}

// Get returns a copy, as a repository does: a change that is not saved stays out of the table.
func (m *memRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	r, ok := m.rows[id]
	if !ok {
		return nil, fmt.Errorf("%w: recording %s", shared.ErrNotFound, id)
	}
	d, _ := r.Duration()
	return recording.Restore(recording.State{
		Version: max(r.Version(), 1), ID: r.ID(), LessonID: r.LessonID(), Lesson: r.LessonSnapshot(),
		Correction: r.LessonCorrection(), Status: r.Status(), MaxDuration: r.MaxDuration(), Duration: d, Chunks: r.Chunks(),
		ChunksDir: r.ChunksDir(), Audio: r.Audio(), Silent: r.Silent(), Problem: r.Problem(), StartedAt: r.StartedAt(),
		StoppedAt: r.StoppedAt(), HeartbeatAt: r.HeartbeatAt(), CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		DeletingAt: r.DeletingAt(), DeletingAssignments: r.DeletingAssignments(),
	})
}

func (m *memRecordings) Save(_ context.Context, r *recording.Recording) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.conflicts > 0 {
		m.conflicts--
		return fmt.Errorf("%w: recording %s", shared.ErrVersionConflict, r.ID())
	}
	m.saves++
	m.rows[r.ID()] = r
	return nil
}

func (m *memRecordings) ListLessons(context.Context) ([]*recording.Recording, []shared.RecordingID, error) {
	if m.listErr != nil {
		return nil, nil, m.listErr
	}
	ids := make([]shared.RecordingID, 0, len(m.rows))
	for id := range m.rows {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]*recording.Recording, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.rows[id])
	}
	return out, m.unreadable, nil
}

func (m *memRecordings) ListReadiness(context.Context) (map[shared.RecordingID]kb.Readiness, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.ready, nil
}

type memSchedule map[shared.LessonID]*lesson.Lesson

func (m memSchedule) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if l, ok := m[id]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("%w: lesson %s", shared.ErrNotFound, id)
}

type memTranscripts map[shared.TranscriptID]*transcript.Transcript

func (m memTranscripts) Get(_ context.Context, id shared.TranscriptID) (*transcript.Transcript, error) {
	if t, ok := m[id]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("%w: transcript %s", shared.ErrNotFound, id)
}

type memSummaries struct {
	rows map[summary.ID]*summary.Summary
	err  error
}

func (m *memSummaries) Get(_ context.Context, id summary.ID) (*summary.Summary, error) {
	if m.err != nil {
		return nil, m.err
	}
	if s, ok := m.rows[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("%w: summary %s", shared.ErrNotFound, id)
}

type memLectures struct {
	rows map[lecturetext.ID]*lecturetext.LectureText
	err  error
}

func (m *memLectures) Get(_ context.Context, id lecturetext.ID) (*lecturetext.LectureText, error) {
	if m.err != nil {
		return nil, m.err
	}
	if l, ok := m.rows[id]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("%w: lecture text %s", shared.ErrNotFound, id)
}

type fakeWriter struct {
	path    string
	content string
	err     error
}

func (f *fakeWriter) WriteFile(_ context.Context, path string, content []byte) error {
	if f.err != nil {
		return f.err
	}
	f.path, f.content = path, string(content)
	return nil
}

type env struct {
	svc         *Service
	recs        *memRecordings
	schedule    memSchedule
	transcripts memTranscripts
	artifacts   *testenv.MemArtifacts
	summaries   *memSummaries
	lectures    *memLectures
	writer      *fakeWriter
	logs        *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		recs:        &memRecordings{rows: map[shared.RecordingID]*recording.Recording{}, ready: map[shared.RecordingID]kb.Readiness{}},
		schedule:    memSchedule{},
		transcripts: memTranscripts{},
		artifacts:   testenv.NewMemArtifacts(),
		summaries:   &memSummaries{rows: map[summary.ID]*summary.Summary{}},
		lectures:    &memLectures{rows: map[lecturetext.ID]*lecturetext.LectureText{}},
		writer:      &fakeWriter{},
		logs:        &bytes.Buffer{},
	}
	svc, err := New(Deps{
		Lessons: e.recs, Recordings: e.recs, Schedule: e.schedule, Transcripts: e.transcripts, Artifacts: e.artifacts,
		Summaries: e.summaries, LectureTexts: e.lectures, Writer: e.writer, Clock: testenv.NewFakeClock(t0.In(msk)),
		Log: slog.New(slog.NewTextHandler(e.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// lessonSpec is a recording to add: of a lesson with a snapshot (subject set) or of a live lesson only (live).
type lessonSpec struct {
	id      shared.RecordingID
	start   time.Duration // after t0
	title   string
	subject string
	kind    string
	live    bool // the recording kept no snapshot; the schedule still has the lesson
}

// add stores a completed recording of 90 minutes.
func (e *env) add(t *testing.T, s lessonSpec) *recording.Recording {
	t.Helper()
	at := t0.Add(s.start)
	spec := recording.Spec{ID: s.id, MaxDuration: 3 * time.Hour}
	if s.title != "" || s.subject != "" {
		spec.LessonID = shared.LessonID("lesson-" + string(s.id))
		kind, err := lesson.KindOf(s.kind)
		if err != nil {
			kind = lesson.KindUnknown
		}
		l, err := lesson.New(lesson.Spec{ID: spec.LessonID, Content: lesson.Content{
			Title: s.title, Subject: s.subject, Kind: kind, Start: at, End: at.Add(time.Hour), TimeZone: "Europe/Moscow",
		}}, at)
		if err != nil {
			t.Fatal(err)
		}
		e.schedule[spec.LessonID] = l
		if !s.live {
			spec.Lesson = values.NewLessonSnapshot(values.LessonSnapshotSpec{Title: s.title, Subject: s.subject, Kind: s.kind})
		}
	}
	r, err := recording.New(spec, at)
	if err != nil {
		t.Fatal(err)
	}
	end := at.Add(90 * time.Minute)
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(r.Begin(at), r.Stop(end), r.Finalize(end), r.Complete(end, track, 90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e.recs.rows[s.id] = r
	return r
}

// addTranscript stores the transcript "tr-<rec>" with segs (one per second) and marks it the newest of rec.
func (e *env) addTranscript(t *testing.T, rec shared.RecordingID, segs ...string) shared.TranscriptID {
	t.Helper()
	id := shared.TranscriptID("tr-" + string(rec))
	ref, err := transcript.DocumentRef(id)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transcript.Restore(transcript.State{Spec: transcript.Spec{
		ID: id, RecordingID: rec, ModelID: "m1", ModelName: "base", ModelVersion: "1", Text: ref,
	}, Version: 1, CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	e.transcripts[id] = tr

	list := make([]transcript.Segment, len(segs))
	for i, s := range segs {
		list[i] = transcript.Segment{StartMs: int64(i) * 1000, EndMs: int64(i)*1000 + 900, Text: s}
	}
	doc, err := transcript.NewDocument("ru", list)
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.Write(context.Background(), ref, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	r := e.recs.ready[rec]
	r.Transcript = id
	e.recs.ready[rec] = r
	return id
}

func (e *env) addSummary(t *testing.T, rec shared.RecordingID, text string, points ...string) {
	t.Helper()
	r := e.recs.ready[rec]
	c, err := summary.NewContent(text, points)
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: r.Transcript, PromptVersion: 1, Content: c}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.summaries.rows[s.ID()] = s
	r.Summary = true
	e.recs.ready[rec] = r
}

func (e *env) addLectureText(t *testing.T, rec shared.RecordingID, blocks ...lecturetext.Block) {
	t.Helper()
	r := e.recs.ready[rec]
	l, err := lecturetext.New(lecturetext.Spec{Transcript: r.Transcript}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.AddPart("run", 1, 0, blocks, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.Publish("run", blocks, 1, t0); err != nil {
		t.Fatal(err)
	}
	e.lectures.rows[l.ID()] = l
	r.LectureText = true
	e.recs.ready[rec] = r
}

func block(t *testing.T, heading string, startMs int64, paragraphs ...string) lecturetext.Block {
	t.Helper()
	b, err := lecturetext.NewBlock(heading, startMs, paragraphs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
