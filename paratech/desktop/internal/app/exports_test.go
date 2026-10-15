package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // database/sql driver "sqlite", for setup rows and the secret scan

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// insertAssignment stores an assignment row through its own connection: the coursework use cases that create one need
// a transcript, which this package does not build.
func insertAssignment(t *testing.T, dataDir string, id shared.AssignmentID, title string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dataDir, dbFile))+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`INSERT INTO assignments (id, title, description, version, created_at, updated_at)
		VALUES (?, ?, 'Pages 40-52', 1, 1, 1)`, string(id), title); err != nil {
		t.Fatal(err)
	}
}

// confirmedEmail is the command of a send_homework_email proposal of a that the user validated and confirmed, with the
// recipient and the export content pinned, as the proposals service does (proposals_test.go runs the service itself).
func confirmedEmail(t *testing.T, a *App, id shared.AssignmentID, recipient string) proposal.EmailCommand {
	t.Helper()
	ctx := context.Background()
	ref, err := proposal.ExportRef(id)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := a.core.store.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest, err := proposal.NewDigest(hex.EncodeToString(sum[:]), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := proposal.NewRecipient(recipient)
	if err != nil {
		t.Fatal(err)
	}

	now := a.env.clock.Now()
	p, err := proposal.New("p-"+proposal.ID(id), values.NewUntrustedOutput(`{"kind":"send_homework_email","assignment_id":"`+string(id)+`"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true, Recipient: r, Attachment: digest}, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm(now); err != nil {
		t.Fatal(err)
	}
	cmd, err := p.EmailCommand()
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

// AC-4 of TASK-057: the draft export lands below <DataDir>/artifacts/exports, the email attachment is that same file,
// and no artifact reference reaches the database next to the artifact root.
func TestExportsStayBelowTheArtifactRoot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	insertAssignment(t, dir, "a1", "Rivers")

	if _, err := a.core.drafts.EditDraft(ctx, homeworkflow.DraftEdit{Assignment: "a1", Title: "Rivers", Body: "Deltas."}); err != nil {
		t.Fatal(err)
	}
	exp, err := a.core.drafts.ExportDraft(ctx, "a1", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, artifactsDir, "exports", "a1", "homework.md")
	data, err := os.ReadFile(want)
	if err != nil || string(data) != "# Rivers\n\nDeltas.\n" {
		t.Fatalf("export file %s = %q, %v", want, data, err)
	}

	// The email of the same assignment attaches exactly that file.
	cmd := confirmedEmail(t, a, "a1", "teacher@school.example")
	if cmd.Attachment() != exp.File || exp.File.Kind() != shared.ArtifactExports {
		t.Fatalf("attachment %s, export %s: want the same exports/ file", cmd.Attachment(), exp.File)
	}
	path, err := a.core.store.Path(ctx, cmd.Attachment())
	if err != nil || !samePath(t, path, want) {
		t.Fatalf("attachment resolves to %q (%v), want %q", path, err, want)
	}

	// The database is beside the artifact root, not below it, and no reference can climb out of the root.
	root := filepath.Join(dir, artifactsDir)
	if rel, err := filepath.Rel(root, filepath.Join(dir, dbFile)); err != nil || !strings.HasPrefix(rel, "..") {
		t.Fatalf("database relative to the artifact root = %q, %v; want outside", rel, err)
	}
	kinds := []shared.ArtifactKind{shared.ArtifactRecordings, shared.ArtifactTranscripts, shared.ArtifactModels, shared.ArtifactExports}
	for _, kind := range kinds {
		for _, p := range []string{
			"../" + dbFile, "../../" + dbFile, "a1/../../" + dbFile, `..\` + dbFile, "..", "/" + dbFile,
			"C:/" + dbFile, "C:" + dbFile, filepath.Join(dir, dbFile),
		} {
			if ref, err := shared.NewArtifactRef(kind, p); err == nil {
				t.Errorf("NewArtifactRef(%s, %q) = %s, want refused", kind, p, ref)
			}
			if ref, err := shared.ParseArtifactRef(string(kind) + "/" + p); err == nil {
				t.Errorf("ParseArtifactRef(%q) = %s, want refused", string(kind)+"/"+p, ref)
			}
		}
		// A file named like the database is written and resolved below the root, never over the database.
		ref, err := shared.NewArtifactRef(kind, dbFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.core.store.Write(ctx, ref, strings.NewReader("not a database")); err != nil {
			t.Fatal(err)
		}
		got, err := a.core.store.Path(ctx, ref)
		if err != nil || samePath(t, got, filepath.Join(dir, dbFile)) || !below(t, root, got) {
			t.Fatalf("Path(%s) = %q, %v; want below %q", ref, got, err, root)
		}
	}
	if _, err := shared.ParseArtifactRef(dbFile); err == nil {
		t.Fatal("a reference without a kind was accepted")
	}
	if _, err := a.core.drafts.DraftVersions(ctx, "a1"); err != nil {
		t.Fatalf("the database after the writes: %v", err)
	}
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	x, err := filepath.Abs(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := filepath.Abs(b)
	if err != nil {
		t.Fatal(err)
	}
	return strings.EqualFold(filepath.Clean(x), filepath.Clean(y))
}

func below(t *testing.T, root, p string) bool {
	t.Helper()
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
