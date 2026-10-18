package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Outcomes of the power policy, for the log.
const (
	outcomeWaitingPower = "waiting_for_power"
	outcomeParked       = "parked"
	outcomeReleased     = "released"
	outcomeRunNow       = "run_now"
)

// ForceRun is the "Run now" action of the user (PRD section 7.4, AC-22): a WAITING_FOR_POWER job goes back to QUEUED and
// a QUEUED job keeps its place, both with the override, so the next claim starts it whatever the power source. The
// override covers one start: the claim clears it, and the started attempt is not stopped when power is lost. Other
// statuses are refused with ErrCannotRunNow.
func (s *Service) ForceRun(ctx context.Context, id shared.JobID) error {
	var err error
	for range changeAttempts {
		var j *job.Job
		if j, err = s.jobs.Get(ctx, id); err != nil {
			return err
		}
		if err = j.ForceRun(s.clock.Now()); err != nil {
			return fmt.Errorf("%w: %w", ErrCannotRunNow, err)
		}
		if err = s.jobs.Save(ctx, j); !errors.Is(err, shared.ErrVersionConflict) {
			if err == nil {
				s.log.Info("job will run now", slog.String("job", id.String()), slog.String("outcome", outcomeRunNow))
			}
			return err
		}
	}
	return err
}

// conditions returns the last known power source and setting.
func (s *Service) conditions() powerpolicy.Conditions {
	s.condMu.Lock()
	defer s.condMu.Unlock()
	return s.cond
}

func (s *Service) setSource(src powerpolicy.Source) {
	s.condMu.Lock()
	defer s.condMu.Unlock()
	s.cond.Source = src
}

// readSettings re-reads the setting, so a change by the user applies at the next tick; a failed read keeps the last value.
func (s *Service) readSettings(ctx context.Context) {
	if s.settings == nil {
		return
	}
	st, err := s.settings.Load(ctx)

	s.condMu.Lock()
	defer s.condMu.Unlock()
	if err != nil {
		if !s.settingsFailing && ctx.Err() == nil {
			s.log.Warn("reading the settings failed, keeping the last power setting", slog.Any("error", err))
		}
		s.settingsFailing = true
		return
	}
	s.settingsFailing = false
	s.cond.OnlyOnACPower = st.ProcessOnlyOnACPower()
}

// pollPower asks the monitor directly; a failed read keeps the last known source (unknown at first, which counts as AC).
func (s *Service) pollPower(ctx context.Context) {
	if s.power == nil {
		return
	}
	onAC, err := s.power.IsOnACPower(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("reading the power source failed, keeping the last known one", slog.Any("error", err))
		}
		return
	}
	s.setSource(powerpolicy.SourceOf(onAC))
}

// initPower reads the setting and the power source before the workers start, then subscribes to power changes.
// It returns the event channel, nil when there is no subscription (the keeper then polls the monitor).
func (s *Service) initPower(ctx context.Context) <-chan bool {
	s.condMu.Lock()
	s.cond = powerpolicy.Conditions{OnlyOnACPower: settings.DefaultProcessOnlyOnACPower, Source: powerpolicy.SourceUnknown}
	s.condMu.Unlock()

	s.readSettings(ctx)
	if s.power == nil {
		return nil
	}
	s.pollPower(ctx)

	events, err := s.power.Subscribe(s.loopCtx)
	if err != nil {
		s.log.Warn("subscribing to power changes failed, polling the power source", slog.Any("error", err))
		return nil
	}
	s.subscribed.Store(true)
	return events
}

// watchPower applies every power change at once: parks or releases jobs; running heavy jobs see it at their next report.
func (s *Service) watchPower(events <-chan bool) {
	defer s.wg.Done()
	for {
		select {
		case <-s.loopCtx.Done():
			return
		case onAC, ok := <-events:
			if !ok {
				if s.loopCtx.Err() == nil {
					s.subscribed.Store(false)
					s.log.Warn("power change events ended, polling the power source")
				}
				return
			}
			s.setSource(powerpolicy.SourceOf(onAC))
			s.enforcePower(s.loopCtx)
		}
	}
}

// enforcePower refreshes the conditions and moves jobs between QUEUED and WAITING_FOR_POWER as the policy decides.
func (s *Service) enforcePower(ctx context.Context) {
	s.enforceMu.Lock()
	defer s.enforceMu.Unlock()

	s.readSettings(ctx)
	if !s.subscribed.Load() {
		s.pollPower(ctx)
	}
	if err := s.parkAndRelease(ctx, s.conditions()); err != nil && ctx.Err() == nil {
		s.log.Error("applying the power policy failed", slog.Any("error", err))
	}
}

func (s *Service) parkAndRelease(ctx context.Context, cond powerpolicy.Conditions) error {
	statuses := []job.Status{job.StatusWaitingForPower}
	if cond.Restricted() {
		statuses = append(statuses, job.StatusQueued) // queued jobs only need a look while heavy work must wait
	}
	jobs, err := s.jobs.ListByStatus(ctx, statuses...)
	if err != nil {
		return err
	}

	now := s.clock.Now()
	policy := s.classes.Load().policy
	for _, j := range jobs {
		var outcome string
		switch {
		case policy.Park(j, cond, now):
			outcome, err = outcomeParked, j.WaitFor(job.StatusWaitingForPower, now)
		case policy.Release(j, cond):
			outcome, err = outcomeReleased, j.Requeue(now)
		default:
			continue
		}
		if err != nil {
			s.log.Error("power policy transition refused", slog.String("job", j.ID().String()), slog.Any("error", err))
			continue
		}

		// A conflict means a worker, the user or another tick changed the job meanwhile; the next tick looks again.
		if err := s.jobs.Save(ctx, j); err != nil {
			if errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound) {
				continue
			}
			return err
		}
		s.log.Info("power policy moved a job", slog.String("job", j.ID().String()), slog.String("type", string(j.Type())),
			slog.String("outcome", outcome), slog.String("status", j.Status().String()))
	}
	return nil
}

// mustWaitForPower reports whether the running attempt of r must stop at this checkpoint.
func (s *Service) mustWaitForPower(r *run) bool {
	return s.classes.Load().policy.Decide(r.typ, s.conditions(), r.forced) == powerpolicy.Wait
}
