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

func (f *fakeLibrary) SetCallLink(_ context.Context, id shared.SubjectID, link string) (*subject.Subject, error) {
	f.note("SetCallLink %s %s", id, link)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) ClearCallLink(_ context.Context, id shared.SubjectID) (*subject.Subject, error) {
	f.note("ClearCallLink %s", id)
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

// OpenCallLink answers as the use case does: f.err, else no link, else what the opener answered (f.openErr).
func (f *fakeLibrary) OpenCallLink(_ context.Context, id shared.SubjectID) error {
	f.note("OpenCallLink %s", id)
	switch {
	case f.err != nil:
		return f.err
	case f.subjects[0].CallLink().IsZero():
		return library.ErrNoCallLink
	case f.openErr != nil:
		return fmt.Errorf("%w: %w", library.ErrCallLinkNotOpened, f.openErr)
	}
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

func TestLibrarySetAndClearCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	ctx := context.Background()

	res := s.SetSubjectCallLink(ctx, "s1", fakeCallLink)
	if res.Problem != "" || res.Subject == nil || res.Subject.CallLink == nil || res.Subject.CallLink.Link != fakeCallLink {
		t.Fatalf("SetSubjectCallLink = %+v", res)
	}
	uc.subjects = []*subject.Subject{testSubject(t)}
	res = s.ClearSubjectCallLink(ctx, "s1")
	if res.Problem != "" || res.Subject == nil || res.Subject.CallLink != nil {
		t.Fatalf("ClearSubjectCallLink = %+v", res)
	}
	want := []string{"SetCallLink s1 " + fakeCallLink, "ClearCallLink s1"}
	if fmt.Sprint(uc.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %q", uc.calls)
	}
	if logs.Len() != 0 {
		t.Fatalf("a success logs nothing: %s", logs.String())
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
				"set":   s.SetSubjectCallLink(ctx, "s1", fakeCallLink),
				"clear": s.ClearSubjectCallLink(ctx, "s1"),
			} {
				if res.Problem != tt.problem || res.Subject != nil {
					t.Errorf("%s = %+v, want problem %q", call, res, tt.problem)
				}
			}
			if tt.problem != "invalid" {
				if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != tt.problem {
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

// «Подключиться» sends only the subject to the use case, which reads the link and opens it; the binding maps the
// outcome to a code.
func TestOpenSubjectCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()

	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "no_link" {
		t.Fatalf("no link: %+v", res)
	}

	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "" {
		t.Fatalf("open = %+v", res)
	}
	if want := []string{"OpenCallLink s1", "OpenCallLink s1"}; fmt.Sprint(uc.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %q", uc.calls)
	}

	// The opener refuses (an environment variable in the link, another OS): failed, and its error is logged; it never
	// holds the link.
	uc.openErr = errors.New("weblink: the call link names an environment variable")
	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "failed" {
		t.Fatalf("refused open = %+v", res)
	}
	if out := logs.String(); !strings.Contains(out, "environment variable") || strings.Contains(out, fakeCallPwd) {
		t.Fatalf("log %s", out)
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
