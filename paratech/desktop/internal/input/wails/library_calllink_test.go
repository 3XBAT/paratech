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
	schedvalues "paratech/desktop/internal/domain/schedule/values"
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

func (f *fakeLibrary) CallLink(_ context.Context, id shared.SubjectID) (subject.CallLink, error) {
	f.note("CallLink %s", id)
	if f.err != nil {
		return subject.CallLink{}, f.err
	}
	return f.subjects[0].CallLink(), nil
}

// fakeCallOpener records the links it was asked to open.
type fakeCallOpener struct {
	opened []string
	err    error
}

func (f *fakeCallOpener) OpenCallLink(_ context.Context, l schedvalues.CallLink) error {
	f.opened = append(f.opened, l.String())
	return f.err
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
			opener := &fakeCallOpener{}
			s.withCallLinkOpener(opener)
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
			if len(opener.opened) != 0 {
				t.Fatalf("opened %q", opener.opened)
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

// «Подключиться» sends only the subject: the link is read again in Go and opened in its canonical form; a subject
// without a link opens nothing.
func TestOpenSubjectCallLink(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	opener := &fakeCallOpener{}
	s.withCallLinkOpener(opener)
	ctx := context.Background()

	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "no_link" || len(opener.opened) != 0 {
		t.Fatalf("no link: %+v, opened %q", res, opener.opened)
	}

	uc.subjects = []*subject.Subject{subjectWithCallLink(t)}
	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "" || fmt.Sprint(opener.opened) != fmt.Sprint([]string{fakeCallLink}) {
		t.Fatalf("open = %+v, opened %q", res, opener.opened)
	}
	if !strings.Contains(fmt.Sprint(uc.calls), "CallLink s1") {
		t.Fatalf("calls = %q", uc.calls)
	}

	// The opener refuses (an environment variable in the link, another OS): failed, and its error is logged without
	// the link.
	opener.err = errors.New("weblink: the call link names an environment variable")
	if res := s.OpenSubjectCallLink(ctx, "s1"); res.Problem != "failed" {
		t.Fatalf("refused open = %+v", res)
	}
	if out := logs.String(); !strings.Contains(out, "environment variable") || strings.Contains(out, fakeCallPwd) {
		t.Fatalf("log %s", out)
	}

	// Without an opener nothing opens.
	bare, _, _ := newLibraryBinding(t, "", &memFiles{})
	if res := bare.OpenSubjectCallLink(ctx, "s1"); res.Problem != "failed" {
		t.Fatalf("no opener = %+v", res)
	}
}
