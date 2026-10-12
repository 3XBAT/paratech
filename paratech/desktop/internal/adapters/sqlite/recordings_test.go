package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

var recT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func newTestRecording(t *testing.T, id string, lesson shared.LessonID) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), LessonID: lesson, MaxDuration: 3 * time.Hour}, recT0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func recRef(t *testing.T, s string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// recordingInStatus builds a recording that reached s by legal moves, with every optional field set where the status
// allows. Times and durations are whole milliseconds, the precision of the schema.
func recordingInStatus(t *testing.T, s recording.Status) *recording.Recording {
	t.Helper()
	r := newTestRecording(t, "r-"+string(s), "")
	at := recT0.Add(time.Second)
	begin := func() error { return r.Begin(at) }
	stop := func() error { return r.Stop(at.Add(time.Hour)) }
	finalize := func() error { return r.Finalize(at.Add(time.Hour)) }
	chunks := func() error { return r.ObserveChunks(60, at.Add(time.Hour)) }
	steps := map[recording.Status][]func() error{
		recording.StatusStarting:   nil,
		recording.StatusRecording:  {begin, func() error { return r.Heartbeat(at.Add(time.Minute)) }},
		recording.StatusStopping:   {begin, stop},
		recording.StatusFinalizing: {begin, func() error { return r.Interrupt(at.Add(time.Hour), "output device changed") }, chunks},
		recording.StatusCompleted: {begin, stop, finalize, chunks, func() error {
			return r.Complete(at.Add(time.Hour+time.Second), recRef(t, "recordings/r-COMPLETED.wav"), time.Hour-1500*time.Millisecond)
		}},
		recording.StatusFailed:           {begin, stop, finalize, chunks, func() error { return r.Fail(at.Add(time.Hour), "disk full") }},
		recording.StatusRecoveryRequired: {begin, func() error { return r.RequireRecovery(at.Add(time.Hour), "stale heartbeat") }},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			t.Fatalf("reaching %s: %v", s, err)
		}
	}
	if r.Status() != s {
		t.Fatalf("reached %s, want %s", r.Status(), s)
	}
	return r
}

func saveRecording(t *testing.T, repo *RecordingRepository, r *recording.Recording) {
	t.Helper()
	if err := repo.Save(context.Background(), r); err != nil {
		t.Fatalf("Save %s: %v", r.ID(), err)
	}
}

func countRecordings(t *testing.T, db *DB) int64 {
	return scanInt(t, db.sql, "SELECT count(*) FROM recordings")
}

func TestRecordingAllStatusesRoundTrip(t *testing.T) {
	repo, ctx := NewRecordingRepository(openTest(t, nil)), context.Background()
	for _, s := range recording.Statuses() {
		t.Run(s.String(), func(t *testing.T) {
			r := recordingInStatus(t, s)
			saveRecording(t, repo, r)
			if r.Version() != 1 {
				t.Fatalf("version after insert = %d", r.Version())
			}
			got, err := repo.Get(ctx, r.ID())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, r) {
				t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
			}
		})
	}
}

func TestRecordingSaveSameInstanceRepeatedly(t *testing.T) {
	repo, ctx := NewRecordingRepository(openTest(t, nil)), context.Background()
	r := newTestRecording(t, "r1", "")
	saveRecording(t, repo, r)

	steps := []func() error{
		func() error { return r.Begin(recT0.Add(time.Second)) },
		func() error { return r.Stop(recT0.Add(time.Hour)) },
		func() error { return r.Finalize(recT0.Add(time.Hour)) },
		func() error { return r.Complete(recT0.Add(time.Hour), recRef(t, "recordings/r1.wav"), time.Hour) },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		saveRecording(t, repo, r)
		if r.Version() != int64(i+2) {
			t.Fatalf("version after save %d = %d", i+2, r.Version())
		}
	}
	got, err := repo.Get(ctx, "r1")
	if err != nil || got.Status() != recording.StatusCompleted || got.Version() != 5 {
		t.Fatalf("stored: %v %v", got, err)
	}
}

func TestRecordingStaleAndPurgedSaves(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	saveRecording(t, repo, recordingInStatus(t, recording.StatusRecording))

	a, _ := repo.Get(ctx, "r-RECORDING")
	b, _ := repo.Get(ctx, "r-RECORDING")
	if err := b.RequireRecovery(recT0.Add(2*time.Hour), "stale heartbeat"); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, b)

	if err := a.Stop(recT0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	err := repo.Save(ctx, a)
	if !errors.Is(err, shared.ErrVersionConflict) || a.Version() != 1 {
		t.Fatalf("stale Save = %v, version %d; want ErrVersionConflict and no version change", err, a.Version())
	}

	if err := repo.Save(ctx, newTestRecording(t, "r-RECORDING", "")); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("insert of an existing id = %v, want ErrVersionConflict", err)
	}

	mustExec(t, db, "DELETE FROM recordings")
	if err := repo.Save(ctx, b); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Save after purge = %v, want ErrVersionConflict", err)
	}
	if n := countRecordings(t, db); n != 0 {
		t.Fatalf("a purged recording was inserted again: %d rows", n)
	}
}

func TestRecordingGetMissingAndCorrupt(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}

	// The schema accepts the row, the aggregate does not: the chunk directory is outside recordings/.
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, version, created_at, updated_at)
		VALUES ('bad', 'FAILED', 1, 1000, 'secrets/bad', 1, 1, 1)`)
	if _, err := repo.Get(ctx, "bad"); err == nil {
		t.Fatal("a corrupt row was restored")
	}
	if _, err := repo.ListByStatus(ctx, recording.StatusFailed); err == nil {
		t.Fatal("a corrupt row was listed")
	}
}

func TestRecordingListByStatus(t *testing.T) {
	repo, ctx := NewRecordingRepository(openTest(t, nil)), context.Background()
	for _, s := range recording.Statuses() {
		saveRecording(t, repo, recordingInStatus(t, s))
	}
	cases := []struct {
		name     string
		statuses []recording.Status
		want     int
	}{
		{"none", nil, 0},
		{"one", []recording.Status{recording.StatusRecording}, 1},
		{"unfinished", recording.UnfinishedStatuses(), 5},
		{"finished", []recording.Status{recording.StatusCompleted, recording.StatusFailed}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.ListByStatus(ctx, tc.statuses...)
			if err != nil || len(got) != tc.want {
				t.Fatalf("ListByStatus = %d recordings, %v; want %d", len(got), err, tc.want)
			}
			for _, r := range got {
				if !slicesContains(tc.statuses, r.Status()) {
					t.Fatalf("listed a %s recording", r.Status())
				}
			}
		})
	}
}

func slicesContains(list []recording.Status, s recording.Status) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestRecordingSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db := openAt(t, path, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	saveRecording(t, repo, recordingInStatus(t, recording.StatusRecording))
	saveRecording(t, repo, recordingInStatus(t, recording.StatusCompleted))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repo2 := NewRecordingRepository(openAt(t, path, nil))
	stale, err := repo2.ListByStatus(ctx, recording.UnfinishedStatuses()...)
	if err != nil || len(stale) != 1 || stale[0].ID() != "r-RECORDING" {
		t.Fatalf("unfinished recordings after restart: %v %v", stale, err)
	}
	// The recovery of an interrupted recording is a domain transition saved with the stored version.
	if err := stale[0].RequireRecovery(recT0.Add(5*time.Hour), "stale heartbeat"); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo2, stale[0])
	got, err := repo2.Get(ctx, "r-RECORDING")
	if err != nil || got.Status() != recording.StatusRecoveryRequired || got.Problem() != "stale heartbeat" {
		t.Fatalf("after recovery: %v %v", got, err)
	}
}

func TestRecordingLessonReference(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	if err := repo.Save(ctx, newTestRecording(t, "r0", "missing")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("unknown lesson = %v, want ErrConflict", err)
	}

	mustExec(t, db, `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, created_at, updated_at)
		VALUES ('l1', 'Math', 0, 1, 'UTC', 0, 0)`)
	r := newTestRecording(t, "r1", "l1")
	saveRecording(t, repo, r)

	// Removing the lesson clears the copy of its id; a later save of the loaded aggregate does not restore it.
	mustExec(t, db, "DELETE FROM schedule_entries WHERE id = 'l1'")
	if err := r.Begin(recT0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, r)
	got, err := repo.Get(ctx, "r1")
	if err != nil || got.LessonID() != "" || got.Status() != recording.StatusRecording {
		t.Fatalf("after the lesson was removed: %v %v", got, err)
	}
}

func TestRecordingDeleteIsVersionGuarded(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	saveRecording(t, repo, recordingInStatus(t, recording.StatusFailed))
	stale, _ := repo.Get(ctx, "r-FAILED")
	stale.Persisted(stale.Version() - 1)

	if err := repo.Delete(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) || countRecordings(t, db) != 1 {
		t.Fatalf("stale Delete = %v, rows %d", err, countRecordings(t, db))
	}
	cur, _ := repo.Get(ctx, "r-FAILED")
	if err := repo.Delete(ctx, cur); err != nil || countRecordings(t, db) != 0 {
		t.Fatalf("Delete = %v, rows %d", err, countRecordings(t, db))
	}
	if err := repo.Delete(ctx, cur); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Delete of a gone row = %v, want ErrVersionConflict", err)
	}
}

func TestRecordingTranscriptsAndDeleteInOneTransaction(t *testing.T) {
	db := openTest(t, nil)
	repo, tr, ctx := NewRecordingRepository(db), NewRecordingTranscripts(db), context.Background()
	saveRecording(t, repo, recordingInStatus(t, recording.StatusCompleted))
	saveRecording(t, repo, recordingInStatus(t, recording.StatusFailed))
	for _, row := range [][2]string{{"t1", "r-COMPLETED"}, {"t2", "r-COMPLETED"}, {"t3", "r-FAILED"}} {
		mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_name, model_version, text_ref, created_at)
			VALUES (?, ?, 'small', '1', ?, 0)`, row[0], row[1], "transcripts/"+row[0]+".txt")
	}

	refs, err := tr.ListArtifactsByRecording(ctx, "r-COMPLETED")
	if err != nil || len(refs) != 2 || refs[0].String() != "transcripts/t1.txt" || refs[1].String() != "transcripts/t2.txt" {
		t.Fatalf("ListArtifactsByRecording = %v, %v", refs, err)
	}
	if refs, err := tr.ListArtifactsByRecording(ctx, "none"); err != nil || len(refs) != 0 {
		t.Fatalf("no transcripts = %v, %v", refs, err)
	}

	// A refusal inside the transaction rolls back the transcript delete too.
	refused := errors.New("refused")
	err = db.Do(ctx, func(ctx context.Context) error {
		if err := tr.DeleteByRecording(ctx, "r-COMPLETED"); err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) || scanInt(t, db.sql, "SELECT count(*) FROM transcripts") != 3 {
		t.Fatalf("rollback: %v", err)
	}

	err = db.Do(ctx, func(ctx context.Context) error {
		r, err := repo.Get(ctx, "r-COMPLETED")
		if err != nil {
			return err
		}
		if err := tr.DeleteByRecording(ctx, r.ID()); err != nil {
			return err
		}
		return repo.Delete(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM transcripts"); n != 1 {
		t.Fatalf("transcripts left = %d, want the one of the other recording", n)
	}
	if err := tr.DeleteByRecording(ctx, "r-COMPLETED"); err != nil {
		t.Fatalf("repeated DeleteByRecording = %v", err)
	}
}
