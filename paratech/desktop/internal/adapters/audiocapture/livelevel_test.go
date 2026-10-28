package audiocapture_test

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

func TestChildCaptureLevelDefault(t *testing.T) {
	if audiocapture.DefaultCaptureLevelInterval != 250*time.Millisecond {
		t.Fatalf("DefaultCaptureLevelInterval = %v, want 4 levels a second", audiocapture.DefaultCaptureLevelInterval)
	}
}

// TASK-099: a running capture reports its level at the configured rate, after its start and never after its end.
func TestChildReportsLevelsWhileCapturing(t *testing.T) {
	tests := []struct {
		name  string
		quiet bool
		late  time.Duration // the first packet comes this late, as on a loaded machine
	}{
		{name: "tone"},
		{name: "tone after a late first packet", late: 150 * time.Millisecond},
		{name: "zeros", quiet: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			const every = 20 * time.Millisecond
			src := &testenv.ToneSource{Format: s16, PacketFrames: 80, Interval: 5 * time.Millisecond}
			if tt.quiet {
				src.Gate = func() bool { return false }
			}
			if tt.late > 0 {
				src.OnPacket = func(n int) {
					if n == 1 {
						time.Sleep(tt.late)
					}
				}
			}
			begun := time.Now()
			run := startChild(t, context.Background(), src, request(t),
				audiocapture.ChildConfig{CaptureLevelInterval: every, StatsInterval: 10 * time.Millisecond})
			// A window before the first packet reaches the meter is rightly 0, and on a loaded runner that can be many
			// windows: the levels are judged only once packets were metered and more windows passed after that.
			testenv.Eventually(t, "packets are metered", func() bool {
				stats := run.out.find(t, "stats")
				if len(stats) == 0 {
					return false
				}
				n, _ := stats[len(stats)-1]["packets"].(float64)
				return n >= 10
			}, nil)
			metered := len(run.out.find(t, "level"))
			testenv.Eventually(t, "levels come", func() bool {
				levels := run.out.find(t, "level")
				return len(levels) >= max(metered+2, 5) && (tt.quiet || heardIn(levels))
			}, nil)
			_ = run.stdin.Close()
			if code := run.exit(t); code != audiocapture.ChildExitOK {
				t.Fatalf("exit code %d", code)
			}
			elapsed := time.Since(begun)

			levels := run.out.find(t, "level")
			if limit := int(elapsed/every) + 1; len(levels) > limit {
				t.Fatalf("%d levels in %v, want at most %d (one per %v)", len(levels), elapsed, limit, every)
			}
			heard := false
			for _, ev := range levels {
				l, ok := ev["level"].(float64)
				if !ok || l < 0 || l > 1 {
					t.Fatalf("level event %v", ev)
				}
				heard = heard || values.SoundLevel(l).Heard()
			}
			if heard == tt.quiet {
				t.Fatalf("heard = %v in %v", heard, levels)
			}

			// Order: started, levels, stopped; nothing after the end.
			var started, stopped, firstLevel, lastLevel = -1, -1, -1, -1
			for i, ev := range run.out.events(t) {
				switch ev["type"] {
				case "started":
					started = i
				case "stopped":
					stopped = i
				case "level":
					if firstLevel < 0 {
						firstLevel = i
					}
					lastLevel = i
				}
			}
			if started < 0 || stopped < 0 || firstLevel < started || lastLevel > stopped {
				t.Fatalf("started %d, levels %d..%d, stopped %d", started, firstLevel, lastLevel, stopped)
			}
		})
	}
}

// heardIn reports whether any level event was heard.
func heardIn(levels []map[string]any) bool {
	for _, ev := range levels {
		if l, ok := ev["level"].(float64); ok && values.SoundLevel(l).Heard() {
			return true
		}
	}
	return false
}

// TASK-099: the parent keeps the latest level of the running capture and reports nothing once it ended.
func TestRecorderSound(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		heard    bool
	}{
		{name: "tone", scenario: testenv.RecorderTone, heard: true},
		{name: "silent source", scenario: testenv.RecorderSilent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newRecorderEnv(t, testConfig(), tt.scenario)
			ctx := context.Background()
			if _, ok := e.rec.Sound(ctx, "rec1"); ok {
				t.Fatal("a sound before the start")
			}
			if err := e.rec.Start(ctx, "rec1", e.chunks, values.WholeComputer()); err != nil {
				t.Fatal(err)
			}
			var got values.SoundReading
			testenv.Eventually(t, "the level arrives", func() bool {
				var ok bool
				got, ok = e.rec.Sound(ctx, "rec1")
				if tt.heard {
					return ok && got.Heard
				}
				return ok && got.Quiet > 0
			}, nil)
			if tt.heard && (got.Level < 0.4 || got.Level > 0.6) {
				t.Fatalf("reading %+v, want the tone of about 0.5", got)
			}
			if !tt.heard && (got.Heard || got.Level != 0) {
				t.Fatalf("reading %+v, want silence", got)
			}

			if err := stop(t, e); err != nil {
				t.Fatal(err)
			}
			if err := e.waitEnded(t); err != nil {
				t.Fatal(err)
			}
			if s, ok := e.rec.Sound(ctx, "rec1"); ok {
				t.Fatalf("Sound after the stop = %+v", s)
			}
		})
	}
}
