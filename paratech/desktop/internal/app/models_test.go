package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// The wired model manager (AC 3, TASK-060): installed models, the default and the pin of an STT job work through the
// core; the catalog of this build is empty (no verified SHA-256 yet), so the catalog list, the download and the import
// refuse with the fixed not-configured error and copy nothing.
func TestWiredModelManager(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	c := a.core
	if c.models == nil || c.downloads == nil || c.imports == nil {
		t.Fatal("the model manager is not wired")
	}

	if _, err := c.models.ListAvailable(ctx, values.Machine{}); !errors.Is(err, errNotConfigured) {
		t.Fatalf("ListAvailable = %v, want the empty catalog of this build", err)
	}
	if _, err := c.downloads.Preflight(ctx, "base-q5_1"); !errors.Is(err, errNotConfigured) {
		t.Fatalf("Preflight = %v, want the empty catalog of this build", err)
	}

	picked := filepath.Join(t.TempDir(), "ggml-custom.bin")
	if err := os.WriteFile(picked, []byte(testenv.WhisperOK), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := c.imports.Inspect(ctx, picked)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	confirmed, err := values.ConfirmImport(preview.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.imports.ImportCustom(ctx, modelcatalog.CustomImport{
		Path: picked, Name: "custom", ModelVersion: "1", Confirmation: confirmed,
	})
	if !errors.Is(err, errNotConfigured) {
		t.Fatalf("ImportCustom = %v, want the empty catalog of this build", err)
	}
	if list, err := c.models.ListInstalled(ctx); err != nil || len(list) != 0 {
		t.Fatalf("ListInstalled = %+v, %v; want nothing installed", list, err)
	}

	// A model installed as a file: the default and the pin go through the wired Service.
	first := installModel(t, a, "m1", testenv.WhisperOK)
	second := installModelNamed(t, a, "m2", "small")
	resolver := modelResolver{models: c.models}
	if _, err := resolver.ResolveForJob(ctx, ""); err == nil {
		t.Fatal("two models without a default must not resolve")
	}
	if err := c.models.SetDefault(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got, err := resolver.ResolveForJob(ctx, ""); err != nil || got != second {
		t.Fatalf("ResolveForJob = %q, %v; want the default %q", got, err, second)
	}
	if got, err := resolver.ResolveForJob(ctx, first); err != nil || got != first {
		t.Fatalf("ResolveForJob(%q) = %q, %v", first, got, err)
	}
	list, err := c.models.ListInstalled(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListInstalled = %+v, %v", list, err)
	}
	for _, m := range list {
		if m.IsDefault != (m.ID == second) {
			t.Fatalf("installed %+v: only %q is the default", m, second)
		}
	}
}

// installModelNamed stores an installed model of another name than installModel's.
func installModelNamed(t *testing.T, a *App, id shared.SttModelID, name values.ModelName) shared.SttModelID {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, "ggml-"+string(id)+".bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.store.Write(context.Background(), file, strings.NewReader(testenv.WhisperOK)); err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: id, Name: name, ModelVersion: "1", File: file, SizeBytes: int64(len(testenv.WhisperOK)),
		SHA256: values.SHA256(strings.Repeat("cd", 32)),
	}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewModelRepository(a.core.db).Save(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return id
}
