package subject

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Fake links per kind: reserved domains, made-up meetings and passwords.
const (
	// Meeting ids are letters only: slog prints a timestamp, and short digit runs in it matched the leak markers.
	generalLink = "https://general.example.org/j/roomgen?pwd=FakeGen1"
	lectureLink = "https://lecture.example.org/j/roomlec?pwd=FakeLec2"
	seminarLink = "https://seminar.example.org/j/roomsem?pwd=FakeSem3"
	labLink     = "https://lab.example.org/j/roomlab?pwd=FakeLab4"
)

var linkOfKind = map[CallKind]string{
	CallGeneral: generalLink, CallLecture: lectureLink, CallSeminar: seminarLink, CallLab: labLink,
}

func mustLink(t *testing.T, s string) CallLink {
	t.Helper()
	l, err := ParseCallLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestParseCallKind(t *testing.T) {
	tests := []struct {
		in      string
		want    CallKind
		wantErr bool
	}{
		{"general", CallGeneral, false},
		{"lecture", CallLecture, false},
		{"seminar", CallSeminar, false},
		{"lab", CallLab, false},
		{"", "", true},
		{"practice", "", true},
		{"Lecture", "", true},
		{" lab", "", true},
	}
	for _, tt := range tests {
		got, err := ParseCallKind(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr || (tt.wantErr && !errors.Is(err, ErrInvalidCallKind)) {
			t.Errorf("ParseCallKind(%q) = %q, %v", tt.in, got, err)
		}
		if CallKind(tt.in).Valid() == tt.wantErr {
			t.Errorf("CallKind(%q).Valid() = %v", tt.in, !tt.wantErr)
		}
	}
}

func TestCallKindOfLesson(t *testing.T) {
	tests := []struct {
		lessonKind string
		want       CallKind
	}{
		{"lecture", CallLecture},
		{"seminar", CallSeminar},
		{"practice", CallSeminar},
		{"lab", CallLab},
		{"", CallGeneral},
		{"unknown", CallGeneral},
		{"Lecture", CallGeneral},
		{"general", CallGeneral},
	}
	for _, tt := range tests {
		if got := CallKindOfLesson(tt.lessonKind); got != tt.want {
			t.Errorf("CallKindOfLesson(%q) = %q, want %q", tt.lessonKind, got, tt.want)
		}
	}
}

func TestCallLinksOf(t *testing.T) {
	c := CallLinks{
		General: mustLink(t, generalLink), Lecture: mustLink(t, lectureLink),
		Seminar: mustLink(t, seminarLink), Lab: mustLink(t, labLink),
	}
	for k, want := range linkOfKind {
		if got := c.Of(k).String(); got != want {
			t.Errorf("Of(%s) = %q", k, got)
		}
	}
	if !c.Of("practice").IsZero() || !c.Of("").IsZero() {
		t.Error("Of of an unknown kind is a link")
	}
	if c.IsZero() || !(CallLinks{}).IsZero() {
		t.Error("IsZero is wrong")
	}
}

// Join over every combination of set links for every lesson kind: the link of the kind if set, else the general one,
// else none; never the link of another kind.
func TestCallLinksJoin(t *testing.T) {
	kinds := []CallKind{CallGeneral, CallLecture, CallSeminar, CallLab}
	for mask := 0; mask < 1<<len(kinds); mask++ {
		var c CallLinks
		set := map[CallKind]bool{}
		for i, k := range kinds {
			if mask&(1<<i) != 0 {
				*c.field(k) = mustLink(t, linkOfKind[k])
				set[k] = true
			}
		}
		for _, lessonKind := range []string{"lecture", "seminar", "practice", "lab", "", "unknown"} {
			own := CallKindOfLesson(lessonKind)
			wantKind := CallKind("")
			switch {
			case own != CallGeneral && set[own]:
				wantKind = own
			case set[CallGeneral]:
				wantKind = CallGeneral
			}
			got, gotKind := c.Join(lessonKind)
			if gotKind != wantKind || got.String() != linkOfKind[wantKind] {
				t.Errorf("links %v, lesson %q: Join = %q %q, want %q %q", set, lessonKind, got.String(), gotKind,
					linkOfKind[wantKind], wantKind)
			}
		}
	}
}

// The named cases of the task, readable on their own.
func TestCallLinksJoinCases(t *testing.T) {
	lecture := CallLinks{Lecture: mustLink(t, lectureLink)}
	general := CallLinks{General: mustLink(t, generalLink)}
	both := CallLinks{General: mustLink(t, generalLink), Seminar: mustLink(t, seminarLink)}
	tests := []struct {
		name       string
		links      CallLinks
		lessonKind string
		want       string
		wantKind   CallKind
	}{
		{"kind only", lecture, "lecture", lectureLink, CallLecture},
		{"general only", general, "seminar", generalLink, CallGeneral},
		{"both: the kind wins", both, "practice", seminarLink, CallSeminar},
		{"both: another kind takes the general", both, "lab", generalLink, CallGeneral},
		{"none", CallLinks{}, "lecture", "", ""},
		{"seminar with only a lecture link", lecture, "seminar", "", ""},
		{"unknown kind with only a lecture link", lecture, "unknown", "", ""},
	}
	for _, tt := range tests {
		got, kind := tt.links.Join(tt.lessonKind)
		if got.String() != tt.want || kind != tt.wantKind {
			t.Errorf("%s: Join(%q) = %q %q, want %q %q", tt.name, tt.lessonKind, got.String(), kind, tt.want, tt.wantKind)
		}
	}
}

// Set and clear per kind: each kind changes only its own link; the same link again, a bad link and a clear of an empty
// kind change nothing and keep the update time.
func TestSetAndClearCallLinkOf(t *testing.T) {
	for _, k := range []CallKind{CallGeneral, CallLecture, CallSeminar, CallLab} {
		t.Run(string(k), func(t *testing.T) {
			s := newSubject(t)
			t1 := t0.Add(time.Hour)
			link := linkOfKind[k]

			changed, err := s.SetCallLinkOf(k, " "+link+" ", t1)
			if err != nil || !changed || s.CallLinks().Of(k).String() != link || !s.UpdatedAt().Equal(t1) {
				t.Fatalf("SetCallLinkOf = %v, %v", changed, err)
			}
			for _, other := range []CallKind{CallGeneral, CallLecture, CallSeminar, CallLab} {
				if other != k && !s.CallLinks().Of(other).IsZero() {
					t.Fatalf("setting %s set %s", k, other)
				}
			}
			if (k == CallGeneral) != !s.CallLink().IsZero() {
				t.Fatalf("CallLink() = %v after setting %s", s.CallLink(), k)
			}

			before := s.State()
			if changed, err := s.SetCallLinkOf(k, strings.Replace(link, "https", "HTTPS", 1), t1.Add(time.Hour)); err != nil || changed {
				t.Fatalf("same link again = %v, %v", changed, err)
			}
			if changed, err := s.SetCallLinkOf(k, "javascript:alert(1)", t1.Add(time.Hour)); !errors.Is(err, ErrInvalidCallLink) || changed {
				t.Fatalf("bad link = %v, %v", changed, err)
			}
			if after := s.State(); fmt.Sprint(after.CallLink, after.CallLinkLecture, after.CallLinkSeminar, after.CallLinkLab) !=
				fmt.Sprint(before.CallLink, before.CallLinkLecture, before.CallLinkSeminar, before.CallLinkLab) ||
				!s.UpdatedAt().Equal(t1) {
				t.Fatalf("an unchanged set changed the subject: %+v", after)
			}

			t2 := t1.Add(2 * time.Hour)
			if changed, err := s.ClearCallLinkOf(k, t2); err != nil || !changed || !s.CallLinks().IsZero() || !s.UpdatedAt().Equal(t2) {
				t.Fatalf("ClearCallLinkOf = %v, %v", changed, err)
			}
			if changed, err := s.ClearCallLinkOf(k, t2.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t2) {
				t.Fatalf("ClearCallLinkOf without a link = %v, %v", changed, err)
			}
		})
	}
}

func TestCallLinkOfBadKindOrRemoved(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetCallLinkOf(CallLecture, lectureLink, t0); err != nil {
		t.Fatal(err)
	}
	for _, k := range []CallKind{"", "practice", "LECTURE"} {
		if changed, err := s.SetCallLinkOf(k, labLink, t0.Add(time.Hour)); !errors.Is(err, ErrInvalidCallKind) || changed {
			t.Errorf("SetCallLinkOf(%q) = %v, %v", k, changed, err)
		}
		if changed, err := s.ClearCallLinkOf(k, t0.Add(time.Hour)); !errors.Is(err, ErrInvalidCallKind) || changed {
			t.Errorf("ClearCallLinkOf(%q) = %v, %v", k, changed, err)
		}
	}
	if !s.UpdatedAt().Equal(t0) || s.CallLinks() != (CallLinks{Lecture: mustLink(t, lectureLink)}) {
		t.Fatalf("a bad kind changed the subject: %v", s.CallLinks())
	}

	if _, err := s.Remove(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetCallLinkOf(CallLab, labLink, t0.Add(2*time.Hour)); !errors.Is(err, ErrRemoved) || changed {
		t.Fatalf("SetCallLinkOf of a removed subject = %v, %v", changed, err)
	}
	if changed, err := s.ClearCallLinkOf(CallLecture, t0.Add(2*time.Hour)); !errors.Is(err, ErrRemoved) || changed {
		t.Fatalf("ClearCallLinkOf of a removed subject = %v, %v", changed, err)
	}
	if s.CallLinks().Lecture.String() != lectureLink {
		t.Fatal("the removed subject lost its link")
	}
}

// Restore reads the four stored strings; a value that no longer parses is no link of that kind only.
func TestRestoreCallLinks(t *testing.T) {
	s := newSubject(t)
	for k, link := range linkOfKind {
		if _, err := s.SetCallLinkOf(k, link, t0); err != nil {
			t.Fatal(err)
		}
	}
	st := s.State()
	st.Version = 2
	if st.CallLink != generalLink || st.CallLinkLecture != lectureLink || st.CallLinkSeminar != seminarLink || st.CallLinkLab != labLink {
		t.Fatalf("state = %q %q %q %q", st.CallLink, st.CallLinkLecture, st.CallLinkSeminar, st.CallLinkLab)
	}
	back, err := Restore(st)
	if err != nil || back.CallLinks() != s.CallLinks() {
		t.Fatalf("Restore = %v, %v", back, err)
	}

	bad := st
	bad.CallLinkSeminar = "javascript:alert(1)"
	bad.CallLinkLab = "HTTPS://lab.example.org/"
	back, err = Restore(bad)
	if err != nil || !back.CallLinks().Seminar.IsZero() || !back.CallLinks().Lab.IsZero() ||
		back.CallLinks().General.String() != generalLink || back.CallLinks().Lecture.String() != lectureLink {
		t.Fatalf("Restore with bad stored links = %v, %v", back.CallLinks(), err)
	}
}

// Every kind's link prints only its host, alone and in CallLinks, through fmt and slog.
func TestPrintingHidesCallLinks(t *testing.T) {
	c := CallLinks{
		General: mustLink(t, generalLink), Lecture: mustLink(t, lectureLink),
		Seminar: mustLink(t, seminarLink), Lab: mustLink(t, labLink),
	}
	leaks := func(out string) bool {
		for _, secret := range []string{"Fake", "/j/", "roomgen", "roomlec", "roomsem", "roomlab"} {
			if strings.Contains(out, secret) {
				return true
			}
		}
		return false
	}
	values := map[string]any{"links": c, "links pointer": &c}
	for k := range linkOfKind {
		values[string(k)] = c.Of(k)
	}
	for label, v := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if out := fmt.Sprintf(verb, v); leaks(out) {
				t.Errorf("%s %s: %s", label, verb, out)
			}
		}
		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "v", v)
		slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "v", v)
		if leaks(text.String()) || leaks(js.String()) {
			t.Errorf("%s through slog: %s %s", label, text.String(), js.String())
		}
	}
	for k, link := range linkOfKind {
		if got, want := fmt.Sprint(c.Of(k)), "call link to "+mustLink(t, link).Host(); got != want {
			t.Errorf("%s prints %q, want %q", k, got, want)
		}
	}
	want := "call links to general general.example.org, lecture lecture.example.org, seminar seminar.example.org, lab lab.example.org"
	if got := fmt.Sprint(c); got != want {
		t.Errorf("links print %q", got)
	}
	if got := fmt.Sprint(CallLinks{}); got != "no call links" {
		t.Errorf("no links print %q", got)
	}
	var text bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("x", "v", c)
	if !strings.Contains(text.String(), "v.lecture=lecture.example.org") {
		t.Errorf("slog of the links: %s", text.String())
	}
}
