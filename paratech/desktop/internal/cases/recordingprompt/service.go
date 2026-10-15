package recordingprompt

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors.
const (
	opLessonStarting = "lesson starting"
	opOfferAdHoc     = "offer ad hoc"
	opStart          = "start recording"
	opSnooze         = "snooze"
	opSkip           = "skip"
	opTick           = "check prompt"
	opSettings       = "read settings"
)

// Service offers recordings through the popup (PRD section 6.1).
type Service struct {
	settings   SettingsReader
	popup      Popup
	recordings Recordings
	clock      Clock
	sleeper    Sleeper
	log        *slog.Logger
	cfg        Config

	// renderMu serializes the popup commands; it is taken before mu. Each render reads the state it shows, so two
	// changes that race end with the popup matching the later one.
	renderMu sync.Mutex

	mu      sync.Mutex
	current *prompt.Prompt // nil when no prompt runs; a prompt is dropped when it ends
	// seq is the last prompt ID. Deliberately not the ID port (ARCH-24): a prompt ID is an in-memory handle of this
	// process, never stored, and a deterministic sequence is unique enough.
	seq uint64
}

// New validates the configuration and builds the Service. It starts nothing; call Run.
func New(deps Deps, cfg Config) (*Service, error) {
	switch {
	case deps.Settings == nil || deps.Popup == nil || deps.Recordings == nil || deps.Clock == nil || deps.Sleeper == nil:
		return nil, fmt.Errorf("%w: settings, popup, recordings, clock and sleeper are required", ErrInvalidConfig)
	case cfg.PollInterval <= 0:
		return nil, fmt.Errorf("%w: poll interval %v", ErrInvalidConfig, cfg.PollInterval)
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		settings: deps.Settings, popup: deps.Popup, recordings: deps.Recordings, clock: deps.Clock, sleeper: deps.Sleeper,
		log: log, cfg: cfg,
	}, nil
}

// LessonStarting offers to record a lesson that is starting (scheduler.Events). With automatic prompts off the
// announcement is dropped and no popup appears (AC-02). The new prompt replaces the current one. It returns quickly:
// one settings read and one popup command.
func (s *Service) LessonStarting(ctx context.Context, e lesson.StartAnnounced) error {
	on, err := s.promptsEnabled(ctx)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opLessonStarting, e.LessonID, err)
	}
	if !on {
		s.log.Debug("recording prompts are off; lesson start dropped", slog.String("lesson", e.LessonID.String()))
		return nil
	}

	s.mu.Lock()
	p, err := prompt.NewLesson(s.nextID(), prompt.Lesson{
		ID: e.LessonID, Title: e.Title, Start: e.Start, End: e.End, TimeZone: e.TimeZone,
	}, s.clock.Now())
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("%s %s: %w", opLessonStarting, e.LessonID, err)
	}
	s.replace(p)
	s.mu.Unlock()

	// The title is imported text: it stays out of the log.
	s.log.Info("recording prompt offered", slog.String("prompt", p.ID().String()), slog.String("lesson", e.LessonID.String()))
	if err := s.render(); err != nil {
		return fmt.Errorf("%s %s: %w", opLessonStarting, e.LessonID, err)
	}
	return nil
}

// OfferAdHoc shows a prompt without a lesson, whatever the prompts setting (an explicit user action). A prompt that
// the popup shows already stays and is shown again.
func (s *Service) OfferAdHoc(context.Context) (View, error) {
	s.mu.Lock()
	p := s.current
	if p == nil || !p.State().Visible() {
		var err error
		if p, err = prompt.NewAdHoc(s.nextID(), s.clock.Now()); err != nil {
			s.mu.Unlock()
			return View{}, fmt.Errorf("%s: %w", opOfferAdHoc, err)
		}
		s.replace(p)
		s.log.Info("ad-hoc recording prompt offered", slog.String("prompt", p.ID().String()))
	}
	view := viewOf(p, s.clock.Now())
	s.mu.Unlock()

	if err := s.render(); err != nil {
		return view, fmt.Errorf("%s: %w", opOfferAdHoc, err)
	}
	return view, nil
}

// Current returns the prompt that runs now, if any; the popup page reads it when it loads and when it is shown.
func (s *Service) Current() (View, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return View{}, false
	}
	return viewOf(s.current, s.clock.Now()), true
}

// Start starts the recording that prompt id offers. On success the popup hides and the main window stays as it is.
// A failed start keeps the prompt on offer with its problem and returns its View with an error wrapping
// ErrStartFailed; until the recordings are ready the problem is prompt.ProblemRecovering. One start runs at a time
// (prompt.ErrInvalidTransition), and an ID that is not current yields ErrNoPrompt.
func (s *Service) Start(ctx context.Context, id prompt.ID) (View, error) {
	s.mu.Lock()
	p, err := s.lookup(id)
	if err == nil {
		err = p.BeginStart()
	}
	if err != nil {
		s.mu.Unlock()
		return View{}, fmt.Errorf("%s %s: %w", opStart, id, err)
	}
	lessonID := p.Lesson().ID
	s.mu.Unlock()

	var rec shared.RecordingID
	startErr, problem := ErrNotReady, prompt.ProblemRecovering
	if s.recordings.Ready() {
		rec, startErr = s.recordings.StartRecording(ctx, lessonID)
		problem = prompt.ProblemOf(startErr)
	}

	// p may have been replaced meanwhile; it is still ours to finish, as only the current prompt takes actions.
	s.mu.Lock()
	if startErr == nil {
		err = p.Started()
		if s.current == p {
			s.current = nil
		}
	} else {
		err = p.StartFailed(problem)
	}
	view := viewOf(p, s.clock.Now())
	s.mu.Unlock()
	if err != nil {
		return view, fmt.Errorf("%s %s: %w", opStart, id, err) // unreachable: STARTING allows both
	}

	if startErr != nil {
		s.log.Warn("recording did not start from the prompt", slog.String("prompt", id.String()),
			slog.String("problem", problem.String()), slog.Any("error", startErr))
		return view, fmt.Errorf("%s %s: %w: %w", opStart, id, ErrStartFailed, startErr)
	}

	s.log.Info("recording started from the prompt", slog.String("prompt", id.String()), slog.String("recording", rec.String()))
	s.renderLogged(opStart)
	return view, nil
}

// Snooze hides prompt id for prompt.SnoozeDelay. Only a lesson prompt whose lesson still runs then can be snoozed
// (prompt.ErrSnoozeNotAllowed).
func (s *Service) Snooze(_ context.Context, id prompt.ID) (View, error) {
	s.mu.Lock()
	p, err := s.lookup(id)
	if err == nil {
		err = p.Snooze(s.clock.Now())
	}
	if err != nil {
		s.mu.Unlock()
		return View{}, fmt.Errorf("%s %s: %w", opSnooze, id, err)
	}
	view := viewOf(p, s.clock.Now())
	s.mu.Unlock()

	s.log.Info("recording prompt snoozed", slog.String("prompt", id.String()), slog.Time("until", p.SnoozedUntil()))
	s.renderLogged(opSnooze)
	return view, nil
}

// Skip ends prompt id and hides the popup; its lesson is not offered again.
func (s *Service) Skip(_ context.Context, id prompt.ID) (View, error) {
	s.mu.Lock()
	p, err := s.lookup(id)
	if err == nil {
		err = p.Skip()
	}
	if err != nil {
		s.mu.Unlock()
		return View{}, fmt.Errorf("%s %s: %w", opSkip, id, err)
	}
	s.current = nil
	view := viewOf(p, s.clock.Now())
	s.mu.Unlock()

	s.log.Info("recording prompt skipped", slog.String("prompt", id.String()))
	s.renderLogged(opSkip)
	return view, nil
}

// Run checks the current prompt every poll interval until ctx ends and then returns ctx's error. A failed check is
// logged and the next one tries again. Run starts no goroutines.
func (s *Service) Run(ctx context.Context) error {
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("recording prompt check failed", slog.Any("error", err))
		}
		if err := s.sleeper.Sleep(ctx, s.cfg.PollInterval); err != nil {
			return err
		}
	}
}

// Tick hides a lesson prompt whose lesson ended, and shows a snoozed prompt that is due again, unless prompts were
// turned off meanwhile: then it ends without showing.
func (s *Service) Tick(ctx context.Context) error {
	now := s.clock.Now()
	s.mu.Lock()
	p := s.current
	switch {
	case p == nil:
		s.mu.Unlock()
		return nil
	case p.ExpiryDue(now):
		err := p.Expire(now)
		s.current = nil
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("%s: %w", opTick, err) // unreachable: ExpiryDue allows it
		}
		s.log.Info("recording prompt expired", slog.String("prompt", p.ID().String()))
		return s.render()
	}
	due := p.ReshowDue(now)
	s.mu.Unlock()
	if !due {
		return nil
	}

	on, err := s.promptsEnabled(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", opTick, err) // still snoozed: the next tick reads again
	}

	now = s.clock.Now()
	s.mu.Lock()
	if s.current != p || !p.ReshowDue(now) {
		s.mu.Unlock()
		return nil // changed meanwhile; the next tick decides again
	}
	if on {
		err = p.Reshow(now)
	} else {
		err = p.Withdraw()
		s.current = nil
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w", opTick, err)
	}

	if on {
		s.log.Info("snoozed recording prompt offered again", slog.String("prompt", p.ID().String()))
	} else {
		s.log.Info("recording prompts are off; snoozed prompt withdrawn", slog.String("prompt", p.ID().String()))
	}
	return s.render()
}

// render makes the popup match the current prompt: shown while it is offered or starting, hidden otherwise.
func (s *Service) render() error {
	s.renderMu.Lock()
	defer s.renderMu.Unlock()

	s.mu.Lock()
	visible := s.current != nil && s.current.State().Visible()
	s.mu.Unlock()
	if visible {
		return s.popup.ShowPopup()
	}
	return s.popup.HidePopup()
}

// renderLogged renders after a user action whose result stands: a popup that cannot follow is only logged.
func (s *Service) renderLogged(op string) {
	if err := s.render(); err != nil {
		s.log.Warn("recording popup did not follow the prompt", slog.String("action", op), slog.Any("error", err))
	}
}

// lookup returns the current prompt if it is id; s.mu is held.
func (s *Service) lookup(id prompt.ID) (*prompt.Prompt, error) {
	if s.current == nil || s.current.ID() != id {
		return nil, ErrNoPrompt
	}
	return s.current, nil
}

// replace makes p the current prompt; s.mu is held.
func (s *Service) replace(p *prompt.Prompt) {
	if old := s.current; old != nil {
		s.log.Info("recording prompt replaced", slog.String("prompt", old.ID().String()),
			slog.String("state", old.State().String()), slog.String("by", p.ID().String()))
	}
	s.current = p
}

// nextID returns a fresh prompt ID; s.mu is held.
func (s *Service) nextID() prompt.ID {
	s.seq++
	return prompt.ID("prompt-" + strconv.FormatUint(s.seq, 10))
}

func (s *Service) promptsEnabled(ctx context.Context) (bool, error) {
	st, err := s.settings.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("%s: %w", opSettings, err)
	}
	return st.RecordingPromptsEnabled(), nil
}
