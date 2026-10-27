package app

// «Удалить» of an assignment and its Google deadline event (TASK-229, ADR-0029 addendum 2026-10-27) on the wired core
// against fake Google: the delete queues the removal and asks for a sync, which deletes the event and the row;
// «Вернуть» before the sync forgets the removal and the event stays.

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// deadlineCore is a started core signed in to fake Google, with the app calendar created by the first sync and the
// deadline event of a1 written by a confirmed proposal. a2 is an assignment without an event.
type deadlineCore struct {
	a     *App
	cal   *testenv.FakeGoogleCalendar
	page  *wails.GoogleService
	calID string
}

func newDeadlineCore(t *testing.T) *deadlineCore {
	t.Helper()
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
	if len(cals) != 1 {
		t.Fatalf("calendars = %+v", cals)
	}

	insertAssignment(t, dir, "a1", homeworkTitle)
	insertAssignment(t, dir, "a2", "Без события")
	due := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	confirmAndExecute(t, a, propose(t, a, `{"kind":"create_deadline_event","assignment_id":"a1","title":"Rivers","due":"`+due+`"}`).ID)
	if events := cal.Events(cals[0].ID); len(events) != 1 || events[0].Summary != "Rivers" {
		t.Fatalf("events after the deadline = %+v", events)
	}
	return &deadlineCore{a: a, cal: cal, page: page, calID: cals[0].ID}
}

// removals counts the queued deadline event removals.
func (d *deadlineCore) removals(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := d.a.core.db.DBTX(ctx).QueryRowContext(ctx, "SELECT count(*) FROM deadline_event_removals").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (d *deadlineCore) deleteAssignment(t *testing.T, id shared.AssignmentID) int64 {
	t.Helper()
	ctx := context.Background()
	v, err := d.a.core.proposals.Assignment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := d.a.core.proposals.DeleteAssignment(ctx, id, v.Version)
	if err != nil {
		t.Fatalf("DeleteAssignment %s: %v", id, err)
	}
	return deleted.Version
}

// The delete asks for a sync that removes the event; an assignment without one is removed too (gone is success), and
// both rows go. Nothing else in Google is touched.
func TestDeleteAssignmentRemovesItsDeadlineEvent(t *testing.T) {
	d := newDeadlineCore(t)

	d.deleteAssignment(t, "a1")
	d.deleteAssignment(t, "a2")
	testenv.Eventually(t, "the deadline event removed", func() bool {
		return len(d.cal.Events(d.calID)) == 0 && d.removals(t) == 0
	}, nil)
	for _, st := range waitCalendarJobs(t, d.a, len(calendarJobs(t, d.a))) {
		if st != job.StatusCompleted {
			t.Fatalf("calendar sync jobs: %v", st)
		}
	}
	if len(d.cal.Calendars()) != 1 || d.cal.ForeignRequests() != 0 {
		t.Fatalf("calendars %+v, foreign requests %d", d.cal.Calendars(), d.cal.ForeignRequests())
	}
}

// «Вернуть» before the sync: no removal is left and the next sync keeps the event.
func TestRestoreBeforeTheSyncKeepsTheDeadlineEvent(t *testing.T) {
	d := newDeadlineCore(t)
	ctx := context.Background()
	// Signed out, the delete requests no sync job and the removal waits.
	if got, _ := d.page.GoogleSignOut(ctx); !got.OK {
		t.Fatalf("GoogleSignOut = %+v", got)
	}
	jobs := len(calendarJobs(t, d.a))

	version := d.deleteAssignment(t, "a1")
	if n := d.removals(t); n != 1 || len(calendarJobs(t, d.a)) != jobs {
		t.Fatalf("after the delete: %d removals, %d sync jobs (was %d)", n, len(calendarJobs(t, d.a)), jobs)
	}
	if _, err := d.a.core.proposals.RestoreAssignment(ctx, "a1", version); err != nil {
		t.Fatalf("RestoreAssignment: %v", err)
	}
	if n := d.removals(t); n != 0 {
		t.Fatalf("%d removals after the restore", n)
	}

	if got, _ := d.page.GoogleSignIn(ctx); !got.OK {
		t.Fatalf("GoogleSignIn = %+v", got)
	}
	if st := waitCalendarJobs(t, d.a, jobs+1); st[jobs] != job.StatusCompleted {
		t.Fatalf("sync after the restore: %v", st)
	}
	if events := d.cal.Events(d.calID); len(events) != 1 || events[0].Summary != "Rivers" {
		t.Fatalf("events after the restore and the sync = %+v", events)
	}
}
