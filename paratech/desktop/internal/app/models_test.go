package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// The wired model manager (AC 3, TASK-060): the embedded catalog lists no model and offers no download, a file the
// user picked is installed by the manual import, and the default and the pin of an STT job go through the core.
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

	if list, err := c.models.ListAvailable(ctx, values.Machine{}); err != nil || len(list) != 0 {
		t.Fatalf("ListAvailable = %+v, %v; want the empty catalog of this build", list, err)
	}
	// Refused as an unknown catalog id before any request: the empty catalog allows no download host either.
	if _, err := c.downloads.Preflight(ctx, "base-q5_1"); !errors.Is(err, modelcatalog.ErrNotInCatalog) {
		t.Fatalf("Preflight = %v, want ErrNotInCatalog", err)
	}
	if _, err := c.downloads.Download(ctx, "base-q5_1"); !errors.Is(err, modelcatalog.ErrNotInCatalog) {
		t.Fatalf("Download = %v, want ErrNotInCatalog", err)
	}
	if list, err := c.models.ListInstalled(ctx); err != nil || len(list) != 0 {
		t.Fatalf("ListInstalled = %+v, %v; want nothing installed", list, err)
	}

	first := importModel(t, c, "custom", testenv.WhisperOK)
	second := importModel(t, c, "small", "other weights")
	resolver := modelResolver{models: c.models}
	if _, err := resolver.ResolveForJob(ctx, ""); err == nil {
		t.Fatal("two models without a default must not resolve")
	}
	if err := c.models.SetDefault(ctx, first); err != nil {
		t.Fatal(err)
	}
	if got, err := resolver.ResolveForJob(ctx, ""); err != nil || got != first {
		t.Fatalf("ResolveForJob = %q, %v; want the default %q", got, err, first)
	}
	if got, err := resolver.ResolveForJob(ctx, second); err != nil || got != second {
		t.Fatalf("ResolveForJob(%q) = %q, %v", second, got, err)
	}
	list, err := c.models.ListInstalled(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListInstalled = %+v, %v", list, err)
	}
	for _, m := range list {
		if m.IsDefault != (m.ID == first) || m.CatalogID != "" {
			t.Fatalf("installed %+v: only %q is the default, neither comes from the catalog", m, first)
		}
	}
}

// importModel installs a model file through the wired importer: Inspect, then ImportCustom with the confirmation of
// the digest Inspect showed. The content selects the fake whisper-cli scenario.
func importModel(t *testing.T, c *core, name values.ModelName, content string) shared.SttModelID {
	t.Helper()
	ctx := context.Background()
	picked := filepath.Join(t.TempDir(), "ggml-"+string(name)+".bin")
	if err := os.WriteFile(picked, []byte(content), 0o600); err != nil {
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
	m, err := c.imports.ImportCustom(ctx, modelcatalog.CustomImport{
		Path: picked, Name: name, ModelVersion: "1", Confirmation: confirmed,
	})
	if err != nil {
		t.Fatalf("ImportCustom: %v", err)
	}
	if m.SHA256 != preview.SHA256 || m.SizeBytes != int64(len(content)) {
		t.Fatalf("ImportCustom = %+v, want the inspected file", m)
	}
	return m.ID
}
