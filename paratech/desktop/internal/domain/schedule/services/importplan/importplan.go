package importplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Limits of one import.
const (
	MaxRows      = 5000
	MaxConflicts = 1000 // more are counted as truncated; the preview cannot show them usefully anyway
)

// ErrUnknownSource is returned for a source that has no namespace here.
var ErrUnknownSource = errors.New("unknown import source")

// ErrUnreadableFile is wrapped by parsers for a file that cannot be read as a whole (wrong encoding, no header, too
// large). Row problems are not errors but RowError values.
var ErrUnreadableFile = errors.New("unreadable schedule file")

// ErrInvalidWindow is returned for a Window whose From is not before To (a parser bug, not a file problem).
var ErrInvalidWindow = errors.New("invalid import window")

// Source is the import format and the namespace of its source uids.
type Source string

// Import sources (PRD section 11.2).
const (
	SourceCSV  Source = "csv"
	SourceICS  Source = "ics"
	SourceXLSX Source = "xlsx"
)

// Validate reports whether the source is known.
func (s Source) Validate() error {
	switch s {
	case SourceCSV, SourceICS, SourceXLSX:
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownSource, string(s))
}

// UID returns the stored source uid of a row key.
func (s Source) UID(key string) string { return string(s) + ":" + key }

// Owns reports whether a stored source uid belongs to this source.
func (s Source) Owns(uid string) bool { return strings.HasPrefix(uid, string(s)+":") }

// Row is one parsed lesson of a file. Line is the line number in the file (the first line is 1).
type Row struct {
	Line    int
	Key     string // identity within the source, see the package comment
	Content lesson.Content
}

// RowError is a problem of one row; Line 0 is a problem of the file as a whole. Reason is short English text for
// the user and quotes no cell values.
type RowError struct {
	Line   int
	Field  string // the column, when known
	Reason string
}

// Window is the time range a file covers: lessons starting in [From, To). The zero Window covers all time.
type Window struct {
	From time.Time
	To   time.Time
}

// IsZero reports whether the window covers all time.
func (w Window) IsZero() bool { return w.From.IsZero() && w.To.IsZero() }

// Contains reports whether t lies in the window.
func (w Window) Contains(t time.Time) bool {
	return w.IsZero() || (!t.Before(w.From) && t.Before(w.To))
}

// Parsed is what a parser produced: the rows it could read and the problems of the rows it could not. Window is
// set by parsers that read only part of a file's time range (ICS expands recurrences within it).
type Parsed struct {
	Rows   []Row
	Errors []RowError
	Window Window
}

// Addition is a row without a live lesson of the same uid: a new lesson.
type Addition struct {
	Line    int
	UID     string
	Content lesson.Content
}

// Change is a row matched to a live lesson. In Plan.Unchanged, Before equals After.
type Change struct {
	Line    int
	UID     string
	Lesson  shared.LessonID
	Version int64
	Before  lesson.Content
	After   lesson.Content
}

// Removal is a live lesson of the source that the file no longer contains.
type Removal struct {
	UID     string
	Lesson  shared.LessonID
	Version int64
	Content lesson.Content
}

// Conflict is a time overlap after the import. WithLine is set for an overlap with another row (the later one),
// WithLesson for an overlap with a live lesson the import keeps.
type Conflict struct {
	Line        int
	UID         string
	Content     lesson.Content
	WithLine    int
	WithLesson  shared.LessonID
	WithContent lesson.Content
}

// Plan is the preview of an import. A plan with Errors has no diff and cannot be committed.
type Plan struct {
	Source             Source
	Window             Window // the import dates, zero for the whole file
	Errors             []RowError
	Added              []Addition
	Changed            []Change
	Unchanged          []Change
	Removed            []Removal
	Conflicts          []Conflict
	ConflictsTruncated bool

	// Fingerprint identifies the plan's content (including the versions of the lessons it touches). Commit recomputes
	// the plan and compares, so the user confirms exactly what is written. Empty for a plan with errors.
	Fingerprint string
}

// Valid reports whether the plan can be committed.
func (p Plan) Valid() bool { return len(p.Errors) == 0 }

// HasChanges reports whether committing the plan writes anything.
func (p Plan) HasChanges() bool { return len(p.Added)+len(p.Changed)+len(p.Removed) > 0 }

// Build validates the parsed file and diffs it against the live lessons. Deleted lessons in current are ignored.
func Build(source Source, parsed Parsed, current []*lesson.Lesson) (Plan, error) {
	if err := source.Validate(); err != nil {
		return Plan{}, err
	}
	if w := parsed.Window; !w.IsZero() && !w.From.Before(w.To) {
		return Plan{}, ErrInvalidWindow
	}
	plan := Plan{Source: source, Window: parsed.Window}

	errs := append([]RowError(nil), parsed.Errors...)
	uids := make([]string, len(parsed.Rows))
	firstLine := map[string]int{}
	switch {
	case len(parsed.Rows) == 0 && len(errs) == 0 && parsed.Window.IsZero():
		errs = append(errs, RowError{Reason: "the file contains no lessons"})
	case len(parsed.Rows) == 0 && len(errs) == 0:
		errs = append(errs, RowError{Reason: "the file contains no lessons in the import dates"})
	case len(parsed.Rows) > MaxRows:
		errs = append(errs, RowError{Reason: fmt.Sprintf("more than %d lessons", MaxRows)})
	}
	for i, r := range parsed.Rows {
		uids[i] = source.UID(r.Key)
		if e, bad := rowProblem(r, uids[i], parsed.Window); bad {
			errs = append(errs, e)
			continue
		}
		if first, dup := firstLine[uids[i]]; dup {
			errs = append(errs, RowError{Line: r.Line, Reason: fmt.Sprintf("the same lesson as line %d", first)})
			continue
		}
		firstLine[uids[i]] = r.Line
	}
	if len(errs) > 0 {
		sort.SliceStable(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
		plan.Errors = errs
		return plan, nil
	}

	live := map[string]*lesson.Lesson{}
	var kept []*lesson.Lesson // live lessons the import does not change
	var owned []*lesson.Lesson
	for _, l := range current {
		switch {
		case l.Deleted():
		case source.Owns(l.SourceUID()):
			live[l.SourceUID()] = l
			owned = append(owned, l)
		default:
			kept = append(kept, l)
		}
	}

	for i, r := range parsed.Rows {
		l, ok := live[uids[i]]
		if !ok {
			plan.Added = append(plan.Added, Addition{Line: r.Line, UID: uids[i], Content: r.Content})
			continue
		}
		delete(live, uids[i])
		c := Change{Line: r.Line, UID: uids[i], Lesson: l.ID(), Version: l.Version(), Before: l.Content(), After: r.Content}
		if l.Content().Equal(r.Content) {
			plan.Unchanged = append(plan.Unchanged, c)
		} else {
			plan.Changed = append(plan.Changed, c)
		}
	}
	// owned keeps the order of current, so kept does not depend on map order.
	for _, l := range owned {
		if _, missing := live[l.SourceUID()]; !missing {
			continue
		}
		if !parsed.Window.Contains(l.Content().Start) {
			kept = append(kept, l) // outside the import dates: the file says nothing about it
			continue
		}
		plan.Removed = append(plan.Removed, Removal{UID: l.SourceUID(), Lesson: l.ID(), Version: l.Version(), Content: l.Content()})
	}

	sort.SliceStable(plan.Added, func(i, j int) bool { return plan.Added[i].Line < plan.Added[j].Line })
	sort.SliceStable(plan.Changed, func(i, j int) bool { return plan.Changed[i].Line < plan.Changed[j].Line })
	sort.SliceStable(plan.Unchanged, func(i, j int) bool { return plan.Unchanged[i].Line < plan.Unchanged[j].Line })
	sort.Slice(plan.Removed, func(i, j int) bool {
		a, b := plan.Removed[i], plan.Removed[j]
		if !a.Content.Start.Equal(b.Content.Start) {
			return a.Content.Start.Before(b.Content.Start)
		}
		return a.Lesson < b.Lesson
	})

	plan.Conflicts, plan.ConflictsTruncated = conflicts(parsed.Rows, uids, kept)
	plan.Fingerprint = fingerprint(plan)
	return plan, nil
}

func rowProblem(r Row, uid string, w Window) (RowError, bool) {
	if strings.TrimSpace(r.Key) == "" {
		return RowError{Line: r.Line, Field: "uid", Reason: "no lesson identity"}, true
	}
	if err := lesson.ValidateSourceUID(uid); err != nil {
		return RowError{Line: r.Line, Field: "uid", Reason: reason(err)}, true
	}
	if err := r.Content.Validate(); err != nil {
		return RowError{Line: r.Line, Reason: reason(err)}, true
	}
	if !w.Contains(r.Content.Start) {
		return RowError{Line: r.Line, Reason: "starts outside the import dates"}, true
	}
	return RowError{}, false
}

// reason strips the sentinel prefix: the user sees "end is not after start", not "invalid lesson: ...".
func reason(err error) string {
	return strings.TrimPrefix(err.Error(), lesson.ErrInvalidLesson.Error()+": ")
}

// span is one time range in the conflict sweep: a row (row >= 0) or a kept lesson.
type span struct {
	row int
	l   *lesson.Lesson
	c   lesson.Content
}

// conflicts finds the overlaps that involve at least one row, with a sweep over the ranges ordered by start.
func conflicts(rows []Row, uids []string, kept []*lesson.Lesson) ([]Conflict, bool) {
	spans := make([]span, 0, len(rows)+len(kept))
	for i, r := range rows {
		spans = append(spans, span{row: i, c: r.Content})
	}
	for _, l := range kept {
		spans = append(spans, span{row: -1, l: l, c: l.Content()})
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].c.Start.Before(spans[j].c.Start) })

	var out []Conflict
	truncated := false
	var active []span
	for _, s := range spans {
		n := 0
		for _, a := range active {
			if a.c.End.After(s.c.Start) {
				active[n] = a
				n++
			}
		}
		active = active[:n]

		for _, a := range active {
			if a.row < 0 && s.row < 0 {
				continue
			}
			if len(out) == MaxConflicts {
				truncated = true
				break
			}
			out = append(out, conflictOf(rows, uids, a, s))
		}
		active = append(active, s)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.WithLine != b.WithLine {
			return a.WithLine < b.WithLine
		}
		return a.WithLesson < b.WithLesson
	})
	return out, truncated
}

func conflictOf(rows []Row, uids []string, a, b span) Conflict {
	if a.row < 0 || (b.row >= 0 && rows[b.row].Line < rows[a.row].Line) {
		a, b = b, a // a is the row with the smaller line
	}
	c := Conflict{Line: rows[a.row].Line, UID: uids[a.row], Content: a.c, WithContent: b.c}
	if b.row >= 0 {
		c.WithLine = rows[b.row].Line
	} else {
		c.WithLesson = b.l.ID()
	}
	return c
}

// fingerprint hashes everything the user sees in the preview; fields are length-prefixed so that no two plans
// encode the same.
func fingerprint(p Plan) string {
	h := sha256.New()
	field := func(s string) { _, _ = fmt.Fprintf(h, "%d:%s", len(s), s) } // a hash never fails to write
	num := func(n int64) { field(strconv.FormatInt(n, 10)) }

	field(string(p.Source))
	for _, a := range p.Added {
		field("A")
		num(int64(a.Line))
		field(a.UID)
		writeContent(field, num, a.Content)
	}
	for i, list := range [][]Change{p.Changed, p.Unchanged} {
		for _, c := range list {
			field([]string{"C", "U"}[i])
			num(int64(c.Line))
			field(string(c.Lesson))
			num(c.Version)
			writeContent(field, num, c.After)
		}
	}
	for _, r := range p.Removed {
		field("R")
		field(string(r.Lesson))
		num(r.Version)
	}
	for _, c := range p.Conflicts {
		field("X")
		num(int64(c.Line))
		num(int64(c.WithLine))
		field(string(c.WithLesson))
		// A kept lesson's version is not in the plan, so its shown content must be, or an edit of it keeps the token.
		writeContent(field, num, c.WithContent)
	}
	if p.ConflictsTruncated {
		field("T")
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeContent(field func(string), num func(int64), c lesson.Content) {
	field(c.Title)
	num(c.Start.UnixNano())
	num(c.End.UnixNano())
	field(c.TimeZone)
	field(c.Subject)
	field(c.Kind.String())
	field(c.Teacher)
}
