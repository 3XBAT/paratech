package assignment

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

func TestEditByStudent(t *testing.T) {
	now := t0.Add(time.Hour)
	due := t0.Add(48 * time.Hour)
	for name, a := range origins(t) {
		t.Run(name, func(t *testing.T) {
			a.Correct(values.NewLessonNames("Старый", "Иванов"), t0)
			err := a.EditByStudent(StudentEdit{Title: "  Решить 5  ", Note: "Стр. 10\nи 11", Due: due, SubjectName: "Алгебра"}, now)
			if err != nil {
				t.Fatalf("EditByStudent: %v", err)
			}
			st := a.State()
			if st.Title != "Решить 5" || st.Description != "Стр. 10\nи 11" || !st.Due.Equal(due) || !st.UserEdited ||
				st.UserSubject != "Алгебра" || st.UserTeacher != "Иванов" || !st.UpdatedAt.Equal(now) {
				t.Fatalf("state = %+v", st)
			}

			// No subject, no note, no deadline: all removed; unchanged texts still count as an edit.
			later := now.Add(time.Minute)
			if err := a.EditByStudent(StudentEdit{Title: "Решить 5"}, later); err != nil {
				t.Fatalf("bare edit: %v", err)
			}
			if st := a.State(); st.UserSubject != "" || st.Description != "" || !st.Due.IsZero() || !st.UpdatedAt.Equal(later) {
				t.Fatalf("bare state = %+v", st)
			}
		})
	}
}

func TestEditByStudentRefusals(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	early, late := t0.AddDate(0, 0, -31).Add(-time.Minute), t0.AddDate(2, 0, 0).Add(time.Minute)
	cases := []struct {
		name               string
		edit               StudentEdit
		student, extracted error // nil: accepted
	}{
		{"empty title", StudentEdit{Title: " "}, ErrInvalidTitle, ErrInvalidTitle},
		{"title too long", StudentEdit{Title: long(MaxTitleRunes + 1)}, ErrInvalidTitle, ErrInvalidTitle},
		{"title of two lines", StudentEdit{Title: "a\nb"}, ErrInvalidTitle, ErrInvalidTitle},
		{"title with a control", StudentEdit{Title: "a\x07b"}, ErrInvalidTitle, ErrInvalidTitle},
		{"title with a bidi override", StudentEdit{Title: "fdp.\u202Eexe"}, ErrInvalidTitle, ErrInvalidTitle},
		{"title with a bidi isolate", StudentEdit{Title: "a\u2066b"}, ErrInvalidTitle, ErrInvalidTitle},
		{"note with a bidi embedding", StudentEdit{Title: "ok", Note: "a\u202Bb"}, ErrInvalidNote, ErrInvalidNote},
		{"note with a bidi isolate", StudentEdit{Title: "ok", Note: "a\u2069b"}, ErrInvalidNote, ErrInvalidNote},
		{"note with a control", StudentEdit{Title: "ok", Note: "a\x00b"}, ErrInvalidNote, ErrInvalidNote},
		{"note with a tab", StudentEdit{Title: "ok", Note: "a\tb"}, ErrInvalidNote, nil},
		{"note over the student limit", StudentEdit{Title: "ok", Note: long(MaxNoteRunes + 1)}, ErrInvalidNote, nil},
		{"note over the description limit", StudentEdit{Title: "ok", Note: long(MaxDescriptionRunes + 1)}, ErrInvalidNote, ErrInvalidNote},
		{"note not utf-8", StudentEdit{Title: "ok", Note: "a\xffb"}, ErrInvalidNote, ErrInvalidNote},
		{"due too early", StudentEdit{Title: "ok", Due: early}, ErrInvalidDue, nil},
		{"due too late", StudentEdit{Title: "ok", Due: late}, ErrInvalidDue, nil},
		{"due at the early edge", StudentEdit{Title: "ok", Due: t0.AddDate(0, 0, -31)}, nil, nil},
		{"due at the late edge", StudentEdit{Title: "ok", Due: t0.AddDate(2, 0, 0)}, nil, nil},
	}
	for _, c := range cases {
		for origin, want := range map[string]error{"by student": c.student, "extracted": c.extracted} {
			t.Run(c.name+"/"+origin, func(t *testing.T) {
				a := origins(t)[origin]
				before := a.State()
				err := a.EditByStudent(c.edit, t0)
				if want == nil {
					if err != nil {
						t.Fatalf("err = %v, want accepted", err)
					}
					return
				}
				if !errors.Is(err, want) || !errors.Is(err, ErrInvalidDetails) {
					t.Fatalf("err = %v, want %v", err, want)
				}
				if a.State() != before {
					t.Fatal("a refused edit changed the assignment")
				}
			})
		}
	}
}

// The form re-sends the current deadline: one aged out of the window stays editable, only a new one meets the window.
func TestEditByStudentKeepsAnAgedDeadline(t *testing.T) {
	due := t0.Add(48 * time.Hour)
	now := due.AddDate(0, 0, 40) // the deadline is 40 days in the past
	cases := []struct {
		name string
		due  time.Time
		want error // nil: accepted
	}{
		{"unchanged deadline", due, nil},
		{"unchanged deadline in another zone", due.In(time.FixedZone("MSK", 3*3600)), nil},
		{"moved but still too early", due.Add(time.Hour), ErrInvalidDue},
		{"moved too late", now.AddDate(2, 0, 0).Add(time.Minute), ErrInvalidDue},
		{"moved into the window", now.Add(24 * time.Hour), nil},
		{"removed", time.Time{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := byStudent(t, StudentSpec{ID: "a1", Title: "Mine", Due: due})
			before := a.State()
			err := a.EditByStudent(StudentEdit{Title: "Renamed", Note: "new note", Due: c.due}, now)
			if c.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want accepted", err)
				}
				if st := a.State(); st.Title != "Renamed" || st.Description != "new note" || !st.Due.Equal(c.due) {
					t.Fatalf("state = %+v", st)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if a.State() != before {
				t.Fatal("a refused edit changed the assignment")
			}
		})
	}

	// No deadline before and after counts as unchanged.
	a := byStudent(t, StudentSpec{ID: "a2", Title: "Mine"})
	if err := a.EditByStudent(StudentEdit{Title: "Renamed"}, now); err != nil {
		t.Fatalf("no deadline: %v", err)
	}
}

func TestEditByStudentOfADeletedOne(t *testing.T) {
	a := byStudent(t, StudentSpec{ID: "a1", Title: "Mine"})
	if err := a.Delete(t0); err != nil {
		t.Fatal(err)
	}
	before := a.State()
	if err := a.EditByStudent(StudentEdit{Title: "Other"}, t0); !errors.Is(err, ErrDeleted) {
		t.Fatalf("err = %v, want ErrDeleted", err)
	}
	if a.State() != before {
		t.Fatal("the deleted assignment changed")
	}
}

// An edited extracted assignment is never overwritten by reprocessing.
func TestEditByStudentMarksAnExtractedOneEdited(t *testing.T) {
	a := extracted(t)
	if err := a.EditByStudent(StudentEdit{Title: "Mine"}, t0); err != nil {
		t.Fatal(err)
	}
	changed, err := a.Reextract(details(t, "Theirs", "", time.Time{}), 2, t0)
	if err != nil || changed || a.Details().Title() != "Mine" || !a.UserEdited() {
		t.Fatalf("Reextract = %v, %v; details %+v", changed, err, a.Details())
	}
}
