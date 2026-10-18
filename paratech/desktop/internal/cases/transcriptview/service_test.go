package transcriptview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 19, 9, 5, 0, 0, time.UTC)

// msk is the location of the clock: titles and file names use it.
var msk = time.FixedZone("MSK", 3*3600)

type fakeRecordings map[shared.RecordingID]*recording.Recording

func (f fakeRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if r, ok := f[id]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("%w: recording %s", shared.ErrNotFound, id)
}

type fakeLessons map[shared.LessonID]*lesson.Lesson

func (f fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if l, ok := f[id]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("%w: lesson %s", shared.ErrNotFound, id)
}

type fakeTranscripts struct {
	rows map[shared.RecordingID]*transcript.Transcript
	err  error
}

func (f *fakeTranscripts) LatestByRecording(_ context.Context, rec shared.RecordingID) (*transcript.Transcript, error) {
	if f.err != nil {
		return nil, f.err
	}
	if t, ok := f.rows[rec]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("%w: transcript of %s", shared.ErrNotFound, rec)
}

type fakeJobs struct {
	byPrefix map[string]*job.Job
	prefixes []string
}

func (f *fakeJobs) LatestByKeyPrefix(_ context.Context, typ job.Type, prefix string) (*job.Job, error) {
	f.prefixes = append(f.prefixes, string(typ)+" "+prefix)
	if j, ok := f.byPrefix[prefix]; ok {
		return j, nil
	}
	return nil, fmt.Errorf("%w: job", shared.ErrNotFound)
}

type fakeLive struct {
	parts map[shared.RecordingID][]transcript.Part
	err   error
}

func (f *fakeLive) ListParts(_ context.Context, rec shared.RecordingID, offset, limit int) ([]transcript.Part, error) {
	if f.err != nil {
		return nil, f.err
	}
	ps := f.parts[rec]
	start := min(offset, len(ps))
	return append([]transcript.Part(nil), ps[start:min(start+limit, len(ps))]...), nil
}

func (f *fakeLive) CountParts(_ context.Context, rec shared.RecordingID) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return len(f.parts[rec]), nil
}

// fakeArtifacts serves documents and tracks from memory and counts document opens.
type fakeArtifacts struct {
	mu    sync.Mutex
	files map[string][]byte
	opens int
}

type seekCloser struct{ *bytes.Reader }

func (seekCloser) Close() error { return nil }

func (f *fakeArtifacts) Open(_ context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	data, ok := f.files[ref.String()]
	if !ok {
		return nil, fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeArtifacts) OpenSeekable(_ context.Context, ref shared.ArtifactRef) (io.ReadSeekCloser, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[ref.String()]
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	return seekCloser{bytes.NewReader(data)}, int64(len(data)), nil
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
	recordings  fakeRecordings
	lessons     fakeLessons
	transcripts *fakeTranscripts
	jobs        *fakeJobs
	live        *fakeLive
	artifacts   *fakeArtifacts
	writer      *fakeWriter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		recordings: fakeRecordings{}, lessons: fakeLessons{},
		transcripts: &fakeTranscripts{rows: map[shared.RecordingID]*transcript.Transcript{}},
		jobs:        &fakeJobs{byPrefix: map[string]*job.Job{}},
		live:        &fakeLive{parts: map[shared.RecordingID][]transcript.Part{}},
		artifacts:   &fakeArtifacts{files: map[string][]byte{}},
		writer:      &fakeWriter{},
	}
	svc, err := New(Deps{
		Recordings: e.recordings, Lessons: e.lessons, Transcripts: e.transcripts, Jobs: e.jobs, Live: e.live,
		Artifacts: e.artifacts, Writer: e.writer, Clock: testenv.NewFakeClock(t0.In(msk)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// addRecording stores a completed recording with a track, optionally of a lesson titled title.
func (e *env) addRecording(t *testing.T, id shared.RecordingID, title string) *recording.Recording {
	t.Helper()
	var lid shared.LessonID
	if title != "" {
		lid = shared.LessonID("lesson-" + string(id))
		l, err := lesson.New(lesson.Spec{ID: lid, Content: lesson.Content{
			Title: title, Start: t0, End: t0.Add(time.Hour), TimeZone: "Europe/Moscow",
		}}, t0)
		if err != nil {
			t.Fatal(err)
		}
		e.lessons[lid] = l
	}
	r, err := recording.New(recording.Spec{ID: id, LessonID: lid, MaxDuration: 3 * time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	end := t0.Add(time.Hour)
	if err := errors.Join(r.Begin(t0), r.Stop(end), r.Finalize(end)); err != nil {
		t.Fatal(err)
	}
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Complete(end, track, time.Hour); err != nil {
		t.Fatal(err)
	}
	e.recordings[id] = r
	e.artifacts.files[track.String()] = []byte("RIFF-audio-bytes")
	return r
}

// addTranscript stores the transcript of rec with the segments as its document.
func (e *env) addTranscript(t *testing.T, rec shared.RecordingID, segs []transcript.Segment) {
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
	doc, err := transcript.NewDocument("ru", segs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	e.artifacts.files[ref.String()] = data
	e.transcripts.rows[rec] = tr
}

func (e *env) addParts(t *testing.T, rec shared.RecordingID, segs []transcript.Segment) {
	t.Helper()
	for i, s := range segs {
		p, err := transcript.NewPart(i, s)
		if err != nil {
			t.Fatal(err)
		}
		e.live.parts[rec] = append(e.live.parts[rec], p)
	}
}

// addJob stores the STT job of rec in status st with percent progress.
func (e *env) addJob(t *testing.T, rec shared.RecordingID, st job.Status, percent int) {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID("j-" + string(rec)), Type: "stt", Key: "stt:" + string(rec) + ":m1", MaxAttempts: 3}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if st != job.StatusQueued {
		if err := j.Claim(t0); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := job.NewProgress(percent, "")
	switch st {
	case job.StatusRunning:
		err = j.ReportProgress(p, t0)
	case job.StatusFailed:
		err = j.Fail(t0, "stt job: transcription failed")
	case job.StatusCancelled:
		err = j.Cancel(t0)
	case job.StatusCompleted:
		err = j.Complete(t0)
	case job.StatusWaitingForPower:
		err = errors.Join(j.ReportProgress(p, t0), j.WaitFor(job.StatusWaitingForPower, t0))
	}
	if err != nil {
		t.Fatal(err)
	}
	e.jobs.byPrefix["stt:"+string(rec)+":"] = j
}

func segsN(n int) []transcript.Segment {
	out := make([]transcript.Segment, n)
	for i := range out {
		out[i] = transcript.Segment{StartMs: int64(i) * 2000, EndMs: int64(i)*2000 + 1500, Text: fmt.Sprintf("фраза %d", i)}
	}
	return out
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(empty) = %v", err)
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env)
		want  Status
	}{
		{
			name:  "nothing yet",
			setup: func(t *testing.T, e *env) {},
			want:  Status{State: StateNone},
		},
		{
			name:  "queued",
			setup: func(t *testing.T, e *env) { e.addJob(t, "r1", job.StatusQueued, 0) },
			want:  Status{State: StateWaiting},
		},
		{
			name: "recognising with live parts",
			setup: func(t *testing.T, e *env) {
				e.addJob(t, "r1", job.StatusRunning, 42)
				e.addParts(t, "r1", segsN(3))
			},
			want: Status{State: StateRecognising, Percent: 42, Segments: 3},
		},
		{
			name:  "waiting for power keeps the progress",
			setup: func(t *testing.T, e *env) { e.addJob(t, "r1", job.StatusWaitingForPower, 30) },
			want:  Status{State: StateWaiting, Percent: 30},
		},
		{
			name: "failed keeps the parts",
			setup: func(t *testing.T, e *env) {
				e.addJob(t, "r1", job.StatusFailed, 0)
				e.addParts(t, "r1", segsN(2))
			},
			want: Status{State: StateFailed, Segments: 2},
		},
		{
			name:  "cancelled",
			setup: func(t *testing.T, e *env) { e.addJob(t, "r1", job.StatusCancelled, 0) },
			want:  Status{State: StateFailed},
		},
		{
			name:  "completed without a transcript",
			setup: func(t *testing.T, e *env) { e.addJob(t, "r1", job.StatusCompleted, 0) },
			want:  Status{State: StateNone},
		},
		{
			name: "done prefers the stored transcript over parts",
			setup: func(t *testing.T, e *env) {
				e.addJob(t, "r1", job.StatusRunning, 10)
				e.addParts(t, "r1", segsN(2))
				e.addTranscript(t, "r1", segsN(5))
			},
			want: Status{State: StateDone, Segments: 5, Final: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			rec := e.addRecording(t, "r1", "")
			tc.setup(t, e)

			got, err := e.svc.Status(context.Background(), "r1")
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			tc.want.Recording, tc.want.StartedAt, tc.want.Playable = "r1", rec.StartedAt(), true
			tc.want.Title = "Запись 19.10.2026, 12:05"
			if got != tc.want {
				t.Fatalf("Status = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestStatusTitleAndErrors(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "Физика")
	got, err := e.svc.Status(context.Background(), "r1")
	if err != nil || got.Title != "Физика" {
		t.Fatalf("Status = %+v, %v; want the lesson title", got, err)
	}
	if !reflect.DeepEqual(e.jobs.prefixes, []string{"stt stt:r1:"}) {
		t.Fatalf("job lookups = %v", e.jobs.prefixes)
	}

	// A lesson removed since gives the ad-hoc title.
	delete(e.lessons, "lesson-r1")
	if got, _ := e.svc.Status(context.Background(), "r1"); got.Title != "Запись 19.10.2026, 12:05" {
		t.Fatalf("title without lesson = %q", got.Title)
	}

	if _, err := e.svc.Status(context.Background(), "missing"); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("missing recording: %v", err)
	}
	if _, err := e.svc.Status(context.Background(), "../x"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid id: %v", err)
	}
	e.transcripts.err = errors.New("disk I/O error")
	if _, err := e.svc.Status(context.Background(), "r1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("storage failure: %v", err)
	}
}

func TestStatusOfUnfinishedRecording(t *testing.T) {
	e := newEnv(t)
	r, err := recording.New(recording.Spec{ID: "r2", MaxDuration: time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.recordings["r2"] = r
	got, err := e.svc.Status(context.Background(), "r2")
	if err != nil || got.Playable || got.State != StateNone {
		t.Fatalf("Status = %+v, %v", got, err)
	}
	if _, err := e.svc.OpenAudio(context.Background(), "r2"); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("OpenAudio of an unfinished recording: %v", err)
	}
}

// A three-hour lecture has about 4000 segments: pages are capped and the document is parsed once.
func TestSegmentsPagesTheDocumentFromCache(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	all := segsN(4000)
	e.addTranscript(t, "r1", all)
	ctx := context.Background()

	p, err := e.svc.Segments(ctx, "r1", 0, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 4000 || !p.Final || len(p.Segments) != MaxPage || p.Segments[0] != all[0] {
		t.Fatalf("first page: total %d, final %v, %d segments", p.Total, p.Final, len(p.Segments))
	}
	for from := MaxPage; from < 4000; from += MaxPage {
		p, err := e.svc.Segments(ctx, "r1", from, MaxPage)
		if err != nil || p.From != from || p.Segments[0] != all[from] {
			t.Fatalf("page at %d: %+v, %v", from, p.From, err)
		}
	}
	if p, err := e.svc.Segments(ctx, "r1", 3990, 100); err != nil || len(p.Segments) != 10 {
		t.Fatalf("last page = %d segments, %v", len(p.Segments), err)
	}
	if p, err := e.svc.Segments(ctx, "r1", 9000, 10); err != nil || len(p.Segments) != 0 || p.Total != 4000 {
		t.Fatalf("past the end = %+v, %v", p, err)
	}
	if e.artifacts.opens != 1 {
		t.Fatalf("document read %d times, want once", e.artifacts.opens)
	}

	for _, bad := range [][2]int{{-1, 10}, {0, 0}, {0, -5}} {
		if _, err := e.svc.Segments(ctx, "r1", bad[0], bad[1]); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Segments(%d, %d) = %v, want ErrInvalidRequest", bad[0], bad[1], err)
		}
	}
}

func TestDocumentCacheIsBounded(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := range cachedDocuments + 2 {
		id := shared.RecordingID(fmt.Sprintf("r%d", i))
		e.addRecording(t, id, "")
		e.addTranscript(t, id, segsN(2))
		if _, err := e.svc.Segments(ctx, id, 0, 10); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.svc.docs) != cachedDocuments {
		t.Fatalf("cache holds %d documents, want %d", len(e.svc.docs), cachedDocuments)
	}
	// The oldest was evicted: reading it again opens the document again.
	before := e.artifacts.opens
	if _, err := e.svc.Segments(ctx, "r0", 0, 10); err != nil {
		t.Fatal(err)
	}
	if e.artifacts.opens != before+1 {
		t.Fatal("the evicted document was not read again")
	}
}

func TestSegmentsFromLiveParts(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	e.addJob(t, "r1", job.StatusRunning, 20)
	e.addParts(t, "r1", segsN(7))

	p, err := e.svc.Segments(context.Background(), "r1", 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.Final || p.Total != 7 || p.From != 5 || len(p.Segments) != 2 || p.Segments[0].Text != "фраза 5" {
		t.Fatalf("page = %+v", p)
	}
	e.live.err = errors.New("database is locked")
	if _, err := e.svc.Segments(context.Background(), "r1", 0, 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("storage failure: %v", err)
	}
}

func TestTextForClipboard(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "Физика")
	ctx := context.Background()

	if _, err := e.svc.Text(ctx, "r1", true); !errors.Is(err, ErrNoTranscript) {
		t.Fatalf("nothing recognised: %v", err)
	}

	e.addParts(t, "r1", segsN(2))
	got, err := e.svc.Text(ctx, "r1", true)
	if err != nil || got != "[00:00] фраза 0\n[00:02] фраза 1\n" {
		t.Fatalf("live text = %q, %v", got, err)
	}

	e.addTranscript(t, "r1", []transcript.Segment{{StartMs: 3_600_000, EndMs: 3_601_000, Text: "итог"}})
	got, err = e.svc.Text(ctx, "r1", false)
	if err != nil || got != "итог\n" {
		t.Fatalf("final text = %q, %v", got, err)
	}
	got, _ = e.svc.Text(ctx, "r1", true)
	if got != "[01:00:00] итог\n" {
		t.Fatalf("final text with timestamps = %q", got)
	}
}

// Text of more parts than one page: all of them, in order.
func TestTextOfManyLiveParts(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	e.addParts(t, "r1", segsN(MaxPage+3))
	got, err := e.svc.Text(context.Background(), "r1", false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != MaxPage+3 || lines[MaxPage+2] != fmt.Sprintf("фраза %d", MaxPage+2) {
		t.Fatalf("%d lines", len(lines))
	}
}

func TestSuggestedName(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", `Физика: "волны"`)
	e.addRecording(t, "r2", "")
	ctx := context.Background()

	tests := []struct {
		rec  shared.RecordingID
		f    transcript.ExportFormat
		want string
	}{
		{"r1", transcript.FormatText, "Физика_ _волны_ 2026-10-19.txt"},
		{"r1", transcript.FormatMarkdown, "Физика_ _волны_ 2026-10-19.md"},
		{"r2", transcript.FormatText, "Расшифровка 2026-10-19 12-05.txt"},
	}
	for _, tc := range tests {
		got, err := e.svc.SuggestedName(ctx, tc.rec, tc.f)
		if err != nil || got != tc.want {
			t.Errorf("SuggestedName(%s, %s) = %q, %v; want %q", tc.rec, tc.f, got, err, tc.want)
		}
	}
	if _, err := e.svc.SuggestedName(ctx, "r1", "html"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown format: %v", err)
	}
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "Физика")
	e.addTranscript(t, "r1", []transcript.Segment{{StartMs: 65_000, EndMs: 66_000, Text: "<b>жирный</b> текст"}})
	ctx := context.Background()

	if err := e.svc.Export(ctx, "r1", transcript.FormatMarkdown, true, `C:\Users\s\Физика.md`); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if e.writer.path != `C:\Users\s\Физика.md` || e.writer.content != "# Физика\n\n**01:05** \\<b\\>жирный\\</b\\> текст\n\n" {
		t.Fatalf("written %q to %q", e.writer.content, e.writer.path)
	}

	if err := e.svc.Export(ctx, "r1", transcript.FormatText, false, `C:\out.txt`); err != nil {
		t.Fatal(err)
	}
	if e.writer.content != "Физика\n\n<b>жирный</b> текст\n" {
		t.Fatalf("txt = %q", e.writer.content)
	}

	for name, call := range map[string]func() error{
		"unknown format": func() error { return e.svc.Export(ctx, "r1", "html", false, `C:\x`) },
		"empty path":     func() error { return e.svc.Export(ctx, "r1", transcript.FormatText, false, "") },
	} {
		if err := call(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}

	e.writer.err = errors.New("textfile: the file cannot be written: access denied")
	if err := e.svc.Export(ctx, "r1", transcript.FormatText, false, `C:\x.txt`); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write failure: %v", err)
	}

	e.addRecording(t, "r2", "")
	if err := e.svc.Export(ctx, "r2", transcript.FormatText, false, `C:\x.txt`); !errors.Is(err, ErrNoTranscript) {
		t.Fatalf("nothing to export: %v", err)
	}
}

func TestOpenAudio(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	a, err := e.svc.OpenAudio(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Reader.Close() }()
	data, _ := io.ReadAll(a.Reader)
	if a.Size != int64(len("RIFF-audio-bytes")) || string(data) != "RIFF-audio-bytes" {
		t.Fatalf("audio = %d bytes %q", a.Size, data)
	}

	// The track file is gone.
	e.artifacts.files = map[string][]byte{}
	if _, err := e.svc.OpenAudio(context.Background(), "r1"); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("missing track: %v", err)
	}
	if _, err := e.svc.OpenAudio(context.Background(), "nope"); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("missing recording: %v", err)
	}
}
