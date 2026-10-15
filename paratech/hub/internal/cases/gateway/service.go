package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in errors and logs.
const (
	opComplete = "complete"
	opStart    = "start"
	opSettle   = "settle"
	opRelease  = "release"
	opPurge    = "purge results"
)

// Outcomes in the log line of a call.
const (
	outcomeCompleted   = "completed"
	outcomeReplayed    = "replayed"
	outcomeInvalid     = "invalid_request"
	outcomeRateLimited = "rate_limited"
	outcomeQuota       = "quota_exceeded"
	outcomeConflict    = "idempotency_conflict"
	outcomeRunning     = "attempt_running"
	outcomeProvider    = "provider_error"
	outcomeCanceled    = "canceled"
	outcomeFailed      = "failed"
)

// ledgerTimeout bounds each Start, Settle or Release, which run on a context detached from the client. It matches the
// bound of one quota transaction in the postgres adapter.
const ledgerTimeout = 5 * time.Second

// Service runs the gateway scenario (gateway.proto, PRD section 9.2): rate limit, validate, reserve, call the provider
// on a context detached from the client, settle and store the result, or release.
type Service struct {
	accounts    Accounts
	results     Results
	provider    Provider
	limiter     RateLimiter
	clock       Clock
	random      Random
	models      *modelallowlist.List
	log         *slog.Logger
	policy      account.Policy
	callTimeout time.Duration
}

// New checks the dependencies and the configuration. The provider call plus the start transaction and the settlement
// must fit in the reservation TTL, so a slow but healthy call is settled before its hold expires.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Accounts == nil || d.Results == nil || d.Provider == nil || d.Limiter == nil || d.Clock == nil || d.Random == nil ||
		d.Models == nil || d.Log == nil {
		return nil, fmt.Errorf("%w: every dependency is required", ErrInvalidConfig)
	}
	p, err := account.NewPolicy(cfg.Unit, cfg.Limit, cfg.ReservationTTL, cfg.KeyRetention)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if cfg.CallTimeout <= 0 || cfg.CallTimeout+2*ledgerTimeout > cfg.ReservationTTL {
		return nil, fmt.Errorf("%w: the call timeout plus %s must be above 0 and within the reservation TTL", ErrInvalidConfig, 2*ledgerTimeout)
	}
	return &Service{
		accounts: d.Accounts, results: d.Results, provider: d.Provider, limiter: d.Limiter, clock: d.Clock, random: d.Random,
		models: d.Models, log: d.Log, policy: p, callTimeout: cfg.CallTimeout,
	}, nil
}

// call is a validated Complete command.
type call struct {
	owner   shared.APIKeyID
	spec    values.Spec
	reserve account.ReserveRequest
	request values.Request
}

// Complete runs one completion for cmd.Owner. Errors: *RateLimitedError, ErrInvalidRequest, *QuotaExceededError,
// ErrIdempotencyConflict, ErrAttemptRunning, *ProviderFailedError, the client's context error, anything else is a
// Hub failure. A provider call that has started is finished, settled and stored even if the client goes away.
func (s *Service) Complete(ctx context.Context, cmd Command) (Result, error) {
	// One log line per call: outcome, operation, alias, reservation, usage and duration; never texts or output.
	begin := s.clock.Now()
	logCall := func(outcome string, attrs ...any) {
		attrs = append([]any{"outcome", outcome, "duration", s.clock.Now().Sub(begin)}, attrs...)
		s.log.InfoContext(ctx, "completion", attrs...)
	}

	if err := cmd.Owner.Validate(); err != nil {
		logCall(outcomeInvalid)
		return Result{}, fmt.Errorf("gateway: %s: %w: %w", opComplete, ErrInvalidRequest, err)
	}
	// Before anything else, so a limited call costs no validation of a large body and writes no ledger row.
	if wait, ok := s.limiter.Allow(cmd.Owner, begin); !ok {
		logCall(outcomeRateLimited, "retry_after", wait)
		return Result{}, &RateLimitedError{RetryAfter: wait}
	}
	c, err := s.validate(cmd)
	if err != nil {
		logCall(outcomeInvalid)
		return Result{}, fmt.Errorf("gateway: %s: %w", opComplete, err)
	}
	attrs := []any{"operation", string(c.spec.Operation()), "alias", c.spec.Alias()}

	rec, stored, quota, err := s.start(ctx, c)
	if err != nil {
		var exceeded *QuotaExceededError
		switch {
		case errors.As(err, &exceeded):
			logCall(outcomeQuota, attrs...)
		case errors.Is(err, ErrIdempotencyConflict):
			logCall(outcomeConflict, attrs...)
		case errors.Is(err, ErrAttemptRunning):
			logCall(outcomeRunning, attrs...)
		case ctx.Err() != nil:
			logCall(outcomeCanceled, attrs...)
		default:
			s.log.ErrorContext(ctx, "completion start failed", "err", err)
			logCall(outcomeFailed, attrs...)
		}
		return Result{}, err
	}
	attrs = append(attrs, "reservation", rec.ID.String())
	if stored.Stored {
		logCall(outcomeReplayed, append(attrs, "input_tokens", stored.InputTokens, "output_tokens", stored.OutputTokens)...)
		return Result{
			Output: stored.Output, InputTokens: stored.InputTokens, OutputTokens: stored.OutputTokens, Quota: quota,
			ReservationID: rec.ID.String(), Replayed: true,
		}, nil
	}

	// The client left before the provider was called: nothing was used, the hold goes back whole.
	if err := ctx.Err(); err != nil {
		s.release(ctx, c, rec.ID, account.ReasonExplicit)
		logCall(outcomeCanceled, attrs...)
		return Result{}, fmt.Errorf("gateway: %s: %w", opComplete, err)
	}

	// The call gets what is left of the hold after the settlement's own bound, at most the configured timeout.
	timeout := min(s.callTimeout, rec.ExpiresAt.Sub(s.clock.Now())-ledgerTimeout)
	if timeout <= 0 {
		s.release(ctx, c, rec.ID, account.ReasonFailed)
		logCall(outcomeFailed, attrs...)
		return Result{}, fmt.Errorf("gateway: %s: no time left for the provider call before the reservation expires", opComplete)
	}
	// Detached from the client (unary rule, gateway.proto): a started call finishes and is paid for and stored.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	completion, err := s.provider.Complete(callCtx, c.request)
	cancel()
	if err == nil {
		err = completion.Failure()
	}
	if err != nil {
		// Also after an answer cut off mid-body: the provider may have billed it, the tester is not charged
		// (accepted gap, ADR-0005(e), ADR-0010).
		s.release(ctx, c, rec.ID, account.ReasonFailed)
		failure := providerFailure(err)
		logCall(outcomeProvider, append(attrs, "transient", failure.Transient, "err", err)...)
		return Result{}, failure
	}

	usage := completion.Usage()
	res, quota, err := s.settle(ctx, c, rec.ID, completion)
	if err != nil {
		s.log.ErrorContext(ctx, "completion settlement failed", "reservation", rec.ID.String(), "err", err)
		logCall(outcomeFailed, append(attrs, "input_tokens", usage.Input(), "output_tokens", usage.Output())...)
		return Result{}, err
	}
	logCall(outcomeCompleted, append(attrs, "input_tokens", usage.Input(), "output_tokens", usage.Output(),
		"stop", completion.Stop().String(), "charged", res.Charged)...)
	return Result{
		Output: completion.Output(), InputTokens: usage.Input(), OutputTokens: usage.Output(), Quota: quota,
		ReservationID: rec.ID.String(),
	}, nil
}

// PurgeResults clears the stored results whose result window has ended (PRD section 13.2) and returns how many.
func (s *Service) PurgeResults(ctx context.Context) (int64, error) {
	n, err := s.results.PurgeResults(ctx, s.policy.ResultCutoff(s.clock.Now()))
	if err != nil {
		return 0, fmt.Errorf("gateway: %s: %w", opPurge, err)
	}
	return n, nil
}

// validate builds the domain values of cmd: the spec, the allowlisted model with its effective output cap, the
// reservation of the most the call may cost with the spec's fingerprint, and the provider request.
func (s *Service) validate(cmd Command) (call, error) {
	spec, err := values.NewSpec(cmd.IdempotencyKey, cmd.Operation, cmd.ModelAlias, cmd.Instructions, cmd.UntrustedData, cmd.MaxOutputTokens)
	if err != nil {
		return call{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	model, maxOutput, err := s.models.Resolve(spec.Alias(), spec.MaxOutput())
	if err != nil {
		return call{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	amount, err := requestlimits.Reservation(spec, maxOutput)
	if err != nil {
		return call{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	reserve, err := account.NewReserveRequest(spec.Key(), string(spec.Operation()), spec.Alias(), string(s.policy.Unit()), amount)
	if err == nil {
		reserve, err = reserve.WithRequestHash(spec.Fingerprint())
	}
	if err != nil {
		return call{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	request, err := values.NewRequest(model, spec.Instructions(), spec.Data(), maxOutput)
	if err != nil {
		return call{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return call{owner: cmd.Owner, spec: spec, reserve: reserve, request: request}, nil
}

// start opens a new attempt or finds the stored result of the latest one, in one transaction on a context detached
// from the client with its own bound: a cancel during COMMIT could leave a committed hold that the caller never
// learns about and never releases. Complete checks the client's context right after it.
func (s *Service) start(ctx context.Context, c call) (account.ReservationRecord, account.Result, Snapshot, error) {
	id, err := s.newID()
	if err != nil {
		return account.ReservationRecord{}, account.Result{}, Snapshot{}, fmt.Errorf("gateway: %s: %w", opStart, err)
	}
	now := s.clock.Now()
	var (
		rec    account.ReservationRecord
		stored account.Result
		quota  Snapshot
	)
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	err = s.accounts.Modify(startCtx, s.scope(c, now, ""), func(a *account.Account) error {
		r, res, err := a.Start(c.reserve, id, now)
		if errors.Is(err, account.ErrQuotaExceeded) {
			return &QuotaExceededError{Quota: snapshotOf(a.Snapshot(now))}
		}
		if err != nil {
			return err
		}
		rec, stored, quota = r, res, snapshotOf(a.Snapshot(now))
		return nil
	})
	switch {
	case err == nil:
		return rec, stored, quota, nil
	case errors.Is(err, account.ErrIdempotencyConflict):
		return rec, stored, quota, fmt.Errorf("gateway: %s: %w", opStart, ErrIdempotencyConflict)
	case errors.Is(err, account.ErrAttemptRunning):
		return rec, stored, quota, fmt.Errorf("gateway: %s: %w", opStart, ErrAttemptRunning)
	default:
		return rec, stored, quota, fmt.Errorf("gateway: %s: %w", opStart, err)
	}
}

// settle charges the provider-reported usage in full and stores the answer in the same transaction, on a context
// detached from the client with its own bound.
func (s *Service) settle(ctx context.Context, c call, id shared.ReservationID, completion values.Completion) (account.ReservationRecord, Snapshot, error) {
	usage := completion.Usage()
	u, err := account.NewUsage(string(s.policy.Unit()), usage.Total(), false)
	if err != nil {
		return account.ReservationRecord{}, Snapshot{}, fmt.Errorf("gateway: %s: %w", opSettle, err)
	}
	res, err := account.NewResult(completion.Output(), usage.Input(), usage.Output())
	if err != nil {
		return account.ReservationRecord{}, Snapshot{}, fmt.Errorf("gateway: %s: %w", opSettle, err)
	}

	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	now := s.clock.Now()
	var (
		rec   account.ReservationRecord
		quota Snapshot
	)
	err = s.accounts.Modify(settleCtx, s.scope(c, now, id), func(a *account.Account) error {
		r, err := a.Complete(id, u, res, now)
		if err != nil {
			return err
		}
		rec, quota = r, snapshotOf(a.Snapshot(now))
		return nil
	})
	if err != nil {
		return account.ReservationRecord{}, Snapshot{}, fmt.Errorf("gateway: %s: %w", opSettle, err)
	}
	return rec, quota, nil
}

// release returns the hold whole on a context detached from the client. A failure is only logged: the hold is
// released by expiry at the latest.
func (s *Service) release(ctx context.Context, c call, id shared.ReservationID, reason account.ReleaseReason) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	now := s.clock.Now()
	err := s.accounts.Modify(releaseCtx, s.scope(c, now, id), func(a *account.Account) error {
		_, err := a.Release(id, reason, now)
		return err
	})
	if err != nil {
		s.log.ErrorContext(ctx, "completion release failed", "reservation", id.String(), "op", opRelease, "err", err)
	}
}

func (s *Service) scope(c call, now time.Time, id shared.ReservationID) account.Scope {
	return account.Scope{Owner: c.owner, Policy: s.policy, Now: now, Key: c.reserve.Key(), Reservation: id}
}

func (s *Service) newID() (shared.ReservationID, error) {
	b := make([]byte, account.ReservationIDBytes)
	if err := s.random.Fill(b); err != nil {
		return "", fmt.Errorf("random reservation id: %w", err)
	}
	return account.NewReservationID(b)
}

// providerFailure classifies a provider error; anything the adapter did not classify is permanent (values.ErrorKind).
func providerFailure(err error) *ProviderFailedError {
	var pe *values.ProviderError
	if errors.As(err, &pe) {
		return &ProviderFailedError{Transient: pe.Kind == values.KindTransient, RetryAfter: pe.RetryAfter, Cause: pe}
	}
	return &ProviderFailedError{Cause: errors.New("unclassified provider failure")}
}
