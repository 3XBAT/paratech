package wails

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// MaxScheduleFileBytes is the largest schedule file the page reads: the limit of the csv, ics and xlsx parsers.
const MaxScheduleFileBytes = 2 << 20

// Errors of the schedule import page. The page sees only these messages (frontend/src/bindings/schedule.ts matches
// them); causes are logged without the file content or its path.
var (
	ErrImportBusy      = errors.New("a file is already being chosen")
	ErrUnsupportedFile = errors.New("only .ics, .csv and .xlsx files can be imported")
	ErrFileTooLarge    = errors.New("the file is too large")
	ErrFileUnreadable  = errors.New("the file cannot be read")
	ErrNotSchedule     = errors.New("the file is not a schedule in a supported format")
	ErrNoPreview       = errors.New("there is no preview to apply")
	ErrStaleImport     = errors.New("the schedule or the file changed, choose the file again")
	ErrImportInvalid   = errors.New("the file has errors, nothing was written")
	ErrImportFailed    = errors.New("the schedule import failed")
)

// Layouts of the preview times: lesson times in the lesson's zone, the import window in UTC.
const (
	lessonTimeLayout = "02.01.2006 15:04"
	windowTimeLayout = "02.01.2006 15:04 UTC"
)

// ScheduleImportUseCase is the import part of the schedule use case (ARCH-42).
type ScheduleImportUseCase interface {
	PreviewImport(ctx context.Context, req schedulesync.ImportRequest) (schedulesync.Preview, error)
	CommitImport(ctx context.Context, req schedulesync.CommitRequest) (schedulesync.CommitResult, error)
}

// ScheduleFileReader reads the file the user chose: a regular file only, and at most maxBytes+1 bytes, so a file over
// the limit is seen without reading it whole. Its errors must not contain the path (the app wires adapters/userfile).
type ScheduleFileReader interface {
	ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error)
}

// FilePicker shows the native open dialog for a schedule file. It returns the chosen path, or "" when the user
// cancels. The shell implements it on Windows; the page never sends a path.
type FilePicker interface {
	PickScheduleFile(ctx context.Context) (string, error)
}

// pickerFunc adapts a function to FilePicker.
type pickerFunc func(ctx context.Context) (string, error)

// PickScheduleFile implements FilePicker.
func (f pickerFunc) PickScheduleFile(ctx context.Context) (string, error) { return f(ctx) }

// LessonText is a lesson as the preview shows it. Title is file text: render it as plain text only.
type LessonText struct {
	Title string `json:"title"`
	Start string `json:"start"` // "02.01.2006 15:04" in the lesson's zone
	End   string `json:"end"`
	Zone  string `json:"zone"`
}

// ImportLesson is an added lesson (Line is its line in the file) or a removed one (Line 0).
type ImportLesson struct {
	Line int `json:"line"`
	LessonText
}

// ImportChange is a lesson the file changes.
type ImportChange struct {
	Line   int        `json:"line"`
	Before LessonText `json:"before"`
	After  LessonText `json:"after"`
}

// ImportConflict is a time overlap: with another line of the file (WithLine > 0) or with a lesson the import keeps.
type ImportConflict struct {
	Line     int        `json:"line"`
	Lesson   LessonText `json:"lesson"`
	WithLine int        `json:"withLine"`
	With     LessonText `json:"with"`
}

// ImportRowError is a problem of one line; Line 0 is a problem of the whole file.
type ImportRowError struct {
	Line   int    `json:"line"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ImportWindow is the time range an ICS import covers; only lessons starting in it are added or removed.
type ImportWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ImportPreview is the JSON form of a chosen file's preview. Cancelled means the user closed the dialog; then every
// other field is empty.
type ImportPreview struct {
	Cancelled          bool             `json:"cancelled"`
	SelectionID        string           `json:"selectionId"`
	FileName           string           `json:"fileName"` // base name only
	Source             string           `json:"source"`   // "csv", "ics" or "xlsx"
	Errors             []ImportRowError `json:"errors"`
	Added              []ImportLesson   `json:"added"`
	Changed            []ImportChange   `json:"changed"`
	Removed            []ImportLesson   `json:"removed"`
	UnchangedCount     int              `json:"unchangedCount"`
	Conflicts          []ImportConflict `json:"conflicts"`
	ConflictsTruncated bool             `json:"conflictsTruncated"`
	Window             *ImportWindow    `json:"window"` // null for CSV and XLSX
	CanCommit          bool             `json:"canCommit"`
}

// ImportResult counts what a commit wrote.
type ImportResult struct {
	Added   int `json:"added"`
	Changed int `json:"changed"`
	Removed int `json:"removed"`
}

// selection is the previewed file the page may commit: the same bytes go to the commit, so the file on disk may
// change meanwhile without effect.
type selection struct {
	id        string
	source    importplan.Source
	data      []byte
	token     string
	canCommit bool
}

// ScheduleImportService is bound to the schedule page like SettingsService
// ("<package path>.ScheduleImportService.<Method>"). It keeps one pending selection: a new file replaces it, and a
// commit or a discard ends it.
type ScheduleImportService struct {
	uc     ScheduleImportUseCase
	files  ScheduleFileReader
	picker FilePicker
	log    *slog.Logger

	mu       sync.Mutex
	choosing bool
	lastID   uint64
	pending  *selection
}

// NewScheduleImportService returns the binding of the schedule page.
func NewScheduleImportService(
	uc ScheduleImportUseCase, files ScheduleFileReader, picker FilePicker, log *slog.Logger,
) *ScheduleImportService {
	return &ScheduleImportService{uc: uc, files: files, picker: picker, log: log}
}

// ChooseFile shows the open dialog, reads the chosen file and returns its preview (PRD section 11.2). Nothing is
// written. A cancelled dialog is a result with cancelled set and keeps the current selection; any error after the
// dialog opens drops it, as the page drops its preview then.
func (s *ScheduleImportService) ChooseFile(ctx context.Context) (ImportPreview, error) {
	if !s.beginChoose() {
		return ImportPreview{}, ErrImportBusy
	}
	defer s.endChoose()

	path, err := s.picker.PickScheduleFile(ctx)
	if err != nil {
		s.setPending(nil)
		s.log.Error("ui: choose schedule file", "err", err)
		return ImportPreview{}, ErrImportFailed
	}
	if path == "" {
		return ImportPreview{Cancelled: true}, nil
	}

	s.setPending(nil) // the page shows the new file, or its error, instead of the old preview
	src, ok := sourceOf(path)
	if !ok {
		return ImportPreview{}, ErrUnsupportedFile
	}
	data, err := s.files.ReadFile(ctx, path, MaxScheduleFileBytes)
	if err != nil {
		s.log.Error("ui: read schedule file", "err", err)
		return ImportPreview{}, ErrFileUnreadable
	}
	if len(data) > MaxScheduleFileBytes {
		return ImportPreview{}, ErrFileTooLarge
	}

	p, err := s.uc.PreviewImport(ctx, schedulesync.ImportRequest{Source: src, Data: data})
	if err != nil {
		return ImportPreview{}, s.importError("preview schedule import", src, err)
	}
	view := previewView(filepath.Base(path), src, p)
	view.SelectionID = s.hold(src, data, p.ConfirmToken, view.CanCommit)
	return view, nil
}

// Commit writes the pending selection selectionID (PRD section 11.2, AC-16). It is refused without calling the use
// case when that selection is not the pending one or cannot be committed. Afterwards the selection is gone, whatever
// the outcome: the page chooses the file again.
func (s *ScheduleImportService) Commit(ctx context.Context, selectionID string) (ImportResult, error) {
	sel := s.take(selectionID)
	if sel == nil {
		return ImportResult{}, ErrNoPreview
	}
	res, err := s.uc.CommitImport(ctx, schedulesync.CommitRequest{Source: sel.source, Data: sel.data, ConfirmToken: sel.token})
	if err != nil {
		return ImportResult{}, s.importError("commit schedule import", sel.source, err)
	}
	return ImportResult{Added: res.Added, Changed: res.Changed, Removed: res.Removed}, nil
}

// Discard drops the pending selection selectionID; another id is ignored.
func (s *ScheduleImportService) Discard(selectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending != nil && s.pending.id == selectionID {
		s.pending = nil
	}
}

func (s *ScheduleImportService) beginChoose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.choosing {
		return false
	}
	s.choosing = true
	return true
}

func (s *ScheduleImportService) endChoose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.choosing = false
}

func (s *ScheduleImportService) setPending(sel *selection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = sel
}

// hold makes the previewed file the pending selection and returns its id: a counter, unique in the process.
func (s *ScheduleImportService) hold(src importplan.Source, data []byte, token string, canCommit bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastID++
	id := strconv.FormatUint(s.lastID, 10)
	s.pending = &selection{id: id, source: src, data: data, token: token, canCommit: canCommit}
	return id
}

// take removes and returns the pending selection id when it can be committed, so one preview is committed at most
// once; otherwise it returns nil and keeps the selection.
func (s *ScheduleImportService) take(id string) *selection {
	s.mu.Lock()
	defer s.mu.Unlock()

	sel := s.pending
	if sel == nil || id == "" || sel.id != id || !sel.canCommit || sel.token == "" {
		return nil
	}
	s.pending = nil
	return sel
}

func (s *ScheduleImportService) importError(op string, src importplan.Source, err error) error {
	switch {
	case errors.Is(err, schedulesync.ErrStalePreview):
		s.log.Info("ui: "+op+": the preview is out of date", "source", string(src))
		return ErrStaleImport
	case errors.Is(err, schedulesync.ErrInvalidImport):
		return ErrImportInvalid
	case errors.Is(err, schedulesync.ErrNotConfirmed):
		return ErrNoPreview
	case errors.Is(err, importplan.ErrUnreadableFile):
		// The parser's message may quote header names of the file, so it is not logged.
		s.log.Info("ui: "+op+": the file is not a readable schedule", "source", string(src))
		return ErrNotSchedule
	}
	s.log.Error("ui: "+op, "source", string(src), "err", err)
	return ErrImportFailed
}

// sourceOf picks the import format from the file extension.
func sourceOf(path string) (importplan.Source, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return importplan.SourceCSV, true
	case ".ics":
		return importplan.SourceICS, true
	case ".xlsx":
		return importplan.SourceXLSX, true
	}
	return "", false
}

func previewView(name string, src importplan.Source, p schedulesync.Preview) ImportPreview {
	plan := p.Plan
	out := ImportPreview{
		FileName: name, Source: string(src), UnchangedCount: len(plan.Unchanged),
		ConflictsTruncated: plan.ConflictsTruncated,
		CanCommit:          p.ConfirmToken != "" && plan.Valid() && plan.HasChanges(),
		Errors:             make([]ImportRowError, 0, len(plan.Errors)),
		Added:              make([]ImportLesson, 0, len(plan.Added)),
		Changed:            make([]ImportChange, 0, len(plan.Changed)),
		Removed:            make([]ImportLesson, 0, len(plan.Removed)),
		Conflicts:          make([]ImportConflict, 0, len(plan.Conflicts)),
	}
	for _, e := range plan.Errors {
		out.Errors = append(out.Errors, ImportRowError{Line: e.Line, Field: e.Field, Reason: e.Reason})
	}
	for _, a := range plan.Added {
		out.Added = append(out.Added, ImportLesson{Line: a.Line, LessonText: lessonText(a.Content)})
	}
	for _, c := range plan.Changed {
		out.Changed = append(out.Changed, ImportChange{Line: c.Line, Before: lessonText(c.Before), After: lessonText(c.After)})
	}
	for _, r := range plan.Removed {
		out.Removed = append(out.Removed, ImportLesson{LessonText: lessonText(r.Content)})
	}
	for _, c := range plan.Conflicts {
		out.Conflicts = append(out.Conflicts, ImportConflict{
			Line: c.Line, Lesson: lessonText(c.Content), WithLine: c.WithLine, With: lessonText(c.WithContent),
		})
	}
	if w := plan.Window; !w.IsZero() {
		out.Window = &ImportWindow{From: windowText(w.From), To: windowText(w.To)}
	}
	return out
}

func lessonText(c lesson.Content) LessonText {
	loc := zoneOf(c.TimeZone)
	return LessonText{Title: c.Title, Start: lessonTime(c.Start, loc), End: lessonTime(c.End, loc), Zone: c.TimeZone}
}

func lessonTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(lessonTimeLayout)
}

func windowText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(windowTimeLayout)
}
