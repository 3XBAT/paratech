package recordingprompt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/ongoing"
	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors.
const (
	opLessonStarting = "lesson starting"
	opOfferAdHoc     = "offer ad hoc"
	opStart          = "start recording"
	opSkip           = "skip"
	opTick           = "check prompt"
	opSettings       = "read settings"
	opSources        = "list sources"
	opRemember       = "remember the source"
	opStartManual    = "start recording from the main window"
)

// rememberAttempts bounds the saves of the remembered source: a version conflict (the settings page saved meanwhile)
// reloads once.
const rememberAttempts = 2

// lessonLookahead ends the lesson query just past now, so a lesson starting right now is listed whatever the store's
// time precision; ongoing.Current keeps only the lessons in progress.
const lessonLookahead = time.Second

// Service offers recordings through the popup (PRD section 6.1).
type Service struct {
	settings   SettingsStore
	apps       AudioApps
	popup      Popup
	recordings Recordings
	lessons    Lessons
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
	case deps.Settings == nil || deps.Apps == nil || deps.Popup == nil || deps.Recordings == nil || deps.Lessons == nil ||
		deps.Clock == nil || deps.Sleeper == nil:
		return nil, fmt.Errorf("%w: settings, apps, popup, recordings, lessons, clock and sleeper are required", ErrInvalidConfig)
	case cfg.PollInterval <= 0:
		return nil, fmt.Errorf("%w: poll interval %v", ErrInvalidConfig, cfg.PollInterval)
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		settings: deps.Settings, apps: deps.Apps, popup: deps.Popup, recordings: deps.Recordings, lessons: deps.Lessons,
		clock: deps.Clock, sleeper: deps.Sleeper, log: log, cfg: cfg,
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
	view := viewOf(p)
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
	return viewOf(s.current), true
}

// Sources lists what the popup offers to record (TASK-059): the open apps, sorted, and the source to preselect. When
// the apps cannot be listed it returns an error together with Sources that hold only the remembered app, so the
// popup can still offer the whole computer. A failed settings read is logged and treated as nothing remembered.
func (s *Service) Sources(ctx context.Context) (Sources, error) {
	var remembered shared.AppName
	if st, err := s.settings.Load(ctx); err != nil {
		s.log.Warn("the remembered recording source could not be read", slog.Any("error", err))
	} else {
		remembered = st.RecordingSource()
	}

	apps, err := s.apps.List(ctx)
	if err != nil {
		return Sources{Selected: values.WholeComputer(), Remembered: remembered}, fmt.Errorf("%s: %w", opSources, err)
	}
	values.SortAudioApps(apps)
	return Sources{Apps: apps, Selected: values.Preselect(remembered, apps), Remembered: remembered}, nil
}

// Start starts the recording of source that prompt id offers. On success the popup hides, the main window stays as
// it is and source is remembered for the next prompt (a failure to remember is only logged). A failed start keeps
// the prompt on offer with its problem and returns its View with an error wrapping ErrStartFailed; until the
// recordings are ready the problem is prompt.ProblemRecovering. One start runs at a time (prompt.ErrInvalidTransition),
// and an ID that is not current yields ErrNoPrompt.
func (s *Service) Start(ctx context.Context, id prompt.ID, source values.Source) (View, error) {
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
		rec, startErr = s.recordings.StartRecording(ctx, lessonID, source)
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
	view := viewOf(p)
	s.mu.Unlock()
	if err != nil {
		return view, fmt.Errorf("%s %s: %w", opStart, id, err) // unreachable: STARTING allows both
	}

	if startErr != nil {
		s.log.Warn("recording did not start from the prompt", slog.String("prompt", id.String()),
			slog.String("problem", problem.String()), slog.Any("error", startErr))
		return view, fmt.Errorf("%s %s: %w: %w", opStart, id, ErrStartFailed, startErr)
	}

	s.log.Info("recording started from the prompt", slog.String("prompt", id.String()), slog.String("recording", rec.String()),
		slog.String("source", source.String()))
	s.renderLogged(opStart)
	s.remember(ctx, source)
	return view, nil
}

// StartManual starts recording source from the main window, without a prompt (TASK-081). The recording is linked to
// the lesson in progress now (ongoing.Current), or to none; a schedule that cannot be read is logged and the recording
// starts without a lesson, as recording the class matters more than the link. On success source is remembered like
// in Start, and a prompt on offer ends as started and hides: the recording it offered runs, and a second one would be
// refused anyway. A prompt whose own start runs is left to finish; one of the two starts is refused as in progress.
// A failed start returns Manual with its problem and an error wrapping ErrStartFailed; until the recordings are ready
// the problem is prompt.ProblemRecovering.
func (s *Service) StartManual(ctx context.Context, source values.Source) (Manual, error) {
	if !s.recordings.Ready() {
		return Manual{Problem: prompt.ProblemRecovering}, fmt.Errorf("%s: %w: %w", opStartManual, ErrStartFailed, ErrNotReady)
	}

	lessonID := s.lessonNow(ctx)
	rec, err := s.recordings.StartRecording(ctx, lessonID, source)
	if err != nil {
		problem := prompt.ProblemOf(err)
		s.log.Warn("recording did not start from the main window", slog.String("lesson", lessonID.String()),
			slog.String("problem", problem.String()), slog.Any("error", err))
		return Manual{Lesson: lessonID, Problem: problem}, fmt.Errorf("%s: %w: %w", opStartManual, ErrStartFailed, err)
	}

	s.log.Info("recording started from the main window", slog.String("recording", rec.String()),
		slog.String("lesson", lessonID.String()), slog.String("source", source.String()))
	s.endOffer()
	s.remember(ctx, source)
	return Manual{Recording: rec, Lesson: lessonID}, nil
}

// lessonNow is the lesson in progress now, or none; a failed read is logged.
func (s *Service) lessonNow(ctx context.Context) shared.LessonID {
	now := s.clock.Now()
	lessons, err := s.lessons.ListStartingBetween(ctx, now.Add(-lesson.MaxDuration), now.Add(lessonLookahead))
	if err != nil {
		s.log.Warn("the lesson in progress could not be read; recording without a lesson", slog.Any("error", err))
		return ""
	}
	if l := ongoing.Current(lessons, now); l != nil {
		return l.ID()
	}
	return ""
}

// endOffer ends an offered prompt as started, because a recording started without it, and hides the popup.
func (s *Service) endOffer() {
	s.mu.Lock()
	p := s.current
	if p == nil || p.State() != prompt.StateOffered {
		s.mu.Unlock()
		return
	}
	err := p.BeginStart()
	if err == nil {
		err = p.Started()
	}
	s.current = nil
	s.mu.Unlock()
	if err != nil { // unreachable: OFFERED allows both
		s.log.Warn("recording prompt did not end as started", slog.String("prompt", p.ID().String()), slog.Any("error", err))
	} else {
		s.log.Info("recording prompt ended by a start from the main window", slog.String("prompt", p.ID().String()))
	}
	s.renderLogged(opStartManual)
}

// SelectSource remembers source as the one recordings and the sound check use (TASK-081): the picker of the
// diagnostics page, the same remembered source the popup preselects. Unlike after a start, a failure to save is
// returned: the user asked for exactly this.
func (s *Service) SelectSource(ctx context.Context, source values.Source) error {
	if err := s.rememberSource(ctx, source); err != nil {
		return err
	}
	s.log.Info("recording source selected", slog.String("source", source.String()))
	return nil
}

// remember stores source as the one to preselect next time. Failures are logged: the recording runs either way.
func (s *Service) remember(ctx context.Context, source values.Source) {
	if err := s.rememberSource(ctx, source); err != nil {
		s.log.Warn("the recording source was not remembered", slog.String("source", source.String()), slog.Any("error", err))
	}
}

// rememberSource saves source in the settings; a version conflict reloads once.
func (s *Service) rememberSource(ctx context.Context, source values.Source) error {
	var err error
	for range rememberAttempts {
		if err = s.saveSource(ctx, source); !errors.Is(err, shared.ErrVersionConflict) {
			break
		}
	}
	return err
}

func (s *Service) saveSource(ctx context.Context, source values.Source) error {
	st, err := s.settings.Load(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", opRemember, err)
	}
	if st.RecordingSource() == source.App() {
		return nil
	}
	if err := st.SetRecordingSource(source.App(), s.clock.Now()); err != nil {
		return fmt.Errorf("%s: %w", opRemember, err)
	}
	if err := s.settings.Save(ctx, st); err != nil {
		return fmt.Errorf("%s: %w", opRemember, err)
	}
	return nil
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
	view := viewOf(p)
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

// Tick hides a lesson prompt whose lesson ended.
func (s *Service) Tick(context.Context) error {
	now := s.clock.Now()
	s.mu.Lock()
	p := s.current
	if p == nil || !p.ExpiryDue(now) {
		s.mu.Unlock()
		return nil
	}
	err := p.Expire(now)
	s.current = nil
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w", opTick, err) // unreachable: ExpiryDue allows it
	}

	s.log.Info("recording prompt expired", slog.String("prompt", p.ID().String()))
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
