package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

func importedRecording(t *testing.T, id, sha string, audio recording.ImportedAudio) *recording.Recording {
	t.Helper()
	r, err := recording.NewImported(recording.ImportSpec{
		ID: shared.RecordingID(id), StartedAt: recT0.Add(-24 * time.Hour), Audio: audio, SHA256: sha,
		Lesson: values.NewLessonSnapshot(values.LessonSnapshotSpec{Subject: "Физика", Kind: "lecture", Title: "Оптика"}),
	}, recT0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TASK-161: an imported recording keeps its origin, digest, snapshot without a lesson and unknown duration; it is found
// by its digest, and a second recording with the same digest is a duplicate.
func TestImportedRecordingRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	sha := strings.Repeat("c4", 32)

	for _, tc := range []struct {
		audio  recording.ImportedAudio
		digest string
	}{
		{recording.ImportedAudio{Format: recording.FormatWAV, Duration: 95*time.Minute + 250*time.Millisecond}, strings.Repeat("a1", 32)},
		{recording.ImportedAudio{Format: recording.FormatMP3}, strings.Repeat("b2", 32)},
	} {
		audio, digest := tc.audio, tc.digest
		t.Run(string(audio.Format), func(t *testing.T) {
			r := importedRecording(t, "imp-"+string(audio.Format), digest, audio)
			saveRecording(t, repo, r)

			got, err := repo.Get(ctx, r.ID())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, r) {
				t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
			}
			if byDigest, err := repo.GetBySourceSHA256(ctx, digest); err != nil || byDigest.ID() != r.ID() {
				t.Fatalf("GetBySourceSHA256 = %v, %v", byDigest, err)
			}
			if _, ok := got.Duration(); ok != (audio.Duration > 0) {
				t.Fatalf("Duration known = %v", ok)
			}
		})
	}

	first := importedRecording(t, "imp-a", sha, recording.ImportedAudio{Format: recording.FormatFLAC})
	saveRecording(t, repo, first)
	again := importedRecording(t, "imp-b", sha, recording.ImportedAudio{Format: recording.FormatFLAC})
	if err := repo.Save(ctx, again); !errors.Is(err, shared.ErrDuplicate) || errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Save of the same digest = %v, want ErrDuplicate", err)
	}

	// A saved change keeps the origin and the digest.
	if err := first.MarkDeleting(recT0.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, first)
	got, err := repo.GetBySourceSHA256(ctx, sha)
	if err != nil || got.Origin() != recording.OriginImported || got.SourceSHA256() != sha || !got.Deleting() {
		t.Fatalf("after an update: %+v, %v", got, err)
	}

	if _, err := repo.GetBySourceSHA256(ctx, strings.Repeat("ee", 32)); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown digest = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetBySourceSHA256(ctx, ""); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("empty digest = %v, want ErrNotFound (captured recordings have none)", err)
	}
	captured := recordingInStatus(t, recording.StatusCompleted)
	saveRecording(t, repo, captured)
	if got, err := repo.Get(ctx, captured.ID()); err != nil || got.Origin() != recording.OriginCaptured || got.SourceSHA256() != "" {
		t.Fatalf("captured recording: %+v, %v", got, err)
	}
}
