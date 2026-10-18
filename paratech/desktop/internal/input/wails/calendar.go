package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/jobs/job"
)

// Sync states as the page reads them.
const (
	calendarIdle              = "idle"
	calendarQueued            = "queued"
	calendarRunning           = "running"
	calendarRetrying          = "retrying"
	calendarWaitingForNetwork = "waitingForNetwork"
	calendarUnavailable       = "unavailable" // the state could not be read
)

// Operation names of the log.
const (
	opCalendarStatus = "ui: google calendar status"
	opCalendarSync   = "ui: google calendar sync"
)

// Fixed texts of the calendar sync (Russian UI).
const (
	msgCalendarUnavailable   = "Не удалось получить состояние синхронизации."
	msgCalendarRunning       = "Синхронизация с Google Календарём выполняется."
	msgCalendarQueued        = "Синхронизация запланирована."
	msgCalendarNoNetwork     = "Нет связи с Google: синхронизация продолжится, когда появится интернет."
	msgCalendarRetrying      = "Google временно недоступен: синхронизация повторится автоматически."
	msgCalendarSignIn        = "Войдите в Google, чтобы занятия появились в календаре «Learning Assistant»."
	msgCalendarSignInAgain   = "Вход в Google истёк: войдите снова, чтобы продолжить синхронизацию."
	msgCalendarNoAccess      = "Нет доступа к Google Календарю: войдите снова и разрешите доступ к календарю."
	msgCalendarFailed        = "Последняя синхронизация не удалась. Нажмите «Синхронизировать», чтобы повторить."
	msgCalendarPending       = "Ждут синхронизации занятий: %d."
	msgCalendarPendingMore   = "Ждут синхронизации занятий: больше %d."
	msgCalendarSynced        = "Календарь «Learning Assistant» синхронизирован."
	msgCalendarSyncStarted   = "Синхронизация запущена."
	msgCalendarSyncScheduled = "Синхронизация уже запланирована."
	msgCalendarSignInFirst   = "Сначала войдите в Google."
	msgCalendarSyncFailed    = "Не удалось запустить синхронизацию."

	// Result of the last finished sync, shown after «Последняя синхронизация: <date, time>».
	msgLastRunOK            = "Успешно."
	msgLastRunSignInExpired = "Ошибка: вход в Google истёк — войдите снова."
	msgLastRunNotSignedIn   = "Не выполнена: нет входа в Google."
	msgLastRunNoAccess      = "Ошибка: нет доступа к Google Календарю — войдите снова и разрешите доступ к календарю."
	msgLastRunFailed        = "Ошибка: синхронизация не удалась."
)

// CalendarSyncUseCase is the calendar synchronization of the Google section (ARCH-42).
type CalendarSyncUseCase interface {
	Status(ctx context.Context) (schedulesync.CalendarStatus, error)
	RequestSync(ctx context.Context) (schedulesync.SyncRequest, error)
}

// CalendarSyncState is the JSON form of the calendar synchronization.
type CalendarSyncState struct {
	SignedIn     bool   `json:"signedIn"`
	Pending      int    `json:"pending"`      // lessons the calendar does not show as they are, at most 999
	PendingMore  bool   `json:"pendingMore"`  // more than pending
	LastSyncedAt string `json:"lastSyncedAt"` // RFC 3339 UTC of the last lesson written; "" if none
	// State is "idle", "queued", "running", "retrying", "waitingForNetwork" or "unavailable".
	State string `json:"state"`
	// LastOutcome is the last run since the app started: "", "ok", "notSignedIn", "signInExpired", "noAccess" or
	// "failed".
	LastOutcome string `json:"lastOutcome"`
	// Attention is what the user must do, decided by the use case: "" (nothing), "signIn" (sign in again) or
	// "noAccess" (sign in again and allow the calendar).
	Attention string `json:"attention"`
	Message   string `json:"message"` // one fixed line that sums the state up

	// The last finished sync job, also of an earlier start of the app (TASK-074). LastRunAt is its end in RFC 3339
	// UTC, "" if none finished; LastRunOutcome takes the values of LastOutcome ("" when the record cannot tell the
	// result); LastRunText is a fixed line of the result, "" if none.
	LastRunAt       string `json:"lastRunAt"`
	LastRunAttempts int    `json:"lastRunAttempts"`
	LastRunOutcome  string `json:"lastRunOutcome"`
	LastRunText     string `json:"lastRunText"`
}

// CalendarSyncResult answers «Синхронизировать»: OK, a fixed text and the state afterwards.
type CalendarSyncResult struct {
	OK      bool              `json:"ok"`
	Message string            `json:"message"`
	State   CalendarSyncState `json:"state"`
}

// GoogleCalendarService is bound to the Google section of the «Подключения» page next to GoogleService
// ("<package path>.GoogleCalendarService.<Method>"). Keep the method set to what the page may call.
type GoogleCalendarService struct {
	uc  CalendarSyncUseCase
	log *slog.Logger
}

// NewGoogleCalendarService returns the binding of the calendar synchronization.
func NewGoogleCalendarService(uc CalendarSyncUseCase, log *slog.Logger) *GoogleCalendarService {
	return &GoogleCalendarService{uc: uc, log: log}
}

// GoogleCalendarStatus returns the synchronization state. It does not call Google.
func (s *GoogleCalendarService) GoogleCalendarStatus(ctx context.Context) (CalendarSyncState, error) {
	return s.state(ctx), nil
}

// GoogleCalendarSync asks for a synchronization now; a sync that has not started yet already covers it.
func (s *GoogleCalendarService) GoogleCalendarSync(ctx context.Context) (CalendarSyncResult, error) {
	req, err := s.uc.RequestSync(ctx)
	out := CalendarSyncResult{}
	switch {
	case errors.Is(err, schedulesync.ErrCalendarNotConnected):
		out.Message = msgCalendarSignInFirst
	case err != nil:
		s.log.Error(opCalendarSync, "err", err)
		out.Message = msgCalendarSyncFailed
	case req.Created:
		out.OK, out.Message = true, msgCalendarSyncStarted
	default:
		out.OK, out.Message = true, msgCalendarSyncScheduled
	}
	out.State = s.state(ctx)
	return out, nil
}

func (s *GoogleCalendarService) state(ctx context.Context) CalendarSyncState {
	st, err := s.uc.Status(ctx)
	if err != nil {
		s.log.Error(opCalendarStatus, "err", err)
		return CalendarSyncState{State: calendarUnavailable, Message: msgCalendarUnavailable}
	}
	out := CalendarSyncState{
		SignedIn: st.SignedIn, Pending: st.Pending, PendingMore: st.PendingMore, State: calendarJobState(st.Job),
		LastOutcome: string(st.LastOutcome), Attention: string(st.Attention),
	}
	if !st.LastWrittenAt.IsZero() {
		out.LastSyncedAt = st.LastWrittenAt.UTC().Format(time.RFC3339)
	}
	if !st.LastRun.At.IsZero() {
		out.LastRunAt = st.LastRun.At.UTC().Format(time.RFC3339)
		out.LastRunAttempts = st.LastRun.Attempts
		out.LastRunOutcome, out.LastRunText = string(st.LastRun.Outcome), lastRunText(st.LastRun.Outcome)
	}
	out.Message = calendarMessage(out)
	return out
}

func calendarJobState(s job.Status) string {
	switch s {
	case "":
		return calendarIdle
	case job.StatusRunning:
		return calendarRunning
	case job.StatusWaitingForNetwork:
		return calendarWaitingForNetwork
	case job.StatusRetryWait:
		return calendarRetrying
	}
	return calendarQueued // QUEUED, or waiting for power or quota: it runs later by itself
}

func lastRunText(o schedulesync.SyncOutcome) string {
	switch o {
	case schedulesync.OutcomeOK:
		return msgLastRunOK
	case schedulesync.OutcomeSignInExpired:
		return msgLastRunSignInExpired
	case schedulesync.OutcomeNotSignedIn:
		return msgLastRunNotSignedIn
	case schedulesync.OutcomeNoAccess:
		return msgLastRunNoAccess
	case schedulesync.OutcomeNone:
		return "" // the record cannot tell how the run ended
	}
	return msgLastRunFailed
}

func calendarMessage(st CalendarSyncState) string {
	// A failure the last-run line already shows is not repeated here.
	shown := st.LastRunOutcome == string(schedulesync.OutcomeFailed) ||
		st.LastRunOutcome == string(schedulesync.OutcomeNoAccess) ||
		st.LastRunOutcome == string(schedulesync.OutcomeSignInExpired)
	switch {
	case st.State == calendarRunning:
		return msgCalendarRunning
	case st.State == calendarWaitingForNetwork:
		return msgCalendarNoNetwork
	case st.State == calendarRetrying:
		return msgCalendarRetrying
	case st.State == calendarQueued:
		return msgCalendarQueued
	case st.Attention == string(schedulesync.AttentionSignIn):
		return msgCalendarSignInAgain
	case !st.SignedIn:
		return msgCalendarSignIn
	case st.Attention == string(schedulesync.AttentionNoAccess) && !shown:
		return msgCalendarNoAccess
	case st.LastOutcome == string(schedulesync.OutcomeFailed) && !shown:
		return msgCalendarFailed
	case st.PendingMore:
		return fmt.Sprintf(msgCalendarPendingMore, st.Pending)
	case st.Pending > 0:
		return fmt.Sprintf(msgCalendarPending, st.Pending)
	}
	return msgCalendarSynced
}
