package googlecal_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// goldenLessonL1 is base32hex(SHA-256("lesson:l1")) in lower case, computed outside Go.
const goldenLessonL1 = "u9okdq49p1jg3fgoceqth3c03ips3umn71jmg09tlvjbra1qp250"

// Token source failures the test classifier knows, like the googleauth sentinels in the app.
var (
	errNoSignIn = errors.New("fake auth: not signed in")
	errExpired  = errors.New("fake auth: invalid_grant")
	errRefused  = errors.New("fake auth: invalid_client")
)

func classify(err error) googlecal.AuthFailure {
	switch {
	case errors.Is(err, errNoSignIn):
		return googlecal.AuthNotSignedIn
	case errors.Is(err, errExpired):
		return googlecal.AuthExpired
	case errors.Is(err, errRefused):
		return googlecal.AuthRejected
	}
	return googlecal.AuthNone
}

// fakeAuth is the googleauth.Authorizer of the tests: a bearer client with a token that ForgetAccessToken can replace.
type fakeAuth struct {
	transport *http.Transport

	mu        sync.Mutex
	token     string
	next      string // the token after ForgetAccessToken; "" keeps the current one
	err       error  // the token source fails with it
	forgotten int
	urls      []string
	before    func(req *http.Request) // runs before each request is sent
}

func (a *fakeAuth) HTTPClient() *http.Client {
	return &http.Client{Transport: a, Timeout: 10 * time.Second}
}

func (a *fakeAuth) RoundTrip(req *http.Request) (*http.Response, error) {
	a.mu.Lock()
	token, err, before := a.token, a.err, a.before
	a.urls = append(a.urls, req.URL.String())
	a.mu.Unlock()
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	if before != nil {
		before(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return a.transport.RoundTrip(r)
}

func (a *fakeAuth) ForgetAccessToken() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.forgotten++
	if a.next != "" {
		a.token = a.next
	}
}

func (a *fakeAuth) set(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn()
}

func (a *fakeAuth) sent() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.urls...)
}

type rig struct {
	c     *googlecal.Client
	api   *testenv.FakeGoogleCalendar
	auth  *fakeAuth
	store *testenv.MemCredentials
}

func newRig(t *testing.T, opts ...googlecal.Option) *rig {
	t.Helper()
	api := testenv.StartFakeGoogleCalendar(t)
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	auth := &fakeAuth{transport: tr, token: testenv.FakeCalendarAccessToken}
	store := testenv.NewMemCredentials()
	c, err := googlecal.New(auth, store, classify, append([]googlecal.Option{googlecal.WithBaseURL(api.BaseURL())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{c: c, api: api, auth: auth, store: store}
}

// calendar runs EnsureCalendar and confirms it, as the synchronization does.
func (r *rig) calendar(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	id, _, err := r.c.EnsureCalendar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.ConfirmCalendar(ctx, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// requests counts the requests with the method whose path ends with suffix.
func (r *rig) requests(method, suffix string) int {
	n := 0
	for _, q := range r.api.Requests() {
		if q.Method == method && strings.HasSuffix(q.Path, suffix) {
			n++
		}
	}
	return n
}

func newLesson(t *testing.T, id shared.LessonID, title string) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Start: time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 20, 8, 30, 0, 0, time.UTC),
		TimeZone: "Europe/Moscow",
	}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func deadline(t *testing.T, assignment, title string) proposal.CalendarCommand {
	t.Helper()
	return deadlineOf(t, "p1", assignment, title)
}

func deadlineOf(t *testing.T, id proposal.ID, assignment, title string) proposal.CalendarCommand {
	t.Helper()
	out := `{"kind":"create_deadline_event","assignment_id":"` + assignment + `","title":"` + title +
		`","due":"2026-10-21T18:00:00Z"}`
	p, err := proposal.New(id, values.NewUntrustedOutput(out), t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true}, t0); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm(t0); err != nil {
		t.Fatal(err)
	}
	cmd, err := p.CalendarCommand()
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

// assertClass checks the reason and the backoff class of err.
func assertClass(t *testing.T, err, reason, class error) {
	t.Helper()
	if !errors.Is(err, reason) || !errors.Is(err, class) {
		t.Fatalf("error %v, want %v and %v", err, reason, class)
	}
}

// TASK-051 test step 2 and PRD section 14 case 8: two syncs in a row give the same events.
func TestUpsertTwiceIsOneEvent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	lessons := []*lesson.Lesson{newLesson(t, "l1", "Алгебра"), newLesson(t, "l2", "Физика")}
	for range 2 {
		for _, l := range lessons {
			if err := r.c.UpsertLesson(ctx, cal, l); err != nil {
				t.Fatal(err)
			}
		}
		if n := len(r.api.Events(cal)); n != 2 {
			t.Fatalf("%d events, want 2", n)
		}
	}

	e, ok := r.api.Event(cal, googlecal.LessonEventID("l1"))
	want := testenv.FakeCalendarEvent{
		ID: googlecal.LessonEventID("l1"), Summary: "Алгебра", Status: testenv.FakeEventConfirmed,
		Start: testenv.FakeEventTime{DateTime: "2026-10-20T07:00:00Z", TimeZone: "Europe/Moscow"},
		End:   testenv.FakeEventTime{DateTime: "2026-10-20T08:30:00Z", TimeZone: "Europe/Moscow"},
	}
	if !ok || e.ID != want.ID || e.Summary != want.Summary || e.Status != want.Status || e.Start != want.Start ||
		e.End != want.End || e.Description == "" {
		t.Fatalf("event %+v, want %+v", e, want)
	}
	if p := e.Private; p["app"] != "paratech" || p["kind"] != "lesson" || p["lessonId"] != "l1" || p["revision"] != "1" {
		t.Fatalf("private properties %v", p)
	}
	if n := r.requests(http.MethodPost, "/events"); n != 2 {
		t.Fatalf("%d inserts, want one per lesson", n)
	}
}

// TASK-051 test step 3: a changed lesson updates its event.
func TestChangedLessonUpdatesTheEvent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	l := newLesson(t, "l1", "Алгебра")
	if err := r.c.UpsertLesson(ctx, cal, l); err != nil {
		t.Fatal(err)
	}
	content := l.Content()
	content.Title = "Геометрия"
	content.End = content.End.Add(30 * time.Minute)
	if _, err := l.ChangeContent(content, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.c.UpsertLesson(ctx, cal, l); err != nil {
		t.Fatal(err)
	}

	events := r.api.Events(cal)
	if len(events) != 1 || events[0].ID != googlecal.LessonEventID("l1") || events[0].Summary != "Геометрия" ||
		events[0].End.DateTime != "2026-10-20T09:00:00Z" || events[0].Private["revision"] != "2" {
		t.Fatalf("events %+v", events)
	}
}

func TestDeleteEvent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	if err := r.c.UpsertLesson(ctx, cal, newLesson(t, "l1", "Алгебра")); err != nil {
		t.Fatal(err)
	}
	for i, id := range []shared.LessonID{"l1", "l1", "never-written"} { // deleted (410) and unknown (404) are success
		if err := r.c.DeleteEvent(ctx, cal, id); err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}
	}
	if n := len(r.api.Events(cal)); n != 0 {
		t.Fatalf("%d events after the delete", n)
	}
}

// The docs state an empty body for events.delete, not the status: any 2xx is success.
func TestDeleteEventAcceptsAny2xx(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodDelete {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)
			r := newRig(t, googlecal.WithBaseURL(srv.URL+"/calendar/v3"))
			if err := r.store.Set(context.Background(), googlecal.CalendarIDName, `{"id":"cal1"}`); err != nil {
				t.Fatal(err)
			}
			if err := r.c.DeleteEvent(context.Background(), "cal1", "l1"); err != nil {
				t.Fatalf("DeleteEvent with %d: %v", status, err)
			}
		})
	}
}

// A deleted event (by the app or by the user in Google) is restored by the next upsert, under the same id, when
// Google restores it on an update (the fake's default; the docs do not state it).
func TestUpsertRestoresADeletedEvent(t *testing.T) {
	tests := []struct {
		name   string
		delete func(r *rig, cal string)
	}{
		{"deleted by the app", func(r *rig, cal string) { _ = r.c.DeleteEvent(context.Background(), cal, "l1") }},
		{"deleted by the user", func(r *rig, cal string) { r.api.CancelEvent(cal, googlecal.LessonEventID("l1")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			cal := r.calendar(t)
			l := newLesson(t, "l1", "Алгебра")
			if err := r.c.UpsertLesson(context.Background(), cal, l); err != nil {
				t.Fatal(err)
			}
			tt.delete(r, cal)
			if err := r.c.UpsertLesson(context.Background(), cal, l); err != nil {
				t.Fatal(err)
			}
			events := r.api.Events(cal)
			if len(events) != 1 || events[0].ID != googlecal.LessonEventID("l1") || events[0].Status != testenv.FakeEventConfirmed {
				t.Fatalf("events %+v", events)
			}
		})
	}
}

// If Google keeps a deleted event cancelled on an update, the upsert fails transiently: the lesson stays pending and
// no second event is made.
func TestUpsertOfAnEventGoogleKeepsDeleted(t *testing.T) {
	tests := []struct {
		name   string
		delete func(r *rig, cal string)
	}{
		{"deleted by the app", func(r *rig, cal string) { _ = r.c.DeleteEvent(context.Background(), cal, "l1") }},
		{"deleted by the user", func(r *rig, cal string) { r.api.CancelEvent(cal, googlecal.LessonEventID("l1")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			cal := r.calendar(t)
			l := newLesson(t, "l1", "Алгебра")
			if err := r.c.UpsertLesson(context.Background(), cal, l); err != nil {
				t.Fatal(err)
			}
			tt.delete(r, cal)
			r.api.KeepCancelledOnUpdate(true)

			err := r.c.UpsertLesson(context.Background(), cal, l)
			assertClass(t, err, googlecal.ErrNotRestored, backoff.ErrTransient)
			if err.Error() != "googlecal: upsert lesson: google calendar kept the event deleted" {
				t.Fatalf("error text %q", err)
			}
			if e, ok := r.api.Event(cal, googlecal.LessonEventID("l1")); !ok || e.Status != testenv.FakeEventCancelled {
				t.Fatalf("event %+v", e)
			}
			if n := r.requests(http.MethodPost, "/events"); n != 1 {
				t.Fatalf("%d inserts, want 1", n)
			}
		})
	}

	// A deadline goes through the same update.
	r := newRig(t)
	cmd := deadline(t, "a1", "t")
	if err := r.c.UpsertDeadline(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	cal := r.api.Calendars()[0].ID
	r.api.CancelEvent(cal, googlecal.DeadlineEventID("a1"))
	r.api.KeepCancelledOnUpdate(true)
	assertClass(t, r.c.UpsertDeadline(context.Background(), cmd), googlecal.ErrNotRestored, backoff.ErrTransient)
}

// Every private property stays within Google's limits (key <= 44, value <= 1024), also for the longest ids, so none
// is dropped or cut silently.
func TestPrivatePropertiesFitGoogleLimits(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	longLesson := shared.LessonID(strings.Repeat("l", 128))
	l := newLesson(t, longLesson, "Алгебра")
	for range 3 { // a revision above 1
		content := l.Content()
		content.Title += "!"
		if _, err := l.ChangeContent(content, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.c.UpsertLesson(ctx, cal, l); err != nil {
		t.Fatal(err)
	}
	longAssignment, longProposal := strings.Repeat("a", 128), proposal.ID(strings.Repeat("p", 128))
	cmd := deadlineOf(t, longProposal, longAssignment, "t")
	if err := r.c.UpsertDeadline(ctx, cmd); err != nil {
		t.Fatal(err)
	}

	lessonEvent, _ := r.api.Event(cal, googlecal.LessonEventID(longLesson))
	deadlineEvent, _ := r.api.Event(cal, googlecal.DeadlineEventID(shared.AssignmentID(longAssignment)))
	want := []map[string]string{
		{"app": "paratech", "kind": "lesson", "lessonId": string(longLesson), "revision": "4"},
		{"app": "paratech", "kind": "deadline", "assignmentId": longAssignment, "proposalKey": cmd.IdempotencyKey()},
	}
	for i, got := range []map[string]string{lessonEvent.Private, deadlineEvent.Private} {
		if len(got) != len(want[i]) {
			t.Fatalf("private %v, want %v", got, want[i])
		}
		for k, v := range want[i] {
			if len(k) > 44 || len(v) > 1024 || got[k] != v {
				t.Fatalf("private %q = %q, want %q within the limits", k, got[k], v)
			}
		}
	}
}

// Another writer inserts the same id between the update and the insert// Another writer inserts the same id between the update and the insert: the 409 is followed by an update.
func TestUpsertAfterAConflictingInsert(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	var once sync.Once
	r.auth.set(func() {
		r.auth.before = func(req *http.Request) {
			if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/events") {
				once.Do(func() {
					if status := insertRival(t, r.api, cal); status != http.StatusOK {
						t.Errorf("rival insert = %d", status)
					}
				})
			}
		}
	})
	if err := r.c.UpsertLesson(ctx, cal, newLesson(t, "l1", "Алгебра")); err != nil {
		t.Fatal(err)
	}
	events := r.api.Events(cal)
	if len(events) != 1 || events[0].Summary != "Алгебра" {
		t.Fatalf("events %+v", events)
	}
	if n := r.requests(http.MethodPut, googlecal.LessonEventID("l1")); n != 2 {
		t.Fatalf("%d updates, want 2 (before and after the 409)", n)
	}
}

// insertRival inserts the event of lesson l1 into cal with a plain client, as a racing writer.
func insertRival(t *testing.T, api *testenv.FakeGoogleCalendar, cal string) int {
	t.Helper()
	body := `{"id":"` + googlecal.LessonEventID("l1") + `","summary":"rival","start":{"dateTime":"2026-10-20T07:00:00Z"},` +
		`"end":{"dateTime":"2026-10-20T08:00:00Z"}}`
	req, err := http.NewRequest(http.MethodPost, api.BaseURL()+"/calendars/"+cal+"/events", strings.NewReader(body))
	if err != nil {
		t.Error(err)
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+testenv.FakeCalendarAccessToken)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Error(err)
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// The insert reaches Google but its answer is lost: the retry updates the event instead of inserting a second one.
func TestLostInsertAnswerIsRetriedWithoutADuplicate(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	l := newLesson(t, "l1", "Алгебра")
	r.api.FailNext(testenv.FaultDropAfterWrite, http.MethodPost)

	err := r.c.UpsertLesson(ctx, cal, l)
	assertClass(t, err, googlecal.ErrNetwork, backoff.ErrTransient)
	if !errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("error %v does not wait for the network", err)
	}
	if err := r.c.UpsertLesson(ctx, cal, l); err != nil {
		t.Fatal(err)
	}
	if n := len(r.api.Events(cal)); n != 1 {
		t.Fatalf("%d events, want 1", n)
	}
}

func TestEnsureCalendarCreatesOneCalendar(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id, fresh, err := r.c.EnsureCalendar(ctx)
	if err != nil || !fresh {
		t.Fatalf("first EnsureCalendar = %q, %v, %v", id, fresh, err)
	}
	cals := r.api.Calendars()
	if len(cals) != 1 || cals[0].ID != id || cals[0].Summary != googlecal.CalendarSummary || cals[0].Description == "" {
		t.Fatalf("calendars %+v", cals)
	}

	// Fresh until confirmed, also across calls.
	again, fresh, err := r.c.EnsureCalendar(ctx)
	if err != nil || again != id || !fresh {
		t.Fatalf("second EnsureCalendar = %q, %v, %v", again, fresh, err)
	}
	if err := r.c.ConfirmCalendar(ctx, "other@group.calendar.google.com"); err != nil {
		t.Fatal(err)
	}
	if _, fresh, _ := r.c.EnsureCalendar(ctx); !fresh {
		t.Fatal("confirming another calendar cleared fresh")
	}
	if err := r.c.ConfirmCalendar(ctx, id); err != nil {
		t.Fatal(err)
	}
	again, fresh, err = r.c.EnsureCalendar(ctx)
	if err != nil || again != id || fresh {
		t.Fatalf("EnsureCalendar after the confirmation = %q, %v, %v", again, fresh, err)
	}
	if n := r.requests(http.MethodPost, "/calendars"); n != 1 {
		t.Fatalf("%d calendar inserts", n)
	}
	if stored, err := r.store.Get(ctx, googlecal.CalendarIDName); err != nil || !strings.Contains(stored, id) {
		t.Fatalf("stored %q, %v", stored, err)
	}
}

func TestEnsureCalendarReplacesAGoneCalendar(t *testing.T) {
	r := newRig(t)
	old := r.calendar(t)
	r.api.DeleteCalendar(old)
	id, fresh, err := r.c.EnsureCalendar(context.Background())
	if err != nil || id == old || !fresh {
		t.Fatalf("EnsureCalendar = %q, %v, %v", id, fresh, err)
	}
	if cals := r.api.Calendars(); len(cals) != 1 || cals[0].ID != id {
		t.Fatalf("calendars %+v", cals)
	}
	if r.requests(http.MethodGet, "/calendars/"+old) != 1 || r.requests(http.MethodGet, "/calendarList/"+old) != 1 {
		t.Fatalf("requests %+v, want calendars.get and calendarList.get", r.api.Requests())
	}
}

// The stored calendar is gone only when calendars.get and calendarList.get both answer 404, 410 or 403; one spurious
// answer, a disagreement or a rate limit fails transiently and creates nothing.
func TestEnsureCalendarGoneNeedsBothChecks(t *testing.T) {
	tests := []struct {
		name    string
		faults  []testenv.CalendarFault // for the next GETs: calendars.get, then calendarList.get
		gone    bool
		reason  error
		listGet int // calendarList.get requests
	}{
		{"spurious 404", []testenv.CalendarFault{testenv.FaultNotFound}, false, googlecal.ErrUnavailable, 1},
		{"unexplained 403", []testenv.CalendarFault{testenv.FaultForbidden}, false, googlecal.ErrUnavailable, 1},
		{"403 and 404: another account", []testenv.CalendarFault{testenv.FaultForbidden, testenv.FaultNotFound}, true, nil, 1},
		{"404 and 404", []testenv.CalendarFault{testenv.FaultNotFound, testenv.FaultNotFound}, true, nil, 1},
		{"404, then the list check rate-limited", []testenv.CalendarFault{testenv.FaultNotFound, testenv.FaultUserRateLimit},
			false, googlecal.ErrRateLimited, 1},
		{"404, then the list check fails", []testenv.CalendarFault{testenv.FaultNotFound, testenv.FaultServerError},
			false, googlecal.ErrUnavailable, 1},
		{"quota on calendars.get", []testenv.CalendarFault{testenv.FaultQuotaExceeded}, false, googlecal.ErrRateLimited, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			old := r.calendar(t)
			for _, f := range tt.faults {
				r.api.FailNext(f, http.MethodGet)
			}
			id, fresh, err := r.c.EnsureCalendar(context.Background())
			if n := r.requests(http.MethodGet, "/calendarList/"+old); n != tt.listGet {
				t.Fatalf("%d calendarList.get requests, want %d", n, tt.listGet)
			}
			if !tt.gone {
				assertClass(t, err, tt.reason, backoff.ErrTransient)
				if n := r.requests(http.MethodPost, "/calendars"); n != 1 {
					t.Fatalf("%d calendar inserts, want 1", n)
				}
				if again, _, err := r.c.EnsureCalendar(context.Background()); err != nil || again != old {
					t.Fatalf("next EnsureCalendar = %q, %v, want %q", again, err, old)
				}
				return
			}
			if err != nil || id == old || !fresh {
				t.Fatalf("EnsureCalendar = %q, %v, %v", id, fresh, err)
			}
			if stored, _ := r.store.Get(context.Background(), googlecal.CalendarIDName); !strings.Contains(stored, id) {
				t.Fatalf("stored %q, want %q", stored, id)
			}
		})
	}
}

// ForgetCalendar drops the binding: the next EnsureCalendar creates a new calendar and never writes the old one.
func TestForgetCalendar(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	old := r.calendar(t)
	if err := r.c.ForgetCalendar(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Get(ctx, googlecal.CalendarIDName); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the calendar id is still stored: %v", err)
	}
	id, fresh, err := r.c.EnsureCalendar(ctx)
	if err != nil || id == old || !fresh {
		t.Fatalf("EnsureCalendar = %q, %v, %v", id, fresh, err)
	}
	if err := r.c.UpsertLesson(ctx, old, newLesson(t, "l1", "Алгебра")); !errors.Is(err, googlecal.ErrForeignCalendar) {
		t.Fatalf("UpsertLesson into the old calendar = %v", err)
	}
}

// A missing, unreadable or foreign stored entry is no binding: a calendar is created.
func TestEnsureCalendarWithoutAUsableStoredEntry(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, r *rig)
	}{
		{"nothing stored", func(t *testing.T, r *rig) {}},
		{"garbage stored", func(t *testing.T, r *rig) {
			if err := r.store.Set(context.Background(), googlecal.CalendarIDName, "not json"); err != nil {
				t.Fatal(err)
			}
		}},
		{"foreign id stored", func(t *testing.T, r *rig) {
			if err := r.store.Set(context.Background(), googlecal.CalendarIDName, `{"id":"primary"}`); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			tt.setup(t, r)
			id, fresh, err := r.c.EnsureCalendar(context.Background())
			if err != nil || !fresh || id == "primary" {
				t.Fatalf("EnsureCalendar = %q, %v, %v", id, fresh, err)
			}
			if cals := r.api.Calendars(); len(cals) != 1 || cals[0].ID != id {
				t.Fatalf("calendars %+v", cals)
			}
			assertOnlyOwnWrites(t, r, id)
		})
	}
}

// The calendar insert reaches Google but its answer is lost (the accepted crash window): the retry creates a second
// calendar, and the first one stays empty.
func TestLostCalendarInsertAnswer(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.api.FailNext(testenv.FaultDropAfterWrite, http.MethodPost)
	if _, _, err := r.c.EnsureCalendar(ctx); !errors.Is(err, backoff.ErrTransient) {
		t.Fatalf("EnsureCalendar = %v, want a transient error", err)
	}
	id, fresh, err := r.c.EnsureCalendar(ctx)
	if err != nil || !fresh {
		t.Fatalf("retry = %q, %v, %v", id, fresh, err)
	}
	if err := r.c.UpsertLesson(ctx, id, newLesson(t, "l1", "Алгебра")); err != nil {
		t.Fatal(err)
	}
	cals := r.api.Calendars()
	if len(cals) != 2 || cals[1].ID != id || len(r.api.Events(cals[0].ID)) != 0 {
		t.Fatalf("calendars %+v", cals)
	}
	assertOnlyOwnWrites(t, r, id)
}

// Two parallel callers (the sync job and a deadline) create one calendar.
func TestParallelEnsureCalendar(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	ids := make([]string, 5)
	for i := range ids {
		wg.Go(func() {
			id, _, err := r.c.EnsureCalendar(context.Background())
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		})
	}
	wg.Wait()
	if cals := r.api.Calendars(); len(cals) != 1 {
		t.Fatalf("%d calendars", len(cals))
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("ids %v", ids)
		}
	}
}

// Writes go only to the app calendar: another id is refused before any request (AC-17).
func TestForeignCalendarIsRefused(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	before := len(r.api.Requests())
	for _, id := range []string{"primary", "someone@gmail.com", "", cal + "x"} {
		if err := r.c.UpsertLesson(ctx, id, newLesson(t, "l1", "Алгебра")); !errors.Is(err, googlecal.ErrForeignCalendar) &&
			!errors.Is(err, googlecal.ErrInvalidRequest) {
			t.Fatalf("UpsertLesson(%q) = %v", id, err)
		}
		if err := r.c.DeleteEvent(ctx, id, "l1"); !errors.Is(err, googlecal.ErrForeignCalendar) &&
			!errors.Is(err, googlecal.ErrInvalidRequest) {
			t.Fatalf("DeleteEvent(%q) = %v", id, err)
		}
	}
	if n := len(r.api.Requests()); n != before {
		t.Fatalf("%d requests for foreign calendars", n-before)
	}
	if n := r.api.ForeignRequests(); n != 0 {
		t.Fatalf("%d foreign requests", n)
	}
}

// assertOnlyOwnWrites checks that every write went to the calendar insert or to cal.
func assertOnlyOwnWrites(t *testing.T, r *rig, cal string) {
	t.Helper()
	for _, q := range r.api.Requests() {
		if q.Method == http.MethodGet || q.Path == "/calendars" {
			continue
		}
		if !strings.HasPrefix(q.Path, "/calendars/"+cal+"/") {
			t.Fatalf("write %s %s outside the app calendar", q.Method, q.Path)
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	deleted := newLesson(t, "l1", "Алгебра")
	if err := deleted.MarkDeleted(t0); err != nil {
		t.Fatal(err)
	}
	before := len(r.api.Requests())
	checks := []struct {
		name string
		err  error
	}{
		{"nil lesson", r.c.UpsertLesson(ctx, cal, nil)},
		{"deleted lesson", r.c.UpsertLesson(ctx, cal, deleted)},
		{"bad lesson id", r.c.DeleteEvent(ctx, cal, "")},
		{"zero command", r.c.UpsertDeadline(ctx, proposal.CalendarCommand{})},
	}
	for _, c := range checks {
		assertClass(t, c.err, googlecal.ErrInvalidRequest, backoff.ErrPermanent)
	}
	if n := len(r.api.Requests()); n != before {
		t.Fatalf("%d requests for invalid calls", n-before)
	}
}

// A sanitized title reaches the calendar: no bidi controls or separators from an imported file.
func TestSanitizedTitle(t *testing.T) {
	r := newRig(t)
	cal := r.calendar(t)
	if err := r.c.UpsertLesson(context.Background(), cal, newLesson(t, "l1", "Алгебра\u202E\u2028 урок\u2066 7 ")); err != nil {
		t.Fatal(err)
	}
	if e, _ := r.api.Event(cal, googlecal.LessonEventID("l1")); e.Summary != "Алгебра урок 7" {
		t.Fatalf("summary %q", e.Summary)
	}
}

func TestUpsertDeadline(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cmd := deadline(t, "a1", "Map of rivers")
	for range 2 {
		if err := r.c.UpsertDeadline(ctx, cmd); err != nil {
			t.Fatal(err)
		}
	}
	cals := r.api.Calendars()
	if len(cals) != 1 {
		t.Fatalf("%d calendars", len(cals))
	}
	events := r.api.Events(cals[0].ID)
	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}
	e := events[0]
	if e.ID != googlecal.DeadlineEventID("a1") || e.Summary != "Map of rivers" ||
		e.Start != (testenv.FakeEventTime{DateTime: "2026-10-21T18:00:00Z", TimeZone: "UTC"}) ||
		e.End != (testenv.FakeEventTime{DateTime: "2026-10-21T18:15:00Z", TimeZone: "UTC"}) ||
		e.Private["kind"] != "deadline" || e.Private["assignmentId"] != "a1" || e.Private["proposalKey"] != cmd.IdempotencyKey() {
		t.Fatalf("event %+v", e)
	}

	// The calendar the deadline created is fresh for the schedule synchronization.
	id, fresh, err := r.c.EnsureCalendar(ctx)
	if err != nil || id != cals[0].ID || !fresh {
		t.Fatalf("EnsureCalendar = %q, %v, %v", id, fresh, err)
	}
}

// A 401 drops the access token and retries once; a second 401 is "sign in again".
func TestUnauthorized(t *testing.T) {
	tests := []struct {
		name      string
		next      string
		forgotten int
		want      error
	}{
		{"refreshed token works", testenv.FakeCalendarAccessToken, 1, nil},
		{"refreshed token refused", "still-stale", 2, googlecal.ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.auth.set(func() { r.auth.token, r.auth.next = "stale", tt.next })
			_, _, err := r.c.EnsureCalendar(context.Background())
			if tt.want == nil && err != nil {
				t.Fatal(err)
			}
			if tt.want != nil {
				assertClass(t, err, tt.want, backoff.ErrPermanent)
			}
			r.auth.mu.Lock()
			forgotten := r.auth.forgotten
			r.auth.mu.Unlock()
			if forgotten != tt.forgotten {
				t.Fatalf("forgotten %d times, want %d", forgotten, tt.forgotten)
			}
		})
	}

	r := newRig(t)
	cal := r.calendar(t)
	r.api.FailNext(testenv.FaultUnauthorized, http.MethodPut)
	if err := r.c.UpsertLesson(context.Background(), cal, newLesson(t, "l1", "Алгебра")); err != nil {
		t.Fatalf("one 401: %v", err)
	}
}

func TestStatusClasses(t *testing.T) {
	tests := []struct {
		fault      testenv.CalendarFault
		reason     error
		class      error
		retryAfter time.Duration
	}{
		{testenv.FaultRateLimited, googlecal.ErrRateLimited, backoff.ErrTransient, 7 * time.Second},
		{testenv.FaultUserRateLimit, googlecal.ErrRateLimited, backoff.ErrTransient, 0},
		{testenv.FaultServerError, googlecal.ErrUnavailable, backoff.ErrTransient, 0},
		{testenv.FaultBadRequest, googlecal.ErrRejected, backoff.ErrPermanent, 0},
		{testenv.FaultForbidden, googlecal.ErrForbidden, backoff.ErrPermanent, 0},
		{testenv.FaultQuotaExceeded, googlecal.ErrRateLimited, backoff.ErrTransient, 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.fault), func(t *testing.T) {
			r := newRig(t)
			cal := r.calendar(t)
			r.api.FailNext(tt.fault, http.MethodPut)
			err := r.c.UpsertLesson(context.Background(), cal, newLesson(t, "l1", "Секретная тема"))
			assertClass(t, err, tt.reason, tt.class)
			var ra interface{ RetryAfter() time.Duration }
			if !errors.As(err, &ra) || ra.RetryAfter() != tt.retryAfter {
				t.Fatalf("RetryAfter of %v, want %v", err, tt.retryAfter)
			}
			for _, secret := range []string{"Секретная", "fake google calendar", cal, testenv.FakeCalendarAccessToken} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q contains %q", err, secret)
				}
			}
			if n := len(r.api.Events(cal)); n != 0 {
				t.Fatalf("%d events after a failed upsert", n)
			}
		})
	}
}

// The calendar is also found when Google rate-limits the stored calendar check: the error waits, nothing is created.
func TestRateLimitedCalendarCheckCreatesNothing(t *testing.T) {
	r := newRig(t)
	r.calendar(t)
	r.api.FailNext(testenv.FaultUserRateLimit, http.MethodGet)
	_, _, err := r.c.EnsureCalendar(context.Background())
	assertClass(t, err, googlecal.ErrRateLimited, backoff.ErrTransient)
	if cals := r.api.Calendars(); len(cals) != 1 {
		t.Fatalf("%d calendars", len(cals))
	}
}

func TestTokenSourceFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason error
		class  error
	}{
		{"not signed in", errNoSignIn, googlecal.ErrNotSignedIn, backoff.ErrPermanent},
		{"expired", errExpired, googlecal.ErrReauthRequired, backoff.ErrPermanent},
		{"expired is not signed in", errExpired, googlecal.ErrNotSignedIn, backoff.ErrPermanent},
		{"refused", errRefused, googlecal.ErrAuthRejected, backoff.ErrPermanent},
		{"network of the token endpoint", shared.ErrNetworkUnavailable, googlecal.ErrNetwork, backoff.ErrTransient},
		{"unknown", errors.New("token endpoint 503"), googlecal.ErrUnavailable, backoff.ErrTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.auth.set(func() { r.auth.err = tt.err })
			_, _, err := r.c.EnsureCalendar(context.Background())
			assertClass(t, err, tt.reason, tt.class)
			if err := r.c.UpsertDeadline(context.Background(), deadline(t, "a1", "t")); !errors.Is(err, tt.reason) {
				t.Fatalf("UpsertDeadline = %v, want %v", err, tt.reason)
			}
			if n := len(r.api.Requests()); n != 0 {
				t.Fatalf("%d requests reached Google", n)
			}
		})
	}
}

func TestNetworkTimeoutAndCancel(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	tests := []struct {
		name   string
		base   string
		cancel bool
		reason error
	}{
		{"no network", closedURL + "/calendar/v3", false, shared.ErrNetworkUnavailable},
		{"timeout", slow.URL + "/calendar/v3", false, googlecal.ErrTimeout},
		{"cancelled", slow.URL + "/calendar/v3", true, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, googlecal.WithBaseURL(tt.base), googlecal.WithRequestTimeout(200*time.Millisecond))
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			defer cancel()
			_, _, err := r.c.EnsureCalendar(ctx)
			if !errors.Is(err, tt.reason) {
				t.Fatalf("EnsureCalendar = %v, want %v", err, tt.reason)
			}
			if !tt.cancel && !errors.Is(err, backoff.ErrTransient) {
				t.Fatalf("EnsureCalendar = %v, want transient", err)
			}
		})
	}
}

// Redirects are not followed and oversized answers are refused.
func TestBadAnswers(t *testing.T) {
	var targetHits sync.Map
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		targetHits.Store(req.URL.Path, true)
	}))
	t.Cleanup(target.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/redirect/"):
			http.Redirect(w, req, target.URL+"/stolen", http.StatusFound)
		case strings.HasPrefix(req.URL.Path, "/huge/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":["`+strings.Repeat("x", 5<<20)+`"]}`)
		case strings.HasPrefix(req.URL.Path, "/garbage/"):
			_, _ = io.WriteString(w, `{"items":`)
		}
	}))
	t.Cleanup(srv.Close)

	for _, base := range []string{"/redirect/v3", "/huge/v3", "/garbage/v3"} {
		t.Run(base, func(t *testing.T) {
			r := newRig(t, googlecal.WithBaseURL(srv.URL+base))
			_, _, err := r.c.EnsureCalendar(context.Background())
			assertClass(t, err, googlecal.ErrBadResponse, backoff.ErrTransient)
		})
	}
	if _, hit := targetHits.Load("/stolen"); hit {
		t.Fatal("the redirect was followed")
	}
}

func TestCredentialStoreFailure(t *testing.T) {
	r := newRig(t)
	r.store.Fail(shared.ErrStorage)
	_, _, err := r.c.EnsureCalendar(context.Background())
	assertClass(t, err, googlecal.ErrCredentials, backoff.ErrTransient)
	if n := len(r.api.Requests()); n != 0 {
		t.Fatalf("%d requests without a readable store", n)
	}
}

// readOnlyStore reads but cannot write, like a credential store that refuses new entries.
type readOnlyStore struct{ *testenv.MemCredentials }

func (readOnlyStore) Set(context.Context, string, string) error { return shared.ErrStorage }

// A store that cannot keep the calendar id fails before calendars.insert: repeated attempts leave no calendars behind
// at Google, with nothing stored and with a stored calendar that is gone.
func TestUnwritableStoreCreatesNoCalendar(t *testing.T) {
	const gone = `{"id":"gone@group.calendar.google.com"}`
	for _, stored := range []string{"", gone} {
		t.Run(fmt.Sprintf("stored %q", stored), func(t *testing.T) {
			ctx := context.Background()
			api := testenv.StartFakeGoogleCalendar(t)
			tr := &http.Transport{}
			t.Cleanup(tr.CloseIdleConnections)
			mem := testenv.NewMemCredentials()
			if stored != "" {
				if err := mem.Set(ctx, googlecal.CalendarIDName, stored); err != nil {
					t.Fatal(err)
				}
			}
			auth := &fakeAuth{transport: tr, token: testenv.FakeCalendarAccessToken}
			c, err := googlecal.New(auth, readOnlyStore{mem}, classify, googlecal.WithBaseURL(api.BaseURL()))
			if err != nil {
				t.Fatal(err)
			}

			for range 3 {
				_, _, err := c.EnsureCalendar(ctx)
				assertClass(t, err, googlecal.ErrCredentials, backoff.ErrTransient)
			}
			if cals := api.Calendars(); len(cals) != 0 {
				t.Fatalf("calendars %+v", cals)
			}
			for _, q := range api.Requests() {
				if q.Method == http.MethodPost {
					t.Fatalf("request %s %s", q.Method, q.Path)
				}
			}
		})
	}
}

// Every request goes to the configured base URL; the production default is the Calendar API itself.
func TestRequestsStayOnTheBaseURL(t *testing.T) {
	if googlecal.DefaultBaseURL != "https://www.googleapis.com/calendar/v3" {
		t.Fatalf("DefaultBaseURL = %q", googlecal.DefaultBaseURL)
	}
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)
	if err := r.c.UpsertLesson(ctx, cal, newLesson(t, "l1", "Алгебра")); err != nil {
		t.Fatal(err)
	}
	if err := r.c.DeleteEvent(ctx, cal, "l1"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.UpsertDeadline(ctx, deadline(t, "a1", "t")); err != nil {
		t.Fatal(err)
	}
	for _, u := range r.auth.sent() {
		if !strings.HasPrefix(u, r.api.BaseURL()+"/") {
			t.Fatalf("request to %s", u)
		}
	}
	assertOnlyOwnWrites(t, r, cal)
}

func TestNewValidates(t *testing.T) {
	auth := &fakeAuth{transport: &http.Transport{}}
	store := testenv.NewMemCredentials()
	if _, err := googlecal.New(auth, store, classify); err != nil {
		t.Fatalf("the defaults: %v", err)
	}
	tests := []struct {
		name     string
		auth     googlecal.Auth
		store    googlecal.Store
		classify googlecal.AuthClassifier
		opts     []googlecal.Option
	}{
		{"no auth", nil, store, classify, nil},
		{"no store", auth, nil, classify, nil},
		{"no classifier", auth, store, nil, nil},
		{"zero timeout", auth, store, classify, []googlecal.Option{googlecal.WithRequestTimeout(0)}},
		{"plain http", auth, store, classify, []googlecal.Option{googlecal.WithBaseURL("http://www.googleapis.com/calendar/v3")}},
		{"other scheme", auth, store, classify, []googlecal.Option{googlecal.WithBaseURL("ftp://127.0.0.1/v3")}},
		{"query", auth, store, classify, []googlecal.Option{googlecal.WithBaseURL("https://www.googleapis.com/calendar/v3?x=1")}},
		{"user", auth, store, classify, []googlecal.Option{googlecal.WithBaseURL("https://u:p@www.googleapis.com/calendar/v3")}},
		{"no host", auth, store, classify, []googlecal.Option{googlecal.WithBaseURL("/calendar/v3")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := googlecal.New(tt.auth, tt.store, tt.classify, tt.opts...); !errors.Is(err, googlecal.ErrInvalidConfig) {
				t.Fatalf("New = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

// Event ids are stable, valid for Google (base32hex a-v0-9, 5..1024 characters) and namespaced.
func TestEventIDs(t *testing.T) {
	ids := map[string]string{
		"lesson l1":      googlecal.LessonEventID("l1"),
		"lesson l2":      googlecal.LessonEventID("l2"),
		"deadline l1":    googlecal.DeadlineEventID("l1"),
		"long lesson id": googlecal.LessonEventID(shared.LessonID(strings.Repeat("z", 128))),
	}
	seen := map[string]string{}
	for name, id := range ids {
		if len(id) != 52 || strings.Trim(id, "0123456789abcdefghijklmnopqrstuv") != "" {
			t.Fatalf("%s: %q is not a valid event id", name, id)
		}
		if other, dup := seen[id]; dup {
			t.Fatalf("%s and %s share %q", name, other, id)
		}
		seen[id] = name
	}
	if googlecal.LessonEventID("l1") != ids["lesson l1"] {
		t.Fatal("the event id is not stable")
	}
	// Golden value: a change would orphan every event already in users' calendars.
	if got := googlecal.LessonEventID("l1"); got != goldenLessonL1 {
		t.Fatalf("LessonEventID(l1) = %q, want %q", got, goldenLessonL1)
	}
}
