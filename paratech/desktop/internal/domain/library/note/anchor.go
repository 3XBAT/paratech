package note

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Bounds of the snapshot of an anchor.
const (
	// MaxSnapshotTitleRunes bounds the lesson title kept in a snapshot.
	MaxSnapshotTitleRunes = 200

	// MaxLessonKindBytes bounds the lesson kind code kept in a snapshot.
	MaxLessonKindBytes = 32
)

// AnchorKind is what a note is attached to.
type AnchorKind string

// Kinds of an anchor.
const (
	AnchorRecording AnchorKind = "recording" // a recorded lecture
	AnchorLesson    AnchorKind = "lesson"    // a schedule lesson without a recording, past or future
	AnchorDay       AnchorKind = "day"       // a local date of the subject, with no schedule lesson
	AnchorUndated   AnchorKind = "undated"   // a lecture without a date
)

// AnchorKinds returns every kind, for the schema checks.
func AnchorKinds() []AnchorKind {
	return []AnchorKind{AnchorRecording, AnchorLesson, AnchorDay, AnchorUndated}
}

const dateLayout = "2006-01-02"

// Date is a local calendar date without a time zone. The zero value is no date.
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate builds a date and checks that the day exists, between the years 2000 and 2100.
func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < 2000 || year > 2100 {
		return Date{}, fmt.Errorf("%w: year %d", ErrInvalidDate, year)
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("%w: no such day", ErrInvalidDate)
	}
	return Date{year: year, month: month, day: day}, nil
}

// ParseDate accepts exactly "YYYY-MM-DD" as String writes it.
func ParseDate(s string) (Date, error) {
	if len(s) != len(dateLayout) {
		return Date{}, fmt.Errorf("%w: want YYYY-MM-DD", ErrInvalidDate)
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("%w: want YYYY-MM-DD", ErrInvalidDate)
	}
	return NewDate(t.Year(), t.Month(), t.Day())
}

// IsZero reports that there is no date.
func (d Date) IsZero() bool { return d == Date{} }

// Year returns the year.
func (d Date) Year() int { return d.year }

// Month returns the month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// String returns "YYYY-MM-DD", or "" for no date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}

// AnchorSpec is the raw data of an anchor for NewAnchor.
type AnchorSpec struct {
	Kind      AnchorKind
	Recording shared.RecordingID // kind recording only
	Lesson    shared.LessonID    // kind lesson only

	// The snapshot. Date is required by every kind but undated, which has none. LessonKind is a short code ("lecture")
	// and Title the lesson title; both are optional, untrusted, and cleaned.
	Date       Date
	LessonKind string
	Title      string
}

// Anchor is what a note is attached to, with a snapshot of the date, the lesson kind and the title taken when it
// was set, so the note still reads right after the recording or the lesson is gone. The zero value is no anchor.
type Anchor struct {
	kind       AnchorKind
	recording  shared.RecordingID
	lesson     shared.LessonID
	date       Date
	lessonKind string
	title      string
}

// NewAnchor checks the spec and cleans the snapshot: the title becomes one line of at most MaxSnapshotTitleRunes
// characters, a lesson kind that is not a short code of ASCII letters, digits, '-' or '_' is dropped (it is
// lower-cased first). Error: ErrInvalidAnchor.
func NewAnchor(spec AnchorSpec) (Anchor, error) {
	a := Anchor{
		kind: spec.Kind, recording: spec.Recording, lesson: spec.Lesson, date: spec.Date,
		lessonKind: lessonKind(spec.LessonKind), title: snapshotTitle(spec.Title),
	}
	if err := a.validate(); err != nil {
		return Anchor{}, err
	}
	return a, nil
}

func (a Anchor) validate() error {
	if !slices.Contains(AnchorKinds(), a.kind) {
		return fmt.Errorf("%w: kind %q", ErrInvalidAnchor, a.kind)
	}
	if (a.kind == AnchorRecording) != (a.recording != "") {
		return fmt.Errorf("%w: a recording id belongs to a recording anchor only", ErrInvalidAnchor)
	}
	if (a.kind == AnchorLesson) != (a.lesson != "") {
		return fmt.Errorf("%w: a lesson id belongs to a lesson anchor only", ErrInvalidAnchor)
	}
	if a.recording != "" {
		if err := a.recording.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidAnchor, err)
		}
	}
	if a.lesson != "" {
		if err := a.lesson.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidAnchor, err)
		}
	}
	if (a.kind == AnchorUndated) != a.date.IsZero() {
		return fmt.Errorf("%w: an %s anchor with date %q", ErrInvalidAnchor, a.kind, a.date)
	}
	return nil
}

// FallBackToDay returns the anchor a deleted recording or lesson leaves: a day anchor with the same snapshot. A day
// or undated anchor is returned as it is.
func (a Anchor) FallBackToDay() Anchor {
	if a.kind != AnchorRecording && a.kind != AnchorLesson {
		return a
	}
	a.kind, a.recording, a.lesson = AnchorDay, "", ""
	return a
}

// Kind returns the kind; "" for no anchor.
func (a Anchor) Kind() AnchorKind { return a.kind }

// Recording returns the recording of a recording anchor; "" for the others.
func (a Anchor) Recording() shared.RecordingID { return a.recording }

// Lesson returns the lesson of a lesson anchor; "" for the others.
func (a Anchor) Lesson() shared.LessonID { return a.lesson }

// Date returns the local date of the snapshot; zero for an undated anchor.
func (a Anchor) Date() Date { return a.date }

// LessonKind returns the lesson kind code of the snapshot; "" if unknown.
func (a Anchor) LessonKind() string { return a.lessonKind }

// Title returns the lesson title of the snapshot; "" if unknown.
func (a Anchor) Title() string { return a.title }

// IsZero reports that there is no anchor.
func (a Anchor) IsZero() bool { return a == Anchor{} }

func snapshotTitle(s string) string {
	s = values.CleanText(s)
	if utf8.RuneCountInString(s) > MaxSnapshotTitleRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxSnapshotTitleRunes]))
	}
	return s
}

func lessonKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if len(kind) > MaxLessonKindBytes || strings.ContainsFunc(kind, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	}) {
		return ""
	}
	return kind
}
