package sqlite

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// silentTestRecording is a COMPLETED recording id whose track is silent (TASK-082).
func silentTestRecording(t *testing.T, id string) *recording.Recording {
	t.Helper()
	r := newTestRecording(t, id, "")
	at := recT0.Add(time.Hour)
	track := values.AssembledTrack{Duration: time.Hour, Seconds: 3600, AudibleSeconds: 0}
	for _, step := range []func() error{
		func() error { return r.Begin(recT0) }, func() error { return r.Stop(at) }, func() error { return r.Finalize(at) },
		func() error { return r.CompleteTrack(at, recRef(t, "recordings/"+id+".wav"), track) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if !r.Silent() {
		t.Fatal("test bug: the recording is not silent")
	}
	return r
}

// A silent recording keeps its mark through the store, and the transcription sweep skips it.
func TestRecordingSilent(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	silent := silentTestRecording(t, "r-silent")
	saveRecording(t, repo, silent)
	saveRecording(t, repo, completedTestRecording(t, "r-sound"))

	got, err := repo.Get(ctx, "r-silent")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Silent() || !reflect.DeepEqual(got, silent) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, silent)
	}
	if n := scanInt(t, db.sql, "SELECT silent FROM recordings WHERE id = 'r-silent'"); n != 1 {
		t.Fatalf("stored silent = %d", n)
	}

	ids, err := repo.ListUntranscribed(ctx, "", 10)
	if err != nil || !slices.Equal(ids, []shared.RecordingID{"r-sound"}) {
		t.Fatalf("ListUntranscribed = %v, %v; want only the recording with sound", ids, err)
	}
}

// The schema refuses other values than 0 and 1.
func TestRecordingSilentCheck(t *testing.T) {
	db := openTest(t, nil)
	saveRecording(t, NewRecordingRepository(db), completedTestRecording(t, "r1"))
	if _, err := db.sql.ExecContext(context.Background(), "UPDATE recordings SET silent = 2 WHERE id = 'r1'"); err == nil {
		t.Fatal("silent = 2 was stored")
	}
}

// insertOldRecording stores a COMPLETED recording id with only the columns every schema version has, for the tests
// of migrations below the latest version.
func insertOldRecording(t *testing.T, db *DB, id string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, audio_ref, version,
		created_at, updated_at) VALUES ('`+id+`', 'COMPLETED', 0, 1000, 'recordings/`+id+`', 'recordings/`+id+`.wav', 1, 0, 0)`)
}

// Version 12 adds recordings.silent, 0 for the existing rows, and goes back down keeping them.
func TestMigrationToVersion12AddsSilent(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 11); err != nil {
		t.Fatalf("up to 11: %v", err)
	}
	insertOldRecording(t, db, "r-old")

	if _, err := p.UpTo(ctx, 12); err != nil {
		t.Fatalf("up to 12: %v", err)
	}
	// Read by SQL: the repository needs the columns of later versions.
	if n := scanInt(t, db.sql, "SELECT silent FROM recordings WHERE id = 'r-old'"); n != 0 {
		t.Fatalf("existing recording silent = %d; want 0", n)
	}

	if _, err := p.DownTo(ctx, 11); err != nil {
		t.Fatalf("down to 11: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('recordings') WHERE name = 'silent'"); n != 0 {
		t.Fatal("recordings.silent still exists at version 11")
	}
	if n := countRecordings(t, db); n != 1 {
		t.Fatalf("recordings = %d, want the recording kept", n)
	}
}
