// Package quota is the use case of the quota ledger (PRD section 9.2, AC-11, AC-12): Reserve before a provider call,
// Settle against the actual usage, Release on cancel or failure, and GetRemaining for the client. The rules live in
// the account aggregate; every method is one transaction of one account (ARCH-23). Reserve, Settle and Release are
// Hub-internal (ADR-0005(f)); only GetRemaining is exposed to clients.
package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in error messages.
const (
	opReserve      = "reserve"
	opSettle       = "settle"
	opRelease      = "release"
	opGetRemaining = "get remaining"
)

// Service runs the quota commands.
type Service struct {
	accounts Accounts
	clock    Clock
	random   Random
	policy   account.Policy
}

// New builds the Service with the quota configuration.
func New(accounts Accounts, clock Clock, random Random, cfg Config) (*Service, error) {
	if accounts == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	p, err := account.NewPolicy(cfg.Unit, cfg.Limit, cfg.ReservationTTL, cfg.KeyRetention)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return &Service{accounts: accounts, clock: clock, random: random, policy: p}, nil
}

// Reserve holds cmd.Amount for cmd.IdempotencyKey, or returns the reservation the key already has (Result.Replayed).
// A shortage is an *ExceededError (account.ErrQuotaExceeded) and stores nothing; a key reused with other arguments
// is account.ErrIdempotencyConflict; malformed arguments are account.ErrInvalidArgument.
func (s *Service) Reserve(ctx context.Context, cmd ReserveCommand) (Result, error) {
	if err := validOwner(cmd.Owner); err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opReserve, err)
	}
	req, err := account.NewReserveRequest(cmd.IdempotencyKey, cmd.Operation, cmd.ModelAlias, cmd.Unit, cmd.Amount)
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opReserve, err)
	}
	id, err := s.newID()
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opReserve, err)
	}

	now := s.clock.Now()
	var res Result
	err = s.accounts.Modify(ctx, s.scope(cmd.Owner, now, req.Key(), ""), func(a *account.Account) error {
		rec, replayed, err := a.Reserve(req, id, now)
		if errors.Is(err, account.ErrQuotaExceeded) {
			return &ExceededError{Quota: snapshotOf(a.Snapshot(now))}
		}
		if err != nil {
			return err
		}
		res = Result{Reservation: reservationOf(rec, s.policy.Unit()), Quota: snapshotOf(a.Snapshot(now)), Replayed: replayed}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opReserve, err)
	}
	return res, nil
}

// Settle charges the actual usage of a reservation of cmd.Owner. Errors: account.ErrReservationNotFound (unknown or
// another tester's id), account.ErrReleased, account.ErrIdempotencyConflict, account.ErrInvalidArgument.
func (s *Service) Settle(ctx context.Context, cmd SettleCommand) (Result, error) {
	id, err := parseReservation(cmd.Owner, cmd.ReservationID)
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opSettle, err)
	}
	u, err := account.NewUsage(cmd.Unit, cmd.Actual, cmd.Partial)
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opSettle, err)
	}
	return s.modifyReservation(ctx, opSettle, cmd.Owner, id, func(a *account.Account, now time.Time) (account.ReservationRecord, error) {
		return a.Settle(id, u, now)
	})
}

// Release returns a reservation of cmd.Owner whole. Errors: account.ErrReservationNotFound, account.ErrInvalidArgument.
func (s *Service) Release(ctx context.Context, cmd ReleaseCommand) (Result, error) {
	id, err := parseReservation(cmd.Owner, cmd.ReservationID)
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", opRelease, err)
	}
	return s.modifyReservation(ctx, opRelease, cmd.Owner, id, func(a *account.Account, now time.Time) (account.ReservationRecord, error) {
		return a.Release(id, account.ReasonExplicit, now)
	})
}

// GetRemaining returns the quota of owner. It writes nothing; overdue holds count as released.
func (s *Service) GetRemaining(ctx context.Context, owner shared.APIKeyID) (Snapshot, error) {
	if err := validOwner(owner); err != nil {
		return Snapshot{}, fmt.Errorf("quota: %s: %w", opGetRemaining, err)
	}
	now := s.clock.Now()
	a, err := s.accounts.Read(ctx, s.scope(owner, now, "", ""))
	if err != nil {
		return Snapshot{}, fmt.Errorf("quota: %s: %w", opGetRemaining, err)
	}
	return snapshotOf(a.Snapshot(now)), nil
}

// modifyReservation runs one command addressed to reservation id in one transaction.
func (s *Service) modifyReservation(ctx context.Context, op string, owner shared.APIKeyID, id shared.ReservationID,
	command func(*account.Account, time.Time) (account.ReservationRecord, error),
) (Result, error) {
	now := s.clock.Now()
	var res Result
	err := s.accounts.Modify(ctx, s.scope(owner, now, "", id), func(a *account.Account) error {
		rec, err := command(a, now)
		if err != nil {
			return err
		}
		res = Result{Reservation: reservationOf(rec, s.policy.Unit()), Quota: snapshotOf(a.Snapshot(now))}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("quota: %s: %w", op, err)
	}
	return res, nil
}

func (s *Service) scope(owner shared.APIKeyID, now time.Time, key account.IdempotencyKey, id shared.ReservationID) account.Scope {
	return account.Scope{Owner: owner, Policy: s.policy, Now: now, Key: key, Reservation: id}
}

func (s *Service) newID() (shared.ReservationID, error) {
	b := make([]byte, account.ReservationIDBytes)
	if err := s.random.Fill(b); err != nil {
		return "", fmt.Errorf("random reservation id: %w", err)
	}
	return account.NewReservationID(b)
}

func validOwner(owner shared.APIKeyID) error {
	if err := owner.Validate(); err != nil {
		return fmt.Errorf("%w: %w", account.ErrInvalidArgument, err)
	}
	return nil
}

func parseReservation(owner shared.APIKeyID, raw string) (shared.ReservationID, error) {
	if err := validOwner(owner); err != nil {
		return "", err
	}
	id := shared.ReservationID(raw)
	if err := id.Validate(); err != nil {
		return "", fmt.Errorf("%w: %w", account.ErrInvalidArgument, err)
	}
	return id, nil
}
