package wails

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/recordingimport"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// fakeImportUC records the requests and answers with res or err.
type fakeImportUC struct {
	reqs []recordingimport.Request
	res  recordingimport.Result
	err  error
}

func (f *fakeImportUC) Import(_ context.Context, req recordingimport.Request) (recordingimport.Result, error) {
	f.reqs = append(f.reqs, req)
	return f.res, f.err
}

const importPath = `C:\Users\student\Music\Лекция 3.mp3` // a fake path, never opened here

func importedResult(t *testing.T, format recording.AudioFormat, d time.Duration) recordingimport.Result {
	t.Helper()
	r, err := recording.NewImported(recording.ImportSpec{
		ID: "imp-1", StartedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), SHA256: strings.Repeat("ab", 32),
		Audio: recording.ImportedAudio{Format: format, Duration: d},
	}, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return recordingimport.Result{Recording: r}
}

func newImportService(uc *fakeImportUC, path string, pickErr error) (*RecordingImportService, *bytes.Buffer, *int) {
	logs, picks := &bytes.Buffer{}, new(int)
	picker := recordingPickerFunc(func(context.Context) (string, error) {
		*picks++
		return path, pickErr
	})
	return NewRecordingImportService(uc, picker, slog.New(slog.NewTextHandler(logs, nil))), logs, picks
}

func TestRecordingImportOfALesson(t *testing.T) {
	uc := &fakeImportUC{res: importedResult(t, recording.FormatWAV, 95*time.Minute+400*time.Millisecond)}
	s, logs, _ := newImportService(uc, importPath, nil)

	got, err := s.Import(context.Background(), RecordingImportLecture{LessonID: "lesson-1"})
	want := RecordingImportResult{RecordingID: "imp-1", Name: "Лекция 3.mp3", Format: "wav", DurationSeconds: 95 * 60}
	if err != nil || got != want {
		t.Fatalf("Import = %+v, %v; want %+v", got, err, want)
	}
	if len(uc.reqs) != 1 || uc.reqs[0] != (recordingimport.Request{Path: importPath, LessonID: "lesson-1"}) {
		t.Fatalf("requests %+v", uc.reqs)
	}
	if strings.Contains(logs.String(), "Лекция") || strings.Contains(logs.String(), `C:\`) {
		t.Fatalf("the log names the file: %s", logs)
	}
}

func TestRecordingImportOfADate(t *testing.T) {
	res := importedResult(t, recording.FormatMP3, 0)
	res.Existing = true
	uc := &fakeImportUC{res: res}
	s, _, _ := newImportService(uc, importPath, nil)

	got, err := s.Import(context.Background(), RecordingImportLecture{
		StartedAt: "2026-10-20T12:00:00+03:00", Subject: "Физика", Kind: "lecture", Title: "Оптика",
	})
	if err != nil || !got.Existing || got.Message != "Эта запись уже добавлена." || got.Format != "mp3" || got.DurationSeconds != 0 {
		t.Fatalf("Import = %+v, %v", got, err)
	}
	r := uc.reqs[0]
	if !r.StartedAt.Equal(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)) || r.Subject != "Физика" || r.Kind != "lecture" ||
		r.Title != "Оптика" || r.LessonID != "" {
		t.Fatalf("request %+v", r)
	}
}

// A bad lecture is refused before the dialog; a cancelled dialog imports nothing.
func TestRecordingImportRefusesBeforeTheDialog(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lecture RecordingImportLecture
		want    error
	}{
		{"bad lesson id", RecordingImportLecture{LessonID: "a b"}, ErrRecordingImportInvalid},
		{"no time", RecordingImportLecture{Subject: "Физика"}, ErrRecordingImportInvalidDate},
		{"time without offset", RecordingImportLecture{StartedAt: "2026-10-20T12:00:00"}, ErrRecordingImportInvalidDate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeImportUC{}
			s, _, picks := newImportService(uc, importPath, nil)
			if _, err := s.Import(context.Background(), tc.lecture); !errors.Is(err, tc.want) {
				t.Fatalf("Import = %v, want %v", err, tc.want)
			}
			if *picks != 0 || len(uc.reqs) != 0 {
				t.Fatalf("dialog shown %d times, %d imports", *picks, len(uc.reqs))
			}
		})
	}

	uc := &fakeImportUC{}
	s, _, _ := newImportService(uc, "", nil)
	if got, err := s.Import(context.Background(), RecordingImportLecture{LessonID: "lesson-1"}); err != nil || !got.Cancelled || len(uc.reqs) != 0 {
		t.Fatalf("cancelled: %+v, %v, %d imports", got, err, len(uc.reqs))
	}
	s, _, _ = newImportService(uc, "", errors.New("dialog failed"))
	if _, err := s.Import(context.Background(), RecordingImportLecture{LessonID: "lesson-1"}); !errors.Is(err, ErrRecordingImportUnreadable) {
		t.Fatalf("dialog error: %v", err)
	}
}

func TestRecordingImportErrors(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("recordingimport: import a recording: %w", err) }
	tests := []struct {
		err  error
		want error
	}{
		{wrap(fmt.Errorf("%w: %w", recording.ErrInvalidRecording, recording.ErrFutureLecture)), ErrRecordingImportFuture},
		{wrap(recording.ErrEmptyAudio), ErrRecordingImportEmpty},
		{wrap(recording.ErrAudioTooLarge), ErrRecordingImportTooLarge},
		{wrap(recording.ErrAudioTooLong), ErrRecordingImportTooLong},
		{wrap(recording.ErrUnsupportedAudio), ErrRecordingImportFormat},
		{wrap(recording.ErrNoDiskSpace), ErrRecordingImportNoSpace},
		{wrap(recordingimport.ErrInvalidRequest), ErrRecordingImportInvalid},
		{wrap(recordingimport.ErrLessonNotFound), ErrRecordingImportNoLesson},
		{wrap(recordingimport.ErrUnreadable), ErrRecordingImportUnreadable},
		{wrap(recordingimport.ErrChanged), ErrRecordingImportChanged},
		{wrap(recordingimport.ErrDeleting), ErrRecordingImportDeleting},
		{wrap(fmt.Errorf("%w: %w", recordingimport.ErrStorage, shared.ErrStorage)), ErrRecordingImportUnavailable},
		{wrap(context.Canceled), ErrRecordingImportUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.want.Error(), func(t *testing.T) {
			s, _, _ := newImportService(&fakeImportUC{err: tc.err}, importPath, nil)
			if _, err := s.Import(context.Background(), RecordingImportLecture{LessonID: "lesson-1"}); !errors.Is(err, tc.want) {
				t.Fatalf("Import = %v, want %v", err, tc.want)
			}
		})
	}
}

// The page may call only Import; the frontend wrapper calls it by its bound name and knows every error text.
func TestFrontendKnowsRecordingImport(t *testing.T) {
	typ := reflect.TypeFor[*RecordingImportService]()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "Import" {
		t.Fatalf("RecordingImportService has %d exported methods, want only Import", typ.NumMethod())
	}
	src, err := os.ReadFile("../../../frontend/src/bindings/recordingImport.ts")
	if err != nil {
		t.Fatal(err)
	}
	if name := typ.Elem().PkgPath() + ".RecordingImportService.Import"; !strings.Contains(string(src), `"`+name+`"`) {
		t.Errorf("frontend/src/bindings/recordingImport.ts does not call %q", name)
	}
	for _, e := range []error{
		ErrRecordingImportInvalid, ErrRecordingImportInvalidDate, ErrRecordingImportFuture, ErrRecordingImportNoLesson, ErrRecordingImportUnreadable, ErrRecordingImportEmpty,
		ErrRecordingImportTooLarge, ErrRecordingImportTooLong, ErrRecordingImportFormat, ErrRecordingImportNoSpace, ErrRecordingImportChanged, ErrRecordingImportDeleting,
		ErrRecordingImportUnavailable,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/recordingImport.ts does not list the error %q", e)
		}
	}
	if !strings.Contains(string(src), `"`+msgRecordingImportAdded+`"`) {
		t.Errorf("frontend/src/bindings/recordingImport.ts does not list %q", msgRecordingImportAdded)
	}
}

// Without the Windows shell there is no dialog.
func TestPickRecordingFileWithoutShell(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pickRecordingFile(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("pickRecordingFile = %v, want ErrNotRunning", err)
	}
	s.ui = &fakeUI{}
	if _, err := s.pickRecordingFile(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("pickRecordingFile on a shell without the dialog = %v, want ErrUnsupported", err)
	}
}
