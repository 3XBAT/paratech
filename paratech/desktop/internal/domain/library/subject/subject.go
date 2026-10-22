package subject

import (
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Limits of the aggregate.
const (
	// MaxAliases bounds the aliases of a subject.
	MaxAliases = 20

	// MaxOwnFolders bounds the folders the student adds to a subject.
	MaxOwnFolders = 50
)

// Subject is a subject of the library. Fields change only through the methods below.
type Subject struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.SubjectID

	name     Name
	aliases  []Name
	semester values.Semester
	folders  []Folder // the system folders first, in the order of Systems, then own folders in the order added

	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a subject provides to New.
type Spec struct {
	ID       shared.SubjectID
	Name     string
	Aliases  []string // optional; spellings with the key of the name or of another alias are dropped
	Semester values.Semester
}

// New creates a subject with its six system folders, never stored (Version() == 0).
func New(spec Spec, now time.Time) (*Subject, error) {
	name, err := NewName(spec.Name)
	if err != nil {
		return nil, err
	}
	s := &Subject{id: spec.ID, name: name, semester: spec.Semester, createdAt: now, updatedAt: now}
	for _, code := range Systems() {
		n, _ := systemName(code)
		s.folders = append(s.folders, Folder{id: SystemFolderID(spec.ID, code), name: n, system: code})
	}
	for _, a := range spec.Aliases {
		if _, err := s.AddAlias(a, now); err != nil {
			return nil, err
		}
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// State is the stored form of a subject, for Restore and the repositories.
type State struct {
	Version   int64
	ID        shared.SubjectID
	Name      string
	Aliases   []string
	Semester  values.Semester
	Folders   []FolderState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored subject without side effects; it only checks that the state is consistent.
func Restore(st State) (*Subject, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidSubject, st.Version)
	}
	name, err := NewName(st.Name)
	if err != nil || name.text != st.Name {
		return nil, fmt.Errorf("%w: stored name", ErrInvalidSubject)
	}
	s := &Subject{
		version: st.Version, id: st.ID, name: name, semester: st.Semester, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	for _, a := range st.Aliases {
		n, err := NewName(a)
		if err != nil || n.text != a {
			return nil, fmt.Errorf("%w: stored alias", ErrInvalidSubject)
		}
		s.aliases = append(s.aliases, n)
	}
	for _, f := range st.Folders {
		n, err := NewFolderName(f.Name)
		if err != nil || n.text != f.Name {
			return nil, fmt.Errorf("%w: stored folder name", ErrInvalidSubject)
		}
		s.folders = append(s.folders, Folder{id: f.ID, name: n, system: f.System})
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// validate checks the invariants: ids, the six system folders in order and with their names, unique folder ids and
// name keys, unique alias keys that differ from the name, the limits and a semester.
func (s *Subject) validate() error {
	if err := s.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSubject, err)
	}
	if s.semester.IsZero() {
		return fmt.Errorf("%w: no semester", ErrInvalidSubject)
	}
	if s.createdAt.IsZero() || s.updatedAt.IsZero() {
		return fmt.Errorf("%w: missing time", ErrInvalidSubject)
	}

	if len(s.aliases) > MaxAliases {
		return fmt.Errorf("%w: more than %d aliases", ErrInvalidSubject, MaxAliases)
	}
	keys := map[string]bool{s.name.Key(): true}
	for _, a := range s.aliases {
		if keys[a.Key()] {
			return fmt.Errorf("%w: alias repeats a name", ErrInvalidSubject)
		}
		keys[a.Key()] = true
	}

	systems := Systems()
	if len(s.folders) < len(systems) || len(s.folders) > len(systems)+MaxOwnFolders {
		return fmt.Errorf("%w: %d folders", ErrInvalidSubject, len(s.folders))
	}
	ids, names := map[shared.FolderID]bool{}, map[string]bool{}
	for i, f := range s.folders {
		if err := f.id.Validate(); err != nil {
			return fmt.Errorf("%w: folder: %w", ErrInvalidSubject, err)
		}
		if i < len(systems) {
			n, _ := systemName(systems[i])
			if f.system != systems[i] || f.id != SystemFolderID(s.id, systems[i]) || f.name != n {
				return fmt.Errorf("%w: system folder %d", ErrInvalidSubject, i)
			}
		} else if f.system != "" {
			return fmt.Errorf("%w: system folder %q repeated", ErrInvalidSubject, f.system)
		}
		if ids[f.id] || names[f.name.Key()] {
			return fmt.Errorf("%w: folder %s repeated", ErrInvalidSubject, f.id)
		}
		ids[f.id], names[f.name.Key()] = true, true
	}
	return nil
}

// AddAlias adds another spelling of the subject. A spelling with the key of the name or of an alias changes nothing
// (changed is false). Errors: ErrInvalidName, ErrTooMany.
func (s *Subject) AddAlias(alias string, now time.Time) (changed bool, err error) {
	n, err := NewName(alias)
	if err != nil {
		return false, err
	}
	if s.HasKey(n.Key()) {
		return false, nil
	}
	if len(s.aliases) >= MaxAliases {
		return false, fmt.Errorf("%w: %d aliases", ErrTooMany, MaxAliases)
	}
	s.aliases = append(s.aliases, n)
	s.updatedAt = now
	return true, nil
}

// HasKey reports whether key (values.Key) is the key of the name or of an alias.
func (s *Subject) HasKey(key string) bool {
	if s.name.Key() == key {
		return true
	}
	return slices.ContainsFunc(s.aliases, func(a Name) bool { return a.Key() == key })
}

// AddFolder adds an own folder with id. Errors: ErrFolderExists for a name key that a folder (also a system one)
// has, ErrTooMany, ErrInvalidSubject for a bad or repeated id.
func (s *Subject) AddFolder(id shared.FolderID, name FolderName, now time.Time) (Folder, error) {
	if err := id.Validate(); err != nil {
		return Folder{}, fmt.Errorf("%w: %w", ErrInvalidSubject, err)
	}
	if name.text == "" {
		return Folder{}, ErrInvalidFolderName
	}
	for _, f := range s.folders {
		if f.name.Key() == name.Key() {
			return Folder{}, ErrFolderExists
		}
		if f.id == id {
			return Folder{}, fmt.Errorf("%w: folder id %s repeated", ErrInvalidSubject, id)
		}
	}
	if len(s.folders)-len(systemNames) >= MaxOwnFolders {
		return Folder{}, fmt.Errorf("%w: %d folders", ErrTooMany, MaxOwnFolders)
	}
	f := Folder{id: id, name: name}
	s.folders = append(s.folders, f)
	s.updatedAt = now
	return f, nil
}

// RemoveFolder removes the own folder id; files is how many files (active or removed) the folder holds. Errors:
// ErrFolderNotFound, ErrSystemFolder, ErrNotEmpty.
func (s *Subject) RemoveFolder(id shared.FolderID, files int, now time.Time) error {
	i := slices.IndexFunc(s.folders, func(f Folder) bool { return f.id == id })
	switch {
	case i < 0:
		return ErrFolderNotFound
	case s.folders[i].IsSystem():
		return ErrSystemFolder
	case files > 0:
		return fmt.Errorf("%w: %d files", ErrNotEmpty, files)
	}
	s.folders = slices.Delete(s.folders, i, i+1)
	s.updatedAt = now
	return nil
}

// Removable reports whether the subject may be removed: files is how many files (active or removed) it holds, and a
// removed file still needs its subject to be restored. Own folders go with the subject. Error: ErrNotEmpty.
func (s *Subject) Removable(files int) error {
	if files > 0 {
		return fmt.Errorf("%w: %d files", ErrNotEmpty, files)
	}
	return nil
}

// Folder returns the folder id of the subject; ok is false when the subject has none.
func (s *Subject) Folder(id shared.FolderID) (Folder, bool) {
	i := slices.IndexFunc(s.folders, func(f Folder) bool { return f.id == id })
	if i < 0 {
		return Folder{}, false
	}
	return s.folders[i], true
}

// SystemFolder returns the system folder code; ok is false for an unknown code.
func (s *Subject) SystemFolder(code System) (Folder, bool) {
	i := slices.IndexFunc(s.folders, func(f Folder) bool { return f.system == code && code != "" })
	if i < 0 {
		return Folder{}, false
	}
	return s.folders[i], true
}

// ID returns the subject id.
func (s *Subject) ID() shared.SubjectID { return s.id }

// Name returns the subject name.
func (s *Subject) Name() Name { return s.name }

// Aliases returns a copy of the aliases in the order added.
func (s *Subject) Aliases() []Name { return slices.Clone(s.aliases) }

// Semester returns the semester of the subject.
func (s *Subject) Semester() values.Semester { return s.semester }

// Folders returns a copy of the folders: the system folders first, then own folders in the order added.
func (s *Subject) Folders() []Folder { return slices.Clone(s.folders) }

// CreatedAt returns the creation time.
func (s *Subject) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns the time of the last change.
func (s *Subject) UpdatedAt() time.Time { return s.updatedAt }

// State returns the stored form, for the repositories.
func (s *Subject) State() State {
	st := State{
		Version: s.version, ID: s.id, Name: s.name.text, Semester: s.semester, CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	}
	for _, a := range s.aliases {
		st.Aliases = append(st.Aliases, a.text)
	}
	for _, f := range s.folders {
		st.Folders = append(st.Folders, f.state())
	}
	return st
}

// Version returns the version this instance was last loaded or saved with: 0 for a subject never stored.
func (s *Subject) Version() int64 { return s.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (s *Subject) Persisted(version int64) { s.version = version }
