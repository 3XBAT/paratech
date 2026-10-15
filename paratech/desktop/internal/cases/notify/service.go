package notify

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	"paratech/desktop/internal/domain/jobs/services/notifypolicy"
)

// Operations named in errors.
const (
	opSettings = "notify: read settings"
	opNotice   = "notify: new notice"
	opRecord   = "notify: record notice"
	opNotify   = "notify: show notification"
)

// Service notifies the user that processing finished (PRD section 15).
type Service struct {
	settings SettingsReader
	notices  NoticeRepository
	notifier Notifier
	clock    Clock
	log      *slog.Logger
	policy   notifypolicy.Policy
}

// New validates the configuration and builds the Service.
func New(deps Deps, cfg Config) (*Service, error) {
	if deps.Settings == nil || deps.Notices == nil || deps.Notifier == nil || deps.Clock == nil {
		return nil, fmt.Errorf("%w: settings, notices, notifier and clock are required", ErrInvalidConfig)
	}
	policy, err := notifypolicy.New(cfg.Types)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		settings: deps.Settings, notices: deps.Notices, notifier: deps.Notifier, clock: deps.Clock, log: log, policy: policy,
	}, nil
}

// JobFinished is told that a job reached snap.Status(). It notifies the user once for that job and status when the
// policy says so; repeated calls, also after a restart, do nothing. The notice is recorded before the notifier is
// called, so a notifier error loses this notification instead of repeating it later; the error is returned.
//
// While the notifier is not ready (for example Recover at startup, before the shell runs) the event is skipped with
// nil and nothing is recorded, so a later event for the same job and status still notifies.
func (s *Service) JobFinished(ctx context.Context, snap job.Snapshot) error {
	// Cancels and other job types are the common case: decide them without a settings read.
	if !s.policy.Concerns(snap.Type(), snap.Status()) {
		return nil
	}
	st, err := s.settings.Load(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", opSettings, err)
	}
	if !s.policy.Notifies(st.NotifyOnProcessingComplete(), snap.Type(), snap.Status()) {
		return nil
	}

	// The shell can still stop between Ready and Notify; that loses this one notification, which is accepted.
	if !s.notifier.Ready() {
		s.log.Info("notifier not ready, notification skipped", slog.String("job", snap.ID().String()),
			slog.String("status", snap.Status().String()))
		return nil
	}

	n, err := notice.New(snap.ID(), snap.Type(), snap.Status(), s.clock.Now())
	if err != nil {
		return fmt.Errorf("%s: %w", opNotice, err)
	}
	first, err := s.notices.Record(ctx, n)
	if err != nil {
		return fmt.Errorf("%s: %w", opRecord, err)
	}
	if !first {
		s.log.Debug("job state already notified", slog.String("job", n.Job().String()), slog.String("status", n.Status().String()))
		return nil
	}

	if err := s.notifier.Notify(ctx, n); err != nil {
		return fmt.Errorf("%s: %w", opNotify, err)
	}
	s.log.Info("user notified", slog.String("job", n.Job().String()), slog.String("type", string(n.Type())),
		slog.String("status", n.Status().String()))
	return nil
}
