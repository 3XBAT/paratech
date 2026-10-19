package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// fakeSchedule is the import use case behind the binding; safe for concurrent use.
type fakeSchedule struct {
	mu         sync.Mutex
	preview    schedulesync.Preview
	previewErr error
	result     schedulesync.CommitResult
	commitErr  error
	previews   []schedulesync.ImportRequest
	commits    []schedulesync.CommitRequest
}

func (f *fakeSchedule) PreviewImport(_ context.Context, req schedulesync.ImportRequest) (schedulesync.Preview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews = append(f.previews, req)
	return f.preview, f.previewErr
}

func (f *fakeSchedule) CommitImport(_ context.Context, req schedulesync.CommitRequest) (schedulesync.CommitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, req)
	return f.result, f.commitErr
}

func (f *fakeSchedule) counts() (previews, commits int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.previews), len(f.commits)
}

// fileReader is the reader of the chosen file: it reads the test's file at most maxBytes+1 bytes, like
// adapters/userfile (tested there with missing files and directories), or fails with err. It counts the reads.
type fileReader struct {
	err   error
	reads atomic.Int32
	limit atomic.Int64
}

func disk() *fileReader { return &fileReader{} }

func (f *fileReader) ReadFile(_ context.Context, path string, maxBytes int64) ([]byte, error) {
	f.reads.Add(1)
	f.limit.Store(maxBytes)
	if f.err != nil {
		return nil, f.err
	}
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	return io.ReadAll(io.LimitReader(fh, maxBytes+1))
}

// fixedPicker "chooses" path; an empty path is a cancelled dialog.
func fixedPicker(path string) FilePicker {
	return pickerFunc(func(context.Context) (string, error) { return path, nil })
}

const fileContent = "title,date,start,end,timezone,uid\nSecret Seminar,2026-10-14,09:00,10:30,Europe/Moscow,m-1\n"

// writeFile stores data as name in a fresh directory and returns its path.
func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func moscow(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func content(t *testing.T, title string, hour int) lesson.Content {
	start := time.Date(2026, 10, 14, hour, 0, 0, 0, moscow(t))
	return lesson.Content{Title: title, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow"}
}

// detailed gives c the subject, kind and teacher of a lesson file (TASK-089).
func detailed(c lesson.Content) lesson.Content {
	c.Subject, c.Kind, c.Teacher = "Math", lesson.KindLecture, "Ivanov I."
	return c
}

// fullPreview has every kind of entry and can be committed.
func fullPreview(t *testing.T) schedulesync.Preview {
	return schedulesync.Preview{ConfirmToken: "tok-1", Plan: importplan.Plan{
		Source:    importplan.SourceCSV,
		Added:     []importplan.Addition{{Line: 2, UID: "csv:id:a", Content: detailed(content(t, "Math <b>", 9))}},
		Changed:   []importplan.Change{{Line: 3, Before: content(t, "Physics", 11), After: content(t, "Physics", 12)}},
		Unchanged: []importplan.Change{{Line: 4}, {Line: 5}},
		Removed:   []importplan.Removal{{UID: "csv:id:r", Content: content(t, "Old", 15)}},
		Conflicts: []importplan.Conflict{
			{Line: 2, Content: content(t, "Math <b>", 9), WithLine: 6, WithContent: content(t, "Lab", 10)},
			{Line: 3, Content: content(t, "Physics", 12), WithLesson: "l-1", WithContent: content(t, "Manual", 12)},
		},
		ConflictsTruncated: true,
		Fingerprint:        "tok-1",
	}}
}

func TestScheduleChooseFileShowsThePreview(t *testing.T) {
	ctx := context.Background()
	path := writeFile(t, "Lessons.CSV", []byte(fileContent))
	uc := &fakeSchedule{preview: fullPreview(t)}
	s := NewScheduleImportService(uc, disk(), fixedPicker(path), discard())

	got, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(uc.previews) != 1 || uc.previews[0].Source != importplan.SourceCSV || string(uc.previews[0].Data) != fileContent {
		t.Fatalf("PreviewImport got %+v, want the CSV source and the file's bytes", uc.previews)
	}
	if got.SelectionID == "" {
		t.Fatal("no selection id")
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// The page reads these names (frontend/src/bindings/schedule.ts); times are in the lesson's zone.
	// A lesson without subject, kind and teacher (TASK-089).
	const none = `,"subject":"","kind":"unknown","teacher":""}`
	want := `{"cancelled":false,"selectionId":"` + got.SelectionID + `","fileName":"Lessons.CSV","source":"csv","errors":[],` +
		`"added":[{"line":2,"title":"Math \u003cb\u003e","start":"14.10.2026 09:00","end":"14.10.2026 10:30","zone":"Europe/Moscow",` +
		`"subject":"Math","kind":"lecture","teacher":"Ivanov I."}],` +
		`"changed":[{"line":3,"before":{"title":"Physics","start":"14.10.2026 11:00","end":"14.10.2026 12:30","zone":"Europe/Moscow"` + none + `,` +
		`"after":{"title":"Physics","start":"14.10.2026 12:00","end":"14.10.2026 13:30","zone":"Europe/Moscow"` + none + `}],` +
		`"removed":[{"line":0,"title":"Old","start":"14.10.2026 15:00","end":"14.10.2026 16:30","zone":"Europe/Moscow"` + none + `],` +
		`"unchangedCount":2,` +
		`"conflicts":[{"line":2,"lesson":{"title":"Math \u003cb\u003e","start":"14.10.2026 09:00","end":"14.10.2026 10:30","zone":"Europe/Moscow"` + none + `,` +
		`"withLine":6,"with":{"title":"Lab","start":"14.10.2026 10:00","end":"14.10.2026 11:30","zone":"Europe/Moscow"` + none + `},` +
		`{"line":3,"lesson":{"title":"Physics","start":"14.10.2026 12:00","end":"14.10.2026 13:30","zone":"Europe/Moscow"` + none + `,` +
		`"withLine":0,"with":{"title":"Manual","start":"14.10.2026 12:00","end":"14.10.2026 13:30","zone":"Europe/Moscow"` + none + `}],` +
		`"conflictsTruncated":true,"window":null,"canCommit":true}`
	if string(raw) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", raw, want)
	}
}

func TestSchedulePreviewOfICSHasTheWindow(t *testing.T) {
	path := writeFile(t, "calendar.ics", []byte("BEGIN:VCALENDAR"))
	from := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	p := fullPreview(t)
	p.Plan.Window = importplan.Window{From: from, To: from.Add(366 * 24 * time.Hour)}
	uc := &fakeSchedule{preview: p}

	got, err := NewScheduleImportService(uc, disk(), fixedPicker(path), discard()).ChooseFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if uc.previews[0].Source != importplan.SourceICS || got.Source != "ics" {
		t.Fatalf("source = %q / %q, want ics", uc.previews[0].Source, got.Source)
	}
	if got.Window == nil || *got.Window != (ImportWindow{From: "14.10.2026 00:00 UTC", To: "15.10.2027 00:00 UTC"}) {
		t.Fatalf("window = %+v", got.Window)
	}
}

func TestScheduleSourceFromExtension(t *testing.T) {
	for path, want := range map[string]importplan.Source{
		"a.csv": importplan.SourceCSV, "B.ICS": importplan.SourceICS, "c.XlSx": importplan.SourceXLSX,
		"d.tar.csv": importplan.SourceCSV,
	} {
		if got, ok := sourceOf(path); !ok || got != want {
			t.Errorf("sourceOf(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"a.xls", "a.txt", "a", "a.csv.exe", "csv", ".ics.bak"} {
		if got, ok := sourceOf(path); ok {
			t.Errorf("sourceOf(%q) = %q, want refused", path, got)
		}
	}
}

func TestScheduleChooseFileRefusals(t *testing.T) {
	big := writeFile(t, "big.csv", bytes.Repeat([]byte("a"), MaxScheduleFileBytes+1))
	// Never read: the extension is refused first.
	unknown := filepath.Join(t.TempDir(), "secret-folder", "lessons.txt")
	unreadable := filepath.Join(t.TempDir(), "secret-folder", "lessons.csv")

	tests := []struct {
		name   string
		picker FilePicker
		err    error // of the reader
		want   error
		reads  int32
	}{
		{name: "larger than the limit", picker: fixedPicker(big), want: ErrFileTooLarge, reads: 1},
		{name: "unknown extension", picker: fixedPicker(unknown), want: ErrUnsupportedFile},
		{
			name: "unreadable", picker: fixedPicker(unreadable), want: ErrFileUnreadable, reads: 1,
			err: errors.New("userfile: the file cannot be read: open: Access is denied."),
		},
		{
			name:   "dialog failure",
			picker: pickerFunc(func(context.Context) (string, error) { return "", errors.New("no dialog") }),
			want:   ErrImportFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			uc := &fakeSchedule{preview: fullPreview(t)}
			files := &fileReader{err: tt.err}
			_, err := NewScheduleImportService(uc, files, tt.picker, slog.New(slog.NewTextHandler(&logs, nil))).ChooseFile(context.Background())
			if !errors.Is(err, tt.want) || err.Error() != tt.want.Error() {
				t.Fatalf("err = %v, want exactly %v", err, tt.want)
			}
			if p, _ := uc.counts(); p != 0 {
				t.Fatal("the use case was called")
			}
			if got := files.reads.Load(); got != tt.reads {
				t.Fatalf("the file was read %d times, want %d", got, tt.reads)
			}
			if strings.Contains(logs.String(), "secret-folder") {
				t.Fatalf("the path leaked: logs %q", logs.String())
			}
		})
	}
}

func TestScheduleReadsAFileAtTheLimit(t *testing.T) {
	data := bytes.Repeat([]byte("a"), MaxScheduleFileBytes)
	uc := &fakeSchedule{preview: fullPreview(t)}
	files := disk()
	s := NewScheduleImportService(uc, files, fixedPicker(writeFile(t, "full.csv", data)), discard())
	if _, err := s.ChooseFile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files.limit.Load() != MaxScheduleFileBytes {
		t.Fatalf("read limit %d, want %d", files.limit.Load(), MaxScheduleFileBytes)
	}
	if len(uc.previews[0].Data) != MaxScheduleFileBytes {
		t.Fatalf("read %d bytes, want %d", len(uc.previews[0].Data), MaxScheduleFileBytes)
	}
}

func TestScheduleCommitRefusedWithoutAPreview(t *testing.T) {
	uc := &fakeSchedule{preview: fullPreview(t)}
	s := NewScheduleImportService(uc, disk(), fixedPicker(""), discard())
	for _, id := range []string{"", "1", "anything"} {
		if _, err := s.Commit(context.Background(), id); !errors.Is(err, ErrNoPreview) {
			t.Errorf("Commit(%q) = %v, want ErrNoPreview", id, err)
		}
	}
	if _, c := uc.counts(); c != 0 {
		t.Fatal("the use case was called without a preview")
	}
}

func TestScheduleCommitPassesTheSameBytesAndToken(t *testing.T) {
	ctx := context.Background()
	path := writeFile(t, "lessons.csv", []byte(fileContent))
	uc := &fakeSchedule{preview: fullPreview(t), result: schedulesync.CommitResult{Added: 1, Changed: 1, Removed: 1}}
	s := NewScheduleImportService(uc, disk(), fixedPicker(path), discard())

	first, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.SelectionID == second.SelectionID {
		t.Fatalf("both selections are %q", first.SelectionID)
	}
	// The file on disk changes after the preview: the commit uses the previewed bytes.
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Commit(ctx, first.SelectionID); !errors.Is(err, ErrNoPreview) {
		t.Fatalf("an older selection: err = %v, want ErrNoPreview", err)
	}
	res, err := s.Commit(ctx, second.SelectionID)
	if err != nil || res != (ImportResult{Added: 1, Changed: 1, Removed: 1}) {
		t.Fatalf("Commit = %+v, %v", res, err)
	}
	want := schedulesync.CommitRequest{Source: importplan.SourceCSV, Data: []byte(fileContent), ConfirmToken: "tok-1"}
	if len(uc.commits) != 1 || uc.commits[0].Source != want.Source || string(uc.commits[0].Data) != fileContent ||
		uc.commits[0].ConfirmToken != want.ConfirmToken {
		t.Fatalf("CommitImport got %+v, want %+v", uc.commits, want)
	}

	if _, err := s.Commit(ctx, second.SelectionID); !errors.Is(err, ErrNoPreview) {
		t.Fatalf("a second commit of one preview: err = %v, want ErrNoPreview", err)
	}
	if _, c := uc.counts(); c != 1 {
		t.Fatalf("CommitImport called %d times, want 1", c)
	}
}

func TestScheduleNothingToCommit(t *testing.T) {
	withErrors := schedulesync.Preview{Plan: importplan.Plan{
		Source: importplan.SourceCSV,
		Errors: []importplan.RowError{{Line: 3, Field: "start", Reason: "not a time"}, {Reason: "the file contains no lessons"}},
	}}
	unchanged := schedulesync.Preview{ConfirmToken: "tok", Plan: importplan.Plan{
		Source: importplan.SourceCSV, Unchanged: []importplan.Change{{Line: 2}}, Fingerprint: "tok",
	}}
	tokenless := fullPreview(t)
	tokenless.ConfirmToken = ""

	for name, p := range map[string]schedulesync.Preview{"row errors": withErrors, "no changes": unchanged, "no token": tokenless} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeSchedule{preview: p}
			s := NewScheduleImportService(uc, disk(), fixedPicker(writeFile(t, "a.csv", []byte(fileContent))), discard())
			got, err := s.ChooseFile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.CanCommit {
				t.Fatal("canCommit is set")
			}
			if _, err := s.Commit(context.Background(), got.SelectionID); !errors.Is(err, ErrNoPreview) {
				t.Fatalf("Commit = %v, want ErrNoPreview", err)
			}
			if _, c := uc.counts(); c != 0 {
				t.Fatal("the use case was called")
			}
		})
	}

	got := previewView("a.csv", importplan.SourceCSV, withErrors)
	wantErrs := []ImportRowError{{Line: 3, Field: "start", Reason: "not a time"}, {Reason: "the file contains no lessons"}}
	if fmt.Sprint(got.Errors) != fmt.Sprint(wantErrs) {
		t.Fatalf("errors = %+v, want %+v", got.Errors, wantErrs)
	}
}

func TestScheduleCommitErrors(t *testing.T) {
	secret := "Secret Seminar " + filepath.Join("C:", "Users", "student", "lessons.csv")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "stale preview", err: fmt.Errorf("commit: %w", schedulesync.ErrStalePreview), want: ErrStaleImport},
		{name: "row errors now", err: fmt.Errorf("%w: 1 row errors", schedulesync.ErrInvalidImport), want: ErrImportInvalid},
		{name: "not confirmed", err: schedulesync.ErrNotConfirmed, want: ErrNoPreview},
		{name: "unreadable", err: fmt.Errorf("parse: %w: %s", importplan.ErrUnreadableFile, secret), want: ErrNotSchedule},
		{name: "anything else", err: errors.New("database is locked: " + secret), want: ErrImportFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var logs bytes.Buffer
			uc := &fakeSchedule{preview: fullPreview(t), commitErr: tt.err}
			s := NewScheduleImportService(uc, disk(), fixedPicker(writeFile(t, "a.csv", []byte(fileContent))),
				slog.New(slog.NewTextHandler(&logs, nil)))
			p, err := s.ChooseFile(ctx)
			if err != nil {
				t.Fatal(err)
			}

			_, err = s.Commit(ctx, p.SelectionID)
			if !errors.Is(err, tt.want) || err.Error() != tt.want.Error() {
				t.Fatalf("err = %v, want exactly %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "Secret") || strings.Contains(err.Error(), "student") {
				t.Fatalf("the error leaks details: %q", err)
			}
			if tt.want == ErrNotSchedule && strings.Contains(logs.String(), "Secret") {
				t.Fatalf("the parser message was logged: %q", logs.String())
			}
			// The selection is gone after any commit: the page chooses the file again.
			if _, err := s.Commit(ctx, p.SelectionID); !errors.Is(err, ErrNoPreview) {
				t.Fatalf("retry: err = %v, want ErrNoPreview", err)
			}
		})
	}
}

func TestSchedulePreviewErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "unreadable", err: fmt.Errorf("parse: %w: column \"Secret\" appears twice", importplan.ErrUnreadableFile), want: ErrNotSchedule},
		{name: "anything else", err: errors.New("disk I/O error"), want: ErrImportFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			uc := &fakeSchedule{previewErr: tt.err}
			s := NewScheduleImportService(uc, disk(), fixedPicker(writeFile(t, "a.xlsx", []byte("PK"))), slog.New(slog.NewTextHandler(&logs, nil)))
			_, err := s.ChooseFile(context.Background())
			if !errors.Is(err, tt.want) || err.Error() != tt.want.Error() {
				t.Fatalf("err = %v, want exactly %v", err, tt.want)
			}
			if strings.Contains(logs.String(), "Secret") {
				t.Fatalf("file content was logged: %q", logs.String())
			}
			if _, err := s.Commit(context.Background(), "1"); !errors.Is(err, ErrNoPreview) {
				t.Fatalf("a failed preview left a selection: %v", err)
			}
		})
	}
}

func TestScheduleCancelKeepsTheSelection(t *testing.T) {
	ctx := context.Background()
	path := writeFile(t, "a.csv", []byte(fileContent))
	var choose string
	picker := pickerFunc(func(context.Context) (string, error) { return choose, nil })
	uc := &fakeSchedule{preview: fullPreview(t)}
	s := NewScheduleImportService(uc, disk(), picker, discard())

	choose = path
	p, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	choose = ""
	got, err := s.ChooseFile(ctx)
	if err != nil || !got.Cancelled || got.SelectionID != "" {
		t.Fatalf("cancelled dialog = %+v, %v", got, err)
	}
	if _, err := s.Commit(ctx, p.SelectionID); err != nil {
		t.Fatalf("the earlier selection was lost: %v", err)
	}
}

func TestScheduleAFailedChoiceDropsTheSelection(t *testing.T) {
	good := writeFile(t, "a.csv", []byte(fileContent))
	tests := []struct {
		name string
		path string
		err  error // of the dialog
		want error
	}{
		{name: "unsupported file", path: "lessons.pdf", want: ErrUnsupportedFile},
		{name: "dialog failure", err: errors.New("no dialog"), want: ErrImportFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			choose, fail := good, error(nil)
			picker := pickerFunc(func(context.Context) (string, error) { return choose, fail })
			s := NewScheduleImportService(&fakeSchedule{preview: fullPreview(t)}, disk(), picker, discard())

			p, err := s.ChooseFile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			choose, fail = tt.path, tt.err
			if _, err := s.ChooseFile(ctx); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			// The page drops its preview on any error, so the core drops the selection too.
			if _, err := s.Commit(ctx, p.SelectionID); !errors.Is(err, ErrNoPreview) {
				t.Fatalf("the replaced selection is still committable: %v", err)
			}
		})
	}
}

func TestScheduleDiscard(t *testing.T) {
	ctx := context.Background()
	uc := &fakeSchedule{preview: fullPreview(t)}
	s := NewScheduleImportService(uc, disk(), fixedPicker(writeFile(t, "a.csv", []byte(fileContent))), discard())
	p, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Discard("other")
	s.Discard(p.SelectionID)
	if _, err := s.Commit(ctx, p.SelectionID); !errors.Is(err, ErrNoPreview) {
		t.Fatalf("Commit after Discard = %v", err)
	}
	if _, c := uc.counts(); c != 0 {
		t.Fatal("the use case was called")
	}
}

func TestScheduleOneDialogAtATime(t *testing.T) {
	opened, release := make(chan struct{}), make(chan struct{})
	picker := pickerFunc(func(context.Context) (string, error) {
		close(opened)
		<-release
		return "", nil
	})
	s := NewScheduleImportService(&fakeSchedule{}, disk(), picker, discard())

	done := make(chan error)
	go func() {
		_, err := s.ChooseFile(context.Background())
		done <- err
	}()
	<-opened
	if _, err := s.ChooseFile(context.Background()); !errors.Is(err, ErrImportBusy) {
		t.Fatalf("second dialog: err = %v, want ErrImportBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Concurrent commits of one preview write it once; concurrent choices and commits are race-free.
func TestScheduleConcurrentUse(t *testing.T) {
	ctx := context.Background()
	path := writeFile(t, "a.csv", []byte(fileContent))
	uc := &fakeSchedule{preview: fullPreview(t)}
	s := NewScheduleImportService(uc, disk(), fixedPicker(path), discard())
	p, err := s.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = s.Commit(ctx, p.SelectionID) })
	}
	wg.Wait()
	if _, c := uc.counts(); c != 1 {
		t.Fatalf("one preview committed %d times", c)
	}

	for i := range 8 {
		wg.Go(func() {
			if i%2 == 0 {
				_, _ = s.ChooseFile(ctx)
			} else {
				_, _ = s.Commit(ctx, fmt.Sprint(i))
				s.Discard(fmt.Sprint(i))
			}
		})
	}
	wg.Wait()
}

// The page may match the error messages, so the frontend wrapper lists each one.
func TestFrontendKnowsScheduleErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/schedule.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrImportBusy, ErrUnsupportedFile, ErrFileTooLarge, ErrFileUnreadable, ErrNotSchedule, ErrNoPreview,
		ErrStaleImport, ErrImportInvalid, ErrImportFailed,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/schedule.ts does not list the error %q", e)
		}
	}
}
