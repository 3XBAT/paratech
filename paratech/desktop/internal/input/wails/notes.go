package wails

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// Errors of the student's own notes (the page shows them as they are). Causes are logged, never shown; neither paths
// nor file names nor texts are logged. The errors of the library a file can meet (empty, too large, unsupported, a
// subject that is gone) are shown with the library's texts.
const (
	ErrNotesInvalidID     = libraryError("Неверный идентификатор: обновите страницу.")
	ErrNotesInvalidDate   = libraryError("Неверная дата.")
	ErrNotesNotFound      = libraryError("Конспект не найден: обновите страницу.")
	ErrNotesRecording     = libraryError("Эта запись ещё не готова или удаляется: прикрепить к ней конспект нельзя.")
	ErrNotesOtherSubject  = libraryError("Этот файл уже лежит в другом предмете.")
	ErrNotesRemoved       = libraryError("Конспект удалён: сначала восстановите его.")
	ErrNotesEmptyTitle    = libraryError("Название конспекта не может быть пустым.")
	ErrNotesNoFiles       = libraryError("Не выбрано ни одного файла.")
	ErrNotesTooManyParts  = libraryError("В конспекте может быть не больше 200 частей.")
	ErrNotesAlreadyAdded  = libraryError("Этот файл уже добавлен.")
	ErrNotesPartNotFound  = libraryError("Этой части нет в конспекте: обновите страницу.")
	ErrNotesInvalidOrder  = libraryError("Порядок частей устарел: обновите страницу.")
	ErrNotesEmptyText     = libraryError("Текст пустой.")
	ErrNotesTextTooLarge  = libraryError("Текст больше 1 МБ.")
	ErrNotesInvalidText   = libraryError("Это не похоже на обычный текст.")
	ErrNotesChanged       = libraryError("Предмет или файл изменился: обновите страницу.")
	ErrNotesUnavailable   = libraryError("Не удалось прочитать конспекты. Попробуйте ещё раз.")
	ErrNotesTooManyPicked = libraryError("За раз можно добавить не больше 200 файлов.")

	// Photos of handwriting (TASK-165); the limits are material.MaxImageBytes and material.MaxImagePixels.
	ErrNotesImageTooLarge   = libraryError("Фото больше 25 МБ или 40 мегапикселей.")
	ErrNotesImageFormat     = libraryError("Фото подходят только в JPEG или PNG: HEIC, GIF, WebP, BMP, TIFF и SVG не подходят.")
	ErrNotesImageUnreadable = libraryError("Не удалось прочитать фото: файл повреждён или обрезан.")
)

// Operation names of the log.
const (
	opNotesPick      = "ui: choose files for notes"
	opNotesRead      = "ui: read a file for notes"
	opNotesAdd       = "ui: add files to notes"
	opNotesPaste     = "ui: paste text into notes"
	opNotesReplace   = "ui: replace a part of notes"
	opNotesUndo      = "ui: undo the replace of a part of notes"
	opNotesReorder   = "ui: reorder notes"
	opNotesReanchor  = "ui: move notes to another lecture"
	opNotesRetitle   = "ui: rename notes"
	opNotesRemove    = "ui: remove notes"
	opNotesRestore   = "ui: restore notes"
	opNotesForLesson = "ui: notes of a lecture"
	opNotesSubject   = "ui: notes of a subject"
	opNotesDrop      = "ui: file dropped on a lecture refused"
)

// NotesUseCase is the use case of the notes (ARCH-42); satisfied by notes.Service.
type NotesUseCase interface {
	Add(ctx context.Context, req notes.AddRequest) (notes.Added, error)
	Paste(ctx context.Context, req notes.PasteRequest) (notes.Added, error)
	ReplacePart(ctx context.Context, id shared.NoteID, old shared.MaterialID, up notes.Upload) (notes.Replaced, error)
	ReplacePartWith(ctx context.Context, id shared.NoteID, old, next shared.MaterialID) (notes.Replaced, error)
	Reorder(ctx context.Context, id shared.NoteID, order []shared.MaterialID) (notes.View, error)
	Reanchor(ctx context.Context, id shared.NoteID, ref notes.AnchorRef) (notes.View, error)
	Retitle(ctx context.Context, id shared.NoteID, title string) (notes.View, error)
	Remove(ctx context.Context, id shared.NoteID) (notes.View, error)
	Restore(ctx context.Context, id shared.NoteID) (notes.View, error)
	ForLesson(ctx context.Context, l notes.Lecture) ([]notes.View, error)
	ForSubject(ctx context.Context, sub shared.SubjectID) ([]notes.View, error)
	PageImage(ctx context.Context, id shared.MaterialID) (notes.Page, error)
}

// NoteFilePicker shows the native open dialog for files of notes: several when multiple, else one. It returns the
// chosen paths, none when the user cancels. The paths never come from the page or a model and never go back to it.
type NoteFilePicker interface {
	PickNoteFiles(ctx context.Context, multiple bool) ([]string, error)
}

// notesPickerFunc adapts a function to NoteFilePicker.
type notesPickerFunc func(ctx context.Context, multiple bool) ([]string, error)

// PickNoteFiles implements NoteFilePicker.
func (f notesPickerFunc) PickNoteFiles(ctx context.Context, multiple bool) ([]string, error) {
	return f(ctx, multiple)
}

// NoteTarget is the lecture files go to, as the page names it. With NoteID the files go to that note and the rest is
// ignored. Kind is "recording" (RecordingID), "lesson" (LessonID), "day" (Date, "YYYY-MM-DD") or "undated"; SubjectID
// is the subject of the notes.
type NoteTarget struct {
	NoteID      string `json:"noteId"`
	SubjectID   string `json:"subjectId"`
	Kind        string `json:"kind"`
	RecordingID string `json:"recordingId"`
	LessonID    string `json:"lessonId"`
	Date        string `json:"date"`
}

// NoteLecture names the lecture whose notes ForLesson lists: RecordingID, LessonID, or SubjectID with Date.
type NoteLecture struct {
	RecordingID string `json:"recordingId"`
	LessonID    string `json:"lessonId"`
	SubjectID   string `json:"subjectId"`
	Date        string `json:"date"`
}

// NoteAnchor is the lecture of a note as it reads now. Detached: it was attached to a recording or a lesson that is
// gone, so it reads as the day of its snapshot. LessonTitle is imported text: plain text only.
type NoteAnchor struct {
	Kind        string `json:"kind"`
	RecordingID string `json:"recordingId"`
	LessonID    string `json:"lessonId"`
	Date        string `json:"date"`
	LessonKind  string `json:"lessonKind"`
	LessonTitle string `json:"lessonTitle"`
	Detached    bool   `json:"detached"`
}

// NotePart is a part of a note: a file of the library. Name is the file name (plain text). Missing: the file is no
// longer in the library; Removed: it was removed there. A photo ("jpeg", "png", textStatus "no_text": «текст не
// распознан») has PageURL, the address its image is shown from by the material id only; "" for any other part.
type NotePart struct {
	MaterialID string `json:"materialId"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	TextStatus string `json:"textStatus"`
	Removed    bool   `json:"removed"`
	Missing    bool   `json:"missing"`
	PageURL    string `json:"pageUrl"`
}

// NoteView is a note of the student. Title is typed text or a file name: plain text only. Times are RFC 3339 in UTC.
// NoTextPages counts the photo pages without text («N стр. без текста»).
type NoteView struct {
	ID          string     `json:"id"`
	SubjectID   string     `json:"subjectId"`
	Title       string     `json:"title"`
	Anchor      NoteAnchor `json:"anchor"`
	Parts       []NotePart `json:"parts"`
	NoTextPages int        `json:"noTextPages"`
	Removed     bool       `json:"removed"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}

// NoteFile is the outcome of one file: MaterialID when it is in the library, or Error, the page's text of the refusal.
// Existing: the same file was in the library already; AlreadyAdded: it is a part of the note already («уже
// добавлено»), no second part was made. Name is the file name (plain text), "" for a dropped path that was refused.
type NoteFile struct {
	Name         string `json:"name"`
	MaterialID   string `json:"materialId"`
	Existing     bool   `json:"existing"`
	AlreadyAdded bool   `json:"alreadyAdded"`
	Error        string `json:"error"`
}

// NotesAdd is the outcome of AddFiles, Paste and a drop on a lecture. Cancelled: the user closed the dialog. Note is
// the note the files went to, null when none could be added; Created: it is new. Files has one entry per file, in
// order. Error is the refusal of the whole drop (the methods return it as their error instead); Target echoes the
// lecture of a drop.
type NotesAdd struct {
	Cancelled bool       `json:"cancelled"`
	Created   bool       `json:"created"`
	Note      *NoteView  `json:"note"`
	Files     []NoteFile `json:"files"`
	Error     string     `json:"error"`
	Target    NoteTarget `json:"target"`
}

// NotesReplace is the outcome of ReplaceFile: the note, the new part and the one it replaced (for UndoReplace).
type NotesReplace struct {
	Cancelled  bool      `json:"cancelled"`
	Note       *NoteView `json:"note"`
	MaterialID string    `json:"materialId"`
	PreviousID string    `json:"previousId"`
	Existing   bool      `json:"existing"`
}

// NotesService is bound to the UI («Мой конспект», TASK-151): every exported method is callable from the page by its
// full name "<package path>.NotesService.<Method>". Nothing here calls the network; a file reaches the notes only
// through the native open dialog or a drop on a lecture, and no path goes back to the page.
type NotesService struct {
	uc     NotesUseCase
	picker NoteFilePicker
	files  MaterialFileReader
	log    *slog.Logger
	drop   sync.Mutex // one drop at a time, so concurrent drops never hold many files in memory
}

// NewNotesService returns the binding of the notes; all arguments are required.
func NewNotesService(uc NotesUseCase, picker NoteFilePicker, files MaterialFileReader, log *slog.Logger) *NotesService {
	return &NotesService{uc: uc, picker: picker, files: files, log: log}
}

// AddFiles asks in the native open dialog for files (several at once) and adds them to the notes of the lecture, one
// file at a time. Only the base name of a file is kept, for display.
func (s *NotesService) AddFiles(ctx context.Context, target NoteTarget) (NotesAdd, error) {
	if _, err := target.anchor(); err != nil {
		return NotesAdd{}, err
	}
	paths, err := s.picker.PickNoteFiles(ctx, true)
	switch {
	case err != nil:
		s.log.Warn(opNotesPick, "err", err)
		return NotesAdd{}, ErrLibraryFileUnreadable
	case len(paths) == 0:
		return NotesAdd{Cancelled: true, Files: []NoteFile{}}, nil
	case len(paths) > note.MaxParts:
		return NotesAdd{}, ErrNotesTooManyPicked
	}
	out, err := s.addPaths(ctx, target, paths, nil)
	if err != nil {
		return NotesAdd{}, err
	}
	return out, nil
}

// Paste adds the text as a TXT file of the notes of the lecture; the same text again is «уже добавлено».
func (s *NotesService) Paste(ctx context.Context, target NoteTarget, text string) (NotesAdd, error) {
	ref, err := target.anchor()
	if err != nil {
		return NotesAdd{}, err
	}
	res, err := s.uc.Paste(ctx, notes.PasteRequest{
		Note: shared.NoteID(target.NoteID), Anchor: ref, Subject: shared.SubjectID(target.SubjectID), Text: text,
	})
	if err != nil {
		return NotesAdd{}, s.fail(opNotesPaste, err)
	}
	out := NotesAdd{Created: res.Created, Note: noteViewOf(res.View), Files: []NoteFile{}}
	for _, p := range res.Parts {
		out.Files = append(out.Files, s.noteFile(opNotesPaste, p))
	}
	return out, nil
}

// ReplaceFile asks in the native open dialog for one file and puts it in the place of the part materialID. The old
// file stays in the library: UndoReplace puts it back.
func (s *NotesService) ReplaceFile(ctx context.Context, noteID, materialID string) (NotesReplace, error) {
	paths, err := s.picker.PickNoteFiles(ctx, false)
	switch {
	case err != nil:
		s.log.Warn(opNotesPick, "err", err)
		return NotesReplace{}, ErrLibraryFileUnreadable
	case len(paths) == 0:
		return NotesReplace{Cancelled: true}, nil
	}
	up, err := s.read(ctx, paths[0])
	if err != nil {
		return NotesReplace{}, err
	}
	res, err := s.uc.ReplacePart(ctx, shared.NoteID(noteID), shared.MaterialID(materialID), up)
	if err != nil {
		return NotesReplace{}, s.fail(opNotesReplace, err)
	}
	return notesReplace(res), nil
}

// UndoReplace puts the part previousID back in the place of currentID.
func (s *NotesService) UndoReplace(ctx context.Context, noteID, currentID, previousID string) (NotesReplace, error) {
	res, err := s.uc.ReplacePartWith(ctx, shared.NoteID(noteID), shared.MaterialID(currentID), shared.MaterialID(previousID))
	if err != nil {
		return NotesReplace{}, s.fail(opNotesUndo, err)
	}
	return notesReplace(res), nil
}

// Reorder sets the order of the parts; order holds every part's materialId exactly once.
func (s *NotesService) Reorder(ctx context.Context, noteID string, order []string) (NoteView, error) {
	ids := make([]shared.MaterialID, 0, len(order))
	for _, id := range order {
		ids = append(ids, shared.MaterialID(id))
	}
	return s.view(opNotesReorder)(s.uc.Reorder(ctx, shared.NoteID(noteID), ids))
}

// Reanchor moves the note to another lecture (target's NoteID and SubjectID are not read).
func (s *NotesService) Reanchor(ctx context.Context, noteID string, target NoteTarget) (NoteView, error) {
	ref, err := target.anchor()
	if err != nil {
		return NoteView{}, err
	}
	return s.view(opNotesReanchor)(s.uc.Reanchor(ctx, shared.NoteID(noteID), ref))
}

// Retitle renames the note.
func (s *NotesService) Retitle(ctx context.Context, noteID, title string) (NoteView, error) {
	return s.view(opNotesRetitle)(s.uc.Retitle(ctx, shared.NoteID(noteID), title))
}

// Remove removes the note softly; Restore brings it back. Its files stay in the library.
func (s *NotesService) Remove(ctx context.Context, noteID string) (NoteView, error) {
	return s.view(opNotesRemove)(s.uc.Remove(ctx, shared.NoteID(noteID)))
}

// Restore brings a removed note back.
func (s *NotesService) Restore(ctx context.Context, noteID string) (NoteView, error) {
	return s.view(opNotesRestore)(s.uc.Restore(ctx, shared.NoteID(noteID)))
}

// ForLesson lists the notes of a lecture.
func (s *NotesService) ForLesson(ctx context.Context, lecture NoteLecture) ([]NoteView, error) {
	l := notes.Lecture{
		Recording: shared.RecordingID(lecture.RecordingID), Lesson: shared.LessonID(lecture.LessonID),
		Subject: shared.SubjectID(lecture.SubjectID),
	}
	if lecture.Date != "" {
		d, err := note.ParseDate(lecture.Date)
		if err != nil {
			return nil, ErrNotesInvalidDate
		}
		l.Date = d
	}
	return s.views(opNotesForLesson)(s.uc.ForLesson(ctx, l))
}

// ForSubject lists the notes of a subject by the date of their lecture, undated last.
func (s *NotesService) ForSubject(ctx context.Context, subjectID string) ([]NoteView, error) {
	return s.views(opNotesSubject)(s.uc.ForSubject(ctx, shared.SubjectID(subjectID)))
}

// addPaths reads the files one at a time and adds each to the notes of target; after the first one the rest go to
// the same note. check, if set, refuses a path before it is read (the drop's allow-list). A refusal of the whole
// request (the lecture, the subject) ends it as an error; once a file is in the note, it ends it as that file's error.
func (s *NotesService) addPaths(ctx context.Context, target NoteTarget, paths []string, check func(string) error) (NotesAdd, error) {
	ref, err := target.anchor()
	if err != nil {
		return NotesAdd{}, err
	}
	req := notes.AddRequest{Note: shared.NoteID(target.NoteID), Anchor: ref, Subject: shared.SubjectID(target.SubjectID)}
	out := NotesAdd{Files: make([]NoteFile, 0, len(paths))}
	for _, path := range paths {
		if check != nil {
			if err := check(path); err != nil {
				s.log.Info(opNotesDrop, "result", err.Error())
				f := NoteFile{Error: err.Error()}
				if !errors.Is(err, ErrLibraryDroppedPath) {
					f.Name = baseName(path) // a malformed path gets no name, so no part of a forged path reaches the page
				}
				out.Files = append(out.Files, f)
				continue
			}
		}
		up, err := s.read(ctx, path)
		if err != nil {
			out.Files = append(out.Files, NoteFile{Name: baseName(path), Error: err.Error()})
			continue
		}
		req.Uploads = []notes.Upload{up}
		res, err := s.uc.Add(ctx, req)
		if err != nil && !fileError(err) {
			if out.Note == nil {
				return NotesAdd{}, s.fail(opNotesAdd, err)
			}
			// Earlier files are in the note already: keep their results and stop at this one.
			out.Files = append(out.Files, NoteFile{Name: up.Name, Error: s.fail(opNotesAdd, err).Error()})
			return out, nil
		}
		f := NoteFile{Name: up.Name}
		if len(res.Parts) == 1 {
			f = s.noteFile(opNotesAdd, res.Parts[0])
		} else if err != nil {
			f.Error = s.fail(opNotesAdd, err).Error()
		}
		out.Files = append(out.Files, f)
		if res.View.Note != nil {
			out.Note, out.Created = noteViewOf(res.View), out.Created || res.Created
			req.Note = res.View.Note.ID()
		}
	}
	return out, nil
}

// pickedPaths drops the empty results of an open dialog ("" and "." are filepath.Clean of an empty result).
func pickedPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" && p != "." {
			out = append(out, p)
		}
	}
	return out
}

// fileError reports whether err refuses one file only, so the next file may still be added.
func fileError(err error) bool {
	for _, e := range []error{
		library.ErrEmptyFile, library.ErrFileTooLarge, library.ErrUnsupportedFile, library.ErrMaterialRemoved,
		notes.ErrOtherSubject, notes.ErrMaterialRemoved,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// read reads the file at path for the notes; the error is the page's text.
func (s *NotesService) read(ctx context.Context, path string) (notes.Upload, error) {
	data, err := s.files.ReadFile(ctx, path, material.MaxBytes)
	switch {
	case err != nil:
		s.log.Warn(opNotesRead, "err", err) // the reader's errors never carry the path
		return notes.Upload{}, ErrLibraryFileUnreadable
	case len(data) > material.MaxBytes:
		return notes.Upload{}, ErrLibraryFileTooLarge
	}
	return notes.Upload{Name: baseName(path), Data: data}, nil
}

// noteFile converts the outcome of one file.
func (s *NotesService) noteFile(op string, p notes.PartResult) NoteFile {
	f := NoteFile{Name: p.Name, Existing: p.Existing, AlreadyAdded: p.AlreadyAdded}
	if p.Err != nil {
		f.Error = s.fail(op, p.Err).Error()
		return f
	}
	if p.Material != nil {
		f.MaterialID = string(p.Material.ID())
	}
	return f
}

// anchor reads the lecture of the target; a bad date is ErrNotesInvalidDate.
func (t NoteTarget) anchor() (notes.AnchorRef, error) {
	ref := notes.AnchorRef{
		Kind: note.AnchorKind(t.Kind), Recording: shared.RecordingID(t.RecordingID), Lesson: shared.LessonID(t.LessonID),
	}
	if t.Date != "" {
		d, err := note.ParseDate(t.Date)
		if err != nil {
			return notes.AnchorRef{}, ErrNotesInvalidDate
		}
		ref.Date = d
	}
	return ref, nil
}

// view returns the converter of a use case result to the page's note, failing as op.
func (s *NotesService) view(op string) func(notes.View, error) (NoteView, error) {
	return func(v notes.View, err error) (NoteView, error) {
		if err != nil {
			return NoteView{}, s.fail(op, err)
		}
		return *noteViewOf(v), nil
	}
}

// views is view for a list.
func (s *NotesService) views(op string) func([]notes.View, error) ([]NoteView, error) {
	return func(list []notes.View, err error) ([]NoteView, error) {
		if err != nil {
			return nil, s.fail(op, err)
		}
		out := make([]NoteView, 0, len(list))
		for _, v := range list {
			out = append(out, *noteViewOf(v))
		}
		return out, nil
	}
}

// fail maps a use case error to the page's text and logs the cause.
func (s *NotesService) fail(op string, err error) error {
	known := []struct {
		err  error
		text libraryError
	}{
		{notes.ErrInvalidRequest, ErrNotesInvalidID},
		{notes.ErrNoteNotFound, ErrNotesNotFound},
		{notes.ErrRecordingUnavailable, ErrNotesRecording},
		{notes.ErrLessonNotFound, ErrLibraryLessonNotFound},
		{notes.ErrOtherSubject, ErrNotesOtherSubject},
		{notes.ErrMaterialNotFound, ErrLibraryFileNotFound},
		{notes.ErrMaterialRemoved, ErrLibraryFileRemoved},
		{notes.ErrRemoved, ErrNotesRemoved},
		{notes.ErrEmptyTitle, ErrNotesEmptyTitle},
		{notes.ErrNoFiles, ErrNotesNoFiles},
		{notes.ErrTooManyParts, ErrNotesTooManyParts},
		{notes.ErrAlreadyAdded, ErrNotesAlreadyAdded},
		{notes.ErrPartNotFound, ErrNotesPartNotFound},
		{notes.ErrInvalidOrder, ErrNotesInvalidOrder},
		{notes.ErrEmptyText, ErrNotesEmptyText},
		{notes.ErrTextTooLarge, ErrNotesTextTooLarge},
		{notes.ErrInvalidText, ErrNotesInvalidText},
		{notes.ErrChanged, ErrNotesChanged},
		{notes.ErrBusy, ErrLibraryBusy},
		// The library, through the use case's Materials port; the photo errors before the classes they wrap.
		{library.ErrImageTooLarge, ErrNotesImageTooLarge},
		{library.ErrUnsupportedImage, ErrNotesImageFormat},
		{library.ErrImageUnreadable, ErrNotesImageUnreadable},
		{library.ErrInvalidRequest, ErrNotesInvalidID},
		{library.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{library.ErrFolderNotFound, ErrNotesChanged},
		{library.ErrMaterialNotFound, ErrLibraryFileNotFound},
		{library.ErrMaterialRemoved, ErrLibraryFileRemoved},
		{library.ErrEmptyFile, ErrLibraryEmptyFile},
		{library.ErrFileTooLarge, ErrLibraryFileTooLarge},
		{library.ErrUnsupportedFile, ErrLibraryUnsupported},
		{library.ErrBusy, ErrLibraryBusy},
	}
	for _, k := range known {
		if errors.Is(err, k.err) {
			s.log.Info(op, "result", k.err.Error())
			return k.text
		}
	}
	s.log.Error(op, "err", err)
	return ErrNotesUnavailable
}

func notesReplace(r notes.Replaced) NotesReplace {
	out := NotesReplace{Note: noteViewOf(r.View), PreviousID: string(r.Previous), Existing: r.Existing}
	if r.Material != nil {
		out.MaterialID = string(r.Material.ID())
	}
	return out
}

// noteViewOf converts a note as it reads now; nil for no note.
func noteViewOf(v notes.View) *NoteView {
	n := v.Note
	if n == nil {
		return nil
	}
	a := v.Anchor
	out := &NoteView{
		ID: string(n.ID()), SubjectID: string(n.Subject()), Title: n.Title(), Removed: !n.Active(),
		CreatedAt: timeText(n.CreatedAt()), UpdatedAt: timeText(n.UpdatedAt()), Parts: []NotePart{},
		NoTextPages: v.NoTextPages,
		Anchor: NoteAnchor{
			Kind: string(a.Kind()), RecordingID: string(a.Recording()), LessonID: string(a.Lesson()), Date: a.Date().String(),
			LessonKind: a.LessonKind(), LessonTitle: a.Title(), Detached: a.Kind() != n.Anchor().Kind(),
		},
	}
	for i, id := range n.Parts() {
		p := NotePart{MaterialID: string(id), Missing: true}
		if i < len(v.Parts) && v.Parts[i] != nil {
			m := v.Parts[i]
			p = NotePart{
				MaterialID: string(id), Name: m.Name(), Type: string(m.Type()), TextStatus: string(m.TextStatus()),
				Removed: !m.Active(),
			}
			if m.Active() && m.Type().Image() {
				p.PageURL = notePageURL(id)
			}
		}
		out.Parts = append(out.Parts, p)
	}
	return out
}
