package shared

import (
	"fmt"
	"strings"
	"unicode"
)

// ArtifactKind is a top-level directory of the private application data root (PRD section 5.2).
type ArtifactKind string

// Artifact kinds, one per top-level directory of the data root.
const (
	ArtifactRecordings  ArtifactKind = "recordings"
	ArtifactTranscripts ArtifactKind = "transcripts"
	ArtifactModels      ArtifactKind = "models"
	ArtifactExports     ArtifactKind = "exports"
)

func (k ArtifactKind) valid() bool {
	switch k {
	case ArtifactRecordings, ArtifactTranscripts, ArtifactModels, ArtifactExports:
		return true
	}
	return false
}

// ArtifactRef names a file or directory inside the data root without exposing an OS path
// (PRD sections 5.2 and 13.1). It is the only way ports talk about files, so use cases and the
// domain never see a real path (hard rule 5).
//
// The path is relative, slash separated and cannot contain "." or ".." segments, absolute paths,
// drive letters or backslashes. This makes a traversal out of the data root unrepresentable
// whatever adapter resolves the reference. The stored form is "<kind>/<path>".
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
	for _, r := range path {
		if r == '\\' || r == ':' || unicode.IsControl(r) {
			return ArtifactRef{}, fmt.Errorf("%w: path %q contains a forbidden character", ErrInvalidArtifactRef, path)
		}
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ArtifactRef{}, fmt.Errorf("%w: path %q has an empty, \".\" or \"..\" segment", ErrInvalidArtifactRef, path)
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
