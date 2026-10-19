package tests

// Scenario tests of TASK-094 on the real stack: one active generation per assignment in the database, and the Word
// file of a version with the title page from the stored profile.

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/testenv"
)

// Concurrent Generate clicks with different instructions make one homework_draft job in the database; the others are
// refused with ErrGenerationRunning or get that job. The handler is not registered, so the job stays queued.
func TestConcurrentDifferentGenerationsMakeOneJob(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e, drafts := newDraftsEnv(t, llm, false)
	a := firstAssignment(e)

	const clicks = 6
	errs := make([]error, clicks)
	created := make([]bool, clicks)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range clicks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := drafts.Generate(context.Background(), a, fmt.Sprintf("variant %d", i))
			errs[i], created[i] = err, res.Created
		}()
	}
	close(start)
	wg.Wait()

	n := 0
	for i := range clicks {
		if errs[i] != nil && !errors.Is(errs[i], homeworkflow.ErrGenerationRunning) {
			t.Fatalf("click %d: %v", i, errs[i])
		}
		if created[i] {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, j := range jobs {
		if j.Type() == homeworkflow.HomeworkDraftJobType {
			stored++
		}
	}
	if stored != 1 {
		t.Fatalf("homework_draft jobs in the database = %d, want 1", stored)
	}
	if v, ok, err := drafts.ActiveGeneration(context.Background(), a); err != nil || !ok || v.Status != job.StatusQueued {
		t.Fatalf("ActiveGeneration = %+v, %v, %v", v, ok, err)
	}
}

// The Word file of a version, saved where the student chose and as the Word export, is a valid package whose title
// page names the stored profile.
func TestDocxOfAVersion(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	llm.Answer(values.OperationHomeworkDraft, `{"title":"Chapter 4","body":"# Chapter 4\n\nDeltas & <rivers>.\n\n## Example\nThe Volga."}`)
	e, drafts := newDraftsEnv(t, llm, true)
	ctx := context.Background()
	a := firstAssignment(e)

	repo := sqlite.NewSettingsRepository(e.db)
	st, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := settings.NewProfile(settings.ProfileFields{FullName: "Иванов Иван", Group: "ИВТ-21", University: "МГУ", City: "Москва"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProfile(p, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, st); err != nil {
		t.Fatal(err)
	}

	res, err := drafts.Generate(ctx, a, "")
	if err != nil {
		t.Fatal(err)
	}
	waitJob(e, res.JobID, job.StatusCompleted)

	path := filepath.Join(t.TempDir(), "Chapter 4.docx")
	if err := drafts.SaveDocx(ctx, a, 1, path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := documentXML(t, saved)
	for _, want := range []string{"МГУ", "Выполнил: студент группы ИВТ-21 Иванов Иван", "Москва 20", "Deltas &amp; &lt;rivers&gt;.", "Heading2"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %q", want)
		}
	}

	exp, err := drafts.ExportDocx(ctx, a, 1)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := e.store.Open(ctx, exp.File)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	exported, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exported, saved) {
		t.Fatal("the Word export differs from the saved file of the same version")
	}
}

func documentXML(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatal("no word/document.xml")
	return ""
}
