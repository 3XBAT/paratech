package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSystemNow(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	after := time.Now()

	if got.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", got.Location())
	}
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("Now = %v, want between %v and %v", got, before, after)
	}
}

func TestLocalNow(t *testing.T) {
	before := time.Now()
	got := Local{}.Now()
	after := time.Now()

	if got.Location() != time.Local {
		t.Errorf("location = %v, want the local zone", got.Location())
	}
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("Now = %v, want between %v and %v", got, before, after)
	}
}

func TestSystemSleep(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		d       time.Duration
		wantErr error
		atLeast time.Duration
	}{
		{name: "zero", ctx: context.Background(), d: 0},
		{name: "negative", ctx: context.Background(), d: -time.Hour},
		{name: "non-positive ignores a cancelled context", ctx: cancelled, d: 0},
		{name: "short", ctx: context.Background(), d: 20 * time.Millisecond, atLeast: 20 * time.Millisecond},
		{name: "cancelled", ctx: cancelled, d: time.Hour, wantErr: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			err := System{}.Sleep(tt.ctx, tt.d)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Sleep = %v, want %v", err, tt.wantErr)
			}
			if took := time.Since(start); took < tt.atLeast || took > 10*time.Second {
				t.Fatalf("Sleep took %v, want at least %v", took, tt.atLeast)
			}
		})
	}
}

func TestSystemSleepEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- System{}.Sleep(ctx, time.Hour) }()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Sleep did not return after the context ended")
	}
}
