package hubclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// A streamed step that lost its result or was cut by the watchdog after the Hub's first message (any message) reached
// the Hub: it matches agent.ErrHubFailure (stops failed), still transient for the automatic retry. Before the first
// message it does not (stops offline). A clean result is unaffected (ADR-0026 addendum 2026-11-08, TASK-398).
func TestStepStreamHubFailureAfterTheFirstMessage(t *testing.T) {
	silentAfter := func(msgs ...*hubv1.AgentStepStreamResponse) func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error {
		return func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
			for _, m := range msgs {
				if err := send(m); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return ctx.Err()
		}
	}
	never := func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
		for !errors.Is(ctx.Err(), context.Canceled) {
			if err := send(stepHeartbeat()); err != nil {
				return err
			}
			time.Sleep(10 * time.Millisecond)
		}
		return ctx.Err()
	}
	short := Config{RequestTimeout: 400 * time.Millisecond, StepIdleTimeout: 150 * time.Millisecond, StepTimeout: 5 * time.Second}
	tests := []struct {
		name   string
		serve  func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error
		cfg    Config
		ok     bool
		failed bool  // matches agent.ErrHubFailure
		want   error // the reason or watchdog cause it must still match
	}{
		{name: "delta then no result", serve: serveAll(nil, stepDelta("всё")), failed: true, want: ErrUnavailable},
		{name: "heartbeat then no result", serve: serveAll(nil, stepHeartbeat()), failed: true, want: ErrUnavailable},
		{name: "unknown event then no result", serve: serveAll(nil, &hubv1.AgentStepStreamResponse{}), failed: true, want: ErrUnavailable},
		{name: "empty stream", serve: serveAll(nil), want: ErrUnavailable},
		{name: "delta then silent", serve: silentAfter(stepDelta("ч")), cfg: short, failed: true, want: errStepSilent},
		{name: "heartbeat then silent", serve: silentAfter(stepHeartbeat()), cfg: short, failed: true, want: errStepSilent},
		{name: "silent from the start", serve: silentAfter(), cfg: short, want: errStepSilent},
		{
			name: "over the cap after messages", serve: never, failed: true, want: errStepTooLong,
			cfg: Config{RequestTimeout: 400 * time.Millisecond, StepIdleTimeout: 400 * time.Millisecond, StepTimeout: 600 * time.Millisecond},
		},
		{name: "clean result", serve: serveAll(nil, stepDelta("ок"), stepResult(okResponse())), ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStreamHub{serve: tt.serve}
			var c *Client
			if tt.cfg == (Config{}) {
				c = startGateway(t, f)
			} else {
				c = startStepGateway(t, f, tt.cfg)
			}
			_, err := c.Step(t.Context(), stepRequest(t), nil)
			if tt.ok {
				if err != nil {
					t.Fatalf("Step: %v", err)
				}
				return
			}
			if got := errors.Is(err, agent.ErrHubFailure); got != tt.failed {
				t.Errorf("error %v: ErrHubFailure %v, want %v", err, got, tt.failed)
			}
			for _, w := range []error{tt.want, shared.ErrServiceUnavailable, backoff.ErrTransient} {
				if !errors.Is(err, w) {
					t.Errorf("error %v does not match %v", err, w)
				}
			}
			if errors.Is(tt.want, errStepSilent) || errors.Is(tt.want, errStepTooLong) {
				if !errors.Is(err, ErrTimeout) {
					t.Errorf("error %v is no timeout", err)
				}
			}
			if backoff.Classify(err) != backoff.ClassTransient {
				t.Errorf("error %v is not transient", err)
			}
			if len(f.seen()) != 0 {
				t.Error("a failed stream fell back to the unary call")
			}
		})
	}
}

// A bare unavailable before any message keeps its class and is no Hub failure.
func TestStepStreamNoHubFailureWithoutAMessage(t *testing.T) {
	c := startGateway(t, &fakeStreamHub{serve: serveAll(connect.NewError(connect.CodeUnavailable, errors.New("down")))})
	_, err := c.Step(t.Context(), stepRequest(t), nil)
	if err == nil || errors.Is(err, agent.ErrHubFailure) || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("Step = %v, want a transient error without ErrHubFailure", err)
	}
}
