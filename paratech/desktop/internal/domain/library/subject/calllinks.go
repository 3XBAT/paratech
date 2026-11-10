package subject

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// CallKind is which call link of a subject is meant (ADR-0032 addendum 2026-11-10): the general one or the one of a
// lesson kind.
type CallKind string

// Call kinds of a subject.
const (
	CallGeneral CallKind = "general"
	CallLecture CallKind = "lecture"
	CallSeminar CallKind = "seminar"
	CallLab     CallKind = "lab"
)

// callKinds is the order of the links in CallLinks and in its printed form.
var callKinds = [...]CallKind{CallGeneral, CallLecture, CallSeminar, CallLab}

// ParseCallKind reads the code of a call kind exactly as written; anything else is ErrInvalidCallKind.
func ParseCallKind(s string) (CallKind, error) {
	if _, ok := CallKind(s).index(); !ok {
		return "", ErrInvalidCallKind
	}
	return CallKind(s), nil
}

// Valid reports whether k is one of the four call kinds.
func (k CallKind) Valid() bool {
	_, ok := k.index()
	return ok
}

func (k CallKind) index() (int, bool) {
	for i, c := range callKinds {
		if c == k {
			return i, true
		}
	}
	return 0, false
}

// CallKindOfLesson tells which link a lesson of lessonKind (the stable code of the schedule kind: "lecture",
// "seminar", "practice", "lab") opens first: practice uses the seminar link (as filing.NoteFolder), an empty or
// unknown kind the general one.
func CallKindOfLesson(lessonKind string) CallKind {
	switch lessonKind {
	case "lecture":
		return CallLecture
	case "seminar", "practice":
		return CallSeminar
	case "lab":
		return CallLab
	default:
		return CallGeneral
	}
}

// CallLinks are the call links of a subject: the general one and one per lesson kind; a zero field is no link. It
// prints (fmt, slog) only the hosts, as CallLink.
type CallLinks struct {
	General, Lecture, Seminar, Lab CallLink
}

// Of returns the link of kind k; zero for an unknown kind.
func (c CallLinks) Of(k CallKind) CallLink {
	if p := c.field(k); p != nil {
		return *p
	}
	return CallLink{}
}

// Join returns the link a lesson of lessonKind opens and the kind it was taken from: the link of
// CallKindOfLesson(lessonKind) if set, else the general one if set, else the zero CallLink and "". The link of another
// kind is never taken: a seminar does not open the lecture link (ADR-0038 D-2).
func (c CallLinks) Join(lessonKind string) (CallLink, CallKind) {
	if k := CallKindOfLesson(lessonKind); !c.Of(k).IsZero() {
		return c.Of(k), k
	}
	if !c.General.IsZero() {
		return c.General, CallGeneral
	}
	return CallLink{}, ""
}

// IsZero reports whether no link is set.
func (c CallLinks) IsZero() bool { return c == CallLinks{} }

// field returns the field of kind k; nil for an unknown kind.
func (c *CallLinks) field(k CallKind) *CallLink {
	switch k {
	case CallGeneral:
		return &c.General
	case CallLecture:
		return &c.Lecture
	case CallSeminar:
		return &c.Seminar
	case CallLab:
		return &c.Lab
	default:
		return nil
	}
}

// String lists the hosts of the set links by kind, never a path or query.
func (c CallLinks) String() string {
	var parts []string
	for _, k := range callKinds {
		if l := c.Of(k); !l.IsZero() {
			parts = append(parts, string(k)+" "+l.host)
		}
	}
	if len(parts) == 0 {
		return "no call links"
	}
	return "call links to " + strings.Join(parts, ", ")
}

// Format prints String for every verb: %#v would print the unexported fields of the links.
func (c CallLinks) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// LogValue is the host of each set link by kind.
func (c CallLinks) LogValue() slog.Value {
	var attrs []slog.Attr
	for _, k := range callKinds {
		if l := c.Of(k); !l.IsZero() {
			attrs = append(attrs, slog.String(string(k), l.host))
		}
	}
	if len(attrs) == 0 {
		return slog.StringValue("no call links")
	}
	return slog.GroupValue(attrs...)
}
