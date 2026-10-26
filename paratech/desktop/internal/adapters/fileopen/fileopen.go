// Package fileopen opens a file of the private artifact store with the program Windows associates with its
// extension (TASK-177: «Открыть в Word / PowerPoint»). On Windows it starts explorer.exe from the Windows directory
// with the file path as its only argument: no shell, no cmd.exe, no rundll32. explorer.exe hands the file to the
// associated program, so that program runs outside our process tree (ADR-0002, as adapters/weblink).
//
// The path is built from an artifact reference only and checked right before the start: cleaned, it must stay below
// the artifact root (its links resolved), pass through plain directories only (no link or junction), name a regular
// file with an allowed extension and hold no character explorer.exe reads as a separator or a quote.
package fileopen

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// ErrUnsupported means this system has no file launcher (only Windows has one).
var ErrUnsupported = errors.New("fileopen: opening a file is supported only on Windows")

// Errors of the check. A refused file wraps shared.ErrInvalidArtifactRef, a missing one shared.ErrNotFound; neither
// names the path.
var (
	errRefused  = fmt.Errorf("%w: the file may not be opened", shared.ErrInvalidArtifactRef)
	errNotFound = fmt.Errorf("fileopen: %w", shared.ErrNotFound)
	errStorage  = fmt.Errorf("fileopen: %w: resolve the path", shared.ErrStorage)
)

// allowedExtensions are the types a student opens from the library.
var allowedExtensions = []string{".docx", ".pptx", ".txt", ".md", ".pdf"}

// runner starts exe with arg as its only argument in dir; a test replaces it so nothing is launched.
type runner func(ctx context.Context, exe, dir, arg string) error

// Opener opens files below one artifact root. Create it with New.
type Opener struct {
	root string
	run  runner
}

// New returns an Opener for the artifact root (an absolute path, the root of adapters/filestore).
func New(root string) *Opener { return &Opener{root: root, run: runProcess} }

// OpenArtifact opens the file ref with the program associated with its extension. It returns once the launcher has
// started (Windows). Errors: one wrapping shared.ErrInvalidArtifactRef for a file the check refuses,
// shared.ErrNotFound for a missing one, ErrUnsupported off Windows, otherwise a start error. No error names the path.
func (o *Opener) OpenArtifact(ctx context.Context, ref shared.ArtifactRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := o.resolve(ref)
	if err != nil {
		return err
	}
	return o.launch(ctx, path)
}

// resolve returns the real path of ref when it passes the check.
func (o *Opener) resolve(ref shared.ArtifactRef) (string, error) {
	if ref.IsZero() || o.root == "" || !filepath.IsAbs(o.root) {
		return "", errRefused
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(o.root))
	if err != nil {
		return "", pathErr(err)
	}

	rel := filepath.Join(string(ref.Kind()), filepath.FromSlash(ref.Path()))
	p := filepath.Join(root, rel)
	if !within(root, p) || !allowedExtension(p) || !plainArg(p) {
		return "", errRefused
	}

	// Below the root every component must be a plain directory and the last one a regular file. EvalSymlinks would
	// not do: it does not follow a junction, so a junction to another folder would pass a check of the text.
	cur := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", pathErr(err)
		}
		last := i == len(parts)-1
		if last && !fi.Mode().IsRegular() || !last && fi.Mode().Type() != fs.ModeDir {
			return "", errRefused
		}
	}
	return p, nil
}

func pathErr(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return errNotFound
	}
	return errStorage
}

// within reports whether path lies strictly below root; both are clean, resolved and absolute.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func allowedExtension(path string) bool {
	return slices.Contains(allowedExtensions, strings.ToLower(filepath.Ext(path)))
}

// plainArg refuses control characters and the characters explorer.exe reads as switch separators or quotes.
func plainArg(path string) bool {
	for _, r := range path {
		if r < 0x20 || r == 0x7f || r == ',' || r == ';' || r == '"' {
			return false
		}
	}
	return true
}
