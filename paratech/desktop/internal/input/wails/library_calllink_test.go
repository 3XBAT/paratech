package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// A fake call link: a reserved domain, a made-up meeting and password.
const (
	fakeCallLink = "https://meet.example.org/j/1234567890?pwd=FakePwd42"
	fakeCallPwd  = "FakePwd42"
)

func (f *fakeLibrary) SetCallLinkOf(_ context.Context, id shared.SubjectID, k subject.CallKind,
	link string) (*subject.Subject, error) {
	f.note("SetCallLinkOf %s %s %s", id, k, link)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) ClearCallLinkOf(_ context.Context, id shared.SubjectID, k subject.CallKind) (*subject.Subject, error) {
	f.note("ClearCallLinkOf %s %s", id, k)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) CreateSubjectWithCallLink(_ context.Context, req library.NewSubject, link string) (*subject.Subject, error) {
	f.created = req
	f.note("CreateSubjectWithCallLink %s", link)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

// OpenJoinLink answers as the use case does: f.err, else no link for the lesson's kind, else what the opener answered
// (f.openErr); the link it would open is recorded in f.opened.
func (f *fakeLibrary) OpenJoinLink(_ context.Context, id shared.SubjectID, lessonKind string) error {
	f.note("OpenJoinLink %s %s", id, lessonKind)
	if f.err != nil {
		return f.err
	}
	link, _ := f.subjects[0].CallLinks().Join(lessonKind)
	switch {
	case link.IsZero():
		return library.ErrNoCallLink
	case f.openErr != nil:
		return fmt.Errorf("%w: %w", library.ErrCallLinkNotOpened, f.openErr)
	}
	f.opened = append(f.opened, link.String())
	return nil
}

// subjectWithCallLink is testSubject with the fake link.
func subjectWithCallLink(t *testing.T) *subject.Subject {
	t.Helper()
	s := testSubject(t)
	if _, err := s.SetCallLink(fakeCallLink, libT0); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLibrarySubjectCarriesCallLink(t *testing.T) {
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()

	list, err := s.Subjects(ctx)
	if err != nil || len(list) != 1 || list[0].CallLink != nil {
		t.Fatalf("no link: %+v, %v", list, err)
	}
	if raw, err := json.Marshal(list[0]); err != nil || !strings.Contains(string(raw), `"callLink":null`) {
		t.Fatalf("json %s, %v", raw, err)
	}

	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	list, err = s.Subjects(ctx)
	if err != nil || list[0].CallLink == nil || *list[0].CallLink != (LibraryCallLink{Link: fakeCallLink, Host: "meet.example.org"}) {
		t.Fatalf("link: %+v, %v", list, err)
	}
	raw, err := json.Marshal(list[0])
	if err != nil || !strings.Contains(string(raw), `"callLink":{"link":"`+fakeCallLink+`","host":"meet.example.org"}`) {
		t.Fatalf("json %s, %v", raw, err)
	}
}

// Fake links per kind: reserved domains, made-up meetings.
const (
	fakeLectureLink = "https://lecture.example.org/j/2"
	fakeLabLink     = "https://lab.example.org/j/4"
)

// subjectWithCallLinks is testSubject with the given links by kind.
func subjectWithCallLinks(t *testing.T, links map[subject.CallKind]string) *subject.Subject {
	t.Helper()
	s := testSubject(t)
	for k, l := range links {
		if _, err := s.SetCallLinkOf(k, l, libT0); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// ADR-0032 addendum 2026-11-10: callLinks holds only the kinds that have a link ({} for none), callLink stays the
// general one; the exact JSON the page reads.
func TestLibrarySubjectCallLinksJSON(t *testing.T) {
	tests := []struct {
		name  string
		links map[subject.CallKind]string
		want  string
	}{
		{"none", nil, `"callLink":null,"callLinks":{}}`},
		{"general only", map[subject.CallKind]string{subject.CallGeneral: fakeCallLink},
			`"callLink":{"link":"` + fakeCallLink + `","host":"meet.example.org"},` +
				`"callLinks":{"general":{"link":"` + fakeCallLink + `","host":"meet.example.org"}}}`},
		{"lecture and lab without the general", map[subject.CallKind]string{subject.CallLecture: fakeLectureLink, subject.CallLab: fakeLabLink},
			`"callLink":null,"callLinks":{"lecture":{"link":"` + fakeLectureLink + `","host":"lecture.example.org"},` +
				`"lab":{"link":"` + fakeLabLink + `","host":"lab.example.org"}}}`},
		{"all four", map[subject.CallKind]string{
			subject.CallGeneral: fakeCallLink, subject.CallLecture: fakeLectureLink,
			subject.CallSeminar: "https://seminar.example.org/j/3", subject.CallLab: fakeLabLink,
		}, `"callLink":{"link":"` + fakeCallLink + `","host":"meet.example.org"},"callLinks":{` +
			`"general":{"link":"` + fakeCallLink + `","host":"meet.example.org"},` +
			`"lecture":{"link":"` + fakeLectureLink + `","host":"lecture.example.org"},` +
			`"seminar":{"link":"https://seminar.example.org/j/3","host":"seminar.example.org"},` +
			`"lab":{"link":"` + fakeLabLink + `","host":"lab.example.org"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(librarySubject(subjectWithCallLinks(t, tt.links)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(string(raw), `"teachers":{"lecture":null,"practice":null,"lab":null},`+tt.want) {
				t.Fatalf("json %s\nwant the end %s", raw, tt.want)
			}
		})
	}
}

func TestLibrarySetAndClearCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	ctx := context.Background()

	res := s.SetSubjectCallLink(ctx, "s1", "general", fakeCallLink)
	if res.Problem != "" || res.Subject == nil || res.Subject.CallLink == nil || res.Subject.CallLink.Link != fakeCallLink ||
		res.Subject.CallLinks.General == nil || res.Subject.CallLinks.General.Link != fakeCallLink {
		t.Fatalf("SetSubjectCallLink = %+v", res)
	}
	if res := s.SetSubjectCallLink(ctx, "s1", "lecture", fakeLectureLink); res.Problem != "" || res.Subject == nil {
		t.Fatalf("SetSubjectCallLink(lecture) = %+v", res)
	}
	uc.subjects = []*subject.Subject{testSubject(t)}
	res = s.ClearSubjectCallLink(ctx, "s1", "general")
	if res.Problem != "" || res.Subject == nil || res.Subject.CallLink != nil || res.Subject.CallLinks != (LibraryCallLinks{}) {
		t.Fatalf("ClearSubjectCallLink = %+v", res)
	}
	if res := s.ClearSubjectCallLink(ctx, "s1", "lab"); res.Problem != "" {
		t.Fatalf("ClearSubjectCallLink(lab) = %+v", res)
	}
	want := []string{
		"SetCallLinkOf s1 general " + fakeCallLink, "SetCallLinkOf s1 lecture " + fakeLectureLink,
		"ClearCallLinkOf s1 general", "ClearCallLinkOf s1 lab",
	}
	if fmt.Sprint(uc.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %q", uc.calls)
	}
	if logs.Len() != 0 {
		t.Fatalf("a success logs nothing: %s", logs.String())
	}
}

// A kind that is not one of the four call kinds is "invalid" and never reaches the use case; the log has the result,
// never the link.
func TestLibraryCallLinkInvalidKind(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"", "practice", "unknown", "Lecture", " lecture", "general;"} {
		t.Run(kind, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
			if res := s.SetSubjectCallLink(ctx, "s1", kind, fakeCallLink); res.Problem != "invalid" || res.Subject != nil {
				t.Errorf("set = %+v, want invalid", res)
			}
			if res := s.ClearSubjectCallLink(ctx, "s1", kind); res.Problem != "invalid" || res.Subject != nil {
				t.Errorf("clear = %+v, want invalid", res)
			}
			if len(uc.calls) != 0 {
				t.Errorf("an invalid kind reached the use case: %q", uc.calls)
			}
			if out := logs.String(); !strings.Contains(out, "invalid") || strings.Contains(out, fakeCallPwd) ||
				strings.Contains(out, "meet.example.org") {
				t.Errorf("log %s", out)
			}
		})
	}
}

func TestLibraryCallLinkProblems(t *testing.T) {
	// A cause that carries the link, as a careless storage error might: it must never reach the log.
	leaky := errors.New("row: " + fakeCallLink)
	tests := []struct {
		name    string
		err     error
		problem string
		logged  bool
	}{
		{"invalid", fmt.Errorf("%w: %w", library.ErrInvalidCallLink, leaky), "invalid", false},
		{"invalid kind", fmt.Errorf("%w: %w", library.ErrInvalidCallKind, leaky), "invalid", true},
		{"not found", library.ErrSubjectNotFound, "not_found", true},
		{"invalid id", library.ErrInvalidRequest, "not_found", true},
		{"busy", library.ErrBusy, "failed", true},
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, leaky), "failed", true},
		{"unexpected", leaky, "failed", true},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.err = fmt.Errorf("wrapped: %w", tt.err)

			for call, res := range map[string]LibraryCallLinkResult{
				"set":   s.SetSubjectCallLink(ctx, "s1", "seminar", fakeCallLink),
				"clear": s.ClearSubjectCallLink(ctx, "s1", "seminar"),
			} {
				if res.Problem != tt.problem || res.Subject != nil {
					t.Errorf("%s = %+v, want problem %q", call, res, tt.problem)
				}
			}
			if tt.problem != "invalid" {
				if res := s.OpenSubjectCallLink(ctx, "s1", "seminar"); res.Problem != tt.problem {
					t.Errorf("open = %+v, want problem %q", res, tt.problem)
				}
			}

			out := logs.String()
			if tt.logged != (out != "") {
				t.Errorf("logged %v, want %v: %s", out != "", tt.logged, out)
			}
			if strings.Contains(out, fakeCallPwd) || strings.Contains(out, "meet.example.org") {
				t.Fatalf("the log has the link: %s", out)
			}
		})
	}
}

// «Подключиться» sends only the subject and the lesson's kind to the use case, which reads the links and opens the
// one of the kind; the binding maps the outcome to a code.
func TestOpenSubjectCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()

	if res := s.OpenSubjectCallLink(ctx, "s1", "lecture"); res.Problem != "no_link" {
		t.Fatalf("no link: %+v", res)
	}

	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	if res := s.OpenSubjectCallLink(ctx, "s1", "unknown"); res.Problem != "" {
		t.Fatalf("open = %+v", res)
	}
	if want := []string{"OpenJoinLink s1 lecture", "OpenJoinLink s1 unknown"}; fmt.Sprint(uc.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %q", uc.calls)
	}

	// The opener refuses (an environment variable in the link, another OS): failed, and its error is logged; it never
	// holds the link.
	uc.openErr = errors.New("weblink: the call link names an environment variable")
	if res := s.OpenSubjectCallLink(ctx, "s1", ""); res.Problem != "failed" {
		t.Fatalf("refused open = %+v", res)
	}
	if out := logs.String(); !strings.Contains(out, "environment variable") || strings.Contains(out, fakeCallPwd) {
		t.Fatalf("log %s", out)
	}
}

// A lecture and a seminar of one subject open different links: the lecture its own, the seminar the general one; a
// lab of a subject with only a lecture link has none (ADR-0032 addendum 2026-11-10).
func TestOpenSubjectCallLinkByLessonKind(t *testing.T) {
	ctx := context.Background()
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithCallLinks(t, map[subject.CallKind]string{
		subject.CallGeneral: fakeCallLink, subject.CallLecture: fakeLectureLink,
	})}
	for _, kind := range []string{"lecture", "seminar"} {
		if res := s.OpenSubjectCallLink(ctx, "s1", kind); res.Problem != "" {
			t.Fatalf("open %s = %+v", kind, res)
		}
	}
	if want := []string{fakeLectureLink, fakeCallLink}; fmt.Sprint(uc.opened) != fmt.Sprint(want) {
		t.Fatalf("opened %q, want %q", uc.opened, want)
	}

	uc.subjects = []*subject.Subject{subjectWithCallLinks(t, map[subject.CallKind]string{subject.CallLecture: fakeLectureLink})}
	if res := s.OpenSubjectCallLink(ctx, "s1", "lab"); res.Problem != "no_link" {
		t.Fatalf("lab without links = %+v", res)
	}
}

// «Создать предмет» of a lesson card: name and link go to the use case in one call, and the outcome is a code.
func TestCreateSubjectWithCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	ctx := context.Background()

	res := s.CreateSubjectWithCallLink(ctx, "Физика", fakeCallLink)
	if res.Problem != "" || res.Subject == nil || res.Subject.ID != "s1" || res.Subject.CallLink == nil ||
		res.Subject.CallLink.Link != fakeCallLink || res.RemovedSubjectID != "" {
		t.Fatalf("CreateSubjectWithCallLink = %+v", res)
	}
	if uc.created.Name != "Физика" || len(uc.created.Aliases) != 0 || uc.created.Semester != "" ||
		fmt.Sprint(uc.calls) != fmt.Sprint([]string{"CreateSubjectWithCallLink " + fakeCallLink}) {
		t.Fatalf("created %+v, calls %q", uc.created, uc.calls)
	}
	raw, err := json.Marshal(res)
	if err != nil || !strings.Contains(string(raw), `"problem":"","subject":{`) || !strings.Contains(string(raw), `"removedSubjectId":"","removedVersion":0`) {
		t.Fatalf("json %s, %v", raw, err)
	}
	if logs.Len() != 0 {
		t.Fatalf("a success logs nothing: %s", logs.String())
	}
}

func TestCreateSubjectWithCallLinkProblems(t *testing.T) {
	leaky := errors.New("row: " + fakeCallLink)
	removed := testSubject(t)
	removed.Persisted(7)
	tests := []struct {
		name    string
		err     error
		removed bool // the removed subject can be read
		problem string
	}{
		{"invalid link", fmt.Errorf("%w: %w", library.ErrInvalidCallLink, leaky), false, "invalid"},
		{"invalid name", library.ErrInvalidName, false, "invalid_name"},
		{"name taken", library.ErrNameTaken, false, "name_taken"},
		{"removed name", &library.RemovedNameError{Subject: "s1"}, true, "removed_name_taken"},
		{"removed subject gone", &library.RemovedNameError{Subject: "s1"}, false, "failed"},
		{"busy", library.ErrBusy, false, "failed"},
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, leaky), false, "failed"},
		{"bad request", library.ErrInvalidRequest, false, "failed"},
		{"unexpected", leaky, false, "failed"},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.err = fmt.Errorf("wrapped: %w", tt.err)
			if tt.removed {
				uc.removed = map[shared.SubjectID]*subject.Subject{"s1": removed}
			}

			res := s.CreateSubjectWithCallLink(ctx, "Физика", fakeCallLink)
			if res.Problem != tt.problem || res.Subject != nil {
				t.Fatalf("CreateSubjectWithCallLink = %+v, want problem %q", res, tt.problem)
			}
			if tt.removed != (res.RemovedSubjectID == "s1" && res.RemovedVersion == 7) {
				t.Fatalf("removed subject = %q at %d", res.RemovedSubjectID, res.RemovedVersion)
			}
			if out := logs.String(); strings.Contains(out, fakeCallPwd) || strings.Contains(out, "meet.example.org") {
				t.Fatalf("the log has the link: %s", out)
			}
		})
	}
}
