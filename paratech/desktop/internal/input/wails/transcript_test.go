package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// fakeTranscript is TranscriptUseCase; err, when set, is returned by every method.
type fakeTranscript struct {
	err      error
	status   transcriptview.Status
	page     transcriptview.Page
	text     string
	name     string
	exported struct {
		id   shared.RecordingID
		f    transcript.ExportFormat
		ts   bool
		path string
	}
	exportErr error
	audio     transcriptview.Audio
	calls     []string
}

func (f *fakeTranscript) Status(_ context.Context, id shared.RecordingID) (transcriptview.Status, error) {
	f.calls = append(f.calls, "Status "+string(id))
	return f.status, f.err
}

func (f *fakeTranscript) Segments(_ context.Context, _ shared.RecordingID, from, limit int) (transcriptview.Page, error) {
	f.calls = append(f.calls, "Segments")
	return f.page, f.err
}

func (f *fakeTranscript) Text(_ context.Context, _ shared.RecordingID, withTimestamps bool) (string, error) {
	f.calls = append(f.calls, "Text")
	if withTimestamps {
		return "[00:01] " + f.text, f.err
	}
	return f.text, f.err
}

func (f *fakeTranscript) SuggestedName(_ context.Context, _ shared.RecordingID, fm transcript.ExportFormat) (string, error) {
	f.calls = append(f.calls, "SuggestedName "+string(fm))
	return f.name, f.err
}

func (f *fakeTranscript) Export(_ context.Context, id shared.RecordingID, fm transcript.ExportFormat, ts bool, path string) error {
	f.calls = append(f.calls, "Export")
	f.exported.id, f.exported.f, f.exported.ts, f.exported.path = id, fm, ts, path
	if f.exportErr != nil {
		return f.exportErr
	}
	return f.err
}

func (f *fakeTranscript) OpenAudio(context.Context, shared.RecordingID) (transcriptview.Audio, error) {
	return f.audio, f.err
}

type fakeClipboard struct {
	text string
	err  error
}

func (c *fakeClipboard) SetText(_ context.Context, text string) error {
	if c.err != nil {
		return c.err
	}
	c.text = text
	return nil
}

type transcriptFixture struct {
	svc      *TranscriptService
	uc       *fakeTranscript
	clip     *fakeClipboard
	picked   []string
	pickPath string
	pickErr  error
	logs     *bytes.Buffer
}

func newTranscriptFixture() *transcriptFixture {
	f := &transcriptFixture{uc: &fakeTranscript{}, clip: &fakeClipboard{}, logs: &bytes.Buffer{}}
	picker := transcriptPickerFunc(func(_ context.Context, name, format string) (string, error) {
		f.picked = append(f.picked, name+" "+format)
		return f.pickPath, f.pickErr
	})
	f.svc = NewTranscriptService(f.uc, picker, f.clip, slog.New(slog.NewTextHandler(f.logs, nil)))
	return f
}

// everyTranscriptCall calls each bound method with the recording id.
var everyTranscriptCall = []struct {
	name string
	call func(s *TranscriptService, id string) error
}{
	{"Status", func(s *TranscriptService, id string) error { _, err := s.Status(context.Background(), id); return err }},
	{"Segments", func(s *TranscriptService, id string) error {
		_, err := s.Segments(context.Background(), id, 0, 100)
		return err
	}},
	{"Text", func(s *TranscriptService, id string) error {
		_, err := s.Text(context.Background(), id, true)
		return err
	}},
	{"CopyAll", func(s *TranscriptService, id string) error { return s.CopyAll(context.Background(), id, false) }},
	{"CopyText", func(s *TranscriptService, id string) error { return s.CopyText(context.Background(), id) }},
	{"SaveAs", func(s *TranscriptService, id string) error {
		_, err := s.SaveAs(context.Background(), id, "txt", true)
		return err
	}},
}

func TestEveryTranscriptMethodIsCovered(t *testing.T) {
	if n := len(everyTranscriptCall); n != reflect.TypeFor[*TranscriptService]().NumMethod() {
		t.Fatalf("everyTranscriptCall has %d calls, TranscriptService has %d methods", n, reflect.TypeFor[*TranscriptService]().NumMethod())
	}
}

// Ids are checked before the use case is called (CopyText takes text, not an id).
func TestTranscriptValidatesIDs(t *testing.T) {
	for _, c := range everyTranscriptCall {
		if c.name == "CopyText" {
			continue
		}
		for _, id := range []string{"", "../x", `a\b`, "a b", "a\x00b"} {
			f := newTranscriptFixture()
			if err := c.call(f.svc, id); !errors.Is(err, ErrTranscriptInvalidID) {
				t.Errorf("%s(%q) = %v, want ErrTranscriptInvalidID", c.name, id, err)
			}
			if len(f.uc.calls) != 0 {
				t.Errorf("%s(%q) called the use case: %v", c.name, id, f.uc.calls)
			}
		}
	}
}

// Use case failures reach the page as fixed texts; the cause is only logged.
func TestTranscriptHidesCauses(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{transcriptview.ErrRecordingNotFound, ErrTranscriptNotFound},
		{transcriptview.ErrNoTranscript, ErrTranscriptEmpty},
		{errors.Join(transcriptview.ErrUnavailable, errors.New(`open C:\Users\student\x: denied`)), ErrTranscriptUnavailable},
	}
	for _, tc := range tests {
		for _, c := range everyTranscriptCall {
			if c.name == "CopyText" {
				continue
			}
			f := newTranscriptFixture()
			f.uc.err = tc.err
			err := c.call(f.svc, "r1")
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "student") {
				t.Errorf("%s with %v = %v, want %v", c.name, tc.err, err, tc.want)
			}
		}
	}
}

func TestTranscriptStatus(t *testing.T) {
	started := time.Date(2026, 10, 19, 9, 5, 0, 0, time.FixedZone("MSK", 3*3600))
	tests := []struct {
		name string
		st   transcriptview.Status
		want TranscriptStatus
	}{
		{
			name: "recognising",
			st: transcriptview.Status{Recording: "r1", Title: "Физика", StartedAt: started, State: transcriptview.StateRecognising,
				Percent: 42, Segments: 12, Playable: true},
			want: TranscriptStatus{RecordingID: "r1", Title: "Физика", StartedAt: "2026-10-19T06:05:00Z", State: "recognising",
				Percent: 42, Message: "Распознаётся… 42 %", Segments: 12, AudioURL: "/media/recording/r1"},
		},
		{
			name: "done without audio",
			st:   transcriptview.Status{Recording: "r%1", State: transcriptview.StateDone, Segments: 3, Final: true},
			want: TranscriptStatus{RecordingID: "r%1", State: "done", Message: msgStateDone, Segments: 3, Final: true},
		},
		{
			name: "playable id is escaped",
			st:   transcriptview.Status{Recording: "r%1#x", State: transcriptview.StateNone, Playable: true},
			want: TranscriptStatus{RecordingID: "r%1#x", State: "none", Message: msgStateNone, AudioURL: "/media/recording/r%251%23x"},
		},
		{
			name: "failed with parts",
			st:   transcriptview.Status{Recording: "r1", State: transcriptview.StateFailed, Segments: 2},
			want: TranscriptStatus{RecordingID: "r1", State: "failed", Message: msgStateFailedParts, Segments: 2},
		},
		{
			name: "failed without parts",
			st:   transcriptview.Status{Recording: "r1", State: transcriptview.StateFailed},
			want: TranscriptStatus{RecordingID: "r1", State: "failed", Message: msgStateFailed},
		},
		{
			name: "waiting",
			st:   transcriptview.Status{Recording: "r1", State: transcriptview.StateWaiting, Percent: 5},
			want: TranscriptStatus{RecordingID: "r1", State: "waiting", Percent: 5, Message: msgStateWaiting},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranscriptFixture()
			f.uc.status = tc.st
			got, err := f.svc.Status(context.Background(), "r1")
			if err != nil || got != tc.want {
				t.Fatalf("Status = %+v, %v\nwant %+v", got, err, tc.want)
			}
		})
	}
}

func TestTranscriptSegments(t *testing.T) {
	f := newTranscriptFixture()
	f.uc.page = transcriptview.Page{From: 10, Total: 4000, Final: true, Segments: []transcript.Segment{
		{StartMs: 61_000, EndMs: 62_000, Text: "<script>не HTML</script>"},
		{StartMs: 3_600_000, EndMs: 3_601_000, Text: "час"},
	}}
	got, err := f.svc.Segments(context.Background(), "r1", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := TranscriptPage{From: 10, Total: 4000, Final: true, Segments: []TranscriptSegment{
		{Index: 10, StartMs: 61_000, EndMs: 62_000, Start: "01:01", Text: "<script>не HTML</script>"},
		{Index: 11, StartMs: 3_600_000, EndMs: 3_601_000, Start: "01:00:00", Text: "час"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Segments = %+v", got)
	}

	for _, bad := range [][2]int{{-1, 10}, {0, 0}} {
		if _, err := f.svc.Segments(context.Background(), "r1", bad[0], bad[1]); !errors.Is(err, ErrTranscriptPage) {
			t.Errorf("Segments(%d, %d) = %v, want ErrTranscriptPage", bad[0], bad[1], err)
		}
	}

	// An empty page is an empty array, not null.
	f.uc.page = transcriptview.Page{}
	got, _ = f.svc.Segments(context.Background(), "r1", 0, 10)
	if data, _ := json.Marshal(got); !strings.Contains(string(data), `"segments":[]`) {
		t.Fatalf("empty page = %s", data)
	}
}

func TestTranscriptCopy(t *testing.T) {
	f := newTranscriptFixture()
	f.uc.text = "текст\n"
	if err := f.svc.CopyAll(context.Background(), "r1", true); err != nil {
		t.Fatal(err)
	}
	if f.clip.text != "[00:01] текст\n" {
		t.Fatalf("clipboard = %q", f.clip.text)
	}
	if err := f.svc.CopyText(context.Background(), "выделенное"); err != nil || f.clip.text != "выделенное" {
		t.Fatalf("CopyText: %v, clipboard %q", err, f.clip.text)
	}
	if err := f.svc.CopyText(context.Background(), strings.Repeat("x", maxCopyBytes+1)); !errors.Is(err, ErrTranscriptCopyTooBig) {
		t.Fatalf("too big: %v", err)
	}

	f.clip.err = errors.New("OpenClipboard: access denied")
	if err := f.svc.CopyAll(context.Background(), "r1", false); !errors.Is(err, ErrTranscriptClipboard) {
		t.Fatalf("clipboard failure: %v", err)
	}
	if strings.Contains(f.logs.String(), "текст") {
		t.Fatal("the transcript text reached the log")
	}
}

func TestTranscriptSaveAs(t *testing.T) {
	f := newTranscriptFixture()
	f.uc.name = "Физика 2026-10-19.md"
	f.pickPath = `C:\Users\student\Документы\Физика 2026-10-19.md`

	got, err := f.svc.SaveAs(context.Background(), "r1", "md", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != (TranscriptSaveResult{Status: transcriptSaved, Message: msgTranscriptSaved, Name: "Физика 2026-10-19.md"}) {
		t.Fatalf("SaveAs = %+v", got)
	}
	if !reflect.DeepEqual(f.picked, []string{"Физика 2026-10-19.md md"}) {
		t.Fatalf("picker got %v", f.picked)
	}
	if e := f.uc.exported; e.id != "r1" || e.f != transcript.FormatMarkdown || !e.ts || e.path != f.pickPath {
		t.Fatalf("exported %+v", e)
	}
}

func TestTranscriptSaveAsOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		pickPath  string
		pickErr   error
		exportErr error
		want      TranscriptSaveResult
		wantErr   error
	}{
		{name: "cancelled", format: "txt", want: TranscriptSaveResult{Status: transcriptCancelled, Message: msgTranscriptCancelled}},
		{name: "dialog failed", format: "txt", pickErr: errors.New("dialog"), want: TranscriptSaveResult{Status: transcriptFailed, Message: msgTranscriptFailed}},
		{name: "write failed", format: "txt", pickPath: `C:\student\x.txt`,
			exportErr: errors.Join(transcriptview.ErrUnavailable, errors.New("access denied")),
			want:      TranscriptSaveResult{Status: transcriptFailed, Message: msgTranscriptFailed}},
		{name: "nothing recognised", format: "txt", pickPath: `C:\student\x.txt`, exportErr: transcriptview.ErrNoTranscript,
			wantErr: ErrTranscriptEmpty},
		{name: "unknown format", format: "html", wantErr: ErrTranscriptFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranscriptFixture()
			f.pickPath, f.pickErr, f.uc.exportErr = tc.pickPath, tc.pickErr, tc.exportErr
			got, err := f.svc.SaveAs(context.Background(), "r1", tc.format, false)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("SaveAs = %+v, %v; want %+v, %v", got, err, tc.want, tc.wantErr)
			}
			if strings.Contains(f.logs.String(), "student") {
				t.Fatalf("the path reached the log: %s", f.logs.String())
			}
		})
	}
}

func TestTranscriptJSONNames(t *testing.T) {
	data, err := json.Marshal(TranscriptStatus{})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"recordingId", "title", "startedAt", "state", "percent", "message", "segments", "final", "audioUrl"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("status JSON %s has no %q", data, k)
		}
	}
	data, _ = json.Marshal(TranscriptPage{Segments: []TranscriptSegment{{}}})
	for _, k := range []string{"from", "total", "final", "segments", "index", "startMs", "endMs", "start", "text"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("page JSON %s has no %q", data, k)
		}
	}
	data, _ = json.Marshal(TranscriptSaveResult{})
	for _, k := range []string{"status", "message", "name"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("save JSON %s has no %q", data, k)
		}
	}
}

// The page shows the error texts as they are; the frontend wrapper lists each one.
func TestFrontendKnowsTranscriptErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/transcript.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrTranscriptInvalidID, ErrTranscriptNotFound, ErrTranscriptEmpty, ErrTranscriptFormat, ErrTranscriptPage,
		ErrTranscriptUnavailable, ErrTranscriptClipboard, ErrTranscriptCopyTooBig,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/transcript.ts does not list the error %q", e)
		}
	}
}
