package wails

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/values"
)

// TASK-099: the live sound of the running recording, its JSON names, and nothing without a recording.
func TestActivityServiceRecordingSound(t *testing.T) {
	tests := []struct {
		name    string
		stopper *fakeStopper
		want    RecordingSound
		json    string
	}{
		{name: "no recording", stopper: &fakeStopper{},
			json: `{"recording":false,"recordingId":"","level":0,"heard":false,"quietMs":0,"quietHint":false}`},
		{name: "sound", stopper: &fakeStopper{sounding: true, sound: recordingsession.LiveSound{ID: "r1",
			Reading: values.SoundReading{Level: 0.123456, Heard: true}}},
			want: RecordingSound{Recording: true, RecordingID: "r1", Level: 0.123, Heard: true},
			json: `{"recording":true,"recordingId":"r1","level":0.123,"heard":true,"quietMs":0,"quietHint":false}`},
		{name: "quiet for long", stopper: &fakeStopper{sounding: true, sound: recordingsession.LiveSound{ID: "r1",
			Reading: values.SoundReading{Quiet: 2*time.Minute + 250*time.Millisecond, Hint: true}}},
			want: RecordingSound{Recording: true, RecordingID: "r1", QuietMs: 120250, QuietHint: true},
			json: `{"recording":true,"recordingId":"r1","level":0,"heard":false,"quietMs":120250,"quietHint":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewActivityService(&fakeActivity{}, tt.stopper, &fakeForcer{}, &fakeRetrier{}, discard()).
				RecordingSound(context.Background())
			if err != nil || got != tt.want {
				t.Fatalf("RecordingSound = %+v, %v, want %+v", got, err, tt.want)
			}
			raw, err := json.Marshal(got)
			if err != nil || string(raw) != tt.json {
				t.Fatalf("JSON = %s, %v, want %s", raw, err, tt.json)
			}
		})
	}
}
