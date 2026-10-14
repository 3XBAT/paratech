package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

var transcriptT0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// transcriptEnv is a database with one completed recording and one installed model.
type transcriptEnv struct {
	db     *DB
	repo   *TranscriptRepository
	models *ModelRepository
	rec    *recording.Recording
}

func newTranscriptEnv(t *testing.T) *transcriptEnv {
	t.Helper()
	db := openTest(t, nil)
	e := &transcriptEnv{db: db, repo: NewTranscriptRepository(db), models: NewModelRepository(db)}
	e.rec = recordingInStatus(t, recording.StatusCompleted)
	saveRecording(t, NewRecordingRepository(db), e.rec)
	saveModel(t, e.models, newModel(t, "m1", "base", "1"))
	return e
}

func (e *transcriptEnv) newTranscript(t *testing.T, id string, lang transcript.Language) *transcript.Transcript {
	t.Helper()
	text, err := transcript.DocumentRef(shared.TranscriptID(id))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transcript.New(transcript.Spec{
		ID: shared.TranscriptID(id), RecordingID: e.rec.ID(), ModelID: "m1", ModelName: "base", ModelVersion: "1",
		Language: lang, Text: text,
	}, transcriptT0)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

type transcriptView struct {
	ID, Recording, Model, Name, Version, Lang, Text string
	Stored                                          int64
	Created                                         time.Time
}

func transcriptViewOf(tr *transcript.Transcript) transcriptView {
	return transcriptView{
		ID: string(tr.ID()), Recording: string(tr.RecordingID()), Model: string(tr.ModelID()), Name: string(tr.ModelName()),
		Version: string(tr.ModelVersion()), Lang: string(tr.Language()), Text: tr.Text().String(), Stored: tr.Version(),
		Created: tr.CreatedAt(),
	}
}

func TestTranscriptSaveGet(t *testing.T) {
	for _, lang := range []transcript.Language{"ru", ""} {
		t.Run("language "+string(lang), func(t *testing.T) {
			e, ctx := newTranscriptEnv(t), context.Background()
			tr := e.newTranscript(t, "job-1", lang)
			if err := e.repo.Save(ctx, tr); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if tr.Version() != 1 {
				t.Fatalf("version after insert = %d, want 1", tr.Version())
			}

			got, err := e.repo.Get(ctx, "job-1")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if transcriptViewOf(got) != transcriptViewOf(tr) {
				t.Fatalf("Get = %+v, want %+v", transcriptViewOf(got), transcriptViewOf(tr))
			}

			// The same instance saves again through the compare-and-swap.
			if err := e.repo.Save(ctx, tr); err != nil || tr.Version() != 2 {
				t.Fatalf("second Save: version %d, %v", tr.Version(), err)
			}
		})
	}
}

func TestTranscriptSaveConflicts(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, e *transcriptEnv) *transcript.Transcript
		want    error
	}{
		{
			name: "second new transcript with the same id",
			prepare: func(t *testing.T, e *transcriptEnv) *transcript.Transcript {
				if err := e.repo.Save(context.Background(), e.newTranscript(t, "job-1", "ru")); err != nil {
					t.Fatal(err)
				}
				return e.newTranscript(t, "job-1", "en")
			},
			want: shared.ErrVersionConflict,
		},
		{
			name: "stale version",
			prepare: func(t *testing.T, e *transcriptEnv) *transcript.Transcript {
				tr := e.newTranscript(t, "job-1", "ru")
				if err := e.repo.Save(context.Background(), tr); err != nil {
					t.Fatal(err)
				}
				stale, err := e.repo.Get(context.Background(), "job-1")
				if err != nil {
					t.Fatal(err)
				}
				if err := e.repo.Save(context.Background(), tr); err != nil {
					t.Fatal(err)
				}
				return stale
			},
			want: shared.ErrVersionConflict,
		},
		{
			name: "stored transcript whose row is gone",
			prepare: func(t *testing.T, e *transcriptEnv) *transcript.Transcript {
				tr := e.newTranscript(t, "job-1", "ru")
				if err := e.repo.Save(context.Background(), tr); err != nil {
					t.Fatal(err)
				}
				if err := NewRecordingTranscripts(e.db).DeleteByRecording(context.Background(), e.rec.ID()); err != nil {
					t.Fatal(err)
				}
				return tr
			},
			want: shared.ErrVersionConflict,
		},
		{
			name: "unknown recording",
			prepare: func(t *testing.T, e *transcriptEnv) *transcript.Transcript {
				tr, err := transcript.New(transcript.Spec{
					ID: "job-2", RecordingID: "no-such-recording", ModelID: "m1", ModelName: "base", ModelVersion: "1",
					Text: e.newTranscript(t, "job-2", "").Text(),
				}, transcriptT0)
				if err != nil {
					t.Fatal(err)
				}
				return tr
			},
			want: shared.ErrConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newTranscriptEnv(t)
			tr := tc.prepare(t, e)
			before := tr.Version()
			if err := e.repo.Save(context.Background(), tr); !errors.Is(err, tc.want) {
				t.Fatalf("Save = %v, want %v", err, tc.want)
			}
			if tr.Version() != before {
				t.Errorf("a failed Save changed the version: %d -> %d", before, tr.Version())
			}
		})
	}
}

func TestTranscriptGetMissing(t *testing.T) {
	e := newTranscriptEnv(t)
	if _, err := e.repo.Get(context.Background(), "none"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

// TestTranscriptKeepsModelIdentityAfterRemoval is AC-08: the model name and version stay readable after the model row
// is removed; only the reference is cleared.
func TestTranscriptKeepsModelIdentityAfterRemoval(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	if err := e.repo.Save(ctx, e.newTranscript(t, "job-1", "ru")); err != nil {
		t.Fatal(err)
	}
	if err := e.models.Delete(ctx, "m1"); err != nil {
		t.Fatalf("Delete model: %v", err)
	}

	got, err := e.repo.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ModelID() != "" || got.ModelName() != "base" || got.ModelVersion() != "1" {
		t.Fatalf("after removal: model %q %q %q, want \"\" base 1", got.ModelID(), got.ModelName(), got.ModelVersion())
	}

	// A save after the removal keeps the cleared reference instead of failing on the foreign key.
	if err := e.repo.Save(ctx, got); err != nil {
		t.Fatalf("Save after removal: %v", err)
	}
}

func TestTranscriptFilesListedForRecordingDeletion(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	tr := e.newTranscript(t, "job-1", "ru")
	if err := e.repo.Save(ctx, tr); err != nil {
		t.Fatal(err)
	}
	refs, err := NewRecordingTranscripts(e.db).ListArtifactsByRecording(ctx, e.rec.ID())
	if err != nil || len(refs) != 1 || refs[0] != tr.Text() {
		t.Fatalf("ListArtifactsByRecording = %v, %v; want [%s]", refs, err, tr.Text())
	}
}

func TestTranscriptCorruptRow(t *testing.T) {
	tests := []struct {
		name, update string
	}{
		{"bad document reference", "UPDATE transcripts SET text_ref = 'nokind'"},
		{"document of another kind", "UPDATE transcripts SET text_ref = 'recordings/x.json'"},
		{"bad language", "UPDATE transcripts SET language = 'RU'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, ctx := newTranscriptEnv(t), context.Background()
			if err := e.repo.Save(ctx, e.newTranscript(t, "job-1", "ru")); err != nil {
				t.Fatal(err)
			}
			if _, err := e.db.sql.ExecContext(ctx, tc.update); err != nil {
				t.Fatal(err)
			}
			if _, err := e.repo.Get(ctx, "job-1"); err == nil || errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("Get of a corrupt row = %v, want a restore error", err)
			}
		})
	}
}
