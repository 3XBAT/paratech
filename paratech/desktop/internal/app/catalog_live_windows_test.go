//go:build windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/input/wails"
)

// liveCatalogID is the smallest model of the embedded catalog.
const liveCatalogID = "base-q5_1"

// TestLiveCatalogDownload downloads base-q5_1 from Hugging Face through the models page as the app wires it (embedded
// catalog, modelcatalog.Downloader, adapters/httpdownload, filestore, SQLite) and checks the installed file against
// the catalog SHA-256 (TASK-067). About 60 MB; manual only.
func TestLiveCatalogDownload(t *testing.T) {
	if os.Getenv("LA_MODEL_DOWNLOAD_LIVE_TEST") != "1" {
		t.Skip("set LA_MODEL_DOWNLOAD_LIVE_TEST=1 to download ggml-base-q5_1 from Hugging Face")
	}
	ctx := context.Background()
	dataDir := t.TempDir()
	a, _ := newTestAppWith(t, dataDir)
	page := modelsPage(startCore(t, a), &modelPicker{})

	entry, ok := embeddedEntries(t).Entry(liveCatalogID)
	if !ok {
		t.Fatalf("%s is not in the embedded catalog", liveCatalogID)
	}
	plan, err := page.PreflightDownload(ctx, liveCatalogID)
	if err != nil || !plan.Enough {
		t.Fatalf("PreflightDownload = %+v, %v", plan, err)
	}

	started := time.Now()
	if err := page.StartDownload(ctx, liveCatalogID); err != nil {
		t.Fatal(err)
	}
	var view wails.ModelsView
	for deadline := started.Add(10 * time.Minute); ; {
		if view, err = page.List(ctx); err != nil {
			t.Fatal(err)
		}
		if !view.Download.Running {
			break
		}
		if time.Now().After(deadline) {
			page.CancelDownload()
			t.Fatalf("download not finished in 10 minutes: %+v", view.Download)
		}
		time.Sleep(time.Second)
	}
	t.Logf("download %+v in %s", view.Download, time.Since(started).Round(time.Second))
	if view.Download.Phase != "installed" || view.Download.Error != "" {
		t.Fatalf("download ended %+v, want installed", view.Download)
	}

	if len(view.Installed) != 1 {
		t.Fatalf("installed %+v, want the downloaded model only", view.Installed)
	}
	m := view.Installed[0]
	if !m.FromCatalog || m.CatalogID != liveCatalogID || m.SHA256 != entry.SHA256().String() || m.SizeBytes != entry.SizeBytes() {
		t.Fatalf("installed %+v, want %s with sha256 %s and %d bytes", m, liveCatalogID, entry.SHA256(), entry.SizeBytes())
	}

	// The file on disk, hashed here independently of the downloader.
	var sums []string
	err = filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".bin") {
			return err
		}
		sums = append(sums, fileSHA256(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0] != entry.SHA256().String() {
		t.Fatalf("model files on disk with sha256 %v, want one with %s", sums, entry.SHA256())
	}
	t.Logf("installed %s, sha256 %s, %d bytes", m.Name, m.SHA256, m.SizeBytes)
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
