package tests

// Cross-check of the two copies of the e-mail rule (ARCH-11; TASK-235 follow-up, TASK-237): the library checks a
// teacher's address when the student types it (subject.NewTeacher), the proposal checks the recipient of a send
// (proposal.NewRecipient). They must agree: an address the library stores but a proposal refuses would turn into a
// silent no_teacher_email, one the proposal accepts but the library refuses is a rule that drifted.

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/library/subject"
)

func TestTeacherEmailAndRecipientAgree(t *testing.T) {
	corpus := []string{
		"teacher@school.example", "a.b+hw@mail.school.example", "x@y.z", "a-b_c@d-e.example", "a.b.c@school.example",
		"UPPER@School.Example", "1@2.3", "a!#$%&'*+/=?^_`{|}~-@b.example",
		"", "a", "ab", "teacher", "@school.example", "teacher@", "teacher@localhost", "a@b@c.example",
		"teacher@school..example", "teacher@.school.example", "teacher@school.example.", ".teacher@school.example",
		"teacher.@school.example", "tea..cher@school.example", "Teacher <teacher@school.example>",
		"teacher@school.example, attacker@evil.example", "teacher@school.example;attacker@evil.example",
		"teacher@school.example\r\nBcc: attacker@evil.example", "t eacher@school.example", "t\teacher@school.example",
		"учитель@school.example", "teacher@школа.example", "\"t\"@school.example", "t@[127.0.0.1]", "t(c)@school.example",
		"t:x@school.example", "t\\x@school.example", "t\x00@school.example", "t\u200b@school.example",
		strings.Repeat("a", 240) + "@b.example", strings.Repeat("a", 250) + "@b.example",
	}
	for _, addr := range corpus {
		_, teacherErr := subject.NewTeacher("Иванова И. И.", addr)
		_, recipientErr := proposal.NewRecipient(addr)
		if (teacherErr == nil) != (recipientErr == nil) {
			t.Errorf("%q: library accepts %v, proposal accepts %v", addr, teacherErr == nil, recipientErr == nil)
		}
	}

	// The library trims what the student typed and stores the trimmed address, which a proposal then accepts.
	for _, addr := range []string{" teacher@school.example", "teacher@school.example\n", "\tteacher@school.example "} {
		tc, err := subject.NewTeacher("Иванова И. И.", addr)
		if err != nil {
			t.Fatalf("%q: %v", addr, err)
		}
		if _, err := proposal.NewRecipient(tc.Email()); err != nil {
			t.Errorf("%q stored as %q: proposal refuses it: %v", addr, tc.Email(), err)
		}
	}
}
