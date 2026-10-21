package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

func parts(t *testing.T, from int, texts ...string) []transcript.Part {
	t.Helper()
	out := make([]transcript.Part, len(texts))
	for i, text := range texts {
		n := from + i
		p, err := transcript.NewPart(n, transcript.Segment{StartMs: int64(n) * 1000, EndMs: int64(n)*1000 + 500, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		out[i] = p
	}
	return out
}

func partTexts(ps []transcript.Part) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Segment().Text
	}
	return out
}

func listAll(t *testing.T, r *TranscriptParts, rec shared.RecordingID) []string {
	t.Helper()
	got, err := r.ListParts(context.Background(), rec, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return partTexts(got)
}

func TestTranscriptPartsSaveListCount(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	saveRecording(t, NewRecordingRepository(db), recordingInStatus(t, recording.StatusCompleted))
	r := NewTranscriptParts(db)
	const rec = shared.RecordingID("r-COMPLETED")

	if n, err := r.CountParts(ctx, rec); err != nil || n != 0 {
		t.Fatalf("count of none = %d, %v", n, err)
	}
	if err := r.SaveParts(ctx, rec, parts(t, 0, "Привет", "мир", "и всё")); err != nil {
		t.Fatalf("SaveParts: %v", err)
	}
	if err := r.SaveParts(ctx, rec, parts(t, 3, "дальше")); err != nil {
		t.Fatalf("SaveParts: %v", err)
	}

	got, err := r.ListParts(ctx, rec, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq() != 1 || got[0].Segment() != (transcript.Segment{StartMs: 1000, EndMs: 1500, Text: "мир"}) ||
		got[1].Seq() != 2 {
		t.Fatalf("page = %+v", got)
	}
	if n, err := r.CountParts(ctx, rec); err != nil || n != 4 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if got, err := r.ListParts(ctx, rec, 10, 5); err != nil || len(got) != 0 {
		t.Fatalf("page past the end = %v, %v", got, err)
	}
	if _, err := r.ListParts(ctx, rec, -1, 5); err == nil {
		t.Fatal("negative offset accepted")
	}
}

// A rerun numbers from 0 again: its parts replace the earlier ones by number, the rest stays until replaced.
func TestTranscriptPartsUpsertOnRerun(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	saveRecording(t, NewRecordingRepository(db), recordingInStatus(t, recording.StatusCompleted))
	r := NewTranscriptParts(db)
	const rec = shared.RecordingID("r-COMPLETED")

	if err := r.SaveParts(ctx, rec, parts(t, 0, "a0", "a1", "a2")); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveParts(ctx, rec, parts(t, 0, "b0", "b1")); err != nil {
		t.Fatal(err)
	}
	if got := listAll(t, r, rec); !reflect.DeepEqual(got, []string{"b0", "b1", "a2"}) {
		t.Fatalf("parts = %v", got)
	}
}

// The parts survive a restart: a new connection and repository read them (TASK-079 AC 2).
func TestTranscriptPartsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	seedFromTemplate(t, path)
	first, err := Open(ctx, Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRecordingRepository(first).Save(ctx, recordingInStatus(t, recording.StatusCompleted)); err != nil {
		t.Fatal(err)
	}
	if err := NewTranscriptParts(first).SaveParts(ctx, "r-COMPLETED", parts(t, 0, "до", "перезапуска")); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openAt(t, path, nil)
	if got := listAll(t, NewTranscriptParts(second), "r-COMPLETED"); !reflect.DeepEqual(got, []string{"до", "перезапуска"}) {
		t.Fatalf("after restart = %v", got)
	}
}

func TestTranscriptPartsDeleteAndCascade(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	recs := NewRecordingRepository(db)
	saveRecording(t, recs, recordingInStatus(t, recording.StatusCompleted))
	saveRecording(t, recs, recordingInStatus(t, recording.StatusFailed))
	r := NewTranscriptParts(db)
	for _, rec := range []shared.RecordingID{"r-COMPLETED", "r-FAILED"} {
		if err := r.SaveParts(ctx, rec, parts(t, 0, "x", "y")); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 { // idempotent
		if err := r.DeleteParts(ctx, "r-COMPLETED"); err != nil {
			t.Fatalf("DeleteParts: %v", err)
		}
	}
	if n, _ := r.CountParts(ctx, "r-COMPLETED"); n != 0 {
		t.Fatalf("%d parts left", n)
	}

	// Deleting the recording deletes its parts (ON DELETE CASCADE).
	failed, err := recs.Get(ctx, "r-FAILED")
	if err != nil {
		t.Fatal(err)
	}
	if err := recs.Delete(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM transcript_parts"); n != 0 {
		t.Fatalf("%d parts left after the recording was deleted", n)
	}
}

func TestTranscriptPartsRefusals(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	saveRecording(t, NewRecordingRepository(db), recordingInStatus(t, recording.StatusCompleted))
	r := NewTranscriptParts(db)

	// An unknown recording is a foreign key conflict, and the whole batch is rolled back.
	if err := r.SaveParts(ctx, "nope", parts(t, 0, "x")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("unknown recording: %v, want ErrConflict", err)
	}

	// A corrupt row is reported, not shown.
	mustExec(t, db, `INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text) VALUES ('r-COMPLETED', 0, 0, 1, ?)`, "bad\nline")
	if _, err := r.ListParts(ctx, "r-COMPLETED", 0, 10); !errors.Is(err, transcript.ErrInvalidDocument) {
		t.Fatalf("corrupt row: %v", err)
	}

	for _, stmt := range []string{
		`INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text) VALUES ('r-COMPLETED', -1, 0, 1, 'x')`,
		`INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text) VALUES ('r-COMPLETED', 5, 10, 1, 'x')`,
		`INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text) VALUES ('r-COMPLETED', 6, -1, 1, 'x')`,
	} {
		if _, err := db.sql.ExecContext(ctx, stmt); !errors.Is(MapError(ctx, err), ErrConstraint) {
			t.Errorf("%s: %v, want ErrConstraint", stmt, err)
		}
	}
}

func TestLatestTranscriptByRecording(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	if _, err := e.repo.LatestByRecording(ctx, e.rec.ID()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("none: %v, want ErrNotFound", err)
	}

	older := e.newTranscript(t, "job-1", "ru")
	if err := e.repo.Save(ctx, older); err != nil {
		t.Fatal(err)
	}
	text, _ := transcript.DocumentRef("job-2")
	newer, err := transcript.New(transcript.Spec{
		ID: "job-2", RecordingID: e.rec.ID(), ModelID: "m1", ModelName: "base", ModelVersion: "1", Text: text,
	}, transcriptT0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Save(ctx, newer); err != nil {
		t.Fatal(err)
	}

	got, err := e.repo.LatestByRecording(ctx, e.rec.ID())
	if err != nil || got.ID() != "job-2" {
		t.Fatalf("latest = %v, %v; want job-2", got, err)
	}
}

func TestLatestJobByKeyPrefix(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	r := NewJobRepository(db)
	if _, err := r.LatestByKeyPrefix(ctx, "stt", "stt:r1:"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("none: %v, want ErrNotFound", err)
	}

	jobT := func(id, key string, at time.Time) {
		j, err := job.New(job.Spec{ID: shared.JobID(id), Type: "stt", Key: key, MaxAttempts: 3}, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	jobT("j1", "stt:r1:m1", jobT0)
	jobT("j2", "stt:r1:m2", jobT0.Add(time.Minute))
	jobT("j3", "stt:r10:m1", jobT0.Add(2*time.Minute)) // another recording whose id starts the same
	jobT("j4", "stt:r2:m1", jobT0.Add(3*time.Minute))

	got, err := r.LatestByKeyPrefix(ctx, "stt", "stt:r1:")
	if err != nil || got.ID() != "j2" {
		t.Fatalf("latest of r1 = %v, %v; want j2", got, err)
	}
	if _, err := r.LatestByKeyPrefix(ctx, "other", "stt:r1:"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("other type: %v, want ErrNotFound", err)
	}
}

// Version 9 adds transcript_parts to a database of version 8 and goes back down.
func TestMigrationToVersion9AddsTranscriptParts(t *testing.T) {
	t.Parallel() // migrates a database of its own for real
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 8); err != nil {
		t.Fatalf("up to 8: %v", err)
	}
	// A row of the version 8 schema: the repository writes the columns of the latest version.
	insertOldRecording(t, db, "r-COMPLETED")

	if _, err := p.UpTo(ctx, 9); err != nil {
		t.Fatalf("up to 9: %v", err)
	}
	if v, err := db.SchemaVersion(ctx); err != nil || v != 9 {
		t.Fatalf("SchemaVersion = %d, %v; want 9", v, err)
	}
	if err := NewTranscriptParts(db).SaveParts(ctx, "r-COMPLETED", parts(t, 0, "x")); err != nil {
		t.Fatalf("SaveParts at version 9: %v", err)
	}

	if _, err := p.DownTo(ctx, 8); err != nil {
		t.Fatalf("down to 8: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM sqlite_master WHERE name = 'transcript_parts'"); n != 0 {
		t.Fatal("transcript_parts still exists at version 8")
	}
	if n := countRecordings(t, db); n != 1 {
		t.Fatalf("recordings = %d, want the recording kept", n)
	}
}
