package hubclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/coursework/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// maxQuotaResponseBytes bounds a quota answer: three amounts and a timestamp take a few dozen bytes.
const maxQuotaResponseBytes = 64 << 10

// errNoAmount marks a snapshot field the Hub left out.
var errNoAmount = errors.New("missing amount")

// GetRemaining reads the tester's quota from the Hub (QuotaService.GetRemaining, the public method; AC-12). The URL,
// the key, the timeout, the redirect rule and the error classes are those of Complete; a snapshot that breaks the
// rules of values.QuotaSnapshot is ErrBadResponse. The answer is for display only: the Hub decides at Reserve.
func (c *Client) GetRemaining(ctx context.Context) (values.QuotaSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return values.QuotaSnapshot{}, fmt.Errorf("%s: %w", opRemaining, err)
	}
	base, err := c.baseURL(ctx, opRemaining)
	if err != nil {
		return values.QuotaSnapshot{}, err
	}
	key, err := c.apiKey(ctx, opRemaining)
	if err != nil {
		return values.QuotaSnapshot{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	quota := hubv1connect.NewQuotaServiceClient(c.http, base, connect.WithReadMaxBytes(maxQuotaResponseBytes))
	creq := connect.NewRequest(&hubv1.GetRemainingRequest{})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := quota.GetRemaining(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return values.QuotaSnapshot{}, fmt.Errorf("%s: %w", opRemaining, perr)
		}
		return values.QuotaSnapshot{}, mapCallError(opRemaining, err)
	}
	s, err := snapshotOf(resp.Msg.GetQuota())
	if err != nil {
		return values.QuotaSnapshot{}, permanent(opRemaining, ErrBadResponse)
	}
	return s, nil
}

// snapshotOf converts the contract snapshot; its errors are replaced by ErrBadResponse.
func snapshotOf(q *hubv1.QuotaSnapshot) (values.QuotaSnapshot, error) {
	if q == nil {
		return values.QuotaSnapshot{}, errNoAmount
	}
	remaining, err := amountOf(q.GetRemaining())
	if err != nil {
		return values.QuotaSnapshot{}, err
	}
	reserved, err := amountOf(q.GetReserved())
	if err != nil {
		return values.QuotaSnapshot{}, err
	}
	limit, err := amountOf(q.GetLimit())
	if err != nil {
		return values.QuotaSnapshot{}, err
	}
	var end time.Time
	if ts := q.GetPeriodEndsAt(); ts != nil {
		if err := ts.CheckValid(); err != nil {
			return values.QuotaSnapshot{}, err
		}
		end = ts.AsTime()
	}
	return values.NewQuotaSnapshot(remaining, reserved, limit, end)
}

func amountOf(a *hubv1.QuotaAmount) (values.QuotaAmount, error) {
	if a == nil {
		return values.QuotaAmount{}, errNoAmount
	}
	var unit values.QuotaUnit
	if a.GetUnit() == hubv1.QuotaUnit_QUOTA_UNIT_TOKENS {
		unit = values.QuotaUnitTokens
	}
	return values.NewQuotaAmount(unit, a.GetValue()) // an unknown unit is refused
}
