package wails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
)

// PromptShownEvent is the Wails event emitted each time the shell shows the popup; the popup page then reads
// PromptService.Current. The page also reads it when it loads.
const PromptShownEvent = "recordingprompt:shown"

// Errors of the popup page. The page sees only these messages (frontend/src/bindings/prompt.ts matches them); causes
// are logged.
var (
	ErrPromptGone    = errors.New("the prompt is no longer current")
	ErrPromptRefused = errors.New("the action is not possible now")
)

// Codes of the JSON view; the page maps them to its fixed texts.
const (
	kindLesson = "lesson"
	kindAdHoc  = "adhoc"

	problemUnavailable = "unavailable"
	problemInProgress  = "in_progress"
	problemRecovering  = "recovering"
	problemFailed      = "failed"
)

// timeOfDay is the layout of the lesson times on the popup.
const timeOfDay = "15:04"

// PromptUseCase is the part of the recording prompt use case the popup and the tray need (ARCH-42).
type PromptUseCase interface {
	Current() (recordingprompt.View, bool)
	OfferAdHoc(ctx context.Context) (recordingprompt.View, error)
	Start(ctx context.Context, id prompt.ID) (recordingprompt.View, error)
	Snooze(ctx context.Context, id prompt.ID) (recordingprompt.View, error)
	Skip(ctx context.Context, id prompt.ID) (recordingprompt.View, error)
}

// PromptView is the JSON form of the current prompt.
type PromptView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`  // "lesson" or "adhoc"
	Title string `json:"title"` // imported text: render as plain text only; empty for an ad-hoc prompt
	// Start and End are "15:04" in the lesson's time zone (UTC when the zone is unknown); empty for an ad-hoc prompt.
	Start string `json:"start"`
	End   string `json:"end"`
	// State is OFFERED, STARTING, STARTED, SNOOZED, SKIPPED, EXPIRED or WITHDRAWN.
	State string `json:"state"`
	// Problem says why the last start failed: "", "unavailable", "in_progress", "recovering" or "failed".
	Problem   string `json:"problem"`
	CanSnooze bool   `json:"canSnooze"`
}

// PromptService is bound to the popup page like SettingsService ("<package path>.PromptService.<Method>"). Keep the
// exported method set to what the page may call; the tray's ad-hoc offer is not exported.
type PromptService struct {
	uc  PromptUseCase
	log *slog.Logger
}

// NewPromptService returns the binding of the popup page.
func NewPromptService(uc PromptUseCase, log *slog.Logger) *PromptService {
	return &PromptService{uc: uc, log: log}
}

// Current returns the prompt that runs now, or null.
func (s *PromptService) Current() *PromptView {
	v, ok := s.uc.Current()
	if !ok {
		return nil
	}
	out := promptView(v)
	return &out
}

// Start starts the recording the prompt offers. A failed start is not an error: the view carries its problem.
func (s *PromptService) Start(ctx context.Context, id string) (PromptView, error) {
	return s.act("start recording", id, func(pid prompt.ID) (recordingprompt.View, error) { return s.uc.Start(ctx, pid) })
}

// Snooze hides the prompt for five minutes; only when the view offers it (canSnooze).
func (s *PromptService) Snooze(ctx context.Context, id string) (PromptView, error) {
	return s.act("snooze prompt", id, func(pid prompt.ID) (recordingprompt.View, error) { return s.uc.Snooze(ctx, pid) })
}

// Skip ends the prompt and hides the popup.
func (s *PromptService) Skip(ctx context.Context, id string) (PromptView, error) {
	return s.act("skip prompt", id, func(pid prompt.ID) (recordingprompt.View, error) { return s.uc.Skip(ctx, pid) })
}

// offerAdHoc is the tray's "Show recording popup": an offer without a lesson.
func (s *PromptService) offerAdHoc(ctx context.Context) {
	if _, err := s.uc.OfferAdHoc(ctx); err != nil {
		s.log.Error("ui: offer ad-hoc recording", "err", err)
	}
}

func (s *PromptService) act(op, id string, do func(prompt.ID) (recordingprompt.View, error)) (PromptView, error) {
	pid := prompt.ID(id)
	if pid.Validate() != nil {
		return PromptView{}, ErrInvalidID
	}
	v, err := do(pid)
	switch {
	case err == nil, errors.Is(err, recordingprompt.ErrStartFailed): // the use case logged the cause
		return promptView(v), nil
	case errors.Is(err, recordingprompt.ErrNoPrompt):
		return PromptView{}, ErrPromptGone
	case errors.Is(err, prompt.ErrInvalidTransition), errors.Is(err, prompt.ErrSnoozeNotAllowed):
		return PromptView{}, ErrPromptRefused
	}
	s.log.Error("ui: "+op, "err", err)
	return PromptView{}, ErrActionFailed
}

func promptView(v recordingprompt.View) PromptView {
	out := PromptView{ID: string(v.ID), State: string(v.State), Problem: problemCode(v.Problem), CanSnooze: v.CanSnooze}
	switch v.Kind {
	case prompt.KindLesson:
		out.Kind = kindLesson
		out.Title = v.Lesson.Title
		loc := zoneOf(v.Lesson.TimeZone)
		out.Start, out.End = clockText(v.Lesson.Start, loc), clockText(v.Lesson.End, loc)
	case prompt.KindAdHoc:
		out.Kind = kindAdHoc
	}
	return out
}

func problemCode(p prompt.Problem) string {
	switch p {
	case prompt.ProblemUnavailable:
		return problemUnavailable
	case prompt.ProblemInProgress:
		return problemInProgress
	case prompt.ProblemRecovering:
		return problemRecovering
	case prompt.ProblemFailed:
		return problemFailed
	}
	return ""
}

// zoneOf loads the lesson's IANA zone; an empty, "Local" or unknown name falls back to UTC, so the popup never shows
// the machine zone by accident.
func zoneOf(name string) *time.Location {
	if name == "" || name == "Local" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func clockText(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(timeOfDay)
}
