package proposal

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// TeacherRole is the kind of lessons a teacher of a subject gives (ADR-0030); the same codes as the library's roles,
// a copy of its own (ARCH-11).
type TeacherRole string

// Teacher roles.
const (
	RoleLecture  TeacherRole = "lecture"  // the lecturer
	RolePractice TeacherRole = "practice" // seminars and practice
	RoleLab      TeacherRole = "lab"      // labs
)

// teacherRoles is the order of the roles: of the options of a choice and of the card.
var teacherRoles = [...]TeacherRole{RoleLecture, RolePractice, RoleLab}

// TeacherRoles returns every role in its order: lecture, practice, lab.
func TeacherRoles() []TeacherRole { return teacherRoles[:] }

// Valid reports whether r is one of TeacherRoles.
func (r TeacherRole) Valid() bool { return r == RoleLecture || r == RolePractice || r == RoleLab }

// RoleOf is the role whose teacher gets the sends of a lesson of kind k: lecture -> lecture, seminar and practice ->
// practice, lab -> lab; "" for an unknown kind.
func RoleOf(k values.LessonKind) TeacherRole {
	switch k {
	case values.LessonKindLecture:
		return RoleLecture
	case values.LessonKindSeminar, values.LessonKindPractice:
		return RolePractice
	case values.LessonKindLab:
		return RoleLab
	}
	return ""
}

// Teacher is a teacher of the subject of a send as the core read it from the library: trusted local data the student
// typed, never model text. It is personal data and prints redacted (fmt, slog).
type Teacher struct {
	Name  string
	Email string
}

// String hides the personal data.
func (t Teacher) String() string {
	if t == (Teacher{}) {
		return "no teacher"
	}
	return "teacher"
}

// Format prints String for every verb, so %+v and %#v show no field either.
func (t Teacher) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, t.String()) }

// LogValue keeps the data out of slog.
func (t Teacher) LogValue() slog.Value { return slog.StringValue(t.String()) }

// Teachers are the teachers of the library subject of a send (ADR-0030). Subject is that subject; "" means the
// assignment (or the work) has none, or it is gone. It prints only the roles with a teacher.
type Teachers struct {
	Subject  shared.SubjectID
	Lecture  Teacher
	Practice Teacher
	Lab      Teacher
}

// Of returns the teacher of role; the zero Teacher for an unknown role.
func (ts Teachers) Of(role TeacherRole) Teacher {
	switch role {
	case RoleLecture:
		return ts.Lecture
	case RolePractice:
		return ts.Practice
	case RoleLab:
		return ts.Lab
	}
	return Teacher{}
}

// String names the roles with a teacher, without their data.
func (ts Teachers) String() string {
	var roles []string
	for _, r := range teacherRoles {
		if ts.Of(r) != (Teacher{}) {
			roles = append(roles, string(r))
		}
	}
	if len(roles) == 0 {
		return "no teachers"
	}
	return "teachers: " + strings.Join(roles, ", ")
}

// Format prints String for every verb.
func (ts Teachers) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, ts.String()) }

// LogValue keeps the data out of slog.
func (ts Teachers) LogValue() slog.Value { return slog.StringValue(ts.String()) }

// TeacherOption is a teacher a send may go to: the role, the name and the address. As a choice option it stands for
// every role with that address; Role is the first of them.
type TeacherOption struct {
	Role      TeacherRole
	Name      string
	Recipient Recipient
}

// Format hides the personal data.
func (o TeacherOption) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "teacher "+string(o.Role))
}

// LogValue keeps the data out of slog.
func (o TeacherOption) LogValue() slog.Value { return slog.StringValue("teacher " + string(o.Role)) }

// filled returns the teacher of role when it has a valid address.
func (ts Teachers) filled(role TeacherRole) (TeacherOption, bool) {
	t := ts.Of(role)
	r, err := NewRecipient(t.Email)
	if err != nil {
		return TeacherOption{}, false
	}
	return TeacherOption{Role: role, Name: t.Name, Recipient: r}, true
}

// all returns the teachers with a valid address, in role order.
func (ts Teachers) all() []TeacherOption {
	var out []TeacherOption
	for _, r := range teacherRoles {
		if o, ok := ts.filled(r); ok {
			out = append(out, o)
		}
	}
	return out
}

// options returns one option per distinct address, in role order.
func (ts Teachers) options() []TeacherOption {
	var out []TeacherOption
	for _, o := range ts.all() {
		dup := false
		for _, seen := range out {
			dup = dup || seen.Recipient == o.Recipient
		}
		if !dup {
			out = append(out, o)
		}
	}
	return out
}

// RecipientChoice is where a send goes: a recipient with its name and role, or a reason it has none. Reason is
// ReasonNoSubject, ReasonNoTeacherEmail (Role: the role that has no address; "" when no role has one and the kind is
// unknown), ReasonChooseTeacher (Options: the teachers to choose from) or, from ChosenRecipient only,
// ReasonRecipientChanged.
type RecipientChoice struct {
	Recipient Recipient
	Name      string
	Role      TeacherRole
	Reason    RejectReason
	Options   []TeacherOption
}

func chosen(o TeacherOption) RecipientChoice {
	return RecipientChoice{Recipient: o.Recipient, Name: o.Name, Role: o.Role}
}

// ChooseRecipient picks the teacher a send of a lesson of kind k goes to (ADR-0030):
//
//   - no subject: ReasonNoSubject;
//   - exactly one role with an address: that teacher, whatever the kind (B-6);
//   - a known kind: the teacher of RoleOf(k), or ReasonNoTeacherEmail with that role when it has no address (B-1);
//   - no lesson or an unknown kind: the one address when every filled role has the same one, else
//     ReasonChooseTeacher with one option per address (B-5): the student's click picks, never the model;
//   - no role with an address: ReasonNoTeacherEmail.
//
// The settings address is never a fallback.
func ChooseRecipient(ts Teachers, k values.LessonKind) RecipientChoice {
	role := RoleOf(k)
	if ts.Subject == "" {
		return RecipientChoice{Reason: ReasonNoSubject, Role: role}
	}
	all := ts.all()
	switch {
	case len(all) == 0:
		return RecipientChoice{Reason: ReasonNoTeacherEmail, Role: role}
	case len(all) == 1:
		return chosen(all[0])
	case role != "":
		if o, ok := ts.filled(role); ok {
			return chosen(o)
		}
		return RecipientChoice{Reason: ReasonNoTeacherEmail, Role: role}
	}
	if options := ts.options(); len(options) > 1 {
		return RecipientChoice{Reason: ReasonChooseTeacher, Options: options}
	}
	return chosen(all[0])
}

// ChosenRecipient checks the teacher the student chose on the card at «Да» (B-5): role is the clicked option and shown
// the address the card showed. An unknown or empty role is ReasonChooseTeacher with the options; no subject
// ReasonNoSubject; a role without an address ReasonNoTeacherEmail; another current address of the role than shown
// ReasonRecipientChanged (the proposal goes STALE).
func ChosenRecipient(ts Teachers, role TeacherRole, shown string) RecipientChoice {
	switch {
	case !role.Valid():
		return RecipientChoice{Reason: ReasonChooseTeacher, Options: ts.options()}
	case ts.Subject == "":
		return RecipientChoice{Reason: ReasonNoSubject, Role: role}
	}
	o, ok := ts.filled(role)
	switch {
	case !ok:
		return RecipientChoice{Reason: ReasonNoTeacherEmail, Role: role}
	case o.Recipient.Address() != shown:
		return RecipientChoice{Reason: ReasonRecipientChanged, Role: role}
	}
	return chosen(o)
}

// Find returns the teacher whose address is r, the first role in order; ok is false for none. The card names the
// pinned recipient with it («Кому: <имя> <адрес>»).
func (ts Teachers) Find(r Recipient) (TeacherOption, bool) {
	for _, o := range ts.all() {
		if !r.IsZero() && o.Recipient == r {
			return o, true
		}
	}
	return TeacherOption{}, false
}
