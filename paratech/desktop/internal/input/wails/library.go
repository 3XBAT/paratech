package wails

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// libraryError is an error of the library: a fixed Russian text the page shows as it is.
type libraryError string

func (e libraryError) Error() string { return string(e) }

// Errors of the library (the page shows them as they are). Causes are logged, never shown; neither paths nor file
// names nor file content are logged.
const (
	ErrLibraryInvalidID       = libraryError("Неверный идентификатор: обновите список.")
	ErrLibraryName            = libraryError("Название предмета: от 1 до 200 символов.")
	ErrLibraryFolderName      = libraryError("Недопустимое имя папки: до 64 символов, без \\ / : * ? \" < > |, не CON, NUL и т. п., без точки в конце.")
	ErrLibraryNameTaken       = libraryError("Предмет с таким названием уже есть.")
	ErrLibraryFolderTaken     = libraryError("Папка с таким именем уже есть.")
	ErrLibrarySubjectNotFound = libraryError("Предмет не найден: обновите список.")
	ErrLibraryFolderNotFound  = libraryError("Папка не найдена: обновите список.")
	ErrLibraryFileNotFound    = libraryError("Файл не найден: обновите список.")
	ErrLibraryLessonNotFound  = libraryError("Занятие не найдено в расписании.")
	ErrLibraryNotEmpty        = libraryError("Сначала уберите отсюда все файлы, в том числе удалённые.")
	ErrLibrarySystemFolder    = libraryError("Эту папку удалить нельзя.")
	ErrLibraryLimit           = libraryError("Достигнут предел: слишком много папок, названий или связей.")
	ErrLibraryFileRemoved     = libraryError("Файл удалён: сначала восстановите его.")
	ErrLibraryEmptyFile       = libraryError("Файл пустой.")
	ErrLibraryFileTooLarge    = libraryError("Файл больше 100 МБ.")
	ErrLibraryUnsupported     = libraryError("Подходят только DOCX, PPTX, PDF и текст в UTF-8.")
	ErrLibraryFileUnreadable  = libraryError("Не удалось прочитать файл.")
	ErrLibraryBusy            = libraryError("Данные сейчас меняются. Попробуйте ещё раз.")
	ErrLibraryUnavailable     = libraryError("Не удалось прочитать библиотеку. Попробуйте ещё раз.")
	ErrLibraryDroppedPath     = libraryError("Этот файл добавить нельзя: перетащите файл с диска этого компьютера.")
)

// maxDroppedPath bounds a dropped path in bytes (the longest Windows path is 32767 UTF-16 units).
const maxDroppedPath = 32767

// droppedExtension reports whether a dropped file may have the extension ext (lower case), as in the open dialog;
// the type is still told from the content.
func droppedExtension(ext string) bool {
	switch ext {
	case ".docx", ".pptx", ".pdf", ".txt":
		return true
	}
	return false
}

// Operation names of the log.
const (
	opLibrarySubjects    = "ui: library subjects"
	opLibraryCreate      = "ui: create subject"
	opLibraryAlias       = "ui: add subject alias"
	opLibraryFolder      = "ui: create folder"
	opLibraryRemoveDir   = "ui: remove folder"
	opLibraryRemoveSub   = "ui: remove subject"
	opLibraryMaterials   = "ui: library files"
	opLibraryPick        = "ui: choose a file for the library"
	opLibraryDrop        = "ui: file dropped on the window refused"
	opLibraryRead        = "ui: read a file for the library"
	opLibraryImport      = "ui: import a file into the library"
	opLibraryPlace       = "ui: file a material"
	opLibraryInbox       = "ui: return a material to the inbox"
	opLibraryLink        = "ui: link a material to a lesson"
	opLibraryUnlink      = "ui: unlink a material from a lesson"
	opLibraryRemove      = "ui: remove a material"
	opLibraryRestore     = "ui: restore a material"
	opLibrarySuggestions = "ui: subject suggestions"
)

// msgPDFNotExtracted is the note of a PDF, whose text is not read (ADR-0024).
const msgPDFNotExtracted = "текст PDF не извлечён"

// LibraryUseCase is the library (ARCH-42); satisfied by library.Service.
type LibraryUseCase interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
	CreateSubject(ctx context.Context, req library.NewSubject) (*subject.Subject, error)
	AddAlias(ctx context.Context, id shared.SubjectID, alias string) (*subject.Subject, error)
	CreateFolder(ctx context.Context, id shared.SubjectID, name string) (subject.Folder, error)
	RemoveFolder(ctx context.Context, id shared.SubjectID, folder shared.FolderID) error
	RemoveSubject(ctx context.Context, id shared.SubjectID) error
	Materials(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error)
	Import(ctx context.Context, up library.Upload) (library.Imported, error)
	Place(ctx context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error)
	MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error)
	Link(ctx context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error)
	Unlink(ctx context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error)
	RemoveMaterial(ctx context.Context, id shared.MaterialID) (*material.Material, error)
	RestoreMaterial(ctx context.Context, id shared.MaterialID) (*material.Material, error)
	SuggestSubjects(ctx context.Context) ([]subjectmatch.Suggestion, error)
}

// MaterialFilePicker shows the native open dialog for a file of the library. It returns the chosen path, or "" when
// the user cancels. The path never comes from the page or a model.
type MaterialFilePicker interface {
	PickMaterialFile(ctx context.Context) (string, error)
}

// materialPickerFunc adapts a function to MaterialFilePicker.
type materialPickerFunc func(ctx context.Context) (string, error)

// PickMaterialFile implements MaterialFilePicker.
func (f materialPickerFunc) PickMaterialFile(ctx context.Context) (string, error) { return f(ctx) }

// MaterialFileReader reads the chosen file: a regular file only, at most maxBytes+1 bytes. Its errors must not
// contain the path (the app wires adapters/userfile).
type MaterialFileReader interface {
	ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error)
}

// LibraryFolder is a folder of a subject. System is the code of a system folder ("lectures", "seminars", "labs",
// "materials", "homework", "tickets"), "" for an own folder; system folders cannot be removed.
type LibraryFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	System string `json:"system"`
}

// LibrarySubject is a subject of the library. Semester is the code ("2026-autumn"), SemesterLabel the text shown
// («Осень 2026»). Names are typed text: plain text only.
type LibrarySubject struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Aliases       []string        `json:"aliases"`
	Semester      string          `json:"semester"`
	SemesterLabel string          `json:"semesterLabel"`
	Folders       []LibraryFolder `json:"folders"`
}

// LibraryMaterial is a file of the library. Name is the original file name, shown only (plain text). Type is "docx",
// "pptx", "txt" or "pdf". Inbox: it is in «Входящие» (SubjectID and FolderID ""). Removed: deleted softly, restorable.
// TextStatus is "pending", "extracted", "failed" or "pdf_not_extracted"; TextNote is the Russian note for the last
// one. CreatedAt is RFC 3339 in UTC.
type LibraryMaterial struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	SizeBytes  int64    `json:"sizeBytes"`
	SubjectID  string   `json:"subjectId"`
	FolderID   string   `json:"folderId"`
	Inbox      bool     `json:"inbox"`
	LessonIDs  []string `json:"lessonIds"`
	Removed    bool     `json:"removed"`
	TextStatus string   `json:"textStatus"`
	TextNote   string   `json:"textNote"`
	CreatedAt  string   `json:"createdAt"`
}

// LibraryImport is the outcome of ImportFile. Cancelled: the user closed the dialog (Material is null). Existing: the
// same file was in the library already; that material is returned (a removed one is back in «Входящие»).
type LibraryImport struct {
	Cancelled bool             `json:"cancelled"`
	Existing  bool             `json:"existing"`
	Material  *LibraryMaterial `json:"material"`
}

// LibrarySuggestion is a subject string of the schedule or the recordings that matches no subject: the page offers
// to create the subject (or to add the string as an alias). Lessons counts lessons of the schedule, Recordings the
// recordings that name it. Name is imported text: plain text only.
type LibrarySuggestion struct {
	Name       string `json:"name"`
	Lessons    int    `json:"lessons"`
	Recordings int    `json:"recordings"`
}

// LibraryService is bound to the UI («Предметы»): every exported method is callable from the page by its full name
// "<package path>.LibraryService.<Method>". Keep the method set to what the page may call. Nothing here calls the
// network; a file reaches the library only through the native open dialog.
type LibraryService struct {
	uc     LibraryUseCase
	picker MaterialFilePicker
	files  MaterialFileReader
	log    *slog.Logger
}

// NewLibraryService returns the binding of the library; all arguments are required.
func NewLibraryService(uc LibraryUseCase, picker MaterialFilePicker, files MaterialFileReader, log *slog.Logger) *LibraryService {
	return &LibraryService{uc: uc, picker: picker, files: files, log: log}
}

// Subjects lists the subjects by name with their folders.
func (s *LibraryService) Subjects(ctx context.Context) ([]LibrarySubject, error) {
	list, err := s.uc.Subjects(ctx)
	if err != nil {
		return nil, s.fail(opLibrarySubjects, err)
	}
	out := make([]LibrarySubject, 0, len(list))
	for _, sub := range list {
		out = append(out, librarySubject(sub))
	}
	return out, nil
}

// CreateSubject creates a subject with its system folders. semester is a code ("2026-autumn") or "" for the current
// one; aliases may be empty (a suggested schedule string is typically passed as one).
func (s *LibraryService) CreateSubject(ctx context.Context, name string, aliases []string, semester string) (LibrarySubject, error) {
	sub, err := s.uc.CreateSubject(ctx, library.NewSubject{Name: name, Aliases: aliases, Semester: semester})
	if err != nil {
		return LibrarySubject{}, s.fail(opLibraryCreate, err)
	}
	return librarySubject(sub), nil
}

// AddAlias adds another spelling to a subject, for example a suggested schedule string.
func (s *LibraryService) AddAlias(ctx context.Context, subjectID, alias string) (LibrarySubject, error) {
	sub, err := s.uc.AddAlias(ctx, shared.SubjectID(subjectID), alias)
	if err != nil {
		return LibrarySubject{}, s.fail(opLibraryAlias, err)
	}
	return librarySubject(sub), nil
}

// CreateFolder adds an own folder to a subject.
func (s *LibraryService) CreateFolder(ctx context.Context, subjectID, name string) (LibraryFolder, error) {
	f, err := s.uc.CreateFolder(ctx, shared.SubjectID(subjectID), name)
	if err != nil {
		return LibraryFolder{}, s.fail(opLibraryFolder, err)
	}
	return libraryFolder(f), nil
}

// RemoveFolder removes an empty own folder.
func (s *LibraryService) RemoveFolder(ctx context.Context, subjectID, folderID string) error {
	if err := s.uc.RemoveFolder(ctx, shared.SubjectID(subjectID), shared.FolderID(folderID)); err != nil {
		return s.fail(opLibraryRemoveDir, err)
	}
	return nil
}

// RemoveSubject removes a subject that holds no file.
func (s *LibraryService) RemoveSubject(ctx context.Context, subjectID string) error {
	if err := s.uc.RemoveSubject(ctx, shared.SubjectID(subjectID)); err != nil {
		return s.fail(opLibraryRemoveSub, err)
	}
	return nil
}

// Materials lists the files of a subject, or of «Входящие» for "", removed ones included (Removed), newest first.
func (s *LibraryService) Materials(ctx context.Context, subjectID string) ([]LibraryMaterial, error) {
	list, err := s.uc.Materials(ctx, shared.SubjectID(subjectID))
	if err != nil {
		return nil, s.fail(opLibraryMaterials, err)
	}
	out := make([]LibraryMaterial, 0, len(list))
	for _, m := range list {
		out = append(out, libraryMaterial(m))
	}
	return out, nil
}

// ImportFile asks in the native open dialog for a file and puts it into «Входящие». The type is told from the
// content; only the base name of the file is kept, for display.
func (s *LibraryService) ImportFile(ctx context.Context) (LibraryImport, error) {
	path, err := s.picker.PickMaterialFile(ctx)
	switch {
	case err != nil:
		s.log.Warn(opLibraryPick, "err", err)
		return LibraryImport{}, ErrLibraryFileUnreadable
	case path == "":
		return LibraryImport{Cancelled: true}, nil
	}
	return s.importPath(ctx, path)
}

// ImportDroppedFile puts a file dropped on the window into «Входящие», as ImportFile does. The path comes from the
// page, so it is untrusted: only a local absolute path to a DOCX, PPTX, PDF or TXT file is read (no network or device
// paths, no "..", no alternate streams); the reader takes regular files only.
func (s *LibraryService) ImportDroppedFile(ctx context.Context, path string) (LibraryImport, error) {
	if err := droppedPathError(path); err != nil {
		s.log.Info(opLibraryDrop, "result", err.Error())
		return LibraryImport{}, err
	}
	return s.importPath(ctx, path)
}

// droppedPathError checks a path the page passed; the error is the page's text.
func droppedPathError(path string) error {
	if path == "" || len(path) > maxDroppedPath || !utf8.ValidString(path) || strings.ContainsRune(path, 0) ||
		!filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return ErrLibraryDroppedPath
	}
	// A volume name is "C:" here; any other colon names an alternate data stream.
	rest := path[len(filepath.VolumeName(path)):]
	if strings.ContainsRune(rest, ':') {
		return ErrLibraryDroppedPath
	}
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." || part == "." {
			return ErrLibraryDroppedPath
		}
	}
	if !droppedExtension(strings.ToLower(filepath.Ext(path))) {
		return ErrLibraryUnsupported
	}
	return nil
}

// importPath reads the file at path and imports it.
func (s *LibraryService) importPath(ctx context.Context, path string) (LibraryImport, error) {
	data, err := s.files.ReadFile(ctx, path, material.MaxBytes)
	switch {
	case err != nil:
		s.log.Warn(opLibraryRead, "err", err) // the reader's errors never carry the path
		return LibraryImport{}, ErrLibraryFileUnreadable
	case len(data) > material.MaxBytes:
		return LibraryImport{}, ErrLibraryFileTooLarge
	}
	res, err := s.uc.Import(ctx, library.Upload{Name: baseName(path), Data: data})
	if err != nil {
		return LibraryImport{}, s.fail(opLibraryImport, err)
	}
	m := libraryMaterial(res.Material)
	return LibraryImport{Existing: res.Existing, Material: &m}, nil
}

// Place files a material in a folder of a subject; folderID "" is «Материалы».
func (s *LibraryService) Place(ctx context.Context, materialID, subjectID, folderID string) (LibraryMaterial, error) {
	return s.material(opLibraryPlace)(s.uc.Place(ctx, shared.MaterialID(materialID), shared.SubjectID(subjectID), shared.FolderID(folderID)))
}

// MoveToInbox returns a material to «Входящие».
func (s *LibraryService) MoveToInbox(ctx context.Context, materialID string) (LibraryMaterial, error) {
	return s.material(opLibraryInbox)(s.uc.MoveToInbox(ctx, shared.MaterialID(materialID)))
}

// Link links a material to a lesson of the schedule.
func (s *LibraryService) Link(ctx context.Context, materialID, lessonID string) (LibraryMaterial, error) {
	return s.material(opLibraryLink)(s.uc.Link(ctx, shared.MaterialID(materialID), shared.LessonID(lessonID)))
}

// Unlink removes the link of a material to a lesson.
func (s *LibraryService) Unlink(ctx context.Context, materialID, lessonID string) (LibraryMaterial, error) {
	return s.material(opLibraryUnlink)(s.uc.Unlink(ctx, shared.MaterialID(materialID), shared.LessonID(lessonID)))
}

// RemoveMaterial removes a material softly; RestoreMaterial brings it back. The file itself is kept.
func (s *LibraryService) RemoveMaterial(ctx context.Context, materialID string) (LibraryMaterial, error) {
	return s.material(opLibraryRemove)(s.uc.RemoveMaterial(ctx, shared.MaterialID(materialID)))
}

// RestoreMaterial brings a removed material back where it was.
func (s *LibraryService) RestoreMaterial(ctx context.Context, materialID string) (LibraryMaterial, error) {
	return s.material(opLibraryRestore)(s.uc.RestoreMaterial(ctx, shared.MaterialID(materialID)))
}

// Suggestions lists the subject strings of the schedule and the recordings that match no subject, the most used
// first. Nothing is created: the page lets the student create the subject.
func (s *LibraryService) Suggestions(ctx context.Context) ([]LibrarySuggestion, error) {
	list, err := s.uc.SuggestSubjects(ctx)
	if err != nil {
		return nil, s.fail(opLibrarySuggestions, err)
	}
	out := make([]LibrarySuggestion, 0, len(list))
	for _, sg := range list {
		out = append(out, LibrarySuggestion{Name: sg.Name, Lessons: sg.Lessons, Recordings: sg.Recordings})
	}
	return out, nil
}

// material returns the converter of a use case result to the page's material, failing as op.
func (s *LibraryService) material(op string) func(*material.Material, error) (LibraryMaterial, error) {
	return func(m *material.Material, err error) (LibraryMaterial, error) {
		if err != nil {
			return LibraryMaterial{}, s.fail(op, err)
		}
		return libraryMaterial(m), nil
	}
}

// fail maps a use case error to the page's text and logs the cause.
func (s *LibraryService) fail(op string, err error) error {
	known := []struct {
		err  error
		text libraryError
	}{
		{library.ErrInvalidRequest, ErrLibraryInvalidID},
		{library.ErrInvalidName, ErrLibraryName},
		{library.ErrInvalidFolderName, ErrLibraryFolderName},
		{library.ErrNameTaken, ErrLibraryNameTaken},
		{library.ErrFolderTaken, ErrLibraryFolderTaken},
		{library.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{library.ErrFolderNotFound, ErrLibraryFolderNotFound},
		{library.ErrMaterialNotFound, ErrLibraryFileNotFound},
		{library.ErrLessonNotFound, ErrLibraryLessonNotFound},
		{library.ErrNotEmpty, ErrLibraryNotEmpty},
		{library.ErrSystemFolder, ErrLibrarySystemFolder},
		{library.ErrTooMany, ErrLibraryLimit},
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
	return ErrLibraryUnavailable
}

func librarySubject(sub *subject.Subject) LibrarySubject {
	out := LibrarySubject{
		ID: string(sub.ID()), Name: sub.Name().String(), Aliases: []string{}, Semester: sub.Semester().Code(),
		SemesterLabel: sub.Semester().Label(), Folders: []LibraryFolder{},
	}
	for _, a := range sub.Aliases() {
		out.Aliases = append(out.Aliases, a.String())
	}
	for _, f := range sub.Folders() {
		out.Folders = append(out.Folders, libraryFolder(f))
	}
	return out
}

func libraryFolder(f subject.Folder) LibraryFolder {
	return LibraryFolder{ID: string(f.ID()), Name: f.Name().String(), System: string(f.System())}
}

func libraryMaterial(m *material.Material) LibraryMaterial {
	out := LibraryMaterial{
		ID: string(m.ID()), Name: m.Name(), Type: string(m.Type()), SizeBytes: m.Size(), SubjectID: string(m.Subject()),
		FolderID: string(m.Folder()), Inbox: m.InInbox(), LessonIDs: []string{}, Removed: !m.Active(),
		TextStatus: string(m.TextStatus()), CreatedAt: timeText(m.CreatedAt()),
	}
	for _, l := range m.Lessons() {
		out.LessonIDs = append(out.LessonIDs, string(l))
	}
	if m.TextStatus() == material.TextPDFNotExtracted {
		out.TextNote = msgPDFNotExtracted
	}
	return out
}
