package proposal

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

// oneTeacher is a subject with only its lecturer filled in: B-6 sends everything to that teacher.
var oneTeacher = Teachers{Subject: "s1", Lecture: Teacher{Name: "Иванова И. И.", Email: "teacher@school.example"}}

// threeTeachers has a teacher with an own address for every role.
var threeTeachers = Teachers{
	Subject:  "s1",
	Lecture:  Teacher{Name: "Лектор", Email: "lecture@school.example"},
	Practice: Teacher{Name: "Семинарист", Email: "practice@school.example"},
	Lab:      Teacher{Name: "Лаборант", Email: "lab@school.example"},
}

func TestChooseRecipient(t *testing.T) {
	twoNoLab := threeTeachers
	twoNoLab.Lab = Teacher{}
	sameAddress := Teachers{
		Subject: "s1", Lecture: Teacher{Name: "Иванова", Email: "one@school.example"},
		Practice: Teacher{Name: "Иванова И.", Email: "one@school.example"},
	}
	badEmail := Teachers{Subject: "s1", Lecture: Teacher{Name: "X", Email: "a@b.example, c@d.example"}}
	onlyLab := Teachers{Subject: "s1", Lab: Teacher{Name: "Лаборант", Email: "lab@school.example"}}

	cases := []struct {
		name    string
		ts      Teachers
		kind    values.LessonKind
		address string
		role    TeacherRole
		reason  RejectReason
		options []TeacherRole
	}{
		{"lecture to the lecturer", threeTeachers, values.LessonKindLecture, "lecture@school.example", RoleLecture, "", nil},
		{"seminar to the practice teacher", threeTeachers, values.LessonKindSeminar, "practice@school.example", RolePractice, "", nil},
		{"practice to the practice teacher", threeTeachers, values.LessonKindPractice, "practice@school.example", RolePractice, "", nil},
		{"lab to the lab teacher", threeTeachers, values.LessonKindLab, "lab@school.example", RoleLab, "", nil},
		{"one teacher gets a lab (B-6)", oneTeacher, values.LessonKindLab, "teacher@school.example", RoleLecture, "", nil},
		{"one teacher gets a seminar", oneTeacher, values.LessonKindSeminar, "teacher@school.example", RoleLecture, "", nil},
		{"one teacher, no lesson", oneTeacher, values.LessonKindUnknown, "teacher@school.example", RoleLecture, "", nil},
		{"only the lab teacher gets a lecture", onlyLab, values.LessonKindLecture, "lab@school.example", RoleLab, "", nil},
		{"no lesson: choose", threeTeachers, values.LessonKindUnknown, "", "", ReasonChooseTeacher,
			[]TeacherRole{RoleLecture, RolePractice, RoleLab}},
		{"unknown kind: choose", twoNoLab, values.LessonKind("exam"), "", "", ReasonChooseTeacher,
			[]TeacherRole{RoleLecture, RolePractice}},
		{"no lesson, one address: no question", sameAddress, values.LessonKindUnknown, "one@school.example", RoleLecture, "", nil},
		{"lab without a lab teacher (B-1)", twoNoLab, values.LessonKindLab, "", RoleLab, ReasonNoTeacherEmail, nil},
		{"same address twice, lab empty", sameAddress, values.LessonKindLab, "", RoleLab, ReasonNoTeacherEmail, nil},
		{"no teachers", Teachers{Subject: "s1"}, values.LessonKindLecture, "", RoleLecture, ReasonNoTeacherEmail, nil},
		{"no teachers, no lesson", Teachers{Subject: "s1"}, values.LessonKindUnknown, "", "", ReasonNoTeacherEmail, nil},
		{"an invalid address is none", badEmail, values.LessonKindLecture, "", RoleLecture, ReasonNoTeacherEmail, nil},
		{"no subject", Teachers{Lecture: oneTeacher.Lecture}, values.LessonKindLecture, "", RoleLecture, ReasonNoSubject, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ChooseRecipient(tc.ts, tc.kind)
			if c.Recipient.Address() != tc.address || c.Role != tc.role || c.Reason != tc.reason {
				t.Fatalf("got %q %q %q, want %q %q %q", c.Recipient.Address(), c.Role, c.Reason, tc.address, tc.role, tc.reason)
			}
			if c.Reason == "" && c.Name != tc.ts.Of(c.Role).Name {
				t.Fatalf("name = %q", c.Name)
			}
			var roles []TeacherRole
			for _, o := range c.Options {
				roles = append(roles, o.Role)
				if o.Recipient.Address() != tc.ts.Of(o.Role).Email || o.Name != tc.ts.Of(o.Role).Name {
					t.Fatalf("option %s = %q %q", o.Role, o.Name, o.Recipient.Address())
				}
			}
			if fmt.Sprint(roles) != fmt.Sprint(tc.options) {
				t.Fatalf("options = %v, want %v", roles, tc.options)
			}
		})
	}
}

// Roles with one address are one option; the first role in order names it.
func TestChooseRecipientMergesOneAddress(t *testing.T) {
	ts := threeTeachers
	ts.Lab.Email = ts.Lecture.Email
	c := ChooseRecipient(ts, values.LessonKindUnknown)
	if c.Reason != ReasonChooseTeacher || len(c.Options) != 2 || c.Options[0].Role != RoleLecture || c.Options[1].Role != RolePractice {
		t.Fatalf("got %+v", c)
	}
}

func TestChosenRecipient(t *testing.T) {
	cases := []struct {
		name    string
		ts      Teachers
		role    TeacherRole
		shown   string
		address string
		reason  RejectReason
	}{
		{"the chosen lab teacher", threeTeachers, RoleLab, "lab@school.example", "lab@school.example", ""},
		{"the chosen lecturer", threeTeachers, RoleLecture, "lecture@school.example", "lecture@school.example", ""},
		{"no role", threeTeachers, "", "lab@school.example", "", ReasonChooseTeacher},
		{"unknown role", threeTeachers, "dean", "lab@school.example", "", ReasonChooseTeacher},
		{"address changed", threeTeachers, RoleLab, "old@school.example", "", ReasonRecipientChanged},
		{"role emptied", oneTeacher, RoleLab, "lab@school.example", "", ReasonNoTeacherEmail},
		{"no subject", Teachers{}, RoleLab, "lab@school.example", "", ReasonNoSubject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ChosenRecipient(tc.ts, tc.role, tc.shown)
			if c.Recipient.Address() != tc.address || c.Reason != tc.reason {
				t.Fatalf("got %q %q, want %q %q", c.Recipient.Address(), c.Reason, tc.address, tc.reason)
			}
			if tc.reason == ReasonChooseTeacher && len(c.Options) != 3 {
				t.Fatalf("options = %v", c.Options)
			}
		})
	}
}

// choosing is a send of a lesson without kind to a subject with three teachers: VALIDATED without a recipient.
func choosing(t *testing.T) (*Proposal, Facts) {
	t.Helper()
	p := mustNew(t, outEmail)
	f := goodFacts(t)
	f.Teachers, f.Lesson = threeTeachers, values.LessonKindUnknown
	if err := p.Validate(f, t0); err != nil {
		t.Fatal(err)
	}
	if p.Status() != StatusValidated || !p.Recipient().IsZero() || !p.AwaitsTeacherChoice() {
		t.Fatalf("status %s recipient %q awaits %v", p.Status(), p.Recipient().Address(), p.AwaitsTeacherChoice())
	}
	return p, f
}

func TestConfirmChoice(t *testing.T) {
	later := t0.Add(time.Minute)
	t.Run("a role is required", func(t *testing.T) {
		p, _ := choosing(t)
		for _, confirm := range []func() error{
			func() error { return p.Confirm(later) },
			func() error { _, err := p.ConfirmOnce(later); return err },
			func() error { _, err := p.ConfirmChoice("", "lab@school.example", later); return err },
			func() error { _, err := p.ConfirmChoice("dean", "lab@school.example", later); return err },
		} {
			if err := confirm(); !errors.Is(err, ErrChooseTeacher) || p.Status() != StatusValidated || !p.Recipient().IsZero() {
				t.Fatalf("err = %v, status %s", err, p.Status())
			}
		}
	})
	t.Run("a bad shown address", func(t *testing.T) {
		p, _ := choosing(t)
		if _, err := p.ConfirmChoice(RoleLab, "a@b.example\r\nBcc: x@evil.example", later); !errors.Is(err, ErrInvalidRecipient) ||
			p.Status() != StatusValidated {
			t.Fatalf("err = %v, status %s", err, p.Status())
		}
	})
	t.Run("the chosen role is sent to", func(t *testing.T) {
		p, f := choosing(t)
		changed, err := p.ConfirmChoice(RoleLab, "lab@school.example", later)
		if err != nil || !changed || p.Status() != StatusConfirmed || p.Recipient().Address() != "lab@school.example" {
			t.Fatalf("changed %v err %v status %s recipient %q", changed, err, p.Status(), p.Recipient().Address())
		}
		if changed, err := p.ConfirmChoice(RoleLab, "lab@school.example", later); err != nil || changed {
			t.Fatalf("repeat: changed %v err %v", changed, err)
		}
		f.ChosenRole = RoleLab
		if err := p.Recheck(f, later); err != nil {
			t.Fatal(err)
		}
		cmd, err := p.EmailCommand()
		if err != nil || cmd.Recipient().Address() != "lab@school.example" {
			t.Fatalf("command to %q, %v", cmd.Recipient().Address(), err)
		}
	})
	t.Run("a pinned send ignores the role", func(t *testing.T) {
		p := inStatus(t, outEmail, StatusValidated)
		if changed, err := p.ConfirmChoice(RoleLab, "lab@school.example", later); err != nil || !changed ||
			p.Recipient().Address() != "teacher@school.example" {
			t.Fatalf("changed %v err %v recipient %q", changed, err, p.Recipient().Address())
		}
	})
}

// The address of the chosen role at «Да» must be the one the card showed; without the role (an Execute after a crash)
// the pinned address must still be one of the options.
func TestRecheckChosenRecipient(t *testing.T) {
	later := t0.Add(time.Minute)
	cases := []struct {
		name   string
		facts  func(f *Facts)
		reason RejectReason
	}{
		{"unchanged", nil, ""},
		{"the chosen role's address changed", func(f *Facts) { f.Teachers.Lab.Email = "new-lab@school.example" }, ReasonRecipientChanged},
		{"the chosen role emptied", func(f *Facts) { f.Teachers.Lab = Teacher{} }, ReasonRecipientChanged},
		{"another role claimed", func(f *Facts) { f.ChosenRole = RoleLecture }, ReasonRecipientChanged},
		{"no role, still an option", func(f *Facts) { f.ChosenRole = "" }, ""},
		{"no role, no longer an option", func(f *Facts) {
			f.ChosenRole, f.Teachers.Lab.Email = "", "new-lab@school.example"
		}, ReasonRecipientChanged},
		{"no subject now", func(f *Facts) { f.Teachers.Subject = "" }, ReasonRecipientChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, f := choosing(t)
			if _, err := p.ConfirmChoice(RoleLab, "lab@school.example", later); err != nil {
				t.Fatal(err)
			}
			f.ChosenRole = RoleLab
			if tc.facts != nil {
				tc.facts(&f)
			}
			err := p.Recheck(f, later)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrStale) || p.Status() != StatusStale || p.Reason() != tc.reason {
				t.Fatalf("err = %v, status %s, reason %q", err, p.Status(), p.Reason())
			}
		})
	}

	t.Run("shown address was not the role's", func(t *testing.T) {
		p, f := choosing(t)
		if _, err := p.ConfirmChoice(RoleLab, "attacker@evil.example", later); err != nil {
			t.Fatal(err)
		}
		f.ChosenRole = RoleLab
		if err := p.Recheck(f, later); !errors.Is(err, ErrStale) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRestoreSendWaitingForChoice(t *testing.T) {
	p, _ := choosing(t)
	st := p.State()
	st.Version = 1
	got, err := Restore(st)
	if err != nil || !got.AwaitsTeacherChoice() {
		t.Fatalf("Restore = %v, awaits %v", err, err == nil && got.AwaitsTeacherChoice())
	}
	if err := got.Decline(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	st = got.State()
	if _, err := Restore(st); err != nil {
		t.Fatalf("declined while choosing: %v", err)
	}

	stale := inStatus(t, outEmail, StatusStale).State()
	stale.Version, stale.Recipient = 1, ""
	if _, err := Restore(stale); err == nil {
		t.Fatal("a stale send without a recipient restored")
	}
}

func TestRecipientView(t *testing.T) {
	t.Run("pinned", func(t *testing.T) {
		p := inStatus(t, outEmail, StatusValidated)
		v := p.RecipientView(oneTeacher, values.LessonKindLab)
		if v.Recipient.Address() != "teacher@school.example" || v.Name != "Иванова И. И." || v.Role != RoleLecture || v.Reason != "" {
			t.Fatalf("got %+v", v)
		}
		if v := p.RecipientView(Teachers{Subject: "s1"}, values.LessonKindLab); v.Recipient.Address() != "teacher@school.example" ||
			v.Name != "" {
			t.Fatalf("gone teacher: %+v", v)
		}
	})
	t.Run("choosing", func(t *testing.T) {
		p, _ := choosing(t)
		v := p.RecipientView(threeTeachers, values.LessonKindUnknown)
		if v.Reason != ReasonChooseTeacher || len(v.Options) != 3 || !v.Recipient.IsZero() {
			t.Fatalf("got %+v", v)
		}
	})
	t.Run("no teacher email", func(t *testing.T) {
		p := mustNew(t, outEmail)
		f := goodFacts(t)
		f.Teachers, f.Lesson = Teachers{Subject: "s1", Lecture: threeTeachers.Lecture, Practice: threeTeachers.Practice}, values.LessonKindLab
		if err := p.Validate(f, t0); err != nil {
			t.Fatal(err)
		}
		v := p.RecipientView(f.Teachers, f.Lesson)
		if p.Reason() != ReasonNoTeacherEmail || v.Reason != ReasonNoTeacherEmail || v.Role != RoleLab {
			t.Fatalf("reason %q view %+v", p.Reason(), v)
		}
	})
	t.Run("other kinds", func(t *testing.T) {
		if v := inStatus(t, outExport, StatusValidated).RecipientView(threeTeachers, values.LessonKindLab); v.Reason != "" ||
			!v.Recipient.IsZero() || v.Options != nil {
			t.Fatalf("got %+v", v)
		}
	})
}

// The teachers are personal data: printed or logged by mistake they show only roles.
func TestTeachersPrintRedacted(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	c := ChooseRecipient(threeTeachers, values.LessonKindUnknown)
	f := goodFacts(t)
	f.Teachers = threeTeachers
	log.Info("x", "teachers", threeTeachers, "teacher", threeTeachers.Lab, "option", c.Options[0])
	for _, s := range []string{
		fmt.Sprintf("%v %+v %#v %s %q", threeTeachers, threeTeachers, threeTeachers, threeTeachers, threeTeachers),
		fmt.Sprintf("%v %+v %#v", threeTeachers.Lab, c.Options[0], c.Options), fmt.Sprintf("%+v", f.Teachers), buf.String(),
	} {
		for _, secret := range []string{"school.example", "Лектор", "Лаборант", "Семинарист"} {
			if strings.Contains(s, secret) {
				t.Fatalf("printed %q: %s", secret, s)
			}
		}
	}
}

func TestRoleOf(t *testing.T) {
	for k, want := range map[values.LessonKind]TeacherRole{
		values.LessonKindLecture: RoleLecture, values.LessonKindSeminar: RolePractice, values.LessonKindPractice: RolePractice,
		values.LessonKindLab: RoleLab, values.LessonKindUnknown: "", "exam": "",
	} {
		if got := RoleOf(k); got != want {
			t.Errorf("RoleOf(%q) = %q, want %q", k, got, want)
		}
	}
}
