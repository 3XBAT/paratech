package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

var modelT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

// newModel builds a never-stored catalog model with its file at models/<id>.bin.
func newModel(t *testing.T, id, name, version string) *sttmodel.Model {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, id+".bin")
	if err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: shared.SttModelID(id), Name: values.ModelName(name), ModelVersion: values.ModelVersion(version),
		CatalogID: values.CatalogID(name + "-q5_1"), File: file, SizeBytes: 1234,
		SHA256:       values.SHA256(fmt.Sprintf("%064x", len(id)+len(name)+len(version))),
		Quantization: "q5_1", Capabilities: []values.Capability{"multilingual", "en"},
	}, modelT0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type modelView struct {
	ID, Name, Version, Catalog, File, SHA, Quant string
	Size, StoredVersion                          int64
	Caps                                         []values.Capability
	InstalledAt                                  time.Time
}

func modelViewOf(m *sttmodel.Model) modelView {
	return modelView{
		ID: string(m.ID()), Name: string(m.Name()), Version: string(m.ModelVersion()), Catalog: string(m.CatalogID()),
		File: m.File().String(), SHA: string(m.SHA256()), Quant: string(m.Quantization()), Size: m.SizeBytes(),
		StoredVersion: m.Version(), Caps: m.Capabilities(), InstalledAt: m.InstalledAt(),
	}
}

func saveModel(t *testing.T, r *ModelRepository, m *sttmodel.Model) {
	t.Helper()
	if err := r.Save(context.Background(), m); err != nil {
		t.Fatalf("Save %s: %v", m.ID(), err)
	}
}

func TestModelSaveGetList(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()

	small, base := newModel(t, "m2", "small", "1"), newModel(t, "m1", "base", "1")
	saveModel(t, r, small)
	saveModel(t, r, base)
	if small.Version() != 1 || base.Version() != 1 {
		t.Fatalf("versions after insert: %d %d, want 1", small.Version(), base.Version())
	}

	got, err := r.Get(ctx, "m2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(modelViewOf(got), modelViewOf(small)) {
		t.Fatalf("Get = %+v, want %+v", modelViewOf(got), modelViewOf(small))
	}

	list, err := r.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID() != "m1" || list[1].ID() != "m2" {
		t.Fatalf("List = %v, %v; want m1 (base) then m2 (small)", list, err)
	}

	if _, err := r.Get(ctx, "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get missing: %v, want shared.ErrNotFound", err)
	}
}

func TestModelSaveImportedWithoutOptionalFields(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	file, _ := shared.NewArtifactRef(shared.ArtifactModels, "own.bin")
	m, err := sttmodel.New(sttmodel.Spec{
		ID: "own", Name: "own", ModelVersion: "1", File: file, SizeBytes: 1, SHA256: values.SHA256(fmt.Sprintf("%064x", 7)),
	}, modelT0)
	if err != nil {
		t.Fatal(err)
	}
	saveModel(t, r, m)

	got, err := r.Get(ctx, "own")
	if err != nil || got.CatalogID() != "" || got.Quantization() != "" || len(got.Capabilities()) != 0 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if caps := scanString(t, db, "SELECT capabilities FROM stt_models WHERE id = 'own'"); caps != "[]" {
		t.Fatalf("stored capabilities %q, want []", caps)
	}
}

func TestModelSaveVersionProtocol(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	m := newModel(t, "m1", "base", "1")
	saveModel(t, r, m)

	saveModel(t, r, m) // the same instance again: compare-and-swap on its own write
	if m.Version() != 2 {
		t.Fatalf("version after the second save = %d, want 2", m.Version())
	}

	stale, err := r.Get(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	saveModel(t, r, m)
	if err := r.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale save: %v, want shared.ErrVersionConflict", err)
	}

	sameID := newModel(t, "m1", "other", "1")
	if err := r.Save(ctx, sameID); !errors.Is(err, shared.ErrVersionConflict) || sameID.Version() != 0 {
		t.Fatalf("insert of an existing id: %v (version %d), want shared.ErrVersionConflict", err, sameID.Version())
	}

	if err := r.Delete(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, m); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("save of a deleted model: %v, want shared.ErrVersionConflict (never re-inserted)", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM stt_models"); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func TestModelSaveDuplicateNameAndVersion(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	saveModel(t, r, newModel(t, "m1", "base", "1"))

	dup := newModel(t, "m2", "base", "1")
	err := r.Save(ctx, dup)
	if !errors.Is(err, shared.ErrDuplicate) || errors.Is(err, shared.ErrVersionConflict) || dup.Version() != 0 {
		t.Fatalf("second base 1: %v (version %d), want shared.ErrDuplicate only", err, dup.Version())
	}

	saveModel(t, r, newModel(t, "m3", "base", "2")) // another version of the same model is fine
	if n := scanInt(t, db.sql, "SELECT count(*) FROM stt_models"); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestModelDelete(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	saveModel(t, r, newModel(t, "m1", "base", "1"))

	if err := r.Delete(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, "m1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if err := r.Delete(ctx, "m1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("second Delete: %v, want shared.ErrNotFound", err)
	}
}

// TestModelDeleteRestrictedByDefault pins ADR-0004(g): the database refuses to remove the settings default.
func TestModelDeleteRestrictedByDefault(t *testing.T) {
	db := openTest(t, nil)
	r, sr, ctx := NewModelRepository(db), NewSettingsRepository(db), context.Background()
	saveModel(t, r, newModel(t, "m1", "base", "1"))

	s := settings.New()
	if err := s.SelectDefaultSttModel("m1", modelT0); err != nil {
		t.Fatal(err)
	}
	if err := sr.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	err := r.Delete(ctx, "m1")
	if !errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrVersionConflict) || errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("Delete of the default: %v, want a plain shared.ErrConflict", err)
	}
	if _, err := r.Get(ctx, "m1"); err != nil {
		t.Fatalf("the default was removed: %v", err)
	}

	if err := s.ClearDefaultSttModel(modelT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := sr.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "m1"); err != nil {
		t.Fatalf("Delete after clearing the default: %v", err)
	}

	missing := settings.New()
	if err := missing.SelectDefaultSttModel("m1", modelT0); err != nil {
		t.Fatal(err)
	}
	if err := sr.Save(ctx, missing); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("selecting a removed model: %v, want shared.ErrConflict", err)
	}
}

// TestModelDeleteKeepsTranscriptCopies pins the ON DELETE SET NULL of transcripts.model_id (AC-08).
func TestModelDeleteKeepsTranscriptCopies(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	saveModel(t, r, newModel(t, "m1", "base", "1"))
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, created_at, updated_at)
		VALUES ('r1', 'FAILED', 1, 1000, 'recordings/r1', 1, 1)`)
	mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_id, model_name, model_version, text_ref, created_at)
		VALUES ('t1', 'r1', 'm1', 'base', '1', 'transcripts/t1', 1)`)

	if err := r.Delete(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	row := scanString(t, db, "SELECT coalesce(model_id, '<null>') || ' ' || model_name || ' ' || model_version FROM transcripts")
	if row != "<null> base 1" {
		t.Fatalf("transcript after the model removal: %q, want the copied name and version without the reference", row)
	}
}

func TestModelSaveRolledBackWithTheOuterTransaction(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewModelRepository(db), context.Background()
	boom := errors.New("boom")

	err := db.Do(ctx, func(ctx context.Context) error {
		if err := r.Save(ctx, newModel(t, "m1", "base", "1")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do = %v", err)
	}
	if _, err := r.Get(ctx, "m1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the save survived the rollback: %v", err)
	}
}

func TestModelGetRejectsCorruptRow(t *testing.T) {
	cases := map[string]string{
		"uppercase digest": `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at, version)
			VALUES ('bad', 'base', '1', 'models/bad.bin', 1, replace(hex(zeroblob(32)), '0', 'A'), 1, 1)`,
		"file outside models": `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at, version)
			VALUES ('bad', 'base', '1', 'exports/bad.bin', 1, lower(hex(zeroblob(32))), 1, 1)`,
		"traversal in file": `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at, version)
			VALUES ('bad', 'base', '1', 'models/../bad.bin', 1, lower(hex(zeroblob(32))), 1, 1)`,
		"version zero": `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at, version)
			VALUES ('bad', 'base', '1', 'models/bad.bin', 1, lower(hex(zeroblob(32))), 1, 0)`,
		"capabilities not a list": `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, capabilities, installed_at, version)
			VALUES ('bad', 'base', '1', 'models/bad.bin', 1, lower(hex(zeroblob(32))), '{}', 1, 1)`,
	}
	for name, insert := range cases {
		t.Run(name, func(t *testing.T) {
			db := openTest(t, nil)
			r, ctx := NewModelRepository(db), context.Background()
			mustExec(t, db, insert)
			if m, err := r.Get(ctx, "bad"); err == nil {
				t.Fatalf("Get = %+v, want an error", m)
			}
			if _, err := r.List(ctx); err == nil {
				t.Fatal("List of a corrupt row succeeded")
			}
		})
	}
}

func TestModelsPersistAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	want := newModel(t, "m1", "base", "1")

	first := openAt(t, path, nil)
	saveModel(t, NewModelRepository(first), want)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openAt(t, path, nil)
	got, err := NewModelRepository(second).Get(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(modelViewOf(got), modelViewOf(want)) {
		t.Fatalf("after restart %+v, want %+v", modelViewOf(got), modelViewOf(want))
	}
}

// insertJobRow stores a job in the given status that pins model (empty: none).
func insertJobRow(t *testing.T, db *DB, id string, status job.Status, model string) {
	t.Helper()
	var finished any
	if status.IsTerminal() {
		finished = int64(1)
	}
	var pinned any
	if model != "" {
		pinned = model
	}
	mustExec(t, db, `INSERT INTO jobs (id, type, status, idempotency_key, pinned_model_id, max_attempts, next_run_at,
		created_at, finished_at, updated_at, version) VALUES (?, 'stt', ?, ?, ?, 3, 1, 1, ?, 1, 1)`,
		id, string(status), "key-"+id, pinned, finished)
}

func TestModelPins(t *testing.T) {
	for _, status := range job.Statuses() {
		t.Run(string(status), func(t *testing.T) {
			db := openTest(t, nil)
			pins, ctx := NewModelPins(db), context.Background()
			insertJobRow(t, db, "j1", status, "m1")
			insertJobRow(t, db, "j2", job.StatusRunning, "m2")
			insertJobRow(t, db, "j3", job.StatusRunning, "")

			got, err := pins.IsPinnedByJobInStatuses(ctx, "m1", job.ActiveStatuses())
			if err != nil {
				t.Fatal(err)
			}
			if want := !status.IsTerminal(); got != want {
				t.Fatalf("pinned by a %s job = %v, want %v", status, got, want)
			}

			only, err := pins.IsPinnedByJobInStatuses(ctx, "m1", []job.Status{status})
			if err != nil || !only {
				t.Fatalf("pinned when asking for %s only = %v, %v; want true", status, only, err)
			}
			none, err := pins.IsPinnedByJobInStatuses(ctx, "m1", nil)
			if err != nil || none {
				t.Fatalf("pinned for no status = %v, %v; want false", none, err)
			}
			other, err := pins.IsPinnedByJobInStatuses(ctx, "m9", job.ActiveStatuses())
			if err != nil || other {
				t.Fatalf("pinned for a model without jobs = %v, %v; want false", other, err)
			}
		})
	}
}

func TestModelPinsSeesJobsOfTheEnclosingTransaction(t *testing.T) {
	db := openTest(t, nil)
	pins, jobs, ctx := NewModelPins(db), NewJobRepository(db), context.Background()

	err := db.Do(ctx, func(ctx context.Context) error {
		j, err := job.New(job.Spec{ID: "j1", Type: "stt", Key: "k1", PinnedModelID: "m1", MaxAttempts: 1}, modelT0)
		if err != nil {
			return err
		}
		if _, _, err := jobs.Enqueue(ctx, j); err != nil {
			return err
		}
		pinned, err := pins.IsPinnedByJobInStatuses(ctx, "m1", job.ActiveStatuses())
		if err != nil {
			return err
		}
		if !pinned {
			return errors.New("a job enqueued in the same transaction is not seen")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scanString(t *testing.T, db *DB, query string) string {
	t.Helper()
	var s string
	if err := db.sql.QueryRowContext(context.Background(), query).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return s
}
