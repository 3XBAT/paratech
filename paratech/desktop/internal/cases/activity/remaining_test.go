package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	transcription "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// fakeTimes takes perHour of processing for each hour of audio of model "m-1".
type fakeTimes struct {
	perHour time.Duration
	err     error
	asked   []time.Duration
}

func (f *fakeTimes) TranscriptionTime(_ context.Context, model shared.SttModelID, audio time.Duration) (time.Duration, bool, error) {
	f.asked = append(f.asked, audio)
	if f.err != nil {
		return 0, false, f.err
	}
	if model != "m-1" {
		return 0, false, nil
	}
	return time.Duration(float64(f.perHour) * audio.Hours()), true, nil
}

// sttJob is the STT job of the recording pinning model, moved by steps at t0.
func sttJob(t *testing.T, model shared.SttModelID, steps ...func(j *job.Job) error) *job.Job {
	t.Helper()
	j, err := job.New(job.Spec{
		ID: "stt-1", Type: transcription.STTJobType, Key: "stt:r1", PinnedModelID: model, MaxAttempts: 3,
	}, t0)
	must(t, err)
	for _, s := range steps {
		must(t, s(j))
	}
	return j
}

func claimAt(d time.Duration) func(*job.Job) error {
	return func(j *job.Job) error { return j.Claim(t0.Add(d)) }
}

func progressAt(percent int, d time.Duration) func(*job.Job) error {
	return func(j *job.Job) error { return reportPercent(j, percent, t0.Add(d)) }
}

// TASK-084: a queued or running transcription shows the approximate time left of its recording (1 h of audio).
func TestListRecordingsRemaining(t *testing.T) {
	tests := []struct {
		name   string
		jobs   []*job.Job
		times  *fakeTimes
		now    time.Duration // after t0
		want   time.Duration
		hasAny bool
	}{
		{"queued: the whole estimate", []*job.Job{sttJob(t, "m-1")}, &fakeTimes{perHour: 40 * time.Minute}, 0, 40 * time.Minute, true},
		{"running at 25 percent", []*job.Job{sttJob(t, "m-1", claimAt(time.Minute), progressAt(25, 5*time.Minute))},
			&fakeTimes{perHour: 40 * time.Minute}, 6 * time.Minute, 30 * time.Minute, true},
		{"running before the first progress", []*job.Job{sttJob(t, "m-1", claimAt(0))},
			&fakeTimes{perHour: 40 * time.Minute}, 10 * time.Minute, 30 * time.Minute, true},
		{"overdue: at least a minute", []*job.Job{sttJob(t, "m-1", claimAt(0))},
			&fakeTimes{perHour: 40 * time.Minute}, 2 * time.Hour, time.Minute, true},
		{"no estimate for the model", []*job.Job{sttJob(t, "m-other")}, &fakeTimes{perHour: time.Hour}, 0, 0, false},
		{"estimate fails", []*job.Job{sttJob(t, "m-1")}, &fakeTimes{err: errors.New("db")}, 0, 0, false},
		{"no pinned model", []*job.Job{sttJob(t, "")}, &fakeTimes{perHour: time.Hour}, 0, 0, false},
		{"no STT job yet", nil, &fakeTimes{perHour: time.Hour}, 0, 0, false},
		{"transcribed", []*job.Job{sttJob(t, "m-1", claimAt(0), func(j *job.Job) error { return j.Complete(t0.Add(time.Minute)) })},
			&fakeTimes{perHour: time.Hour}, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := recordingIn(t, recording.StatusCompleted)
			jobs := &fakeJobs{chain: map[shared.RecordingID][]*job.Job{rec.ID(): tt.jobs}}
			s, err := New(&fakeRecordings{list: []*recording.Recording{rec}}, jobs, &fakeLessons{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			s.WithRemaining(tt.times, testenv.NewFakeClock(t0.Add(tt.now)))

			rows, err := s.ListRecordings(context.Background())
			if err != nil || len(rows) != 1 {
				t.Fatalf("ListRecordings = %v, %v", rows, err)
			}
			if rows[0].HasRemaining != tt.hasAny || rows[0].Remaining != tt.want {
				t.Errorf("remaining %v (%v), want %v (%v); chain %+v", rows[0].Remaining, rows[0].HasRemaining, tt.want,
					tt.hasAny, rows[0].Chain)
			}
		})
	}
}

func TestListRecordingsWithoutRemaining(t *testing.T) {
	rec := recordingIn(t, recording.StatusCompleted)
	jobs := &fakeJobs{chain: map[shared.RecordingID][]*job.Job{rec.ID(): {sttJob(t, "m-1")}}}
	s, err := New(&fakeRecordings{list: []*recording.Recording{rec}}, jobs, &fakeLessons{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRecordings(context.Background())
	if err != nil || len(rows) != 1 || rows[0].HasRemaining {
		t.Fatalf("rows = %+v, %v; want no time left without WithRemaining", rows, err)
	}
}
