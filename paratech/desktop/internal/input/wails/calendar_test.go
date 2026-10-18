package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/jobs/job"
)

// fakeCalendarSync is the use case behind the calendar binding.
type fakeCalendarSync struct {
	status    schedulesync.CalendarStatus
	statusErr error
	req       schedulesync.SyncRequest
	reqErr    error
	requests  int
}

func (f *fakeCalendarSync) Status(context.Context) (schedulesync.CalendarStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeCalendarSync) RequestSync(context.Context) (schedulesync.SyncRequest, error) {
	f.requests++
	return f.req, f.reqErr
}

var _ CalendarSyncUseCase = (*schedulesync.CalendarSync)(nil)

// The page calls these names (frontend/src/bindings, written with the page).
func TestGoogleCalendarServiceMethodSet(t *testing.T) {
	typ := reflect.TypeFor[*GoogleCalendarService]()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if want := []string{"GoogleCalendarStatus", "GoogleCalendarSync"}; !slices.Equal(got, want) {
		t.Fatalf("GoogleCalendarService methods %v, want %v: every exported method is callable from the page", got, want)
	}
}

func TestGoogleCalendarStatusJSON(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	s := NewGoogleCalendarService(&fakeCalendarSync{status: schedulesync.CalendarStatus{
		SignedIn: true, Pending: 3, LastWrittenAt: at, LastOutcome: schedulesync.OutcomeOK, LastRunAt: at,
	}}, discard())
	st, err := s.GoogleCalendarStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"signedIn":true,"pending":3,"pendingMore":false,"lastSyncedAt":"2026-10-16T06:30:00Z","state":"idle",` +
		`"lastOutcome":"ok","message":"Ждут синхронизации занятий: 3.",` +
		`"lastRunAt":"","lastRunOk":false,"lastRunAttempts":0,"lastRunOutcome":"","lastRunText":""}`
	if string(raw) != want {
		t.Fatalf("JSON = %s\nwant   %s", raw, want)
	}
}

// The last finished sync job: its end in UTC, the attempts and a fixed line of the result (TASK-074).
func TestGoogleCalendarStatusLastRun(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	lastRun := func(o schedulesync.SyncOutcome, attempts int) schedulesync.LastRun {
		return schedulesync.LastRun{At: at, Attempts: attempts, Outcome: o}
	}
	tests := []struct {
		name string
		st   schedulesync.CalendarStatus
		want CalendarSyncState
	}{
		{
			name: "none finished",
			st:   schedulesync.CalendarStatus{SignedIn: true},
			want: CalendarSyncState{SignedIn: true, State: "idle", Message: msgCalendarSynced},
		},
		{
			name: "ok after three attempts",
			st:   schedulesync.CalendarStatus{SignedIn: true, LastRun: lastRun(schedulesync.OutcomeOK, 3)},
			want: CalendarSyncState{
				SignedIn: true, State: "idle", Message: msgCalendarSynced, LastRunAt: "2026-10-16T06:30:00Z",
				LastRunOK: true, LastRunAttempts: 3, LastRunOutcome: "ok", LastRunText: "Успешно.",
			},
		},
		{
			name: "failed in an earlier start",
			st:   schedulesync.CalendarStatus{SignedIn: true, Pending: 2, LastRun: lastRun(schedulesync.OutcomeFailed, 5)},
			want: CalendarSyncState{
				SignedIn: true, Pending: 2, State: "idle", Message: msgCalendarFailed, LastRunAt: "2026-10-16T06:30:00Z",
				LastRunAttempts: 5, LastRunOutcome: "failed", LastRunText: "Ошибка: синхронизация не удалась.",
			},
		},
		{
			name: "sign-in expired",
			st: schedulesync.CalendarStatus{
				LastOutcome: schedulesync.OutcomeSignInExpired, LastRun: lastRun(schedulesync.OutcomeSignInExpired, 1),
			},
			want: CalendarSyncState{
				State: "idle", LastOutcome: "signInExpired", Message: msgCalendarSignInAgain,
				LastRunAt: "2026-10-16T06:30:00Z", LastRunAttempts: 1, LastRunOutcome: "signInExpired",
				LastRunText: "Ошибка: вход в Google истёк — войдите снова.",
			},
		},
		{
			name: "not signed in",
			st:   schedulesync.CalendarStatus{LastRun: lastRun(schedulesync.OutcomeNotSignedIn, 1)},
			want: CalendarSyncState{
				State: "idle", Message: msgCalendarSignIn, LastRunAt: "2026-10-16T06:30:00Z", LastRunAttempts: 1,
				LastRunOutcome: "notSignedIn", LastRunText: "Не выполнена: нет входа в Google.",
			},
		},
		{
			name: "no access to the calendar",
			st: schedulesync.CalendarStatus{
				SignedIn: true, Pending: 1, LastOutcome: schedulesync.OutcomeNoAccess,
				LastRun: lastRun(schedulesync.OutcomeNoAccess, 1),
			},
			want: CalendarSyncState{
				SignedIn: true, Pending: 1, State: "idle", LastOutcome: "noAccess",
				Message:   "Нет доступа к Google Календарю: войдите снова и разрешите доступ к календарю.",
				LastRunAt: "2026-10-16T06:30:00Z", LastRunAttempts: 1, LastRunOutcome: "noAccess",
				LastRunText: "Ошибка: нет доступа к Google Календарю — войдите снова и разрешите доступ к календарю.",
			},
		},
		{
			name: "no access in an earlier start",
			st:   schedulesync.CalendarStatus{SignedIn: true, LastRun: lastRun(schedulesync.OutcomeNoAccess, 1)},
			want: CalendarSyncState{
				SignedIn: true, State: "idle", Message: msgCalendarNoAccess, LastRunAt: "2026-10-16T06:30:00Z",
				LastRunAttempts: 1, LastRunOutcome: "noAccess", LastRunText: msgLastRunNoAccess,
			},
		},
		{
			name: "outcome unknown while signed out",
			st:   schedulesync.CalendarStatus{LastRun: lastRun(schedulesync.OutcomeNone, 2)},
			want: CalendarSyncState{
				State: "idle", Message: msgCalendarSignIn, LastRunAt: "2026-10-16T06:30:00Z", LastRunAttempts: 2,
			},
		},
		{
			name: "the run of this process decides the message",
			st: schedulesync.CalendarStatus{
				SignedIn: true, LastOutcome: schedulesync.OutcomeOK, LastRun: lastRun(schedulesync.OutcomeFailed, 1),
			},
			want: CalendarSyncState{
				SignedIn: true, State: "idle", LastOutcome: "ok", Message: msgCalendarSynced,
				LastRunAt: "2026-10-16T06:30:00Z", LastRunAttempts: 1, LastRunOutcome: "failed",
				LastRunText: "Ошибка: синхронизация не удалась.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewGoogleCalendarService(&fakeCalendarSync{status: tt.st}, discard())
			got, err := s.GoogleCalendarStatus(context.Background())
			if err != nil || got != tt.want {
				t.Fatalf("GoogleCalendarStatus = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestGoogleCalendarStatusMessages(t *testing.T) {
	tests := []struct {
		name      string
		st        schedulesync.CalendarStatus
		state     string
		msg       string
		statusErr error
	}{
		{"synced", schedulesync.CalendarStatus{SignedIn: true, LastOutcome: schedulesync.OutcomeOK}, "idle", msgCalendarSynced, nil},
		{"never ran", schedulesync.CalendarStatus{SignedIn: true}, "idle", msgCalendarSynced, nil},
		{"pending", schedulesync.CalendarStatus{SignedIn: true, Pending: 2}, "idle", "Ждут синхронизации занятий: 2.", nil},
		{"many pending", schedulesync.CalendarStatus{SignedIn: true, Pending: 999, PendingMore: true}, "idle",
			"Ждут синхронизации занятий: больше 999.", nil},
		{"not signed in", schedulesync.CalendarStatus{Pending: 2}, "idle", msgCalendarSignIn, nil},
		{"sign-in expired", schedulesync.CalendarStatus{LastOutcome: schedulesync.OutcomeSignInExpired}, "idle", msgCalendarSignInAgain, nil},
		{"failed", schedulesync.CalendarStatus{SignedIn: true, Pending: 1, LastOutcome: schedulesync.OutcomeFailed}, "idle",
			msgCalendarFailed, nil},
		{"queued", schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusQueued}, "queued", msgCalendarQueued, nil},
		{"waiting for power", schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusWaitingForPower}, "queued", msgCalendarQueued, nil},
		{"running", schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusRunning}, "running", msgCalendarRunning, nil},
		{"retrying", schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusRetryWait, LastOutcome: schedulesync.OutcomeFailed},
			"retrying", msgCalendarRetrying, nil},
		{"no network", schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusWaitingForNetwork}, "waitingForNetwork",
			msgCalendarNoNetwork, nil},
		{"unreadable", schedulesync.CalendarStatus{}, "unavailable", msgCalendarUnavailable, errors.New("store down: secret-value")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewGoogleCalendarService(&fakeCalendarSync{status: tt.st, statusErr: tt.statusErr}, discard())
			st, err := s.GoogleCalendarStatus(context.Background())
			if err != nil || st.State != tt.state || st.Message != tt.msg || strings.Contains(st.Message, "secret") {
				t.Fatalf("GoogleCalendarStatus = %+v, %v; want %s %q", st, err, tt.state, tt.msg)
			}
		})
	}
}

func TestGoogleCalendarSync(t *testing.T) {
	tests := []struct {
		name string
		req  schedulesync.SyncRequest
		err  error
		ok   bool
		msg  string
	}{
		{"created", schedulesync.SyncRequest{JobID: "j1", Created: true}, nil, true, msgCalendarSyncStarted},
		{"already queued", schedulesync.SyncRequest{JobID: "j1"}, nil, true, msgCalendarSyncScheduled},
		{"not signed in", schedulesync.SyncRequest{}, fmt.Errorf("x: %w", schedulesync.ErrCalendarNotConnected), false, msgCalendarSignInFirst},
		{"failed", schedulesync.SyncRequest{}, errors.New("database locked: secret-value"), false, msgCalendarSyncFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			f := &fakeCalendarSync{req: tt.req, reqErr: tt.err, status: schedulesync.CalendarStatus{SignedIn: true, Job: job.StatusQueued}}
			s := NewGoogleCalendarService(f, slog.New(slog.NewTextHandler(&logs, nil)))
			res, err := s.GoogleCalendarSync(context.Background())
			if err != nil || res.OK != tt.ok || res.Message != tt.msg || res.State.State != "queued" || f.requests != 1 {
				t.Fatalf("GoogleCalendarSync = %+v, %v", res, err)
			}
			raw, _ := json.Marshal(res)
			if strings.Contains(string(raw), "secret") {
				t.Fatalf("the answer carries the raw error: %s", raw)
			}
		})
	}
}
