package testenv

import (
	"context"

	"paratech/desktop/internal/domain/jobs/notice"
)

// FakeNotifier stands for the toast notifier of the Wails shell (notify.Notifier). It records every Notify call,
// including failed ones, and is safe for concurrent use.
type FakeNotifier struct {
	log callLog[*notice.Notice]
}

// Notify records the notice; a nil notice is refused with ErrZeroCommand.
func (f *FakeNotifier) Notify(ctx context.Context, n *notice.Notice) error {
	return f.log.call(ctx, n, n == nil)
}

// Calls returns the recorded notices in call order.
func (f *FakeNotifier) Calls() []*notice.Notice { return f.log.snapshot() }

// Fail makes every later call return err after recording it; nil heals the fake.
func (f *FakeNotifier) Fail(err error) { f.log.fail(err) }
