package tests

// Scenario tests of the transcript viewer (TASK-079) on the real stack: the STT job with whispercli (the test binary
// plays whisper-cli), SQLite, the file store and the text file writer. Only the clock and whisper-cli are fake.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// The adapters satisfy the ports of the transcript viewer and the live parts port of the STT job.
var (
	_ transcribe.LiveParts            = (*sqlite.TranscriptParts)(nil)
	_ transcriptview.RecordingReader  = (*sqlite.RecordingRepository)(nil)
	_ transcriptview.LessonReader     = (*sqlite.LessonRepository)(nil)
	_ transcriptview.TranscriptFinder = (*sqlite.TranscriptRepository)(nil)
	_ transcriptview.JobFinder        = (*sqlite.JobRepository)(nil)
	_ transcriptview.LiveParts        = (*sqlite.TranscriptParts)(nil)
	_ transcriptview.ArtifactStore    = (*filestore.Store)(nil)
	_ transcriptview.TextWriter       = textfile.Writer{}
)

func newViewer(t *testing.T, db *sqlite.DB, store *filestore.Store, clock *testenv.FakeClock) *transcriptview.Service {
	t.Helper()
	v, err := transcriptview.New(transcriptview.Deps{
		Recordings: sqlite.NewRecordingRepository(db), Lessons: sqlite.NewLessonRepository(db),
		Transcripts: sqlite.NewTranscriptRepository(db), Jobs: sqlite.NewJobRepository(db),
		Live: sqlite.NewTranscriptParts(db), Artifacts: store, Writer: textfile.New(), Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func pageTexts(p transcriptview.Page) []string {
	out := make([]string, len(p.Segments))
	for i, s := range p.Segments {
		out[i] = s.Text
	}
	return out
}

func liveTexts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("Фраза %d.", i)
	}
	return out
}

// While whisper-cli runs, its segments show up in the viewer in batches; a cancelled job keeps them, the pending
// ones included, and a new database connection (a restart) reads them.
func TestLiveTranscriptDuringRecognitionAndAfterRestart(t *testing.T) {
	e := newSTTEnvWith(t, sttOptions{live: true})
	m := e.installModel("m1", "base", "1", testenv.WhisperLive)
	e.enqueue("job-1", e.rec.ID(), m.ID())
	viewer := newViewer(t, e.db, e.store, e.clock)
	ctx := context.Background()
	e.start()

	var st transcriptview.Status
	testenv.Eventually(t, "a batch of live parts", func() bool {
		var err error
		st, err = viewer.Status(ctx, e.rec.ID())
		return err == nil && st.Segments >= transcript.LiveBatchParts
	}, nil)
	if st.State != transcriptview.StateRecognising || st.Final || !st.Playable {
		t.Fatalf("status while recognising = %+v", st)
	}
	p, err := viewer.Segments(ctx, e.rec.ID(), 0, transcriptview.MaxPage)
	if err != nil {
		t.Fatal(err)
	}
	if got := pageTexts(p); !reflect.DeepEqual(got, liveTexts(transcript.LiveBatchParts)) {
		t.Fatalf("live page = %v", got)
	}
	if p.Segments[1].StartMs != 2000 || p.Segments[1].EndMs != 4000 {
		t.Fatalf("live times = %+v", p.Segments[1])
	}

	if err := e.runner.Cancel(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	e.waitFor("job-1", job.StatusCancelled)

	// A restart: another connection to the same database file.
	db := openRunnerDB(t, filepath.Join(filepath.Dir(e.dataRoot), "app.db"))
	t.Cleanup(func() { _ = db.Close() })
	after := newViewer(t, db, e.store, e.clock)
	st, err = after.Status(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != transcriptview.StateFailed || st.Segments != testenv.WhisperLiveParts {
		t.Fatalf("status after the cancellation = %+v", st)
	}
	text, err := after.Text(ctx, e.rec.ID(), true)
	if err != nil || !strings.HasPrefix(text, "[00:00] Фраза 0.\n[00:02] Фраза 1.\n") ||
		!strings.HasSuffix(text, "[00:40] Фраза 20.\n") {
		t.Fatalf("text after the restart = %q, %v", text, err)
	}
}

// When the transcript is stored, the live parts are removed and the viewer shows the document: copy, export and
// playback all work from local files.
func TestTranscriptViewerAfterRecognition(t *testing.T) {
	e := newSTTEnvWith(t, sttOptions{live: true})
	m := e.installModel("m1", "base", "1", testenv.WhisperOK)
	e.enqueue("job-1", e.rec.ID(), m.ID())
	e.start()
	e.waitFor("job-1", job.StatusCompleted)

	ctx := context.Background()
	if n, err := sqlite.NewTranscriptParts(e.db).CountParts(ctx, e.rec.ID()); err != nil || n != 0 {
		t.Fatalf("live parts left after the transcript was stored: %d, %v", n, err)
	}

	viewer := newViewer(t, e.db, e.store, e.clock)
	st, err := viewer.Status(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != transcriptview.StateDone || !st.Final || st.Segments != len(testenv.WhisperOKSegments) {
		t.Fatalf("status = %+v", st)
	}
	p, err := viewer.Segments(ctx, e.rec.ID(), 0, 10)
	if err != nil || !reflect.DeepEqual(pageTexts(p), testenv.WhisperOKSegments) {
		t.Fatalf("page = %v, %v", pageTexts(p), err)
	}

	out := filepath.Join(t.TempDir(), "Расшифровка.md")
	if err := viewer.Export(ctx, e.rec.ID(), transcript.FormatMarkdown, true, out); err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Запись 14.10.2026, 07:00\n\n**00:00** Добрый день.\n\n**00:02** Homework: exercise 5.\n\n"
	if string(data) != want {
		t.Fatalf("exported %q, want %q", data, want)
	}

	audio, err := viewer.OpenAudio(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = audio.Reader.Close() }()
	if _, err := audio.Reader.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(audio.Reader)
	if audio.Size != int64(len("RIFF fake audio")) || string(rest) != "fake audio" {
		t.Fatalf("audio %d bytes, after seek %q", audio.Size, rest)
	}
}
