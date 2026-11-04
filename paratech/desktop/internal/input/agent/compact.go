package agent

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"

	"paratech/desktop/internal/cases/agentcompact"
	"paratech/desktop/internal/domain/shared"
)

// opCompact names a failed compaction in the log.
const opCompact = "agent: context compaction failed"

// Compactor is the context compaction use case (cases/agentcompact) as the Driver uses it: started is called right
// before the Hub call, never when no compaction is needed or it fails before.
type Compactor interface {
	Compact(ctx context.Context, id shared.ThreadID, started func()) (agentcompact.Result, error)
}

// errCompactPanic is the error of a compaction that panicked.
var errCompactPanic = errors.New("agent: compaction panicked")

// compactCode names an error of agentcompact for the log: its error text may wrap a cause that holds a text.
func compactCode(err error) string {
	switch {
	case errors.Is(err, errCompactPanic):
		return "panic"
	case errors.Is(err, agentcompact.ErrInvalidConfig):
		return "invalid_config"
	case errors.Is(err, agentcompact.ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, agentcompact.ErrBusy):
		return "busy"
	case errors.Is(err, agentcompact.ErrChanged):
		return "changed"
	case errors.Is(err, agentcompact.ErrQuota):
		return "quota"
	case errors.Is(err, agentcompact.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, agentcompact.ErrRefused):
		return "refused"
	case errors.Is(err, agentcompact.ErrStore):
		return "store"
	}
	return "unknown"
}

// compactBefore compacts chat id before an Ask asks it (ADR-0028 «Context compaction»). Once the Hub is called the
// panel gets an update that a compaction runs, and one that it ended, with the chat's new fill when it stored a
// summary; the end is published whatever happens, a panic too. A failure never stops the request: it is logged by its
// code only and the Ask goes on with the history as it is. An Ask without a chat id targets the current chat, which
// the Driver cannot name before the step use case loads it: no compaction then. A chat whose request runs is refused
// by the use case before any Hub call (busy), so it gets no update.
func (d *Driver) compactBefore(ctx context.Context, id shared.ThreadID) {
	if d.compact == nil || id == "" {
		return
	}
	var (
		once    sync.Once
		started bool
		res     agentcompact.Result
		err     error
	)
	defer func() {
		if r := recover(); r != nil {
			err = errCompactPanic
			d.log.ErrorContext(ctx, opCompact, "thread", id.String(), "code", compactCode(err), "stack", string(debug.Stack()))
		} else if err != nil {
			d.log.WarnContext(ctx, opCompact, "thread", id.String(), "code", compactCode(err))
		}
		if !started {
			return
		}
		end := Update{Thread: id, Compaction: true}
		if err == nil {
			fill := res.Fill
			end.Fill = &fill
		}
		d.sink.Publish(end)
	}()
	res, err = d.compact.Compact(ctx, id, func() {
		once.Do(func() {
			started = true
			d.sink.Publish(Update{Thread: id, Compaction: true, Compacting: true})
		})
	})
}
