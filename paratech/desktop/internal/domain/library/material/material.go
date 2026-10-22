package material

import (
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Limits of the aggregate.
const (
	// MaxNameRunes bounds the original file name kept for display.
	MaxNameRunes = 255

	// MaxLessons bounds the lessons one material is linked to.
	MaxLessons = 50
)

// Status is whether the material is in the library or removed (soft delete).
type Status string

// Statuses of a material.
const (
	StatusActive  Status = "active"
	StatusRemoved Status = "removed"
)

// Statuses returns every status, for the schema checks.
func Statuses() []Status { return []Status{StatusActive, StatusRemoved} }

// transitions is the status machine (ARCH-19).
var transitions = map[Status][]Status{
	StatusActive:  {StatusRemoved},
	StatusRemoved: {StatusActive},
}

// TextStatus is how far the text of the material was read; the extraction itself comes with the agent tools.
type TextStatus string

// Text statuses.
const (
	TextPending         TextStatus = "pending"           // not read yet
	TextExtracted       TextStatus = "extracted"         // read
	TextFailed          TextStatus = "failed"            // could not be read
	TextPDFNotExtracted TextStatus = "pdf_not_extracted" // PDF text is not read (ADR-0024): «текст PDF не извлечён»
)

// TextStatuses returns every text status, for the schema checks.
func TextStatuses() []TextStatus {
	return []TextStatus{TextPending, TextExtracted, TextFailed, TextPDFNotExtracted}
}

func (s TextStatus) valid() bool { return slices.Contains(TextStatuses(), s) }

// Material is one file of the library. Fields change only through the methods below.
type Material struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.MaterialID

	digest Digest
	size   int64
	typ    Type
	name   string // original file name, display only

	// Both empty: in «Входящие». Both set: filed in folder of subject.
	subject shared.SubjectID
	folder  shared.FolderID
	lessons []shared.LessonID

	status Status
	text   TextStatus

	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a material provides to New.
type Spec struct {
	ID     shared.MaterialID
	Digest Digest
	Size   int64
	Type   Type
	Name   string // the original file name; cleaned and cut for display
}

// New creates an active material in «Входящие», never stored (Version() == 0). A PDF starts with
// TextPDFNotExtracted, the others with TextPending.
func New(spec Spec, now time.Time) (*Material, error) {
	m := &Material{
		id: spec.ID, digest: spec.Digest, size: spec.Size, typ: spec.Type, name: displayName(spec.Name),
		status: StatusActive, text: TextPending, createdAt: now, updatedAt: now,
	}
	if spec.Type == TypePDF {
		m.text = TextPDFNotExtracted
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// displayName cleans a file name to one line of at most MaxNameRunes characters.
func displayName(s string) string {
	s = values.CleanText(s)
	if utf8.RuneCountInString(s) > MaxNameRunes {
		s = string([]rune(s)[:MaxNameRunes])
	}
	return s
}

// State is the stored form of a material, for Restore and the repositories.
type State struct {
	Version   int64
	ID        shared.MaterialID
	Digest    Digest
	Size      int64
	Type      Type
	Name      string
	Subject   shared.SubjectID // empty with Folder: «Входящие»
	Folder    shared.FolderID
	Lessons   []shared.LessonID
	Status    Status
	Text      TextStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored material without side effects; it only checks that the state is consistent.
func Restore(st State) (*Material, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidMaterial, st.Version)
	}
	if displayName(st.Name) != st.Name {
		return nil, fmt.Errorf("%w: stored name", ErrInvalidMaterial)
	}
	m := &Material{
		version: st.Version, id: st.ID, digest: st.Digest, size: st.Size, typ: st.Type, name: st.Name,
		subject: st.Subject, folder: st.Folder, lessons: slices.Clone(st.Lessons), status: st.Status, text: st.Text,
		createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Material) validate() error {
	if err := m.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
	}
	if err := m.digest.Validate(); err != nil {
		return err
	}
	switch {
	case m.size < 1 || m.size > MaxBytes:
		return fmt.Errorf("%w: size %d", ErrInvalidMaterial, m.size)
	case !m.typ.valid():
		return fmt.Errorf("%w: type %q", ErrInvalidMaterial, m.typ)
	case m.status != StatusActive && m.status != StatusRemoved:
		return fmt.Errorf("%w: status %q", ErrInvalidMaterial, m.status)
	case !m.text.valid():
		return fmt.Errorf("%w: text status %q", ErrInvalidMaterial, m.text)
	case m.createdAt.IsZero() || m.updatedAt.IsZero():
		return fmt.Errorf("%w: missing time", ErrInvalidMaterial)
	case (m.subject == "") != (m.folder == ""):
		return fmt.Errorf("%w: a subject without a folder or the reverse", ErrInvalidMaterial)
	case len(m.lessons) > MaxLessons:
		return fmt.Errorf("%w: %d lessons", ErrInvalidMaterial, len(m.lessons))
	}
	if m.subject != "" {
		if err := m.subject.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
		}
		if err := m.folder.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
		}
	}
	seen := map[shared.LessonID]bool{}
	for _, l := range m.lessons {
		if err := l.Validate(); err != nil || seen[l] {
			return fmt.Errorf("%w: lesson %q", ErrInvalidMaterial, l)
		}
		seen[l] = true
	}
	return nil
}

// Place files the material in folder of subject. The caller has checked that the folder belongs to the subject (the
// subject aggregate knows its folders). Placing it where it is changes nothing. Errors: ErrRemoved,
// ErrInvalidMaterial for bad ids.
func (m *Material) Place(subject shared.SubjectID, folder shared.FolderID, now time.Time) (changed bool, err error) {
	if err := subject.Validate(); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
	}
	if err := folder.Validate(); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
	}
	if m.status != StatusActive {
		return false, ErrRemoved
	}
	if m.subject == subject && m.folder == folder {
		return false, nil
	}
	m.subject, m.folder, m.updatedAt = subject, folder, now
	return true, nil
}

// MoveToInbox returns the material to «Входящие» (the undo of Place); the file is kept. Error: ErrRemoved.
func (m *Material) MoveToInbox(now time.Time) (changed bool, err error) {
	if m.status != StatusActive {
		return false, ErrRemoved
	}
	if m.InInbox() {
		return false, nil
	}
	m.subject, m.folder, m.updatedAt = "", "", now
	return true, nil
}

// Link links the material to lesson; a lesson already linked changes nothing. Errors: ErrRemoved,
// ErrTooManyLessons, ErrInvalidMaterial for a bad id.
func (m *Material) Link(lesson shared.LessonID, now time.Time) (changed bool, err error) {
	if err := lesson.Validate(); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidMaterial, err)
	}
	if m.status != StatusActive {
		return false, ErrRemoved
	}
	if slices.Contains(m.lessons, lesson) {
		return false, nil
	}
	if len(m.lessons) >= MaxLessons {
		return false, ErrTooManyLessons
	}
	m.lessons = append(m.lessons, lesson)
	m.updatedAt = now
	return true, nil
}

// Unlink removes the link to lesson; a lesson not linked changes nothing. Error: ErrRemoved.
func (m *Material) Unlink(lesson shared.LessonID, now time.Time) (changed bool, err error) {
	if m.status != StatusActive {
		return false, ErrRemoved
	}
	i := slices.Index(m.lessons, lesson)
	if i < 0 {
		return false, nil
	}
	m.lessons = slices.Delete(m.lessons, i, i+1)
	m.updatedAt = now
	return true, nil
}

// Remove removes the material softly: it keeps its place, links and file, so Restore brings it back as it was. A
// removed one changes nothing.
func (m *Material) Remove(now time.Time) (changed bool, err error) {
	if m.status == StatusRemoved {
		return false, nil
	}
	return true, m.transition(StatusRemoved, now)
}

// Restore brings a removed material back where it was. An active one changes nothing.
func (m *Material) Restore(now time.Time) (changed bool, err error) {
	if m.status == StatusActive {
		return false, nil
	}
	return true, m.transition(StatusActive, now)
}

// Reupload is the same file uploaded again: a removed material comes back into «Входящие» as a new upload would;
// an active one stays where it is (changed is false).
func (m *Material) Reupload(now time.Time) (changed bool, err error) {
	if m.status == StatusActive {
		return false, nil
	}
	if err := m.transition(StatusActive, now); err != nil {
		return false, err
	}
	m.subject, m.folder = "", ""
	return true, nil
}

func (m *Material) transition(to Status, now time.Time) error {
	if !slices.Contains(transitions[m.status], to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, m.status, to)
	}
	m.status, m.updatedAt = to, now
	return nil
}

// ID returns the material id.
func (m *Material) ID() shared.MaterialID { return m.id }

// Digest returns the sha256 of the file.
func (m *Material) Digest() Digest { return m.digest }

// Size returns the size of the file in bytes.
func (m *Material) Size() int64 { return m.size }

// Type returns the detected type.
func (m *Material) Type() Type { return m.typ }

// Name returns the original file name, for display only; it may be "".
func (m *Material) Name() string { return m.name }

// Subject returns the subject it is filed under; "" in «Входящие».
func (m *Material) Subject() shared.SubjectID { return m.subject }

// Folder returns the folder it is filed in; "" in «Входящие».
func (m *Material) Folder() shared.FolderID { return m.folder }

// InInbox reports whether the material is in «Входящие».
func (m *Material) InInbox() bool { return m.subject == "" }

// Lessons returns a copy of the linked lessons in the order linked.
func (m *Material) Lessons() []shared.LessonID { return slices.Clone(m.lessons) }

// Status returns the status.
func (m *Material) Status() Status { return m.status }

// Active reports whether the material is in the library (not removed).
func (m *Material) Active() bool { return m.status == StatusActive }

// TextStatus returns how far the text was read.
func (m *Material) TextStatus() TextStatus { return m.text }

// CreatedAt returns the time of the first upload.
func (m *Material) CreatedAt() time.Time { return m.createdAt }

// UpdatedAt returns the time of the last change.
func (m *Material) UpdatedAt() time.Time { return m.updatedAt }

// State returns the stored form, for the repositories.
func (m *Material) State() State {
	return State{
		Version: m.version, ID: m.id, Digest: m.digest, Size: m.size, Type: m.typ, Name: m.name, Subject: m.subject,
		Folder: m.folder, Lessons: slices.Clone(m.lessons), Status: m.status, Text: m.text, CreatedAt: m.createdAt,
		UpdatedAt: m.updatedAt,
	}
}

// Version returns the version this instance was last loaded or saved with: 0 for a material never stored.
func (m *Material) Version() int64 { return m.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (m *Material) Persisted(version int64) { m.version = version }
