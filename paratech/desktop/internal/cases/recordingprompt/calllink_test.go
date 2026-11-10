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
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
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
	links map[shared.SubjectID]subject.CallLinks
	err   error
	ids   []shared.SubjectID
}

func (f *fakeCallLinks) CallLinkSets(_ context.Context, ids []shared.SubjectID) (map[shared.SubjectID]subject.CallLinks, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, ids...)
	if f.err != nil {
		return nil, f.err
	}
	out := map[shared.SubjectID]subject.CallLinks{}
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
	running  *fakeRunning
	logs     *bytes.Buffer
}

func newCallEnv(t *testing.T, subj string) *callEnv {
	t.Helper()
	return newCallEnvOf(t, subj, lesson.KindUnknown)
}

// newCallEnvOf is newCallEnv with a lesson of kind k; "sub-1" keeps testCallLink as its general link.
func newCallEnvOf(t *testing.T, subj string, k lesson.Kind) *callEnv {
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
		links:   &fakeCallLinks{links: map[shared.SubjectID]subject.CallLinks{"sub-1": {General: link}}},
		running: &fakeRunning{},
	}
	svc, err := New(Deps{
		Settings: e.settings, Apps: e.apps, Popup: e.popup, Recordings: e.recordings, Lessons: e.lessons, Clock: e.clock,
		Sleeper: e.clock, Subjects: c.subjects, CallLinks: c.links, Running: c.running,
		Log: slog.New(slog.NewTextHandler(c.logs, nil)),
	}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc

	l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
		Title: "History", Subject: subj, Kind: k, Start: t0, End: lessonEnd, TimeZone: "Europe/Moscow",
	}}, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e.lessons.lessons = append(e.lessons.lessons, l)
	return c
}

// wantCall is the Call of subject id for the unknown-kind lesson of newCallEnv; "" is no call.
func wantCall(id shared.SubjectID) Call {
	if id == "" {
		return Call{}
	}
	return Call{Subject: id, LessonKind: "unknown"}
}

// The popup is offered a call only when the lesson's kind finds a link (ADR-0032 addendum 2026-11-10): the kind's
// link, else the general one; the lesson kind travels with the subject so the click opens the same link.
func TestCallSubjectByLessonKind(t *testing.T) {
	ctx := context.Background()
	general, lecture, seminar := mustCallLink(t, testCallLink), mustCallLink(t, "https://lecture.example.org/j/2"),
		mustCallLink(t, "https://seminar.example.org/j/3")
	tests := []struct {
		name  string
		kind  lesson.Kind
		links subject.CallLinks
		want  Call
	}{
		{"lecture with its own link", lesson.KindLecture, subject.CallLinks{Lecture: lecture},
			Call{Subject: "sub-1", LessonKind: "lecture"}},
		{"seminar with only the general", lesson.KindSeminar, subject.CallLinks{General: general, Lecture: lecture},
			Call{Subject: "sub-1", LessonKind: "seminar"}},
		{"seminar never takes the lecture", lesson.KindSeminar, subject.CallLinks{Lecture: lecture}, Call{}},
		{"lab without links", lesson.KindLab, subject.CallLinks{}, Call{}},
		{"practice takes the seminar", lesson.KindPractice, subject.CallLinks{Seminar: seminar},
			Call{Subject: "sub-1", LessonKind: "practice"}},
		{"unknown kind takes the general", lesson.KindUnknown, subject.CallLinks{General: general},
			Call{Subject: "sub-1", LessonKind: "unknown"}},
		{"unknown kind without the general", lesson.KindUnknown, subject.CallLinks{Lecture: lecture, Seminar: seminar},
			Call{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCallEnvOf(t, callSubjectName, tt.kind)
			c.links.links = map[shared.SubjectID]subject.CallLinks{"sub-1": tt.links}
			v := c.offer("lesson-1")
			if got := c.svc.CallSubject(ctx, v.ID); got != tt.want {
				t.Fatalf("CallSubject = %+v, want %+v", got, tt.want)
			}
		})
	}

	// While the recording started from the popup runs, the call keeps the lesson's kind (TASK-405).
	c := newCallEnvOf(t, callSubjectName, lesson.KindLecture)
	c.links.links = map[shared.SubjectID]subject.CallLinks{"sub-1": {Lecture: lecture}}
	c.running.set("rec-1")
	v := c.offer("lesson-1")
	if _, err := c.svc.Start(ctx, v.ID, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if got := c.svc.CallSubject(ctx, v.ID); got != (Call{Subject: "sub-1", LessonKind: "lecture"}) {
		t.Fatalf("running: %+v", got)
	}
}

func mustCallLink(t *testing.T, s string) subject.CallLink {
	t.Helper()
	l, err := subject.ParseCallLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
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
		// The link is offered for the OFFERED prompt only, even while a prompt in another state is still current.
		{name: "starting prompt", subject: callSubjectName, edit: moveCurrent(func(p *prompt.Prompt) error {
			return p.BeginStart()
		})},
		{name: "started prompt", subject: callSubjectName, edit: moveCurrent(func(p *prompt.Prompt) error {
			if err := p.BeginStart(); err != nil {
				return err
			}
			return p.Started()
		})},
		{name: "skipped prompt", subject: callSubjectName, edit: moveCurrent(func(p *prompt.Prompt) error { return p.Skip() })},
		{name: "expired prompt", subject: callSubjectName, edit: moveCurrent(func(p *prompt.Prompt) error {
			return p.Expire(lessonEnd)
		})},
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

			if got := c.svc.CallSubject(ctx, prompt.ID(id)); got != wantCall(tt.want) {
				t.Fatalf("CallSubject = %+v, want %q", got, tt.want)
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

// moveCurrent moves the current prompt to another state but leaves it current, so only its state stops the link.
func moveCurrent(move func(p *prompt.Prompt) error) func(c *callEnv) {
	return func(c *callEnv) {
		c.svc.mu.Lock()
		defer c.svc.mu.Unlock()
		if err := move(c.svc.current); err != nil {
			panic(err)
		}
	}
}

// A prompt whose start runs keeps being current, yet offers no link; the subjects are not even read.
func TestCallSubjectWhileStarting(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	v := c.offer("lesson-1")
	entered, release := c.recordings.hold()
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := c.svc.Start(ctx, v.ID, values.WholeComputer())
		done <- err
	}()
	<-entered
	if cur, ok := c.svc.Current(); !ok || cur.ID != v.ID || cur.State != prompt.StateStarting {
		t.Fatalf("current %+v, %v, want STARTING", cur, ok)
	}
	if got := c.svc.CallSubject(ctx, v.ID); got != (Call{}) || len(c.subjects.names) != 0 {
		t.Fatalf("while starting: %+v, names %v", got, c.subjects.names)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Nothing is read for a prompt that cannot offer a link, and the links are read only for the matched subject.
func TestCallSubjectReadsOnlyWhatItNeeds(t *testing.T) {
	ctx := context.Background()
	c := newCallEnv(t, callSubjectName)
	if _, err := c.svc.OfferAdHoc(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.svc.CallSubject(ctx, "prompt-1"); got != (Call{}) || len(c.subjects.names) != 0 || len(c.links.ids) != 0 {
		t.Fatalf("ad hoc: %+v, names %v, ids %v", got, c.subjects.names, c.links.ids)
	}

	v := c.offer("lesson-1")
	if got := c.svc.CallSubject(ctx, v.ID); got != wantCall("sub-1") {
		t.Fatalf("CallSubject = %+v", got)
	}
	if !slices.Equal(c.subjects.names, []string{callSubjectName}) || !slices.Equal(c.links.ids, []shared.SubjectID{"sub-1"}) {
		t.Fatalf("names %v, ids %v", c.subjects.names, c.links.ids)
	}

	// A prompt that ended is not current: nothing to offer.
	if _, err := c.svc.Skip(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if got := c.svc.CallSubject(ctx, v.ID); got != (Call{}) {
		t.Fatalf("after skip: %+v", got)
	}
}

// Without the resolver or the call links (Deps left them out) the popup offers no link.
func TestCallSubjectNotWired(t *testing.T) {
	e := newEnv(t)
	v := e.offer("lesson-1")
	if got := e.svc.CallSubject(context.Background(), v.ID); got != (Call{}) {
		t.Fatalf("CallSubject = %+v, want none", got)
	}
}
