package hubclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// startStepGateway is startGateway with the timeouts of cfg.
func startStepGateway(t *testing.T, f hubv1connect.GatewayServiceHandler, cfg Config) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL, cfg.HTTPClient = srv.URL, srv.Client()
	c, err := New(cfg, creds)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// serveSlowly sends n deltas every gap, then the result (or nothing when result is false), then waits for ctx.
func serveSlowly(n int, gap time.Duration, result bool) func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error {
	return func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
		for i := range n {
			if i > 0 {
				if err := sleepCtx(ctx, gap); err != nil {
					return err
				}
			}
			if err := send(stepDelta("ч")); err != nil {
				return err
			}
		}
		if result {
			return send(stepResult(okResponse()))
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

func assertStepTimeout(t *testing.T, err, cause error) {
	t.Helper()
	for _, w := range []error{ErrTimeout, cause, shared.ErrServiceUnavailable} {
		if !errors.Is(err, w) {
			t.Errorf("error %v does not match %v", err, w)
		}
	}
	if backoff.Classify(err) != backoff.ClassTransient {
		t.Errorf("error %v is not transient", err)
	}
}

// A stream that keeps writing has no total cap of RequestTimeout or of the idle window: each message resets the wait.
func TestStepStreamOutlastsTheIdleWindow(t *testing.T) {
	const n = 10
	f := &fakeStreamHub{serve: serveSlowly(n, 40*time.Millisecond, true)}
	c := startStepGateway(t, f, Config{RequestTimeout: 200 * time.Millisecond, StepIdleTimeout: 200 * time.Millisecond, StepTimeout: 5 * time.Second})
	d := &drafts{}

	start := time.Now()
	if _, err := c.Step(t.Context(), stepRequest(t), d.add); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if took := time.Since(start); took < 300*time.Millisecond {
		t.Fatalf("the stream took %v, not longer than the idle window and RequestTimeout", took)
	}
	if len(d.got) != n || d.got[n-1] != strings.Repeat("ч", n) {
		t.Fatalf("drafts = %q", d.got)
	}
}

// A stream that goes silent after some deltas ends as a timeout after the idle window, not RequestTimeout; the draft
// had the earlier deltas and gets nothing after the call returned.
func TestStepStreamGoesSilent(t *testing.T) {
	f := &fakeStreamHub{serve: serveSlowly(2, time.Millisecond, false)}
	c := startStepGateway(t, f, Config{RequestTimeout: 5 * time.Second, StepIdleTimeout: 100 * time.Millisecond, StepTimeout: 5 * time.Second})
	d := &drafts{}

	start := time.Now()
	_, err := c.Step(t.Context(), stepRequest(t), d.add)
	took := time.Since(start)
	got := len(d.got)
	assertStepTimeout(t, err, errStepSilent)
	if took >= 4*time.Second {
		t.Fatalf("the silent stream ended after %v, by RequestTimeout", took)
	}
	if got != 2 || d.got[1] != "чч" {
		t.Fatalf("drafts = %q", d.got)
	}
	time.Sleep(50 * time.Millisecond)
	if len(d.got) != got || len(f.seen()) != 0 {
		t.Fatalf("drafts after the call %q, unary calls %d", d.got, len(f.seen()))
	}
}

// Before the first message the wait is RequestTimeout, not the idle window: a step that only calls tools streams
// nothing until its result.
func TestStepStreamFirstMessageWindow(t *testing.T) {
	tests := []struct {
		name  string
		serve func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error
		ok    bool
	}{
		{
			name: "result after the idle window",
			serve: func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
				if err := sleepCtx(ctx, 150*time.Millisecond); err != nil {
					return err
				}
				return send(stepResult(okResponse()))
			},
			ok: true,
		},
		{name: "never a message", serve: serveSlowly(0, 0, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startStepGateway(t, &fakeStreamHub{serve: tt.serve},
				Config{RequestTimeout: 500 * time.Millisecond, StepIdleTimeout: 30 * time.Millisecond, StepTimeout: 5 * time.Second})
			start := time.Now()
			_, err := c.Step(t.Context(), stepRequest(t), nil)
			if tt.ok {
				if err != nil {
					t.Fatalf("Step: %v", err)
				}
				return
			}
			assertStepTimeout(t, err, errStepSilent)
			if took := time.Since(start); took < 450*time.Millisecond {
				t.Fatalf("a silent first wait ended after %v, before RequestTimeout", took)
			}
		})
	}
}

// A stream that never goes silent still ends at StepTimeout. The fake ignores its own deadline (sent by the client), so
// only the client's cap ends the call.
func TestStepStreamOverTheCap(t *testing.T) {
	f := &fakeStreamHub{serve: func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
		for !errors.Is(ctx.Err(), context.Canceled) {
			if err := send(stepDelta("ы")); err != nil {
				return err
			}
			time.Sleep(10 * time.Millisecond)
		}
		return ctx.Err()
	}}
	c := startStepGateway(t, f, Config{RequestTimeout: 200 * time.Millisecond, StepIdleTimeout: 200 * time.Millisecond, StepTimeout: 300 * time.Millisecond})
	_, err := c.Step(t.Context(), stepRequest(t), nil)
	assertStepTimeout(t, err, errStepTooLong)
	if errors.Is(err, errStepSilent) {
		t.Fatalf("a never-silent stream = %v", err)
	}
}

// The caller going away (a cancel or the caller's own deadline) is the caller's error, never a Hub timeout.
func TestStepStreamCallerEndIsNoTimeout(t *testing.T) {
	tests := []struct {
		name string
		ctx  func(context.Context) (context.Context, context.CancelFunc)
		want error
	}{
		{"cancel", func(ctx context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(ctx)
			time.AfterFunc(50*time.Millisecond, cancel)
			return ctx, cancel
		}, context.Canceled},
		{"caller deadline", func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 50*time.Millisecond)
		}, context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startStepGateway(t, &fakeStreamHub{serve: serveSlowly(1, 0, false)},
				Config{RequestTimeout: 5 * time.Second, StepIdleTimeout: time.Second, StepTimeout: 5 * time.Second})
			ctx, cancel := tt.ctx(t.Context())
			defer cancel()
			_, err := c.Step(ctx, stepRequest(t), nil)
			if !errors.Is(err, tt.want) || errors.Is(err, ErrTimeout) {
				t.Fatalf("Step = %v, want %v and no timeout", err, tt.want)
			}
		})
	}
}

// The unary fallback keeps RequestTimeout as its total cap, on a context of its own: the stream's wait for
// unimplemented does not count, and StepTimeout does not apply.
func TestStepUnaryFallbackKeepsRequestTimeout(t *testing.T) {
	older := func(ctx context.Context, _ func(*hubv1.AgentStepStreamResponse) error) error {
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
		return connect.NewError(connect.CodeUnimplemented, errors.New("older hub"))
	}
	tests := []struct {
		name  string
		delay time.Duration
		ok    bool
	}{
		{"within the cap", 200 * time.Millisecond, true},
		{"over the cap", time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStreamHub{serve: older}
			f.answer = func() (*hubv1.AgentStepResponse, error) {
				time.Sleep(tt.delay)
				return agentOK()
			}
			c := startStepGateway(t, f, Config{RequestTimeout: 350 * time.Millisecond, StepTimeout: 10 * time.Second})
			_, err := c.Step(t.Context(), stepRequest(t), nil)
			if tt.ok {
				if err != nil {
					t.Fatalf("Step: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrTimeout) || errors.Is(err, errStepSilent) || errors.Is(err, errStepTooLong) {
				t.Fatalf("Step = %v, want the plain call timeout", err)
			}
		})
	}
}

// The step timeouts take their defaults, refuse negative values and an idle window above the cap, and the http.Client
// backstop never cuts a step first; a Timeout of an injected client is kept.
func TestStepTimeoutConfig(t *testing.T) {
	creds := testenv.NewMemCredentials()
	const base = "https://hub.example.com"
	tests := []struct {
		name             string
		cfg              Config
		wantErr          bool
		wantCap, wantIdl time.Duration
	}{
		{"defaults", Config{}, false, DefaultStepTimeout, DefaultStepIdleTimeout},
		{"request timeout above the step default", Config{RequestTimeout: 15 * time.Minute}, false, 15 * time.Minute, DefaultStepIdleTimeout},
		{"short cap clamps the default idle", Config{StepTimeout: 30 * time.Second}, false, 30 * time.Second, 30 * time.Second},
		{"set", Config{StepTimeout: 20 * time.Minute, StepIdleTimeout: 2 * time.Minute}, false, 20 * time.Minute, 2 * time.Minute},
		{"negative cap", Config{StepTimeout: -time.Second}, true, 0, 0},
		{"negative idle", Config{StepIdleTimeout: -time.Second}, true, 0, 0},
		{"idle above the cap", Config{StepTimeout: time.Minute, StepIdleTimeout: 2 * time.Minute}, true, 0, 0},
		{"idle above the default cap", Config{StepIdleTimeout: 11 * time.Minute}, true, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.BaseURL = base
			c, err := New(tt.cfg, creds)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("New = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.stepTO != tt.wantCap || c.stepIdle != tt.wantIdl {
				t.Fatalf("step cap %v, idle %v; want %v, %v", c.stepTO, c.stepIdle, tt.wantCap, tt.wantIdl)
			}
			if c.http.Timeout < max(c.timeout, c.answerTO, c.stepTO)+clientGrace {
				t.Fatalf("backstop %v shorter than the step cap %v", c.http.Timeout, c.stepTO)
			}
		})
	}

	c, err := New(Config{BaseURL: base, HTTPClient: &http.Client{Timeout: 7 * time.Second}}, creds)
	if err != nil || c.http.Timeout != 7*time.Second {
		t.Fatalf("injected client timeout = %v, %v", c.http.Timeout, err)
	}
}
