package hubclient

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const answerOut = `{"claims":[],"missing":["Всё"]}`

func heartbeat() *hubv1.CompleteStreamResponse {
	return &hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Heartbeat{Heartbeat: &hubv1.StreamHeartbeat{}}}
}

// paced sends msgs with gap before each but the first, then waits for ctx when the last is not a result.
func paced(gap time.Duration, msgs ...*hubv1.CompleteStreamResponse) func(context.Context, int, func(*hubv1.CompleteStreamResponse) error) error {
	return func(ctx context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
		for i, m := range msgs {
			if i > 0 {
				if err := sleepCtx(ctx, gap); err != nil {
					return err
				}
			}
			if err := send(m); err != nil {
				return err
			}
		}
		if len(msgs) > 0 && msgs[len(msgs)-1].GetResult() != nil {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

func repeated(n int, m *hubv1.CompleteStreamResponse) []*hubv1.CompleteStreamResponse {
	out := make([]*hubv1.CompleteStreamResponse, n)
	for i := range out {
		out[i] = m
	}
	return out
}

// The answer stream is cut by the Hub's silence, not by its length: any message resets the watchdog, a heartbeat or an
// event the client does not know only does that (never a partial, never the result), and an older Hub that sends no
// heartbeat reads as before. The durations are those of production scaled down: idle 60 s → 300 ms, heartbeat every
// 15 s → 75 ms, the old 5 min whole-read cap → half of AnswerTimeout.
func TestAnswerQuestionStreamWatchdog(t *testing.T) {
	const idle = 300 * time.Millisecond
	cfg := Config{RequestTimeout: idle, StepIdleTimeout: idle, AnswerTimeout: 2 * time.Second}
	silentBeats := append(append([]*hubv1.CompleteStreamResponse{delta("{")}, repeated(8, heartbeat())...), result(answerOut))
	tests := []struct {
		name     string
		stream   func(context.Context, int, func(*hubv1.CompleteStreamResponse) error) error
		cause    error // nil: the answer comes
		partials []string
		atLeast  time.Duration
	}{
		{
			name: "no text for longer than idle, heartbeats keep it", stream: paced(75*time.Millisecond, silentBeats...),
			partials: []string{"{"}, atLeast: 2 * idle,
		},
		{
			name: "heartbeat first and an unknown event", stream: paced(75*time.Millisecond,
				heartbeat(), &hubv1.CompleteStreamResponse{}, delta("{"), heartbeat(), result(answerOut)),
			partials: []string{"{"},
		},
		{
			// Longer than half the cap (the old whole-read cap) and than idle, with text only: an older Hub.
			name: "older hub writing for long", stream: paced(100*time.Millisecond, append(repeated(12, delta("ы")), result(answerOut))...),
			atLeast: 1100 * time.Millisecond,
		},
		{name: "older hub goes silent", stream: paced(0, delta("{")), cause: errStepSilent, partials: []string{"{"}},
		{
			name: "heartbeats stop", stream: paced(75*time.Millisecond, delta("{"), heartbeat(), heartbeat()),
			cause: errStepSilent, partials: []string{"{"},
		},
		{
			name:  "over the cap",
			cause: errStepTooLong,
			stream: func(ctx context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
				// The fake ignores its own deadline (sent by the client), so only the client's cap ends the call.
				for !errors.Is(ctx.Err(), context.Canceled) {
					if err := send(heartbeat()); err != nil {
						return err
					}
					time.Sleep(10 * time.Millisecond)
				}
				return ctx.Err()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &streamHub{fakeHub: &fakeHub{}, stream: tt.stream}
			c := startStepGateway(t, h, cfg)
			p := &partials{}

			start := time.Now()
			got, err := c.AnswerQuestion(t.Context(), answerRequest(t), p.add)
			took := time.Since(start)
			if tt.cause == nil {
				if err != nil || got != answerOut {
					t.Fatalf("AnswerQuestion = %q, %v", got, err)
				}
				if took < tt.atLeast {
					t.Fatalf("the stream took %v, want at least %v", took, tt.atLeast)
				}
			} else {
				if got != "" {
					t.Fatalf("output %q of a cut stream", got)
				}
				assertStepTimeout(t, err, tt.cause)
				other := errStepSilent
				if errors.Is(tt.cause, errStepSilent) {
					other = errStepTooLong
				}
				if errors.Is(err, other) {
					t.Fatalf("error %v also matches %v", err, other)
				}
			}
			if tt.partials != nil && !slices.Equal(p.got, tt.partials) {
				t.Fatalf("partials %q, want %q", p.got, tt.partials)
			}
			if reqs, _ := h.calls(); len(reqs) != 0 {
				t.Fatalf("the stream fell back to Complete: %d calls", len(reqs))
			}
		})
	}
}

// Before the first message the wait is RequestTimeout, not the idle window: the Hub's reask or a slow provider sends
// nothing for a while.
func TestAnswerQuestionFirstMessageWindow(t *testing.T) {
	cfg := Config{RequestTimeout: 1500 * time.Millisecond, StepIdleTimeout: 90 * time.Millisecond, AnswerTimeout: 5 * time.Second}

	h := &streamHub{fakeHub: &fakeHub{}, stream: func(ctx context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
		if err := sleepCtx(ctx, 300*time.Millisecond); err != nil {
			return err
		}
		return send(result(answerOut))
	}}
	if got, err := startStepGateway(t, h, cfg).AnswerQuestion(t.Context(), answerRequest(t), nil); err != nil || got != answerOut {
		t.Fatalf("result after the idle window = %q, %v", got, err)
	}

	start := time.Now()
	_, err := startStepGateway(t, &streamHub{fakeHub: &fakeHub{}, stream: paced(0)}, cfg).AnswerQuestion(t.Context(), answerRequest(t), nil)
	assertStepTimeout(t, err, errStepSilent)
	if took := time.Since(start); took < 1350*time.Millisecond {
		t.Fatalf("a silent first wait ended after %v, before RequestTimeout", took)
	}
}

// The caller going away mid-stream (a cancel or the caller's own deadline) is the caller's error, never a Hub timeout.
func TestAnswerQuestionCallerEndIsNoTimeout(t *testing.T) {
	tests := []struct {
		name string
		ctx  func(context.Context) (context.Context, context.CancelFunc)
		want error
	}{
		{"cancel", func(ctx context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(ctx)
			time.AfterFunc(100*time.Millisecond, cancel)
			return ctx, cancel
		}, context.Canceled},
		{"caller deadline", func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 100*time.Millisecond)
		}, context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &streamHub{fakeHub: &fakeHub{}, stream: paced(20*time.Millisecond, repeated(100, heartbeat())...)}
			c := startStepGateway(t, h, Config{RequestTimeout: 5 * time.Second, StepIdleTimeout: 3 * time.Second, AnswerTimeout: 5 * time.Second})
			ctx, cancel := tt.ctx(t.Context())
			defer cancel()
			_, err := c.AnswerQuestion(ctx, answerRequest(t), nil)
			if !errors.Is(err, tt.want) || errors.Is(err, ErrTimeout) {
				t.Fatalf("AnswerQuestion = %v, want %v and no timeout", err, tt.want)
			}
		})
	}
}

// The unary fallback keeps RequestTimeout as its total cap, on a context of its own: the stream's wait for
// unimplemented does not count, and AnswerTimeout does not apply. Each wait is under RequestTimeout, both exceed it.
func TestAnswerQuestionUnaryFallbackKeepsRequestTimeout(t *testing.T) {
	older := func(ctx context.Context, _ int, _ func(*hubv1.CompleteStreamResponse) error) error {
		if err := sleepCtx(ctx, 600*time.Millisecond); err != nil {
			return err
		}
		return connect.NewError(connect.CodeUnimplemented, errors.New("older hub"))
	}
	tests := []struct {
		name  string
		delay time.Duration
		ok    bool
	}{
		{"within the cap", 600 * time.Millisecond, true},
		{"over the cap", 2 * time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeHub{answer: func(ctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
				if err := sleepCtx(ctx, tt.delay); err != nil {
					return nil, err
				}
				return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: answerOut}}, nil
			}}
			c := startStepGateway(t, &streamHub{fakeHub: f, stream: older}, Config{RequestTimeout: time.Second, AnswerTimeout: 10 * time.Second})
			got, err := c.AnswerQuestion(t.Context(), answerRequest(t), nil)
			if tt.ok {
				if err != nil || got != answerOut {
					t.Fatalf("AnswerQuestion = %q, %v", got, err)
				}
				return
			}
			if !errors.Is(err, ErrTimeout) || backoff.Classify(err) != backoff.ClassTransient ||
				errors.Is(err, errStepSilent) || errors.Is(err, errStepTooLong) {
				t.Fatalf("AnswerQuestion = %v, want the plain call timeout", err)
			}
		})
	}
}

// The answer cap takes its default, refuses a negative value and an explicit idle window above it, and the http.Client
// backstop covers it.
func TestAnswerTimeoutConfig(t *testing.T) {
	creds := testenv.NewMemCredentials()
	const base = "https://hub.example.com"
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		wantCap time.Duration
	}{
		{"defaults", Config{}, false, DefaultAnswerTimeout},
		{"short request timeout", Config{RequestTimeout: time.Second}, false, DefaultAnswerTimeout},
		{"request timeout above the answer default", Config{RequestTimeout: 15 * time.Minute}, false, 15 * time.Minute},
		{"set", Config{AnswerTimeout: 20 * time.Minute}, false, 20 * time.Minute},
		{"short cap with the default idle", Config{AnswerTimeout: 30 * time.Second}, false, 30 * time.Second},
		{"negative", Config{AnswerTimeout: -time.Second}, true, 0},
		{"idle above the cap", Config{AnswerTimeout: time.Minute, StepIdleTimeout: 2 * time.Minute}, true, 0},
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
			if c.answerTO != tt.wantCap || c.http.Timeout < c.answerTO+clientGrace {
				t.Fatalf("answer cap %v, backstop %v; want %v", c.answerTO, c.http.Timeout, tt.wantCap)
			}
		})
	}
	if DefaultAnswerTimeout != 10*time.Minute || DefaultStepIdleTimeout != time.Minute {
		t.Fatalf("defaults %v and %v, want the ADR-0026 10 min and 60 s", DefaultAnswerTimeout, DefaultStepIdleTimeout)
	}
}
