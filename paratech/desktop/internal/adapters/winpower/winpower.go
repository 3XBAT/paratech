// Package winpower reports whether the computer runs on AC power (PRD section 7.4). It satisfies the PowerMonitor
// ports of jobrunner and transcribe structurally.
package winpower

import (
	"context"
	"errors"
	"time"
)

// ErrQuery means the power status could not be read from the OS.
var ErrQuery = errors.New("winpower: cannot query power status")

// DefaultInterval is how often the power source is polled.
const DefaultInterval = 5 * time.Second

// Monitor polls the OS power status. Create it with New.
type Monitor struct {
	read     func() (bool, error)
	interval time.Duration
}

// Option configures a Monitor.
type Option func(*Monitor)

// WithInterval sets the polling interval; non-positive values are ignored.
func WithInterval(d time.Duration) Option {
	return func(m *Monitor) {
		if d > 0 {
			m.interval = d
		}
	}
}

// New returns a Monitor backed by the OS (always on AC power on non-Windows systems).
func New(opts ...Option) *Monitor {
	return newMonitor(readACPower, opts...)
}

func newMonitor(read func() (bool, error), opts ...Option) *Monitor {
	m := &Monitor{read: read, interval: DefaultInterval}
	for _, o := range opts {
		o(m)
	}
	return m
}

// IsOnACPower reports whether the computer runs on AC power; an unknown source counts as AC.
func (m *Monitor) IsOnACPower(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	v, err := m.read()
	if err != nil {
		return false, err
	}
	return v, nil
}

// Subscribe sends the current state first, then every change. The channel holds one value, so a slow reader
// sees only the latest state and the sender never blocks. It is closed when ctx is done.
func (m *Monitor) Subscribe(ctx context.Context) (<-chan bool, error) {
	cur, err := m.IsOnACPower(ctx)
	if err != nil {
		return nil, err
	}
	ch := make(chan bool, 1)
	ch <- cur
	go func() {
		defer close(ch)
		t := time.NewTicker(m.interval)
		defer t.Stop()
		last := cur
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			v, err := m.read()
			if err != nil || v == last { // keep the last known state on a failed read
				continue
			}
			last = v
			publish(ch, v)
		}
	}()
	return ch, nil
}

// publish replaces an unread value; only the polling goroutine sends, so the final send cannot block.
func publish(ch chan bool, v bool) {
	select {
	case ch <- v:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- v
	}
}

// acOffline is SYSTEM_POWER_STATUS.ACLineStatus for battery; 1 is AC, 255 is unknown.
const acOffline byte = 0

// onAC maps ACLineStatus to the port semantics: only an explicit "offline" means battery.
func onAC(acLineStatus byte) bool {
	return acLineStatus != acOffline
}
