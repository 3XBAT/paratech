package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

func TestClassifyGoogleAuth(t *testing.T) {
	tests := []struct {
		err  error
		want googlecal.AuthFailure
	}{
		{googleauth.ErrNotSignedIn, googlecal.AuthNotSignedIn},
		{googleauth.ErrNotConfigured, googlecal.AuthNotSignedIn},
		{googleauth.ErrReauthRequired, googlecal.AuthExpired},
		{fmt.Errorf("%w; %w", googleauth.ErrReauthRequired, googleauth.ErrCredentials), googlecal.AuthExpired},
		{googleauth.ErrInvalidClient, googlecal.AuthRejected},
		{googleauth.ErrRejected, googlecal.AuthRejected},
		{googleauth.ErrBadResponse, googlecal.AuthRejected},
		{googleauth.ErrNetwork, googlecal.AuthNone},
		{googleauth.ErrUnavailable, googlecal.AuthNone},
		{googleauth.ErrCredentials, googlecal.AuthNone},
		{errors.New("other"), googlecal.AuthNone},
	}
	for _, tt := range tests {
		if got := classifyGoogleAuth(fmt.Errorf("googleauth: access token: %w", tt.err)); got != tt.want {
			t.Errorf("classifyGoogleAuth(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// The lesson bridge adds the use case's sign-in sentinels and keeps the adapter error with its class.
func TestLessonCalendarErrorMapping(t *testing.T) {
	notSignedIn := backoff.Permanent(fmt.Errorf("op: %w", googlecal.ErrNotSignedIn))
	expired := backoff.Permanent(fmt.Errorf("op: %w: %w", googlecal.ErrReauthRequired, googlecal.ErrNotSignedIn))
	transient := backoff.Transient(fmt.Errorf("op: %w", googlecal.ErrUnavailable))
	tests := []struct {
		name                  string
		from                  error
		notConnected, expired bool
	}{
		{"none", nil, false, false},
		{"not signed in", notSignedIn, true, false},
		{"sign in again", expired, true, true},
		{"unavailable", transient, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := lessonCalendarError(tt.from)
			if errors.Is(err, schedulesync.ErrCalendarNotConnected) != tt.notConnected ||
				errors.Is(err, schedulesync.ErrCalendarSignInExpired) != tt.expired || !errors.Is(err, tt.from) {
				t.Fatalf("lessonCalendarError = %v", err)
			}
			if tt.from != nil && backoff.Classify(err) != backoff.Classify(tt.from) {
				t.Fatalf("the class changed: %v", err)
			}
		})
	}
}

// The write of one lesson gets schedulesync.ErrLessonRejected for the reasons of that event only, keeping its class;
// failures of the calendar, the sign-in or the network do not.
func TestLessonWriteErrorMapping(t *testing.T) {
	tests := []struct {
		reason   error
		class    func(error) error
		rejected bool
	}{
		{googlecal.ErrInvalidRequest, backoff.Permanent, true},
		{googlecal.ErrRejected, backoff.Permanent, true},
		{googlecal.ErrConflict, backoff.Transient, true},
		{googlecal.ErrNotRestored, backoff.Transient, true},
		{googlecal.ErrForbidden, backoff.Permanent, false},
		{googlecal.ErrNotFound, backoff.Permanent, false},
		{googlecal.ErrUnauthorized, backoff.Permanent, false},
		{googlecal.ErrForeignCalendar, backoff.Transient, false},
		{googlecal.ErrRateLimited, backoff.Transient, false},
		{googlecal.ErrUnavailable, backoff.Transient, false},
		{googlecal.ErrNetwork, backoff.Transient, false},
		{googlecal.ErrCredentials, backoff.Transient, false},
		{googlecal.ErrNotSignedIn, backoff.Permanent, false},
	}
	for _, tt := range tests {
		from := tt.class(fmt.Errorf("googlecal: upsert lesson: %w", tt.reason))
		err := lessonWriteError(from)
		if errors.Is(err, schedulesync.ErrLessonRejected) != tt.rejected || !errors.Is(err, from) ||
			backoff.Classify(err) != backoff.Classify(from) {
			t.Errorf("lessonWriteError(%v) = %v, want rejected %v", tt.reason, err, tt.rejected)
		}
	}
	if err := lessonWriteError(nil); err != nil {
		t.Fatalf("lessonWriteError(nil) = %v", err)
	}
}

// calendarJobs returns the calendar sync jobs of the core.
func calendarJobs(t *testing.T, a *App) []*job.Job {
	t.Helper()
	all, err := sqlite.NewJobRepository(a.core.db).ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		t.Fatal(err)
	}
	var out []*job.Job
	for _, j := range all {
		if j.Type() == schedulesync.CalendarSyncJobType {
			out = append(out, j)
		}
	}
	return out
}

// waitCalendarJobs waits until the core has n calendar sync jobs and none of them is QUEUED or RUNNING, and returns
// their statuses.
func waitCalendarJobs(t *testing.T, a *App, n int) []job.Status {
	t.Helper()
	var statuses []job.Status
	testenv.Eventually(t, fmt.Sprintf("%d settled calendar sync jobs", n), func() bool {
		statuses = statuses[:0]
		jobs := calendarJobs(t, a)
		for _, j := range jobs {
			if j.Status() == job.StatusQueued || j.Status() == job.StatusRunning {
				return false
			}
			statuses = append(statuses, j.Status())
		}
		return len(jobs) == n
	}, nil)
	return statuses
}

// importCSV previews and commits a CSV schedule through the core's import use case.
func importCSV(t *testing.T, core wails.Core, csv string) schedulesync.CommitResult {
	t.Helper()
	ctx := context.Background()
	req := schedulesync.ImportRequest{Source: importplan.SourceCSV, Data: []byte(csv)}
	p, err := core.Schedule.PreviewImport(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.Schedule.CommitImport(ctx, schedulesync.CommitRequest{Source: req.Source, Data: req.Data, ConfirmToken: p.ConfirmToken})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

const csvHeader = "title,date,start,end,timezone,uid\n"

// The wired core against fake Google: an import without a sign-in enqueues nothing; the sign-in asks for a sync that
// creates the app calendar and writes the lesson; a re-import updates the same event; a confirmed deadline proposal
// writes one event, and a repeated write keeps it one; nothing outside the app calendar is touched.
func TestWiredCalendarSync(t *testing.T) {
	ctx := context.Background()
	g := testenv.StartFakeGoogleOAuth(t)
	cal := testenv.StartFakeGoogleCalendar(t)
	cal.AcceptTokens(g.IsLiveAccessToken)
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	useFakeGoogle(a, g)
	a.env.googleCalendar = []googlecal.Option{googlecal.WithBaseURL(cal.BaseURL())}
	core := startCore(t, a)
	page := wails.NewGoogleService(core.Google, a.log)

	if res := importCSV(t, core, csvHeader+"Math,2026-10-20,09:00,10:30,Europe/Moscow,m1\n"); res.Added != 1 {
		t.Fatalf("import = %+v", res)
	}
	if jobs := calendarJobs(t, a); len(jobs) != 0 {
		t.Fatalf("%d calendar sync jobs without a sign-in", len(jobs))
	}

	if got, _ := page.SetGoogleClient(ctx, testenv.FakeGoogleClientID, testenv.FakeGoogleClientSecret); !got.OK {
		t.Fatalf("SetGoogleClient = %+v", got)
	}
	if got, _ := page.GoogleSignIn(ctx); !got.OK {
		t.Fatalf("GoogleSignIn = %+v", got)
	}
	if st := waitCalendarJobs(t, a, 1); st[0] != job.StatusCompleted {
		t.Fatalf("sync after the sign-in: %v", st)
	}
	cals := cal.Calendars()
	if len(cals) != 1 || cals[0].Summary != googlecal.CalendarSummary {
		t.Fatalf("calendars = %+v", cals)
	}
	events := cal.Events(cals[0].ID)
	if len(events) != 1 || events[0].Summary != "Math" || events[0].Start.TimeZone != "Europe/Moscow" {
		t.Fatalf("events = %+v", events)
	}

	if res := importCSV(t, core, csvHeader+"Algebra,2026-10-20,11:00,12:30,Europe/Moscow,m1\n"); res.Changed != 1 {
		t.Fatalf("re-import = %+v", res)
	}
	if st := waitCalendarJobs(t, a, 2); st[1] != job.StatusCompleted {
		t.Fatalf("sync after the re-import: %v", st)
	}
	again := cal.Events(cals[0].ID)
	if len(again) != 1 || again[0].ID != events[0].ID || again[0].Summary != "Algebra" ||
		again[0].Start.DateTime != "2026-10-20T08:00:00Z" {
		t.Fatalf("events after the re-import = %+v", again)
	}

	// Deadlines reach the calendar only through a confirmed proposal; a repeated write keeps one event.
	insertAssignment(t, dir, "a1", homeworkTitle)
	due := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	confirmAndExecute(t, a, propose(t, a, `{"kind":"create_deadline_event","assignment_id":"a1","title":"Rivers","due":"`+due+`"}`).ID)
	executed := cal.Events(cals[0].ID)
	if len(executed) != 2 || !slices.ContainsFunc(executed, func(e testenv.FakeCalendarEvent) bool { return e.Summary == "Rivers" }) {
		t.Fatalf("events after the executed proposal = %+v", executed)
	}
	if err := (deadlineCalendar{a.core.calendar}).UpsertDeadline(ctx, confirmedDeadline(t)); err != nil {
		t.Fatal(err)
	}
	withDeadline := cal.Events(cals[0].ID)
	if len(withDeadline) != 2 || len(cal.Calendars()) != 1 || cal.ForeignRequests() != 0 {
		t.Fatalf("events %+v, calendars %d, foreign requests %d", withDeadline, len(cal.Calendars()), cal.ForeignRequests())
	}

	// «Синхронизировать» on the page binding; nothing is pending, so nothing is written.
	calPage := wails.NewGoogleCalendarService(core.Calendar, a.log)
	if res, _ := calPage.GoogleCalendarSync(ctx); !res.OK || res.Message != "Синхронизация запущена." {
		t.Fatalf("GoogleCalendarSync = %+v", res)
	}
	if st := waitCalendarJobs(t, a, 3); st[2] != job.StatusCompleted {
		t.Fatalf("sync on request: %v", st)
	}
	st, _ := calPage.GoogleCalendarStatus(ctx)
	if !st.SignedIn || st.State != "idle" || st.Pending != 0 || st.LastOutcome != "ok" || st.LastSyncedAt == "" ||
		st.Message != "Календарь «Learning Assistant» синхронизирован." {
		t.Fatalf("GoogleCalendarStatus = %+v", st)
	}

	// Sign-out keeps the calendar binding: «Синхронизировать» asks to sign in, and the same account signing in again
	// gets its calendar back, no second one.
	if got, _ := page.GoogleSignOut(ctx); !got.OK {
		t.Fatalf("GoogleSignOut = %+v", got)
	}
	if res, _ := calPage.GoogleCalendarSync(ctx); res.OK || res.Message != "Сначала войдите в Google." || len(calendarJobs(t, a)) != 3 {
		t.Fatalf("GoogleCalendarSync after the sign-out = %+v", res)
	}
	if got, _ := page.GoogleSignIn(ctx); !got.OK {
		t.Fatalf("GoogleSignIn again = %+v", got)
	}
	if st := waitCalendarJobs(t, a, 4); st[3] != job.StatusCompleted {
		t.Fatalf("sync after the second sign-in: %v", st)
	}
	if got := cal.Calendars(); len(got) != 1 || got[0].ID != cals[0].ID || len(cal.Events(cals[0].ID)) != 2 {
		t.Fatalf("calendars after the second sign-in = %+v", got)
	}
}

// At start a calendar sync is requested only when signed in.
func TestStartRequestsCalendarSyncWhenSignedIn(t *testing.T) {
	for _, signedIn := range []bool{false, true} {
		t.Run(fmt.Sprint("signed in ", signedIn), func(t *testing.T) {
			a, f := newTestAppWith(t, t.TempDir())
			if signedIn {
				if err := f.secrets[googleSecretsService].Set(context.Background(), googleauth.RefreshTokenName, "fake-refresh"); err != nil {
					t.Fatal(err)
				}
			}
			startCore(t, a)
			want := 0
			if signedIn {
				want = 1
			}
			if n := len(calendarJobs(t, a)); n != want {
				t.Fatalf("%d calendar sync jobs after the start, want %d", n, want)
			}
		})
	}
}
