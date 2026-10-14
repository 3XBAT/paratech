package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// rangeMargin widens the listed range of starts: the store keeps instants at a coarser precision than the clock, and
// a lesson listed in excess is filtered out by the aggregate anyway.
const rangeMargin = time.Minute

// Operations named in errors.
const (
	opList    = "list lessons"
	opSave    = "save start mark"
	opPublish = "publish start"
)

// Service announces the lessons that are starting (PRD section 6.1).
type Service struct {
	lessons Lessons
	clock   Clock
	sleeper Sleeper
	events  Events
	log     *slog.Logger
	cfg     Config
}

// New validates the configuration and builds the Service. It starts nothing; call Run.
func New(deps Deps, cfg Config) (*Service, error) {
	switch {
	case deps.Lessons == nil || deps.Clock == nil || deps.Sleeper == nil || deps.Events == nil:
		return nil, fmt.Errorf("%w: lessons, clock, sleeper and events are required", ErrInvalidConfig)
	case cfg.PollInterval <= 0 || cfg.PollInterval >= lesson.StartLateGrace:
		// A poll must land inside the late grace, or a lesson with a zero lead could be skipped between two polls.
		// The real gap is PollInterval plus Tick time, and sleep or a clock jump can skip the window entirely; a
		// missed window is not caught up (owner decision), see the package doc.
		return nil, fmt.Errorf("%w: poll interval %v, want more than 0 and less than %v",
			ErrInvalidConfig, cfg.PollInterval, lesson.StartLateGrace)
	}
	if err := lesson.ValidateStartLead(cfg.Lead); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		lessons: deps.Lessons, clock: deps.Clock, sleeper: deps.Sleeper, events: deps.Events, log: log, cfg: cfg,
	}, nil
}

// Run ticks every poll interval until ctx ends and then returns ctx's error. A failed tick is logged and the next
// one tries again. Run starts no goroutines.
func (s *Service) Run(ctx context.Context) error {
	for {
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("lesson start scheduling failed", slog.Any("error", err))
		}
		if err := s.sleeper.Sleep(ctx, s.cfg.PollInterval); err != nil {
			return err
		}
	}
}

// Tick announces every lesson that is due now and returns how many it announced. Each announcement is saved before
// it is published, so a crash or a restart cannot repeat it. A lesson changed meanwhile (version conflict) is
// skipped and re-read on the next tick. Errors of single lessons do not stop the others and are returned joined.
func (s *Service) Tick(ctx context.Context) (int, error) {
	now, lead := s.clock.Now(), s.cfg.Lead

	// Only a lesson that started less than lesson.StartLateGrace ago or starts within the lead can be due.
	from, to := now.Add(-lesson.StartLateGrace-rangeMargin), now.Add(lead+rangeMargin)
	candidates, err := s.lessons.ListStartingBetween(ctx, from, to)
	if err != nil {
		return 0, fmt.Errorf("scheduler: %s: %w", opList, err)
	}

	announced := 0
	var errs []error
	for _, l := range candidates {
		if err := ctx.Err(); err != nil {
			return announced, err
		}
		if !l.StartAnnouncementDue(now, lead) {
			continue
		}
		ok, err := s.announce(ctx, l, now)
		if ok {
			announced++
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("scheduler: lesson %s: %w", l.ID(), err))
		}
	}
	return announced, errors.Join(errs...)
}

// announce marks one due lesson and publishes its event. It reports whether the mark was stored.
func (s *Service) announce(ctx context.Context, l *lesson.Lesson, now time.Time) (bool, error) {
	ev, err := l.AnnounceStart(now, s.cfg.Lead)
	if err != nil {
		return false, err
	}

	if err := s.lessons.Save(ctx, l); err != nil {
		if errors.Is(err, shared.ErrVersionConflict) {
			s.log.Debug("lesson changed while announcing; next tick re-reads it", slog.String("lesson", string(l.ID())))
			return false, nil
		}
		return false, fmt.Errorf("%s: %w", opSave, err)
	}

	if err := s.events.LessonStarting(ctx, ev); err != nil {
		return true, fmt.Errorf("%s: %w", opPublish, err)
	}
	s.log.Info("lesson start announced", slog.String("lesson", string(l.ID())), slog.Time("start", ev.Start))
	return true, nil
}
