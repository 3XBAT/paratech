package values

import (
	"fmt"
	"slices"

	"paratech/desktop/internal/domain/shared"
)

// IDKind is the type of an ID the agent handles. The provenance check is typed: an ID passes only if it was seen in a
// field of the same kind (ADR-0021). Lessons (schedule entries) and recordings are distinct kinds.
type IDKind string

// ID kinds.
const (
	KindSubject    IDKind = "subject"
	KindFolder     IDKind = "folder"
	KindLesson     IDKind = "lesson"
	KindRecording  IDKind = "recording"
	KindMaterial   IDKind = "material"
	KindUpload     IDKind = "upload"
	KindAssignment IDKind = "assignment"
	KindJob        IDKind = "job"
	KindWork       IDKind = "work"
	KindVersion    IDKind = "version" // a draft version of an assignment
	KindProposal   IDKind = "proposal"
	KindPreview    IDKind = "preview"
	KindAnswer     IDKind = "answer"
)

// IDKinds returns every kind.
func IDKinds() []IDKind {
	return []IDKind{
		KindSubject, KindFolder, KindLesson, KindRecording, KindMaterial, KindUpload, KindAssignment, KindJob,
		KindWork, KindVersion, KindProposal, KindPreview, KindAnswer,
	}
}

// Valid reports whether k is one of the kinds.
func (k IDKind) Valid() bool { return slices.Contains(IDKinds(), k) }

// maxRefLen matches the identifier limit of domain/shared.
const maxRefLen = 128

// Ref is a typed reference to an object of another context. Zero means "none".
type Ref struct {
	kind IDKind
	id   string
}

// NewRef checks the kind and the ID: 1..128 ASCII letters, digits or "-_.@", and a safe file-name segment, the same
// shape the desktop ID generators produce.
func NewRef(kind IDKind, id string) (Ref, error) {
	if !kind.Valid() {
		return Ref{}, fmt.Errorf("%w: unknown kind", ErrInvalidRef)
	}
	if id == "" || len(id) > maxRefLen {
		return Ref{}, fmt.Errorf("%w: %s id must have 1..%d bytes", ErrInvalidRef, kind, maxRefLen)
	}
	for i := range len(id) {
		if !idByte(id[i]) {
			return Ref{}, fmt.Errorf("%w: %s id has a forbidden character", ErrInvalidRef, kind)
		}
	}
	if why := shared.NameSegmentProblem(id); why != "" {
		return Ref{}, fmt.Errorf("%w: %s id %s", ErrInvalidRef, kind, why)
	}
	return Ref{kind: kind, id: id}, nil
}

func idByte(c byte) bool { return asciiAlnum(c) || c == '-' || c == '_' || c == '.' || c == '@' }

// Kind returns the kind of the referenced object.
func (r Ref) Kind() IDKind { return r.kind }

// ID returns the opaque identifier.
func (r Ref) ID() string { return r.id }

// IsZero reports whether r refers to nothing.
func (r Ref) IsZero() bool { return r == Ref{} }

// String returns "<kind>:<id>".
func (r Ref) String() string { return string(r.kind) + ":" + r.id }

func compareRefs(a, b Ref) int {
	if a.kind != b.kind {
		if a.kind < b.kind {
			return -1
		}
		return 1
	}
	switch {
	case a.id < b.id:
		return -1
	case a.id > b.id:
		return 1
	}
	return 0
}

// RefSet is a set of references, the known IDs of the provenance check.
type RefSet map[Ref]struct{}

// NewRefSet returns a set of the non-zero refs.
func NewRefSet(refs ...Ref) RefSet {
	s := RefSet{}
	s.Add(refs...)
	return s
}

// Add puts the non-zero refs into the set.
func (s RefSet) Add(refs ...Ref) {
	for _, r := range refs {
		if !r.IsZero() {
			s[r] = struct{}{}
		}
	}
}

// Has reports whether r is in the set.
func (s RefSet) Has(r Ref) bool {
	_, ok := s[r]
	return ok
}

// Sorted returns the refs ordered by kind and ID, for storage and tests.
func (s RefSet) Sorted() []Ref {
	out := make([]Ref, 0, len(s))
	for r := range s {
		out = append(out, r)
	}
	slices.SortFunc(out, compareRefs)
	return out
}

// Clone returns an independent copy.
func (s RefSet) Clone() RefSet {
	out := make(RefSet, len(s))
	for r := range s {
		out[r] = struct{}{}
	}
	return out
}
