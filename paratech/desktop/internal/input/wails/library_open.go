package wails

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Statuses of OpenMaterial.
const (
	OpenStatusOpened   = "opened"
	OpenStatusNotFound = "not_found"
	OpenStatusRefused  = "refused"
	OpenStatusFailed   = "failed"
)

// Texts of OpenMaterial, shown as they are; none names a path.
const (
	msgOpenOpened   = "Файл открывается в программе по умолчанию."
	msgOpenNotFound = "Файл не найден: возможно, его удалили."
	msgOpenRefused  = "Этот файл нельзя открыть отсюда."
	msgOpenFailed   = "Не удалось открыть файл. Попробуйте ещё раз."
)

// Copies opened from the library live in exports/<openDir>/<material id>/<name>.
const (
	openDir      = "library"
	openFallback = "файл"
	maxOpenName  = 64 // runes of the name of a copy without the extension: Word refuses long full paths
)

// LibraryOpen is the outcome of OpenMaterial. Status is "opened", "not_found", "refused" or "failed"; Message is the
// Russian text to show.
type LibraryOpen struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// MaterialViews is the private artifact store as far as opening a file of the library needs it (the app wires
// adapters/filestore): a stored file has no extension, so it is opened through a copy under its name.
type MaterialViews interface {
	// Size returns the size of a stored file; a missing one wraps shared.ErrNotFound.
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)
	// Open opens a stored file for reading; a missing one wraps shared.ErrNotFound.
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
	// Write stores the content atomically.
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}

// FileOpener opens a file of the artifact store with the program associated with its extension (the app wires
// adapters/fileopen). It checks the file right before the start: a refused file wraps shared.ErrInvalidArtifactRef,
// a missing one shared.ErrNotFound.
type FileOpener interface {
	OpenArtifact(ctx context.Context, ref shared.ArtifactRef) error
}

// withOpener adds «Открыть в Word / PowerPoint».
func (s *LibraryService) withOpener(views MaterialViews, opener FileOpener) *LibraryService {
	s.views, s.opener = views, opener
	return s
}

// openMaterial returns the status and the code logged; causes are not logged, as they may name the file.
func (s *LibraryService) openMaterial(ctx context.Context, id shared.MaterialID) (status, code string) {
	if s.views == nil || s.opener == nil {
		return OpenStatusFailed, "not_wired"
	}
	m, err := s.uc.Material(ctx, id)
	switch {
	case errors.Is(err, library.ErrMaterialNotFound):
		return OpenStatusNotFound, "no_material"
	case errors.Is(err, library.ErrInvalidRequest):
		return OpenStatusRefused, "invalid_id"
	case err != nil:
		return OpenStatusFailed, "lookup"
	case !m.Active():
		return OpenStatusNotFound, "removed"
	}

	view, err := viewRef(m)
	if err != nil {
		return OpenStatusRefused, "type"
	}
	blob, err := material.BlobRef(m.Digest())
	if err != nil {
		return OpenStatusFailed, "blob_ref"
	}
	if err := s.ensureView(ctx, blob, view); err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return OpenStatusNotFound, "no_file"
		}
		return OpenStatusFailed, "copy"
	}

	err = s.opener.OpenArtifact(ctx, view)
	switch {
	case err == nil:
		return OpenStatusOpened, OpenStatusOpened
	case errors.Is(err, shared.ErrInvalidArtifactRef):
		return OpenStatusRefused, "refused"
	case errors.Is(err, shared.ErrNotFound):
		return OpenStatusNotFound, "no_copy"
	}
	return OpenStatusFailed, "launch"
}

// ensureView copies the stored file to view unless the copy is there already; an existing copy is never
// overwritten, it may hold the student's edits.
func (s *LibraryService) ensureView(ctx context.Context, blob, view shared.ArtifactRef) error {
	_, err := s.views.Size(ctx, view)
	if err == nil || !errors.Is(err, shared.ErrNotFound) {
		return err
	}
	r, err := s.views.Open(ctx, blob)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return s.views.Write(ctx, view, r)
}

// viewRef is the copy of m opened by the program: exports/library/<id>/<name>.<ext>, the name made of the letters,
// digits and plain punctuation of the file's name (a name explorer.exe or Windows could misread falls back).
func viewRef(m *material.Material) (shared.ArtifactRef, error) {
	ext, err := openExtension(m.Type())
	if err != nil {
		return shared.ArtifactRef{}, err
	}
	dir, err := shared.NewArtifactRef(shared.ArtifactExports, openDir+"/"+string(m.ID()))
	if err != nil {
		return shared.ArtifactRef{}, err
	}
	if ref, err := dir.Child(openName(m.Name()) + ext); err == nil {
		return ref, nil
	}
	return dir.Child(openFallback + ext)
}

// openExtension is the extension a program is chosen by; the type was told from the content.
func openExtension(t material.Type) (string, error) {
	switch t {
	case material.TypeDOCX, material.TypePPTX, material.TypeTXT, material.TypePDF, material.TypeJPEG, material.TypePNG:
		return "." + string(t), nil
	}
	return "", shared.ErrInvalidArtifactRef
}

// openName keeps letters, digits, spaces and "-_()" of name without its extension, at most maxOpenName runes.
func openName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	var b strings.Builder
	n := 0
	for _, r := range name {
		if n == maxOpenName {
			break
		}
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '(', r == ')':
		case unicode.IsSpace(r):
			r = ' '
		default:
			r = '_'
		}
		b.WriteRune(r)
		n++
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if strings.Trim(out, "_ ") == "" {
		return openFallback
	}
	return out
}

func openResult(status string) LibraryOpen {
	msg := msgOpenFailed
	switch status {
	case OpenStatusOpened:
		msg = msgOpenOpened
	case OpenStatusNotFound:
		msg = msgOpenNotFound
	case OpenStatusRefused:
		msg = msgOpenRefused
	}
	return LibraryOpen{Status: status, Message: msg}
}
