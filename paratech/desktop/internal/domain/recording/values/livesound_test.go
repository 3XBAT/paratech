package values_test

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

func TestLiveSoundReading(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	type level struct {
		v  values.SoundLevel
		at time.Duration
	}
	tests := []struct {
		name   string
		levels []level
		now    time.Duration
		want   values.SoundReading
	}{
		{name: "nothing yet", now: time.Minute, want: values.SoundReading{}},
		{name: "sound now", levels: []level{{0.3, 0}}, now: 100 * time.Millisecond,
			want: values.SoundReading{Level: 0.3, Heard: true}},
		{name: "a pause between words is still heard", levels: []level{{0.3, 0}, {0, 250 * time.Millisecond}},
			now: 500 * time.Millisecond, want: values.SoundReading{Level: 0, Heard: true}},
		{name: "below the heard level is quiet", levels: []level{{0.005, 0}}, now: 200 * time.Millisecond,
			want: values.SoundReading{Level: 0.005, Quiet: 200 * time.Millisecond}},
		{name: "quiet counts from the last sound", levels: []level{{0.3, 0}, {0, 10 * time.Second}},
			now: 10*time.Second + 500*time.Millisecond, want: values.SoundReading{Quiet: 10*time.Second + 500*time.Millisecond}},
		{name: "quiet since the start", levels: []level{{0, 0}, {0, time.Minute}}, now: time.Minute,
			want: values.SoundReading{Quiet: time.Minute}},
		{name: "two minutes of quiet show the hint", levels: []level{{0, 0}, {0, 2 * time.Minute}}, now: 2 * time.Minute,
			want: values.SoundReading{Quiet: 2 * time.Minute, Hint: true}},
		{name: "just under two minutes", levels: []level{{0, 0}}, now: 2*time.Minute - time.Millisecond,
			want: values.SoundReading{Quiet: 2*time.Minute - time.Millisecond}},
		{name: "a stale level reads as silence", levels: []level{{0.5, 0}}, now: 3 * time.Second,
			want: values.SoundReading{Quiet: 3 * time.Second}},
		{name: "a clock that went back starts again", levels: []level{{0.5, time.Hour}, {0, 0}},
			now: 100 * time.Millisecond, want: values.SoundReading{Quiet: 100 * time.Millisecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s values.LiveSound
			for _, l := range tt.levels {
				s = s.Hear(l.v, at(l.at))
			}
			if got := s.Reading(at(tt.now)); got != tt.want {
				t.Fatalf("Reading = %+v, want %+v", got, tt.want)
			}
		})
	}
}
