package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the agent retention, used in its error messages.
const (
	opListEndedRequests = "sqlite: list ended agent requests"
	opReadRequest       = "sqlite: read agent request entries"
	opPurgeDetails      = "sqlite: purge agent request details"
)

// AgentRetention reads and purges the technical details of earlier final agent requests (the agentretention.Requests
// port, ADR-0028 «Retention»): their calls and results; texts, requests, threads and the journal stay. Every write
// re-checks in its statement that the request is final, not current and ended before the cutoff.
type AgentRetention struct{ db *DB }

// NewAgentRetention returns the agent retention store of db.
func NewAgentRetention(db *DB) *AgentRetention { return &AgentRetention{db: db} }

// EndedBefore returns at most limit earlier final requests that ended before cutoff and still have calls or results,
// in (EndedAt, Thread, Number) order after the key of after (the zero value: from the start).
func (r *AgentRetention) EndedBefore(ctx context.Context, cutoff time.Time, after thread.EndedRequest, limit int) ([]thread.EndedRequest, error) {
	afterAt := int64(-1) // before every stored time
	if !after.EndedAt.IsZero() {
		afterAt = jobMs(after.EndedAt)
	}
	rows, err := r.db.Queries(ctx).ListEndedAgentTurns(ctx, sqlcgen.ListEndedAgentTurnsParams{
		Cutoff: jobMs(cutoff), AfterAt: afterAt, AfterThread: string(after.Thread), AfterNumber: int64(after.Number),
		MaxRows: int64(max(limit, 0)),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListEndedRequests, MapError(ctx, err))
	}
	out := make([]thread.EndedRequest, len(rows))
	for i, row := range rows {
		out[i] = thread.EndedRequest{
			Thread: shared.ThreadID(row.ThreadID), Number: int(row.Number), EndedAt: jobFromMs(row.UpdatedAt),
			ReplyFrozen: row.Frozen == 1,
		}
	}
	return out, nil
}

// Entries returns the stored entries of request number of a thread, oldest first, decoded and checked as Since does;
// one that cannot be read is an error.
func (r *AgentRetention) Entries(ctx context.Context, id shared.ThreadID, number int) ([]values.HistoryTurn, error) {
	rows, err := r.db.Queries(ctx).ListAgentTurnSteps(ctx, sqlcgen.ListAgentTurnStepsParams{
		ThreadID: string(id), Turn: int64(number), MaxRows: maxAgentHistoryRows + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s/%d: %w", opReadRequest, id, number, MapError(ctx, err))
	}
	if len(rows) > maxAgentHistoryRows {
		return nil, fmt.Errorf("%s %s/%d: more than %d entries", opReadRequest, id, number, maxAgentHistoryRows)
	}
	out := make([]values.HistoryTurn, len(rows))
	for i, row := range rows {
		if out[i], err = historyTurnFrom(row.Entry, row.Body); err != nil {
			return nil, fmt.Errorf("stored agent request %s/%d: entry %d: %w", id, number, i+1, err)
		}
	}
	return out, nil
}

// PurgeDetails freezes line as the reply line of request number while it has none ("" freezes nothing), then deletes
// its calls and results, in one transaction; both only while the request is final, not current and ended before
// cutoff, and the delete only once it has a reply line. It reports whether anything was deleted.
func (r *AgentRetention) PurgeDetails(ctx context.Context, id shared.ThreadID, number int, line string, cutoff time.Time) (bool, error) {
	if line != "" {
		if err := (values.HistoryTurn{AgentReply: line}).Validate(); err != nil {
			return false, fmt.Errorf("%s %s/%d: %w", opPurgeDetails, id, number, err)
		}
	}
	var n int64
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if line != "" {
			if err := q.FreezeEndedAgentTurnReply(ctx, sqlcgen.FreezeEndedAgentTurnReplyParams{
				ReplyLine: jobNullString(line), ThreadID: string(id), Number: int64(number), Cutoff: jobMs(cutoff),
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		var err error
		n, err = q.DeleteEndedAgentTurnDetails(ctx, sqlcgen.DeleteEndedAgentTurnDetailsParams{
			ThreadID: string(id), Number: int64(number), Cutoff: jobMs(cutoff),
		})
		return MapError(ctx, err)
	})
	if err != nil {
		return false, fmt.Errorf("%s %s/%d: %w", opPurgeDetails, id, number, err)
	}
	return n > 0, nil
}
