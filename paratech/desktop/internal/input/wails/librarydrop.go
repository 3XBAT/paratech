package wails

import (
	"context"
	"errors"
)

// LibraryDropEvent is emitted after files are dropped on the main window, with a LibraryDrop. It carries no path:
// the paths of a drop stay in Go.
const LibraryDropEvent = "library:dropped"

// maxDroppedFiles bounds one drop; a larger one is refused whole and nothing is read.
const maxDroppedFiles = 10

// LibraryDrop is the outcome of one drop. Error is "" or the refusal of the whole drop (too many files); otherwise
// Files has one entry per dropped file, in drop order.
type LibraryDrop struct {
	Error string            `json:"error"`
	Files []LibraryDropFile `json:"files"`
}

// LibraryDropFile is the outcome of one dropped file: Material when it is in the library (Existing: it was there
// already), or Error, the page's text of the refusal. Name is the file name (plain text), "" when the path itself
// was refused.
type LibraryDropFile struct {
	Name     string           `json:"name"`
	Existing bool             `json:"existing"`
	Material *LibraryMaterial `json:"material"`
	Error    string           `json:"error"`
}

// importDropped puts the files dropped on the main window into «Входящие», each on its own, and emits the outcome.
// The paths come through the Wails runtime of the page, so they are untrusted and checked as droppedPathError says.
// Drops run one at a time; nothing is emitted once ctx is done (the app is shutting down).
func (s *LibraryService) importDropped(ctx context.Context, paths []string, emit EventEmitter) {
	if len(paths) == 0 {
		return
	}
	s.drop.Lock()
	defer s.drop.Unlock()
	if ctx.Err() != nil {
		return
	}
	out := s.dropOutcome(ctx, paths)
	if ctx.Err() != nil {
		return
	}
	emit.Emit(LibraryDropEvent, out)
}

func (s *LibraryService) dropOutcome(ctx context.Context, paths []string) LibraryDrop {
	if len(paths) > maxDroppedFiles {
		s.log.Info(opLibraryDrop, "result", "too many files", "count", len(paths))
		return LibraryDrop{Error: ErrLibraryTooManyDropped.Error(), Files: []LibraryDropFile{}}
	}

	out := LibraryDrop{Files: make([]LibraryDropFile, 0, len(paths))}
	for _, path := range paths {
		out.Files = append(out.Files, s.dropFile(ctx, path))
	}
	return out
}

func (s *LibraryService) dropFile(ctx context.Context, path string) LibraryDropFile {
	if err := droppedPathError(path); err != nil {
		s.log.Info(opLibraryDrop, "result", err.Error())
		f := LibraryDropFile{Error: err.Error()}
		// A malformed path gets no name, so no part of a forged path reaches the page.
		if !errors.Is(err, ErrLibraryDroppedPath) {
			f.Name = baseName(path)
		}
		return f
	}

	f := LibraryDropFile{Name: baseName(path)}
	res, err := s.importPath(ctx, path)
	if err != nil {
		f.Error = err.Error()
		return f
	}
	f.Existing, f.Material = res.Existing, res.Material
	return f
}
