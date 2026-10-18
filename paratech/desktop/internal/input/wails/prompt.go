package wails

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
	_ "time/tzdata" // Windows has no zone database for time.LoadLocation; the binary carries its own

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// PromptShownEvent is the Wails event emitted each time the shell shows the popup; the popup page then reads
// PromptService.Current. The page also reads it when it loads.
const PromptShownEvent = "recordingprompt:shown"

// Errors of the popup page. The page sees only these messages (frontend/src/bindings/prompt.ts matches them); causes
// are logged.
var (
	ErrPromptGone    = errors.New("the prompt is no longer current")
	ErrPromptRefused = errors.New("the action is not possible now")
	ErrInvalidSource = errors.New("invalid source")
)

// Codes of the JSON view; the page maps them to its fixed texts.
const (
	kindLesson = "lesson"
	kindAdHoc  = "adhoc"

	problemUnavailable = "unavailable"
	problemInProgress  = "in_progress"
	problemRecovering  = "recovering"
	problemFailed      = "failed"
	problemSourceGone  = "source_gone"
)

// timeOfDay is the layout of the lesson times on the popup.
const timeOfDay = "15:04"

// Icons reach the page only as small PNG data URLs; anything else is dropped.
const (
	maxIconBytes  = 16 << 10
	iconURLPrefix = "data:image/png;base64,"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// PromptUseCase is the part of the recording prompt use case the popup and the tray need (ARCH-42).
type PromptUseCase interface {
	Current() (recordingprompt.View, bool)
	Sources(ctx context.Context) (recordingprompt.Sources, error)
	OfferAdHoc(ctx context.Context) (recordingprompt.View, error)
	Start(ctx context.Context, id prompt.ID, source values.Source) (recordingprompt.View, error)
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
	// State is OFFERED, STARTING, STARTED, SKIPPED or EXPIRED.
	State string `json:"state"`
	// Problem says why the last start failed: "", "unavailable", "in_progress", "recovering", "source_gone" (the chosen
	// app does not run) or "failed".
	Problem string `json:"problem"`
}

// SourcesView is the JSON form of the source picker. App names are exe names ("" = the whole computer).
type SourcesView struct {
	Apps       []SourceView `json:"apps"`
	Selected   string       `json:"selected"`
	Remembered string       `json:"remembered"`
	// ListError reports that the apps could not be listed; only the whole computer is offered then.
	ListError bool `json:"listError"`
}

// SourceView is one app of the picker.
type SourceView struct {
	App     string `json:"app"`
	Title   string `json:"title"` // window title: untrusted text, render as plain text only
	Playing bool   `json:"playing"`
	Muted   bool   `json:"muted"` // muted in the Windows mixer: the recording would be silent
	Browser bool   `json:"browser"`
	Icon    string `json:"icon"` // "" or "data:image/png;base64,..."
}

// PromptService is bound to the popup page like SettingsService ("<package path>.PromptService.<Method>"). Keep the
// exported method set to what the page may call; the tray's ad-hoc offer is not exported.
type PromptService struct {
	uc  PromptUseCase
	log *slog.Logger
	// listFailing is set while the apps cannot be listed: the page asks every 3 s, the failure is logged once.
	listFailing atomic.Bool
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

// Sources lists what the popup may record. When the apps cannot be listed it still answers, with listError set.
func (s *PromptService) Sources(ctx context.Context) (SourcesView, error) {
	src, err := s.uc.Sources(ctx)
	out := SourcesView{
		Apps:       make([]SourceView, 0, len(src.Apps)),
		Selected:   src.Selected.App().String(),
		Remembered: src.Remembered.String(),
		ListError:  err != nil,
	}
	if err != nil {
		if !s.listFailing.Swap(true) {
			s.log.Warn("ui: list recording sources", "err", err)
		}
		return out, nil
	}
	s.listFailing.Store(false)
	for _, a := range src.Apps {
		out.Apps = append(out.Apps, SourceView{
			App: a.App.String(), Title: a.Title, Playing: a.Playing, Muted: a.Muted,
			Browser: values.IsBrowser(a.App), Icon: iconURL(a.IconPNG),
		})
	}
	return out, nil
}

// Start starts the recording the prompt offers from app (an exe name; "" = the whole computer). A failed start is
// not an error: the view carries its problem.
func (s *PromptService) Start(ctx context.Context, id, app string) (PromptView, error) {
	source := values.WholeComputer()
	if app != "" {
		name, err := shared.ParseAppName(app)
		if err != nil {
			return PromptView{}, ErrInvalidSource
		}
		if source, err = values.SourceOf(name); err != nil {
			return PromptView{}, ErrInvalidSource
		}
	}
	return s.act("start recording", id, func(pid prompt.ID) (recordingprompt.View, error) {
		return s.uc.Start(ctx, pid, source)
	})
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
	case errors.Is(err, prompt.ErrInvalidTransition):
		return PromptView{}, ErrPromptRefused
	}
	s.log.Error("ui: "+op, "err", err)
	return PromptView{}, ErrActionFailed
}

func promptView(v recordingprompt.View) PromptView {
	out := PromptView{ID: string(v.ID), State: string(v.State), Problem: problemCode(v.Problem)}
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

// iconURL is the data URL of a small PNG icon, or "" for anything that is not one.
func iconURL(png []byte) string {
	if len(png) == 0 || len(png) > maxIconBytes || !bytes.HasPrefix(png, pngSignature) {
		return ""
	}
	return iconURLPrefix + base64.StdEncoding.EncodeToString(png)
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
	case prompt.ProblemSourceGone:
		return problemSourceGone
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
