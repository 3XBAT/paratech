package connect

import "time"

// DefaultHeartbeatInterval is the silence after which a stream that asked for it gets a StreamHeartbeat (ADR-0026
// addendum 2026-11-08, point 6).
const DefaultHeartbeatInterval = 15 * time.Second

// WithHeartbeatInterval sets the silence before a StreamHeartbeat; d <= 0 keeps DefaultHeartbeatInterval.
func WithHeartbeatInterval(d time.Duration) GatewayOption {
	return func(g *Gateway) {
		if d > 0 {
			g.heartbeat = d
		}
	}
}

// heartbeat is the heartbeat of one stream: send writes an empty StreamHeartbeat. A nil send means none was asked for.
type heartbeat struct {
	send  func() error
	every time.Duration
	// ticks, when set, replaces the timer: a receive is one elapsed silence. Tests drive it without wall-clock races;
	// production leaves it nil. With ticks the silence is not restarted by a sent message.
	ticks <-chan time.Time
}

// heartbeatOf is the heartbeat of a stream whose request set the flag to on. The flag is transport only: it never
// reaches the use case, so the fingerprint and the key do not depend on it.
func (g *Gateway) heartbeatOf(on bool, send func() error) heartbeat {
	if !on {
		return heartbeat{}
	}
	return heartbeat{send: send, every: g.heartbeat, ticks: g.ticks}
}
