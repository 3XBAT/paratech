package hubclient

import (
	"context"
	"errors"
	"time"
)

// Causes of a streamed read cut by the client (an agent step or an answer); each is also the detail of its ErrTimeout,
// so desktop.log tells them apart.
var (
	errStepSilent  = errors.New("no message from the hub in time")
	errStepTooLong = errors.New("the stream ran over its time cap")
)

// watchStream derives the context of one streamed read: cut with errStepTooLong after total, and with errStepSilent
// when no message comes for first before the first one or for idle after one. The reader calls seen on every message
// it receives, whatever its event (a heartbeat, an unknown member), from one goroutine; release ends the watch.
func watchStream(ctx context.Context, first, idle, total time.Duration) (callCtx context.Context, seen, release func()) {
	capCtx, cancelCap := context.WithTimeoutCause(ctx, total, errStepTooLong)
	callCtx, stop := context.WithCancelCause(capCtx)
	watch := time.AfterFunc(min(first, total), func() { stop(errStepSilent) })

	seen = func() {
		if watch.Stop() { // false: it fired, the call is cut already
			watch.Reset(idle)
		}
	}
	release = func() {
		watch.Stop()
		stop(nil)
		cancelCap()
	}
	return callCtx, seen, release
}

// watchTimeout is the ErrTimeout of op when watchStream cut callCtx, else nil.
func watchTimeout(callCtx context.Context, op string) error {
	cause := context.Cause(callCtx)
	if !errors.Is(cause, errStepSilent) && !errors.Is(cause, errStepTooLong) {
		return nil
	}
	e := unavailable(op, ErrTimeout)
	e.detail = cause
	return e
}
