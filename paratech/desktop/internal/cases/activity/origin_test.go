package activity

import (
	"context"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
)

// TASK-300: a row carries the origin of its recording, so the «импортирована» mark survives a restart.
func TestListRecordingsOrigin(t *testing.T) {
	imported, err := recording.NewImported(recording.ImportSpec{
		ID: "r-imported", StartedAt: t0.Add(-time.Hour), SHA256: strings.Repeat("ab", 32),
		Audio: recording.ImportedAudio{Format: recording.FormatWAV, Duration: time.Hour},
	}, t0)
	must(t, err)
	captured := newRecording(t, "r-captured")

	s, err := New(&fakeRecordings{list: []*recording.Recording{imported, captured}}, &fakeJobs{}, &fakeLessons{}, nil)
	must(t, err)
	got, err := s.ListRecordings(context.Background())
	must(t, err)

	want := map[string]recording.Origin{"r-imported": recording.OriginImported, "r-captured": recording.OriginCaptured}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for _, r := range got {
		if r.Origin != want[string(r.ID)] {
			t.Errorf("%s: origin = %q, want %q", r.ID, r.Origin, want[string(r.ID)])
		}
	}
}
