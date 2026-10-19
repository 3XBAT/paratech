package wails

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// TASK-094: the dialog prepares and confirms the send with the format the user chose.
func TestSendWithAFormat(t *testing.T) {
	ctx := context.Background()
	e := newHWEnv()
	e.proposals.proposal.AttachmentName, e.proposals.proposal.AttachmentFormat = "homework.docx", proposal.FormatDocx
	got, err := e.svc.PrepareSend(ctx, "a1", "docx")
	if err != nil || got.Status != "ready" || got.AttachmentFormat != "docx" || got.AttachmentName != "homework.docx" ||
		e.proposals.format != proposal.FormatDocx {
		t.Fatalf("PrepareSend(docx) = %+v, %v; use case got %q", got, err, e.proposals.format)
	}
	if err := e.svc.ConfirmSend(ctx, "p1", got.Subject, got.AttachmentFormat); err != nil || e.proposals.shownFormat != proposal.FormatDocx {
		t.Fatalf("ConfirmSend = %v, shown format %q", err, e.proposals.shownFormat)
	}

	for _, bad := range []string{"", "pdf", "DOCX", "md"} {
		e := newHWEnv()
		if _, err := e.svc.PrepareSend(ctx, "a1", bad); !errors.Is(err, ErrSendFormat) {
			t.Errorf("PrepareSend(%q) = %v, want ErrSendFormat", bad, err)
		}
		if err := e.svc.ConfirmSend(ctx, "p1", "ДЗ", bad); !errors.Is(err, ErrSendFormat) {
			t.Errorf("ConfirmSend(%q) = %v, want ErrSendFormat", bad, err)
		}
		if calls := e.calls.list(); len(calls) != 0 {
			t.Errorf("format %q reached the use case: %v", bad, calls)
		}
	}

	// No Word file yet: the dialog says how to make one.
	e = newHWEnv()
	e.proposals.proposal = homeworkflow.ProposalView{
		ID: "p2", Kind: proposal.KindSendHomework, Status: proposal.StatusRejected, Reason: proposal.ReasonNoArtifact,
		AttachmentFormat: proposal.FormatDocx,
	}
	if got, err := e.svc.PrepareSend(ctx, "a1", "docx"); err != nil || got.Status != "rejected" || got.Message != msgNoDocx {
		t.Fatalf("no Word file: %+v, %v", got, err)
	}
}

func TestSaveDocx(t *testing.T) {
	ctx := context.Background()
	e := newHWEnv()
	got, err := e.svc.SaveDocx(ctx, "a1", 2)
	want := DocxSaveResult{Status: "saved", Message: msgDocxSaved, Name: "Rivers.docx", AttachmentName: "homework.docx"}
	if err != nil || got != want {
		t.Fatalf("SaveDocx = %+v, %v; want %+v", got, err, want)
	}
	if e.picker.name != "Rivers.docx" || e.drafts.savedTo != e.picker.path {
		t.Fatalf("dialog proposed %q, saved to %q", e.picker.name, e.drafts.savedTo)
	}
	if calls := e.calls.list(); !slices.Equal(calls, []string{"DraftVersion", "DocxName", "SaveDocx", "ExportDocx"}) {
		t.Fatalf("calls = %v", calls)
	}

	e = newHWEnv()
	e.picker.path = ""
	if got, err := e.svc.SaveDocx(ctx, "a1", 2); err != nil || got.Status != "cancelled" || e.called("SaveDocx") {
		t.Fatalf("cancelled: %+v, %v, calls %v", got, err, e.calls.list())
	}

	e = newHWEnv()
	e.picker.err = errors.New("dialog failed")
	if got, err := e.svc.SaveDocx(ctx, "a1", 2); err != nil || got.Status != "failed" || e.called("SaveDocx") {
		t.Fatalf("dialog failure: %+v, %v", got, err)
	}

	e = newHWEnv()
	e.drafts.saveErr = errors.New(`textfile: write C:\Users\student\x.docx: access denied`)
	got, err = e.svc.SaveDocx(ctx, "a1", 2)
	if err != nil || got.Status != "failed" || got.Message != msgDocxFailed || strings.Contains(got.Message, "student") || e.called("ExportDocx") {
		t.Fatalf("write failure: %+v, %v", got, err)
	}

	// Saved, but the attachment is locked by an unresolved email.
	e = newHWEnv()
	e.drafts.exportErr = fmt.Errorf("x: %w", homeworkflow.ErrExportLocked)
	if got, err := e.svc.SaveDocx(ctx, "a1", 2); err != nil || got.Status != "saved" || got.AttachmentName != "" || got.Message != msgDocxNotAttached {
		t.Fatalf("locked attachment: %+v, %v", got, err)
	}

	e = newHWEnv()
	e.drafts.err = fmt.Errorf("x: %w", shared.ErrNotFound)
	if _, err := e.svc.SaveDocx(ctx, "a1", 9); !errors.Is(err, ErrDraftVersionNotFound) || e.called("DocxName") {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := newHWEnv().svc.SaveDocx(ctx, "a1", 0); !errors.Is(err, ErrDraftVersionNotFound) {
		t.Fatalf("version 0: %v", err)
	}

	noPicker := NewHomeworkService(newHWEnv().core(), discard())
	if got, err := noPicker.SaveDocx(ctx, "a1", 1); err != nil || got.Status != "failed" {
		t.Fatalf("without a dialog: %+v, %v", got, err)
	}
}

func TestExportDocx(t *testing.T) {
	ctx := context.Background()
	e := newHWEnv()
	if got, err := e.svc.ExportDocx(ctx, "a1", 2); err != nil || got != (ExportResult{Version: 2, FileName: "homework.docx"}) {
		t.Fatalf("ExportDocx = %+v, %v", got, err)
	}
	if _, err := e.svc.ExportDocx(ctx, "a1", 0); !errors.Is(err, ErrDraftVersionNotFound) {
		t.Fatalf("version 0: %v", err)
	}
	e.drafts.exportErr = fmt.Errorf("x: %w", homeworkflow.ErrExportLocked)
	if _, err := e.svc.ExportDocx(ctx, "a1", 2); !errors.Is(err, ErrExportLocked) {
		t.Fatalf("locked: %v", err)
	}
	e.drafts.exportErr = fmt.Errorf("x: %w", shared.ErrNotFound)
	if _, err := e.svc.ExportDocx(ctx, "a1", 2); !errors.Is(err, ErrDraftVersionNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

// TASK-094: the page learns from the core that a generation runs, also after it was left and opened again, and
// polls the latest version number to pick up a new one.
func TestGenerationStateOfTheAssignment(t *testing.T) {
	ctx := context.Background()
	e := newHWEnv()
	got, err := e.svc.OpenAssignment(ctx, "a1")
	if err != nil || got.Generation != nil {
		t.Fatalf("no generation: %+v, %v", got.Generation, err)
	}
	if st, err := e.svc.DraftStatus(ctx, "a1"); err != nil || st != (DraftStatus{}) {
		t.Fatalf("DraftStatus = %+v, %v", st, err)
	}

	e.drafts.active = &homeworkflow.GenerationView{JobID: "j7", Status: job.StatusRunning, Percent: 50}
	e.drafts.versions = []homeworkflow.DraftVersionView{
		{Number: 1, Origin: homeworkdraft.OriginGenerated, Title: "T1", Body: "B1", CreatedAt: hwT0},
		{Number: 2, Origin: homeworkdraft.OriginEdited, Title: "T2", Body: "B2", Latest: true, CreatedAt: hwT0},
	}
	want := &ActiveGeneration{JobID: "j7", State: "generating", Percent: 50}
	got, err = e.svc.OpenAssignment(ctx, "a1")
	if err != nil || got.Generation == nil || *got.Generation != *want {
		t.Fatalf("OpenAssignment generation = %+v, %v", got.Generation, err)
	}
	st, err := e.svc.DraftStatus(ctx, "a1")
	if err != nil || st.LatestVersion != 2 || st.Generation == nil || *st.Generation != *want {
		t.Fatalf("DraftStatus = %+v, %v", st, err)
	}

	e.drafts.active = &homeworkflow.GenerationView{JobID: "j7", Status: job.StatusWaitingForNetwork}
	if st, err := e.svc.DraftStatus(ctx, "a1"); err != nil || st.Generation.State != "waitingNetwork" || st.Generation.Message != msgWaitingNetwork {
		t.Fatalf("waiting: %+v, %v", st.Generation, err)
	}

	e.drafts.err = fmt.Errorf("x: %w", shared.ErrNotFound)
	if _, err := e.svc.DraftStatus(ctx, "a1"); !errors.Is(err, ErrHomeworkNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}
}

func TestGenerateWhileOneRuns(t *testing.T) {
	e := newHWEnv()
	e.drafts.err = fmt.Errorf("g: %w: job j1", homeworkflow.ErrGenerationRunning)
	if _, err := e.svc.Generate(context.Background(), "a1", 1, "longer"); !errors.Is(err, ErrGenerationRunning) {
		t.Fatalf("Generate = %v, want ErrGenerationRunning", err)
	}
	if !strings.Contains(ErrGenerationRunning.Error(), "Черновик уже готовится") {
		t.Fatalf("text = %q", ErrGenerationRunning)
	}
}

func TestGenerationStatusPercent(t *testing.T) {
	e := newHWEnv()
	e.drafts.generation = homeworkflow.GenerationView{JobID: "j1", Status: job.StatusRunning, Percent: 50}
	if got, err := e.svc.GenerationStatus(context.Background(), "j1"); err != nil || got.Percent != 50 || got.State != "generating" {
		t.Fatalf("GenerationStatus = %+v, %v", got, err)
	}
	e.drafts.generation = homeworkflow.GenerationView{JobID: "j1", Status: job.StatusCompleted}
	if got, _ := e.svc.GenerationStatus(context.Background(), "j1"); got.Percent != 100 {
		t.Fatalf("done percent = %d", got.Percent)
	}
}
