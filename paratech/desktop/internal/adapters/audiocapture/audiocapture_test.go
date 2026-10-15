package audiocapture

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

func TestUnavailableRefusesEverything(t *testing.T) {
	ctx := context.Background()
	chunks, err := shared.NewArtifactRef(shared.ArtifactRecordings, "r1")
	if err != nil {
		t.Fatal(err)
	}
	track, err := shared.NewArtifactRef(shared.ArtifactRecordings, "r1.wav")
	if err != nil {
		t.Fatal(err)
	}
	r := Unavailable{}

	tests := []struct {
		name string
		call func() error
	}{
		{"start", func() error { return r.Start(ctx, "r1", chunks) }},
		{"stop", func() error { return r.Stop(ctx, "r1") }},
		{"wait", func() error { return r.Wait(ctx, "r1") }},
		{"finalize", func() error { _, err := r.Finalize(ctx, chunks, track); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			// ErrNoAudio would fail the recording for good; without a recorder its chunks must wait instead.
			if errors.Is(err, recording.ErrNoAudio) {
				t.Fatal("the unavailable recorder must not report missing audio")
			}
		})
	}
}
