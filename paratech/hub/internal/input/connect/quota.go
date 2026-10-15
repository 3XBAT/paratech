package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/quota"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const msgQuotaUnavailable = "quota is temporarily unavailable"

// QuotaReader is what the public quota handler needs from the quota use case (ARCH-42).
type QuotaReader interface {
	GetRemaining(ctx context.Context, owner shared.APIKeyID) (quota.Snapshot, error)
}

// Quota serves the public part of QuotaService: GetRemaining. Reserve, Settle and Release are Hub-internal
// (ADR-0005(f)); the embedded handler answers them unimplemented, and the router does not even route them.
type Quota struct {
	hubv1connect.UnimplementedQuotaServiceHandler
	svc QuotaReader
	log *slog.Logger
}

var _ hubv1connect.QuotaServiceHandler = (*Quota)(nil)

// NewQuota builds the handler.
func NewQuota(svc QuotaReader, log *slog.Logger) (*Quota, error) {
	if svc == nil || log == nil {
		return nil, errors.New("connect: quota service and logger are required")
	}
	return &Quota{svc: svc, log: log}, nil
}

// GetRemaining returns the quota of the authenticated caller (AC-12). A storage failure is unavailable without
// detail; the reason goes to the log only.
func (q *Quota) GetRemaining(ctx context.Context, _ *connectrpc.Request[hubv1.GetRemainingRequest]) (*connectrpc.Response[hubv1.GetRemainingResponse], error) {
	owner, ok := APIKeyFrom(ctx)
	if !ok || owner == "" {
		return nil, rejectedError() // the auth middleware is missing; fail closed
	}
	s, err := q.svc.GetRemaining(ctx, owner)
	if err != nil {
		if ctx.Err() != nil {
			return nil, connectrpc.NewError(connectrpc.CodeCanceled, errors.New(msgCanceled))
		}
		q.log.WarnContext(ctx, "quota lookup failed", "err", err)
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, errors.New(msgQuotaUnavailable))
	}
	snapshot, err := snapshotToProto(s)
	if err != nil {
		q.log.ErrorContext(ctx, "quota snapshot cannot be encoded", "err", err)
		return nil, connectrpc.NewError(connectrpc.CodeInternal, errors.New("internal error"))
	}
	return connectrpc.NewResponse(&hubv1.GetRemainingResponse{Quota: snapshot}), nil
}

// snapshotToProto maps a snapshot of the quota use case.
func snapshotToProto(s quota.Snapshot) (*hubv1.QuotaSnapshot, error) {
	return quotaToProto(s.Unit, s.Remaining, s.Reserved, s.Limit)
}

// quotaToProto maps a snapshot of any use case; the quota does not renew in MVP, so period_ends_at stays unset.
func quotaToProto(unitName string, remaining, reserved, limit int64) (*hubv1.QuotaSnapshot, error) {
	unit, err := unitToProto(unitName)
	if err != nil {
		return nil, err
	}
	amount := func(v int64) *hubv1.QuotaAmount { return &hubv1.QuotaAmount{Unit: unit, Value: v} }
	return &hubv1.QuotaSnapshot{Remaining: amount(remaining), Reserved: amount(reserved), Limit: amount(limit)}, nil
}

func unitToProto(u string) (hubv1.QuotaUnit, error) {
	switch u {
	case string(account.UnitTokens):
		return hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, nil
	default:
		return hubv1.QuotaUnit_QUOTA_UNIT_UNSPECIFIED, fmt.Errorf("unknown quota unit %q", u)
	}
}
