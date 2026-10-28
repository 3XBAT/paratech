package subject

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// TeacherRole is the kind of lessons a teacher of a subject gives (ADR-0030, B-4 and B-5).
type TeacherRole string

// The roles of the teachers of a subject.
const (
	// TeacherLecture is the lecturer (лектор).
	TeacherLecture TeacherRole = "lecture"

	// TeacherPractice gives the seminars and the practice (семинары и практика).
	TeacherPractice TeacherRole = "practice"

	// TeacherLab gives the labs (лабораторные).
	TeacherLab TeacherRole = "lab"
)

// teacherRoles is the order of the roles: in the stored state, in Roles and on the subject page.
var teacherRoles = [...]TeacherRole{TeacherLecture, TeacherPractice, TeacherLab}

// TeacherRoles returns every role in its order: lecture, practice, lab.
func TeacherRoles() []TeacherRole { return teacherRoles[:] }

// Valid reports whether r is one of TeacherRoles.
func (r TeacherRole) Valid() bool {
	_, ok := r.index()
	return ok
}

func (r TeacherRole) index() (int, bool) {
	for i, known := range teacherRoles {
		if r == known {
			return i, true
		}
	}
	return 0, false
}

// Teachers are the teachers of a subject by role, at most one per role. It prints only the roles that have a
// teacher (fmt, slog), never a name or an address. The zero value has none.
type Teachers struct {
	byRole [len(teacherRoles)]Teacher
}

// Of returns the teacher of role; the zero Teacher for a role without one or an unknown role.
func (ts Teachers) Of(role TeacherRole) Teacher {
	i, ok := role.index()
	if !ok {
		return Teacher{}
	}
	return ts.byRole[i]
}

// Roles returns the roles that have a teacher, in the order of TeacherRoles.
func (ts Teachers) Roles() []TeacherRole {
	var out []TeacherRole
	for i, t := range ts.byRole {
		if !t.IsZero() {
			out = append(out, teacherRoles[i])
		}
	}
	return out
}

// IsZero reports whether no role has a teacher.
func (ts Teachers) IsZero() bool { return ts == Teachers{} }

// String names the roles that have a teacher, without their data.
func (ts Teachers) String() string {
	roles := ts.Roles()
	if len(roles) == 0 {
		return "no teachers"
	}
	parts := make([]string, len(roles))
	for i, r := range roles {
		parts[i] = string(r)
	}
	return "teachers: " + strings.Join(parts, ", ")
}

// Format prints String for every verb, %#v included, so the data never reaches a message.
func (ts Teachers) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, ts.String()) }

// LogValue keeps the data out of slog.
func (ts Teachers) LogValue() slog.Value { return slog.StringValue(ts.String()) }

// TeacherState is a stored teacher of a subject. It prints only its role.
type TeacherState struct {
	Role  TeacherRole
	Name  string
	Email string
}

// Format prints the role only, for every verb.
func (t TeacherState) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "teacher "+string(t.Role))
}

// LogValue keeps the data out of slog.
func (t TeacherState) LogValue() slog.Value { return slog.StringValue("teacher " + string(t.Role)) }
