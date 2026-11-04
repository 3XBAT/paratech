package e2ehooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Names of the open dialogs: the "dialog" of a picks.jsonl line.
const (
	DialogSchedule  = "schedule"  // «Импорт расписания»
	DialogModel     = "model"     // the model import of the models page
	DialogMaterial  = "material"  // a file of the library
	DialogNotes     = "notes"     // «Мой конспект», one file or several
	DialogRecording = "recording" // «Импортировать запись лекции»
)

// Kinds of the opened.jsonl lines.
const (
	KindLink      = "link"      // a web page (models page, help screens)
	KindFile      = "file"      // «Открыть в Word / PowerPoint»: the artifact reference
	KindCall      = "call"      // «Подключиться»: the call link of a subject
	KindClipboard = "clipboard" // «Скопировать»: the text
)

// Files and folders of the control dir.
const (
	picksFile    = "picks.jsonl"
	consumedFile = "consumed.jsonl"
	openedFile   = "opened.jsonl"
	fixturesDir  = "fixtures"
	savedDir     = "saved"
)

// Operation names of the log lines.
const (
	opPick   = "e2e: open dialog"
	opSave   = "e2e: save dialog"
	opRecord = "e2e: record"
)

// Hooks are the dialogs, openers and clipboard of one E2E run. Create them with New; safe for concurrent use.
type Hooks struct {
	dir string
	log *slog.Logger
	now func() time.Time

	// mu serializes the pick queue (read the index, use the line, record it) and the appends.
	mu sync.Mutex
}

// New returns the hooks of the control dir (absolute; config checks it). A nil log discards.
func New(dir string, log *slog.Logger) *Hooks {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hooks{dir: dir, log: log, now: time.Now}
}

// OpenLink records a web page instead of opening it (wails.LinkOpener).
func (h *Hooks) OpenLink(_ context.Context, u values.SourceURL) error {
	return h.record(KindLink, u.String())
}

// OpenArtifact records a library file instead of starting its program (wails.FileOpener).
func (h *Hooks) OpenArtifact(_ context.Context, ref shared.ArtifactRef) error {
	return h.record(KindFile, ref.String())
}

// OpenSubjectCallLink records the call link of a subject instead of opening it (library.CallOpener).
func (h *Hooks) OpenSubjectCallLink(_ context.Context, l subject.CallLink) error {
	return h.record(KindCall, l.String())
}

// SetText records a clipboard text instead of copying it (wails.Clipboard).
func (h *Hooks) SetText(_ context.Context, text string) error {
	return h.record(KindClipboard, text)
}

// openedLine is a line of opened.jsonl.
type openedLine struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	At    string `json:"at"`
}

func (h *Hooks) record(kind, value string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	line := openedLine{Kind: kind, Value: value, At: h.now().UTC().Format(time.RFC3339)}
	if err := h.appendJSON(openedFile, line); err != nil {
		h.log.Error(opRecord, "kind", kind, "err", err)
		return fmt.Errorf("e2ehooks: record %s: %w", kind, err)
	}
	h.log.Info(opRecord, "kind", kind)
	return nil
}

// errNotRegular means a record or queue file is a link, a folder or another non-regular file.
var errNotRegular = errors.New("not a regular file")

// appendJSON appends v as one line to the file name of the control dir, in one write of a file opened for append.
func (h *Hooks) appendJSON(name string, v any) error {
	if !filepath.IsAbs(h.dir) {
		return errDirNotAbsolute
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	path := filepath.Join(h.dir, name)
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", name, errNotRegular)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600) //nolint:gosec // G304: a fixed name in the e2e dir
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	return errors.Join(werr, f.Close())
}

// readSmall reads the file name of the control dir, at most limit bytes; a missing file reads as nil.
func (h *Hooks) readSmall(name string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(h.dir) {
		return nil, errDirNotAbsolute
	}
	path := filepath.Join(h.dir, name)
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	f, err := os.Open(path) //nolint:gosec // G304: a fixed name in the e2e dir
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err := errors.Join(err, f.Close()); err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", name, limit)
	}
	return b, nil
}
