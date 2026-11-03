package recordingprompt

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

const (
	testCallLink    = "https://meet.example.org/j/1?pwd=FakePwd42"
	callSubjectName = "История"
)

// fakeSubjects resolves the subject strings of lessons like the library.
type fakeSubjects struct {
	mu    sync.Mutex
	found map[string]subjectmatch.Resolution
	err   error
	names []string
}

func (f *fakeSubjects) LookupSubjects(_ context.Context, names []string) (map[string]subjectmatch.Resolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, names...)
	if f.err != nil {
		return nil, errors.New("subjects: " + f.err.Error())
	}
	out := map[string]subjectmatch.Resolution{}
	for _, n := range names {
		if r, ok := f.found[n]; ok {
			out[n] = r
		}
	}
	return out, nil
}

// fakeCallLinks holds the call links of the subjects.
type fakeCallLinks struct {
	mu    sync.Mutex
	links map[shared.SubjectID]subject.CallLink
	err   error
	ids   []shared.SubjectID
}

func (f *fakeCallLinks) CallLinks(_ context.Context, ids []shared.SubjectID) (map[shared.SubjectID]subject.CallLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, ids...)
	if f.err != nil {
		return nil, f.err
	}
	out := map[shared.SubjectID]subject.CallLink{}
	for _, id := range ids {
		if l, ok := f.links[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}

// callEnv is the prompt with a resolver and call links; the lesson "lesson-1" has the subject string subj.
type callEnv struct {
	*env
	subjects *fakeSubjects
	links    *fakeCallLinks
	logs     *bytes.Buffer
}

func newCallEnv(t *testing.T, subj string) *callEnv {
	t.Helper()
	e := newEnv(t)
	link, err := subject.ParseCallLink(testCallLink)
	if err != nil {
		t.Fatal(err)
	}
	c := &callEnv{
		env: e, logs: &bytes.Buffer{},
		subjects: &fakeSubjects{found: map[string]subjectmatch.Resolution{
			callSubjectName: {Outcome: subjectmatch.Matched, Match: subjectmatch.Match{ID: "sub-1", Name: callSubjectName}},
			"Матан":         {Outcome: subjectmatch.Ambiguous},
			"Физика":        {Outcome: subjectmatch.NoMatch},
		}},
		links: &fakeCallLinks{links: map[shared.SubjectID]subject.CallLink{"sub-1": link}},
	}
	svc, err := New(Deps{
		Settings: e.settings, Apps: e.apps, Popup: e.popup, Recordings: e.recordings, Lessons: e.lessons, Clock: e.clock,
		Sleeper: e.clock, Subjects: c.subjects, CallLinks: c.links, Log: slog.New(slog.NewTextHandler(c.logs, nil)),
	}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc

	l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
		Title: "History", Subject: subj, Start: t0, End: lessonEnd, TimeZone: "Europe/Moscow",
	}}, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e.lessons.lessons = append(e.lessons.lessons, l)
	return c
}

// TASK-370: only the current lesson prompt whose subject resolves to one subject with a link gets that subject.
func TestCallSubject(t *testing.T) {
	ctx := context.Background()
	storage := errors.New(`read C:\Users\student\db: row "` + testCallLink + `"`)
	tests := []struct {
		name    string
		subject string
		edit    func(c *callEnv)
		id      func(v View) string // the id the popup sends; nil: the current prompt
		adHoc   bool
		want    shared.SubjectID
		failed  string // the result logged, "" for none
	}{
		{name: "linked subject", subject: callSubjectName, want: "sub-1"},
		{name: "subject without a link", subject: callSubjectName, edit: func(c *callEnv) { c.links.links = nil }},
		{name: "ambiguous subject", subject: "Матан"},
		{name: "unknown subject", subject: "Физика"},
		{name: "no subject string", subject: ""},
		{name: "resolver leaves the name out", subject: "Химия"},
		{name: "other prompt id", subject: callSubjectName, id: func(View) string { return "prompt-9" }},
		{name: "ad hoc prompt", subject: callSubjectName, adHoc: true},
		{name: "lesson gone", subject: callSubjectName, edit: func(c *callEnv) { c.lessons.lessons = nil }},
		{
			name: "lesson unread", subject: callSubjectName, edit: func(c *callEnv) { c.lessons.err = storage },
			failed: "lesson unread",
		},
		{
			name: "subjects unread", subject: callSubjectName, edit: func(c *callEnv) { c.subjects.err = storage },
			failed: "subjects unread",
		},
		{
			name: "call links unread", subject: callSubjectName, edit: func(c *callEnv) { c.links.err = storage },
			failed: "call links unread",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCallEnv(t, tt.subject)
			var v View
			if tt.adHoc {
				var err error
				if v, err = c.svc.OfferAdHoc(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				v = c.offer("lesson-1")
			}
			if tt.edit != nil {
				tt.edit(c)
			}
			id := string(v.ID)
			if tt.id != nil {
				id = tt.id(v)
			}

			if got := c.svc.CallSubject(ctx, prompt.ID(id)); got != tt.want {
				t.Fatalf("CallSubject = %q, want %q", got, tt.want)
			}
			logs := c.logs.String()
			if tt.failed != "" && !strings.Contains(logs, tt.failed) {
				t.Errorf("log %q, want %q", logs, tt.failed)
			}
			if tt.failed == "" && strings.Contains(logs, opCallSubject) {
				t.Errorf("unexpected failure logged: %q", logs)
			}
			for _, secret := range []string{"student", "FakePwd42", "meet.example.org"} {
				if strings.Contains(logs, secret) {
					t.Errorf("log carries %q: %q", secret, logs)
				}
			}
		})
	}
}

// Nothing is read for a prompt that cannot offer a link, and the links are read only for the matched subject.
func TestCallSubjectReadsOnlyWhatItNeeds(t *testing.T) {
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	if _, err := c.svc.OfferAdHoc(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.svc.CallSubject(ctx, "prompt-1"); got != "" || len(c.subjects.names) != 0 || len(c.links.ids) != 0 {
		t.Fatalf("ad hoc: %q, names %v, ids %v", got, c.subjects.names, c.links.ids)
	}

	v := c.offer("lesson-1")
	if got := c.svc.CallSubject(ctx, v.ID); got != "sub-1" {
		t.Fatalf("CallSubject = %q", got)
	}
	if !slices.Equal(c.subjects.names, []string{callSubjectName}) || !slices.Equal(c.links.ids, []shared.SubjectID{"sub-1"}) {
		t.Fatalf("names %v, ids %v", c.subjects.names, c.links.ids)
	}

	// A prompt that ended is not current: nothing to offer.
	if _, err := c.svc.Skip(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if got := c.svc.CallSubject(ctx, v.ID); got != "" {
		t.Fatalf("after skip: %q", got)
	}
}

// Without the resolver or the call links (Deps left them out) the popup offers no link.
func TestCallSubjectNotWired(t *testing.T) {
	e := newEnv(t)
	v := e.offer("lesson-1")
	if got := e.svc.CallSubject(context.Background(), v.ID); got != "" {
		t.Fatalf("CallSubject = %q, want none", got)
	}
}
