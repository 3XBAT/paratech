package agent

import (
	"context"

	"paratech/desktop/internal/domain/agent/values"
	cwvalues "paratech/desktop/internal/domain/coursework/values"
)

// QuotaStatus is «Обновить» of the remaining quota on the connections page (cases/connections RefreshQuota): the
// Hub's answer as the page shows it.
type QuotaStatus interface {
	RefreshQuota(ctx context.Context) (cwvalues.QuotaReading, error)
}

// quotaStatus is quota_status: the remaining share of the quota and the day it renews, nothing else (no key, no amount).
// A Hub without an address or a key is "unavailable", any other failed read "failed": a stale value is never shown as
// the current one.
func (t tools) quotaStatus(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	r, err := t.actions.controls.Quota.RefreshQuota(ctx)
	if err != nil {
		t.log.InfoContext(ctx, opTool, "tool", call.Tool().String(), "err", err)
		return values.RefusalResult(call.ID(), quotaRefusal(r.Outcome())), nil, nil
	}
	snap, ok := r.Snapshot()
	if !ok || r.Stale() {
		return values.RefusalResult(call.ID(), values.RefuseFailed), nil, nil
	}
	pct, ok := remainingPercent(snap)
	if !ok {
		return values.RefusalResult(call.ID(), values.RefuseFailed), nil, nil
	}
	row := values.QuotaRow{RemainingPercent: pct}
	if end, ok := snap.PeriodEnd(); ok {
		row.Resets = dateOf(end, t.clock.Now().Location())
	}
	res, err := values.QuotaResult(call.ID(), row)
	return res, nil, err
}

// quotaRefusal is the refusal of a failed read: the Hub is not set up, or it failed now.
func quotaRefusal(o cwvalues.QuotaOutcome) values.Refusal {
	switch o {
	case cwvalues.QuotaNotConfigured, cwvalues.QuotaNoKey, cwvalues.QuotaKeyRejected:
		return values.RefuseUnavailable
	}
	return values.RefuseFailed
}

// remainingPercent is the remaining share of the limit in whole percent, rounded down; ok is false without a limit,
// which has no honest share.
func remainingPercent(s cwvalues.QuotaSnapshot) (int, bool) {
	limit, left := s.Limit().Value(), s.Remaining().Value()
	if limit <= 0 {
		return 0, false
	}
	if left <= 0 {
		return 0, true
	}
	return int(min(left*100/limit, 100)), true
}
