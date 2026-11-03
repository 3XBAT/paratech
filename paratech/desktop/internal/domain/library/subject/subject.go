package subject

import (
	"fmt"
	"io"
	"log/slog"
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

// RestoreWindow is how long a removed subject can be restored; from then on it is due to be purged (ADR-0027).
const RestoreWindow = 72 * time.Hour

// Status is whether the subject is in the library or removed softly with everything filed under it (ADR-0027).
type Status string

// Statuses of a subject.
const (
	StatusActive  Status = "active"
	StatusRemoved Status = "removed"
)

// transitions is the status machine (ARCH-19).
var transitions = map[Status][]Status{
	StatusActive:  {StatusRemoved},
	StatusRemoved: {StatusActive},
}

// Subject is a subject of the library. Fields change only through the methods below. It prints (fmt, slog) only its
// id and name: the teachers are personal data, the call link may hold a meeting password.
type Subject struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.SubjectID

	name     Name
	aliases  []Name
	semester values.Semester
	folders  []Folder // the system folders first, in the order of Systems, then own folders in the order added
	teachers Teachers
	callLink CallLink // the student's link to the online class (ADR-0032, TASK-364); zero: none

	status    Status
	removedAt time.Time // zero while active

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
	s := &Subject{id: spec.ID, name: name, semester: spec.Semester, status: StatusActive, createdAt: now, updatedAt: now}
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
	Version  int64
	ID       shared.SubjectID
	Name     string
	Aliases  []string
	Semester values.Semester
	Folders  []FolderState
	// Teachers holds one entry per role that has a teacher, in the order of TeacherRoles.
	Teachers []TeacherState
	// CallLink is the canonical call link; "" none. A stored value that no longer parses is restored as no link
	// (ADR-0032), so a tightened rule never fails the read.
	CallLink string
	// RemovedAt is when the subject was removed; zero while it is active (the status follows from it).
	RemovedAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LogValue is the id and name only: a JSON log would marshal the teachers' fields.
func (st State) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", string(st.ID)), slog.String("name", st.Name))
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
		version: st.Version, id: st.ID, name: name, semester: st.Semester, status: StatusActive, removedAt: st.RemovedAt,
		createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if !st.RemovedAt.IsZero() {
		s.status = StatusRemoved
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
	for _, ts := range st.Teachers {
		i, ok := ts.Role.index()
		if !ok || !s.teachers.byRole[i].IsZero() {
			return nil, fmt.Errorf("%w: stored teacher role", ErrInvalidSubject)
		}
		t, err := NewTeacher(ts.Name, ts.Email)
		if err != nil || t.name != ts.Name || t.email != ts.Email {
			return nil, fmt.Errorf("%w: stored teacher", ErrInvalidSubject)
		}
		s.teachers.byRole[i] = t
	}
	if l, err := ParseCallLink(st.CallLink); err == nil && l.link == st.CallLink {
		s.callLink = l
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
	if (s.status == StatusRemoved) == s.removedAt.IsZero() || (s.status != StatusActive && s.status != StatusRemoved) {
		return fmt.Errorf("%w: status %q with removal time %v", ErrInvalidSubject, s.status, s.removedAt)
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

// Remove removes the subject softly (ADR-0027): its notes and files are hidden with it and nothing else changes, so
// Restore brings everything back as it was. A removed subject changes nothing (changed is false).
func (s *Subject) Remove(now time.Time) (changed bool, err error) {
	if s.status == StatusRemoved {
		return false, nil
	}
	if now.IsZero() {
		return false, fmt.Errorf("%w: removal without a time", ErrInvalidSubject)
	}
	if err := s.transition(StatusRemoved, now); err != nil {
		return false, err
	}
	s.removedAt = now
	return true, nil
}

// Restore brings a removed subject back while RestoreWindow has not passed since its removal; an active one changes
// nothing (changed is false). Error: ErrRestoreExpired.
func (s *Subject) Restore(now time.Time) (changed bool, err error) {
	if s.status == StatusActive {
		return false, nil
	}
	if s.PurgeDue(now) {
		return false, ErrRestoreExpired
	}
	if err := s.transition(StatusActive, now); err != nil {
		return false, err
	}
	s.removedAt = time.Time{}
	return true, nil
}

// PurgeDue reports whether the subject was removed at least RestoreWindow before now, so it can no longer be restored
// and is to be purged.
func (s *Subject) PurgeDue(now time.Time) bool {
	return s.status == StatusRemoved && !now.Before(s.PurgeAt())
}

// PurgeAt returns when the restore window of a removed subject ends and it is due to be purged; zero while it is
// active.
func (s *Subject) PurgeAt() time.Time {
	if s.status != StatusRemoved {
		return time.Time{}
	}
	return s.removedAt.Add(RestoreWindow)
}

// PurgeCutoff is the latest removal time whose subject is due to be purged at now: PurgeDue(now) holds exactly for a
// removed subject whose RemovedAt is not after it. The store re-checks a purge against it.
func PurgeCutoff(now time.Time) time.Time { return now.Add(-RestoreWindow) }

func (s *Subject) transition(to Status, now time.Time) error {
	if !slices.Contains(transitions[s.status], to) {
		return fmt.Errorf("%w: status %s to %s", ErrInvalidSubject, s.status, to)
	}
	s.status, s.updatedAt = to, now
	return nil
}

// active refuses a change of a removed subject.
func (s *Subject) active() error {
	if s.status != StatusActive {
		return ErrRemoved
	}
	return nil
}

// AddAlias adds another spelling of the subject. A spelling with the key of the name or of an alias changes nothing
// (changed is false). Errors: ErrRemoved, ErrInvalidName, ErrTooMany.
func (s *Subject) AddAlias(alias string, now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
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

// RemoveAlias removes the alias with the key of alias (values.Key), for undoing aliases added from the schedule. The
// name is never removed: a spelling with its key, or with no alias's key, changes nothing (changed is false). Error:
// ErrRemoved.
func (s *Subject) RemoveAlias(alias string, now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
	key := values.Key(alias)
	i := slices.IndexFunc(s.aliases, func(a Name) bool { return a.Key() == key })
	if key == "" || i < 0 {
		return false, nil
	}
	s.aliases = slices.Delete(s.aliases, i, i+1)
	s.updatedAt = now
	return true, nil
}

// SetTeacher sets the teacher of role (ADR-0030), replacing the one set before; the same name and address again
// change nothing (changed is false). Errors: ErrRemoved, or errors wrapping ErrInvalidTeacherRole,
// ErrInvalidTeacherName or ErrInvalidTeacherEmail.
func (s *Subject) SetTeacher(role TeacherRole, name, email string, now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
	i, ok := role.index()
	if !ok {
		return false, ErrInvalidTeacherRole
	}
	t, err := NewTeacher(name, email)
	if err != nil {
		return false, err
	}
	if t == s.teachers.byRole[i] {
		return false, nil
	}
	s.teachers.byRole[i] = t
	s.updatedAt = now
	return true, nil
}

// ClearTeacher removes the teacher of role; a role without one changes nothing (changed is false). Errors: ErrRemoved,
// ErrInvalidTeacherRole.
func (s *Subject) ClearTeacher(role TeacherRole, now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
	i, ok := role.index()
	if !ok {
		return false, ErrInvalidTeacherRole
	}
	if s.teachers.byRole[i].IsZero() {
		return false, nil
	}
	s.teachers.byRole[i] = Teacher{}
	s.updatedAt = now
	return true, nil
}

// SetCallLink sets the link to the online class of the subject (ADR-0032, TASK-364), replacing the one set before;
// the same canonical link again changes nothing (changed is false). Errors: ErrRemoved, or an error wrapping
// ErrInvalidCallLink.
func (s *Subject) SetCallLink(text string, now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
	l, err := ParseCallLink(text)
	if err != nil {
		return false, err
	}
	if l == s.callLink {
		return false, nil
	}
	s.callLink = l
	s.updatedAt = now
	return true, nil
}

// ClearCallLink removes the link to the online class; a subject without one changes nothing (changed is false).
// Error: ErrRemoved.
func (s *Subject) ClearCallLink(now time.Time) (changed bool, err error) {
	if err := s.active(); err != nil {
		return false, err
	}
	if s.callLink.IsZero() {
		return false, nil
	}
	s.callLink = CallLink{}
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
// has, ErrTooMany, ErrInvalidSubject for a bad or repeated id, ErrRemoved.
func (s *Subject) AddFolder(id shared.FolderID, name FolderName, now time.Time) (Folder, error) {
	if err := s.active(); err != nil {
		return Folder{}, err
	}
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
// ErrRemoved, ErrFolderNotFound, ErrSystemFolder, ErrNotEmpty.
func (s *Subject) RemoveFolder(id shared.FolderID, files int, now time.Time) error {
	i := slices.IndexFunc(s.folders, func(f Folder) bool { return f.id == id })
	switch {
	case s.status != StatusActive:
		return ErrRemoved
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

// Removable reports whether the subject may be deleted for good at once (the agent's undo of creating it): files is
// how many files (active or removed) it holds, and a removed file still needs its subject to be restored. Own folders
// go with the subject. Errors: ErrRemoved (a removed subject waits for its purge), ErrNotEmpty.
func (s *Subject) Removable(files int) error {
	if err := s.active(); err != nil {
		return err
	}
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

// Teacher returns the teacher of role; the zero Teacher while none is set or for an unknown role.
func (s *Subject) Teacher(role TeacherRole) Teacher { return s.teachers.Of(role) }

// Teachers returns the teachers of every role (a copy).
func (s *Subject) Teachers() Teachers { return s.teachers }

// CallLink returns the link to the online class; the zero CallLink while none is set.
func (s *Subject) CallLink() CallLink { return s.callLink }

// String names the subject by id and name only, never its teachers or its call link.
func (s *Subject) String() string {
	if s == nil {
		return "<nil subject>"
	}
	return fmt.Sprintf("subject %s %q", s.id, s.name.text)
}

// Format prints String for every verb: %+v and %#v would print the teachers in the unexported fields.
func (s *Subject) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, s.String()) }

// LogValue is the id and name only.
func (s *Subject) LogValue() slog.Value {
	if s == nil {
		return slog.StringValue(s.String())
	}
	return slog.GroupValue(slog.String("id", string(s.id)), slog.String("name", s.name.text))
}

// Status returns the status.
func (s *Subject) Status() Status { return s.status }

// Active reports whether the subject is not removed.
func (s *Subject) Active() bool { return s.status == StatusActive }

// RemovedAt returns when the subject was removed; zero while it is active.
func (s *Subject) RemovedAt() time.Time { return s.removedAt }

// CreatedAt returns the creation time.
func (s *Subject) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns the time of the last change.
func (s *Subject) UpdatedAt() time.Time { return s.updatedAt }

// State returns the stored form, for the repositories.
func (s *Subject) State() State {
	st := State{
		Version: s.version, ID: s.id, Name: s.name.text, Semester: s.semester, CallLink: s.callLink.link,
		RemovedAt: s.removedAt, CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	}
	for i, t := range s.teachers.byRole {
		if !t.IsZero() {
			st.Teachers = append(st.Teachers, TeacherState{Role: teacherRoles[i], Name: t.name, Email: t.email})
		}
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
