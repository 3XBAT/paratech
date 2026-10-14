package testenv

import (
	"context"
	"errors"
	"sync"

	"paratech/desktop/internal/domain/coursework/proposal"
)

// ErrZeroCommand is returned by the side-effect fakes for a zero command, which a real adapter must refuse too.
var ErrZeroCommand = errors.New("zero command")

// callLog records every call of a side-effect fake and can fail the next calls.
type callLog[C any] struct {
	mu    sync.Mutex
	calls []C
	err   error
}

func (l *callLog[C]) call(ctx context.Context, cmd C, zero bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if zero {
		return ErrZeroCommand
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, cmd)
	return l.err
}

func (l *callLog[C]) snapshot() []C {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]C(nil), l.calls...)
}

func (l *callLog[C]) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

// FakeMailer stands for the homework email adapter (SMTP). It records every SendHomework call, including failed ones,
// so a test can assert that model output never reached it.
type FakeMailer struct {
	log callLog[proposal.EmailCommand]
}

// SendHomework records the command.
func (f *FakeMailer) SendHomework(ctx context.Context, cmd proposal.EmailCommand) error {
	return f.log.call(ctx, cmd, cmd.IsZero())
}

// Calls returns the recorded commands in call order.
func (f *FakeMailer) Calls() []proposal.EmailCommand { return f.log.snapshot() }

// Fail makes every later call return err after recording it; nil heals the fake.
func (f *FakeMailer) Fail(err error) { f.log.fail(err) }

// FakeExporter stands for the homework export over the Storage Manager. It records every ExportHomework call.
type FakeExporter struct {
	log callLog[proposal.ExportCommand]
}

// ExportHomework records the command.
func (f *FakeExporter) ExportHomework(ctx context.Context, cmd proposal.ExportCommand) error {
	return f.log.call(ctx, cmd, cmd.IsZero())
}

// Calls returns the recorded commands in call order.
func (f *FakeExporter) Calls() []proposal.ExportCommand { return f.log.snapshot() }

// Fail makes every later call return err after recording it; nil heals the fake.
func (f *FakeExporter) Fail(err error) { f.log.fail(err) }

// FakeCalendar stands for the Google Calendar adapter. It records every UpsertDeadline call.
type FakeCalendar struct {
	log callLog[proposal.CalendarCommand]
}

// UpsertDeadline records the command.
func (f *FakeCalendar) UpsertDeadline(ctx context.Context, cmd proposal.CalendarCommand) error {
	return f.log.call(ctx, cmd, cmd.IsZero())
}

// Calls returns the recorded commands in call order.
func (f *FakeCalendar) Calls() []proposal.CalendarCommand { return f.log.snapshot() }

// Fail makes every later call return err after recording it; nil heals the fake.
func (f *FakeCalendar) Fail(err error) { f.log.fail(err) }
