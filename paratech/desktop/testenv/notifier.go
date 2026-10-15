package testenv

import (
	"context"
	"sync/atomic"

	"paratech/desktop/internal/domain/jobs/notice"
)

// FakeNotifier stands for the toast notifier of the Wails shell (notify.Notifier). It records every Notify call,
// including failed ones, and is safe for concurrent use. It is ready until SetReady(false).
type FakeNotifier struct {
	log      callLog[*notice.Notice]
	notReady atomic.Bool // inverted, so the zero value is ready
}

// Notify records the notice; a nil notice is refused with ErrZeroCommand.
func (f *FakeNotifier) Notify(ctx context.Context, n *notice.Notice) error {
	return f.log.call(ctx, n, n == nil)
}

// Ready reports what SetReady set last; true by default.
func (f *FakeNotifier) Ready() bool { return !f.notReady.Load() }

// SetReady makes the fake ready or not, like a shell that runs or not.
func (f *FakeNotifier) SetReady(ready bool) { f.notReady.Store(!ready) }

// Calls returns the recorded notices in call order.
func (f *FakeNotifier) Calls() []*notice.Notice { return f.log.snapshot() }

// Fail makes every later call return err after recording it; nil heals the fake.
func (f *FakeNotifier) Fail(err error) { f.log.fail(err) }
