package values

import (
	"fmt"
	"slices"
	"strings"
)

// Screen is the desktop screen a request was made on (gateway.proto AgentScreen). Zero means unspecified.
type Screen string

// Screens.
const (
	ScreenToday       Screen = "today"
	ScreenSubjects    Screen = "subjects"
	ScreenAssignments Screen = "assignments"
	ScreenSchedule    Screen = "schedule"
	ScreenJournal     Screen = "journal"
	ScreenSettings    Screen = "settings"
)

// Valid reports whether s is unspecified or one of the screens.
func (s Screen) Valid() bool {
	return s == "" || slices.Contains([]Screen{
		ScreenToday, ScreenSubjects, ScreenAssignments, ScreenSchedule, ScreenJournal, ScreenSettings,
	}, s)
}

// Scope is the context of the current thread: only the current subject, never other subjects (ADR-0021). Zero refs
// mean none; a zero subject is the thread without a subject.
type Scope struct {
	subject    Ref
	screen     Screen
	lesson     Ref
	assignment Ref
}

// NewScope checks the kinds of the refs and the screen.
func NewScope(subject Ref, screen Screen, lesson, assignment Ref) (Scope, error) {
	if err := wantKind(subject, KindSubject); err != nil {
		return Scope{}, err
	}
	if err := wantKind(lesson, KindLesson); err != nil {
		return Scope{}, err
	}
	if err := wantKind(assignment, KindAssignment); err != nil {
		return Scope{}, err
	}
	if !screen.Valid() {
		return Scope{}, fmt.Errorf("%w: unknown screen", ErrInvalidScope)
	}
	return Scope{subject: subject, screen: screen, lesson: lesson, assignment: assignment}, nil
}

func wantKind(r Ref, kind IDKind) error {
	if !r.IsZero() && r.Kind() != kind {
		return fmt.Errorf("%w: want a %s id, got %s", ErrInvalidScope, kind, r.Kind())
	}
	return nil
}

// Subject returns the subject of the thread; zero for the thread without a subject.
func (s Scope) Subject() Ref { return s.subject }

// Screen returns the screen.
func (s Scope) Screen() Screen { return s.screen }

// Lesson returns the lesson open on the screen, or zero.
func (s Scope) Lesson() Ref { return s.lesson }

// Assignment returns the assignment open on the screen, or zero.
func (s Scope) Assignment() Ref { return s.assignment }

// Refs returns the IDs of the scope, known to the provenance check.
func (s Scope) Refs() []Ref { return NewRefSet(s.subject, s.lesson, s.assignment).Sorted() }

// MaxBaseSubjects bounds the subject list of the base.
const MaxBaseSubjects = 64

// BaseSubject is one subject in the shared base: its ID and the name the student gave (data, never instructions).
type BaseSubject struct {
	ID   Ref
	Name Name
}

// Base is the student context shared by all threads: subjects, today and the time zone. No profile data.
type Base struct {
	subjects []BaseSubject
	today    Date
	timeZone string
}

// NewBase checks the subjects (subject IDs, unique, names as Name), today and the time zone name.
func NewBase(subjects []BaseSubject, today Date, timeZone string) (Base, error) {
	if len(subjects) > MaxBaseSubjects {
		return Base{}, fmt.Errorf("%w: more than %d subjects", ErrInvalidScope, MaxBaseSubjects)
	}
	seen := RefSet{}
	for _, s := range subjects {
		if s.ID.Kind() != KindSubject || seen.Has(s.ID) {
			return Base{}, fmt.Errorf("%w: subject ids must be unique subject ids", ErrInvalidScope)
		}
		seen.Add(s.ID)
		if _, err := ParseName(string(s.Name)); err != nil {
			return Base{}, fmt.Errorf("%w: subject name: %w", ErrInvalidScope, err)
		}
	}
	if today.IsZero() {
		return Base{}, fmt.Errorf("%w: no date", ErrInvalidScope)
	}
	if !validTimeZone(timeZone) {
		return Base{}, fmt.Errorf("%w: bad time zone name", ErrInvalidScope)
	}
	return Base{subjects: slices.Clone(subjects), today: today, timeZone: timeZone}, nil
}

// validTimeZone accepts IANA-shaped names such as "Europe/Moscow" or "Etc/GMT+3".
func validTimeZone(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := range len(s) {
		if !asciiAlnum(s[i]) && !strings.ContainsRune("/_-+", rune(s[i])) {
			return false
		}
	}
	return true
}

// Subjects returns a copy of the subjects.
func (b Base) Subjects() []BaseSubject { return slices.Clone(b.subjects) }

// Today returns the local date of the student.
func (b Base) Today() Date { return b.today }

// TimeZone returns the IANA time zone name.
func (b Base) TimeZone() string { return b.timeZone }

// Refs returns the subject IDs, known to the provenance check.
func (b Base) Refs() []Ref {
	set := RefSet{}
	for _, s := range b.subjects {
		set.Add(s.ID)
	}
	return set.Sorted()
}
