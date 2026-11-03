package values

import (
	"errors"
	"testing"
	"time"
)

func TestSettingsResult(t *testing.T) {
	tests := []struct {
		name string
		rows []SettingRow
		want string // "" when refused
	}{
		{"three toggles", []SettingRow{{SettingRecordingPrompts, "on"}, {SettingACPowerOnly, "off"}, {SettingRecognitionLoad, "max"}},
			`{"settings":{"ac_power_only":"off","recognition_load":"max","recording_prompts":"on"}}`},
		{"none", nil, `{"settings":{}}`},
		{"a value of another setting", []SettingRow{{SettingRecognitionLoad, "on"}}, ""},
		{"an unknown setting", []SettingRow{{"free_work_notice", "off"}}, ""},
		{"twice", []SettingRow{{SettingACPowerOnly, "off"}, {SettingACPowerOnly, "on"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SettingsResult("c1", tt.rows)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidResult) {
					t.Fatalf("err = %v, want ErrInvalidResult", err)
				}
				return
			}
			if err != nil || got.ResultJSON != tt.want || got.IsError {
				t.Fatalf("got %+v, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestQuotaResult(t *testing.T) {
	day, err := NewDate(2026, time.November, 1)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		q    QuotaRow
		want string
	}{
		{QuotaRow{RemainingPercent: 25, Resets: day}, `{"remaining_percent":25,"resets":"2026-11-01"}`},
		{QuotaRow{RemainingPercent: 0}, `{"remaining_percent":0}`},
		{QuotaRow{RemainingPercent: 100}, `{"remaining_percent":100}`},
		{QuotaRow{RemainingPercent: 101}, ""},
		{QuotaRow{RemainingPercent: -1}, ""},
	}
	for _, tt := range tests {
		got, err := QuotaResult("c1", tt.q)
		if tt.want == "" {
			if !errors.Is(err, ErrInvalidResult) {
				t.Errorf("%+v: err = %v", tt.q, err)
			}
			continue
		}
		if err != nil || got.ResultJSON != tt.want {
			t.Errorf("%+v: got %s, %v", tt.q, got.ResultJSON, err)
		}
	}
}

func TestRecordingResult(t *testing.T) {
	rec := mustRef(t, KindRecording, "rec-1")
	tests := []struct {
		name string
		row  RecordingRow
		want string
		seen int
	}{
		{"started", RecordingRow{RecordingStarted, rec}, `{"changed":true,"done":true,"recording":"rec-1","state":"started"}`, 1},
		{"already recording", RecordingRow{RecordingRunning, rec}, `{"changed":false,"done":true,"recording":"rec-1","state":"already_recording"}`, 1},
		{"stopped", RecordingRow{RecordingStopped, rec}, `{"changed":true,"done":true,"recording":"rec-1","state":"stopped"}`, 1},
		{"stopped, replayed", RecordingRow{RecordingStopped, Ref{}}, `{"changed":true,"done":true,"state":"stopped"}`, 0},
		{"not recording", RecordingRow{RecordingNotActive, Ref{}}, `{"changed":false,"done":true,"state":"not_recording"}`, 0},
		{"started without an id", RecordingRow{RecordingStarted, Ref{}}, "", 0},
		{"not recording with an id", RecordingRow{RecordingNotActive, rec}, "", 0},
		{"another kind", RecordingRow{RecordingStarted, mustRef(t, KindJob, "j1")}, "", 0},
		{"unknown state", RecordingRow{"paused", rec}, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, seen, err := RecordingResult("c1", tt.row)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidResult) {
					t.Fatalf("err = %v, want ErrInvalidResult", err)
				}
				return
			}
			if err != nil || got.ResultJSON != tt.want || len(seen) != tt.seen {
				t.Fatalf("got %s %v, %v; want %s", got.ResultJSON, seen, err, tt.want)
			}
		})
	}
}

// An action line offers only a button that needs no ID (recording{stop}: «Начать запись»).
func TestActionLineOffer(t *testing.T) {
	tests := []struct {
		offer OfferAction
		ok    bool
	}{
		{"", true},
		{OfferStartRecording, true},
		{OfferOpenHelp, true},
		{OfferOpenSummary, false}, // needs a recording id
		{"open_url", false},
	}
	for _, tt := range tests {
		if err := (ActionLine{Action: "act1", Offer: tt.offer}).Validate(); (err == nil) != tt.ok {
			t.Errorf("offer %q: %v", tt.offer, err)
		}
	}
}
