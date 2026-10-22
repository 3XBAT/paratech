package shared

import (
	"fmt"
	"strings"
)

// ArtifactKind is a top-level directory of the private application data root. The directory layout (recordings,
// transcripts, models, exports) is a convention of this project defined by TASK-006; PRD section 5.2 only requires
// that large artifacts stay outside SQLite and are reached through the Storage Manager.
type ArtifactKind string

// Artifact kinds, one per top-level directory of the data root.
const (
	ArtifactRecordings  ArtifactKind = "recordings"
	ArtifactTranscripts ArtifactKind = "transcripts"
	ArtifactModels      ArtifactKind = "models"
	ArtifactExports     ArtifactKind = "exports"
	// ArtifactMaterials holds the files of the library, one per sha256 (ADR-0024).
	ArtifactMaterials ArtifactKind = "materials"
)

func (k ArtifactKind) valid() bool {
	switch k {
	case ArtifactRecordings, ArtifactTranscripts, ArtifactModels, ArtifactExports, ArtifactMaterials:
		return true
	}
	return false
}

// ArtifactRef names a file or directory inside the data root without exposing an OS path
// (PRD section 13.1 deletion semantics; PRD section 5.2 for the Storage Manager). It is the only way ports talk
// about files, so use cases and the domain never see a real path (hard rule 5). The "<kind>/<path>" form is an
// identifier convention of this project (TASK-006), not something the PRD prescribes.
//
// The path is relative and slash separated. Every segment must be a safe file name on Windows and elsewhere: no
// empty, "." or ".." segment, no backslash, colon, control character or any of * ? < > | ", no trailing dot or
// space, and not a reserved device name (CON, PRN, AUX, NUL, COM1-9, LPT1-9, in any case, with or without an
// extension). This makes a traversal out of the data root unrepresentable whatever adapter resolves the
// reference. The stored form is "<kind>/<path>".
type ArtifactRef struct {
	kind ArtifactKind
	path string
}

// NewArtifactRef validates and builds a reference.
func NewArtifactRef(kind ArtifactKind, path string) (ArtifactRef, error) {
	if !kind.valid() {
		return ArtifactRef{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidArtifactRef, kind)
	}
	if path == "" {
		return ArtifactRef{}, fmt.Errorf("%w: empty path", ErrInvalidArtifactRef)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			return ArtifactRef{}, fmt.Errorf("%w: path %q has an empty segment", ErrInvalidArtifactRef, path)
		}
		if why := nameProblem(seg); why != "" {
			return ArtifactRef{}, fmt.Errorf("%w: path %q: segment %q %s", ErrInvalidArtifactRef, path, seg, why)
		}
	}
	return ArtifactRef{kind: kind, path: path}, nil
}

// ParseArtifactRef rebuilds a reference from its stored form "<kind>/<path>".
func ParseArtifactRef(s string) (ArtifactRef, error) {
	kind, path, ok := strings.Cut(s, "/")
	if !ok {
		return ArtifactRef{}, fmt.Errorf("%w: %q has no \"<kind>/<path>\" form", ErrInvalidArtifactRef, s)
	}
	return NewArtifactRef(ArtifactKind(kind), path)
}

// Kind returns the top-level directory of the reference.
func (r ArtifactRef) Kind() ArtifactKind { return r.kind }

// Path returns the relative slash-separated path below the kind directory.
func (r ArtifactRef) Path() string { return r.path }

// IsZero reports whether the reference is the zero value (no artifact).
func (r ArtifactRef) IsZero() bool { return r == ArtifactRef{} }

// String returns the stored form "<kind>/<path>".
func (r ArtifactRef) String() string {
	if r.IsZero() {
		return ""
	}
	return string(r.kind) + "/" + r.path
}

// Child returns a reference to a direct child entry of r (for example a chunk file inside a
// recording directory). The name is validated like any other path.
func (r ArtifactRef) Child(name string) (ArtifactRef, error) {
	if r.IsZero() {
		return ArtifactRef{}, fmt.Errorf("%w: child of the zero reference", ErrInvalidArtifactRef)
	}
	if strings.Contains(name, "/") {
		return ArtifactRef{}, fmt.Errorf("%w: child name %q contains a separator", ErrInvalidArtifactRef, name)
	}
	return NewArtifactRef(r.kind, r.path+"/"+name)
}
