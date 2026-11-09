package connect

import "time"

// WithHeartbeatTicks makes every heartbeat-enabled stream take its silences from c instead of a timer, so tests need
// no wall-clock waits.
func WithHeartbeatTicks(c <-chan time.Time) GatewayOption {
	return func(g *Gateway) { g.ticks = c }
}
