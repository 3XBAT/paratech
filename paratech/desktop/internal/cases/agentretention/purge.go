package agentretention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
)

// Operation names of the log and the errors.
const (
	opPurge   = "agentretention: purge request details"
	opSkipped = "agentretention: request details kept"
)

// Default bounds of one run: batches of requests, and at most so many batches; the next run goes on.
const (
	defaultBatchSize  = 50
	defaultMaxBatches = 20
)

// Errors of the use case. A skipped request is logged by its chat, number and one of the codes, never by the cause
// (it may hold a text of the chat).
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("agentretention: invalid configuration")

	// ErrUnreadable means the entries of a request could not be read or decoded.
	ErrUnreadable = errors.New("agentretention: the request entries cannot be read")

	// ErrInvalidLine means the reply line built from the entries is not a valid agent reply.
	ErrInvalidLine = errors.New("agentretention: invalid reply line")

	// ErrStore means the list, the freeze or the delete failed in the local store.
	ErrStore = errors.New("agentretention: the local store failed")
)

// Deps are the ports of the Purger; Log is optional, and BatchSize and MaxBatches default to 50 and 20.
type Deps struct {
	Requests   Requests
	Tx         Tx
	Clock      Clock
	Log        *slog.Logger
	BatchSize  int
	MaxBatches int
}

// Purger removes the details of the requests that ended longer than thread.DetailsRetention ago.
type Purger struct {
	mu sync.Mutex // the startup cleanup and the daily loop may overlap

	requests   Requests
	tx         Tx
	clock      Clock
	log        *slog.Logger
	batchSize  int
	maxBatches int
}

// New builds the Purger.
func New(d Deps) (*Purger, error) {
	if d.Requests == nil || d.Tx == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	p := &Purger{
		requests: d.Requests, tx: d.Tx, clock: d.Clock, log: d.Log, batchSize: d.BatchSize, maxBatches: d.MaxBatches,
	}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	if p.batchSize <= 0 {
		p.batchSize = defaultBatchSize
	}
	if p.maxBatches <= 0 {
		p.maxBatches = defaultMaxBatches
	}
	return p, nil
}

// Purge removes the details of at most BatchSize×MaxBatches due requests and returns how many requests lost them. A
// request that cannot be purged is logged and skipped (the next run tries it again); a failed list or a cancelled
// context ends the run with the error.
func (p *Purger) Purge(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoff := thread.DetailsCutoff(p.clock.Now())
	var after thread.EndedRequest
	removed := 0
	for range p.maxBatches {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		list, err := p.requests.EndedBefore(ctx, cutoff, after, p.batchSize)
		if err != nil {
			return removed, fmt.Errorf("%s: %w: %w", opPurge, ErrStore, err)
		}
		for _, r := range list {
			if err := ctx.Err(); err != nil {
				return removed, err
			}
			after = r // a skipped request does not hold up the run
			done, err := p.purge(ctx, r, cutoff)
			switch {
			case err != nil && ctx.Err() != nil:
				return removed, ctx.Err()
			case err != nil:
				p.log.WarnContext(ctx, opSkipped, "thread", r.Thread.String(), "turn", r.Number, "code", codeOf(err))
			case done:
				removed++
			}
		}
		if len(list) < p.batchSize {
			break
		}
	}
	return removed, nil
}

// purge freezes the reply line of r when it has none and deletes its details, in one transaction.
func (p *Purger) purge(ctx context.Context, r thread.EndedRequest, cutoff time.Time) (bool, error) {
	var done bool
	err := p.tx.Do(ctx, func(ctx context.Context) error {
		line := ""
		if !r.ReplyFrozen {
			entries, err := p.requests.Entries(ctx, r.Thread, r.Number)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrUnreadable, err)
			}
			if line, err = thread.FrozenReply(entries); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidLine, err)
			}
		}
		var err error
		if done, err = p.requests.PurgeDetails(ctx, r.Thread, r.Number, line, cutoff); err != nil {
			return fmt.Errorf("%w: %w", ErrStore, err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return done, nil
}

// codeOf is the code of a skipped request for the log.
func codeOf(err error) string {
	for _, code := range []error{ErrUnreadable, ErrInvalidLine, ErrStore} {
		if errors.Is(err, code) {
			return code.Error()
		}
	}
	return ErrStore.Error() // the transaction itself failed
}
