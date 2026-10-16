package app

// The homework page on the wired core (TASK-045; PRD section 10, AC-13, AC-14, AC-15): real SQLite, the Storage
// Manager and the smtp adapter against a fake SMTP server. The page's calls go through the binding the shell
// registers, so the path is the one the window takes.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

func homeworkPage(a *App) *wails.HomeworkService {
	return wails.NewHomeworkService(a.core.ui.Homework, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// edit -> export -> PrepareSend (dialog) -> ConfirmSend -> the email is sent and visible in Deliveries. Nothing is sent
// before the confirmation, a second confirmation sends nothing, and a cancelled send cannot be confirmed.
func TestHomeworkPageSendsOnlyAfterTheConfirmation(t *testing.T) {
	ctx := context.Background()
	mail := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: testMailUser, Password: testMailPassword})
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	a.env.mailRoots = mail.RootCAs()
	startCore(t, a)
	insertAssignment(t, dir, "a1", homeworkTitle)
	if err := a.core.deliveries.SetMailPassword(ctx, testMailPassword); err != nil {
		t.Fatal(err)
	}
	configureMailAndHub(t, a, "", mail.Host(), mail.Port())
	page := homeworkPage(a)

	list, err := page.ListAssignments(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "a1" || list[0].Title != homeworkTitle {
		t.Fatalf("ListAssignments = %+v, %v", list, err)
	}
	v, err := page.EditDraft(ctx, "a1", 0, "Rivers", "Deltas form where rivers reach the sea.")
	if err != nil || v.Number != 1 || !v.Latest {
		t.Fatalf("EditDraft = %+v, %v", v, err)
	}
	opened, err := page.OpenAssignment(ctx, "a1")
	if err != nil || len(opened.Versions) != 1 || opened.Latest == nil || opened.Latest.Body != v.Body {
		t.Fatalf("OpenAssignment = %+v, %v", opened, err)
	}
	exp, err := page.ExportDraft(ctx, "a1", 1)
	if err != nil || exp.FileName != "homework.md" {
		t.Fatalf("ExportDraft = %+v, %v", exp, err)
	}
	info, err := os.Stat(exportFile(dir))
	if err != nil {
		t.Fatal(err)
	}

	prep, err := page.PrepareSend(ctx, "a1")
	if err != nil || prep.Status != "ready" || prep.Recipient != teacher || prep.Subject != senddelivery.DefaultSubject ||
		prep.AttachmentName != "homework.md" || prep.AttachmentSize != info.Size() {
		t.Fatalf("PrepareSend = %+v, %v", prep, err)
	}
	if deliveries, _, _ := sideEffects(t, dir); deliveries != 0 || len(mail.Messages()) != 0 {
		t.Fatalf("before the confirmation: %d deliveries, %d messages", deliveries, len(mail.Messages()))
	}

	if err := page.ConfirmSend(ctx, prep.ProposalID); err != nil {
		t.Fatalf("ConfirmSend: %v", err)
	}
	testenv.Eventually(t, "the email is sent", func() bool {
		ds, err := page.Deliveries(ctx, "a1")
		return err == nil && len(ds) == 1 && ds[0].Status == "SENT"
	}, nil)
	ds, err := page.Deliveries(ctx, "a1")
	if err != nil || ds[0].Recipient != teacher || ds[0].CanMarkSent || ds[0].CanSendAgain || ds[0].Notice != "" ||
		ds[0].SentAt == "" || len(mail.Messages()) != 1 {
		t.Fatalf("Deliveries = %+v, %v, messages %d", ds, err, len(mail.Messages()))
	}

	if err := page.ConfirmSend(ctx, prep.ProposalID); !errors.Is(err, wails.ErrSendCannotConfirm) {
		t.Fatalf("second ConfirmSend = %v", err)
	}
	again, err := page.PrepareSend(ctx, "a1")
	if err != nil || again.Status != "ready" {
		t.Fatalf("PrepareSend = %+v, %v", again, err)
	}
	if err := page.CancelSend(ctx, again.ProposalID); err != nil {
		t.Fatal(err)
	}
	if err := page.ConfirmSend(ctx, again.ProposalID); !errors.Is(err, wails.ErrSendCannotConfirm) {
		t.Fatalf("ConfirmSend after CancelSend = %v", err)
	}
	if deliveries, _, _ := sideEffects(t, dir); deliveries != 1 || len(mail.Messages()) != 1 {
		t.Fatalf("after the repeats: %d deliveries, %d messages, want 1 and 1", deliveries, len(mail.Messages()))
	}
}

// Without a recipient or an export the dialog explains why and offers nothing to confirm; nothing is stored as a
// delivery.
func TestHomeworkPageRejectsASendItCannotMake(t *testing.T) {
	ctx := context.Background()
	t.Run("no recipient", func(t *testing.T) {
		a, dir := proposalCore(t, "")
		page := homeworkPage(a)
		if _, err := page.EditDraft(ctx, "a1", 0, "Rivers", "Deltas."); err != nil {
			t.Fatal(err)
		}
		if _, err := page.ExportDraft(ctx, "a1", 1); err != nil {
			t.Fatal(err)
		}
		prep, err := page.PrepareSend(ctx, "a1")
		if err != nil || prep.Status != "rejected" || !strings.Contains(prep.Message, "«Подключения»") || prep.Recipient != "" {
			t.Fatalf("PrepareSend = %+v, %v", prep, err)
		}
		if err := page.ConfirmSend(ctx, prep.ProposalID); !errors.Is(err, wails.ErrSendCannotConfirm) {
			t.Fatalf("ConfirmSend of a rejected send = %v", err)
		}
		if deliveries, jobs, _ := sideEffects(t, dir); deliveries != 0 || jobs != 0 {
			t.Fatalf("%d deliveries, %d jobs, want none", deliveries, jobs)
		}
	})
	t.Run("nothing exported", func(t *testing.T) {
		a, dir := proposalCore(t, teacher)
		prep, err := homeworkPage(a).PrepareSend(ctx, "a1")
		if err != nil || prep.Status != "rejected" || prep.Reason != "homework_artifact_missing" {
			t.Fatalf("PrepareSend = %+v, %v", prep, err)
		}
		if deliveries, _, files := sideEffects(t, dir); deliveries != 0 || files != 0 {
			t.Fatalf("%d deliveries, %d files, want none", deliveries, files)
		}
	})
}

// AC-15: an email with an unknown outcome offers both resolutions with the owner's text, and "mark as sent" resolves
// it without sending.
func TestHomeworkPageResolvesAnUnknownOutcome(t *testing.T) {
	ctx := context.Background()
	a, dir := proposalCore(t, teacher) // not started: the delivery stays as planted
	page := homeworkPage(a)
	if _, err := page.EditDraft(ctx, "a1", 0, "Rivers", "Deltas."); err != nil {
		t.Fatal(err)
	}
	if _, err := page.ExportDraft(ctx, "a1", 1); err != nil {
		t.Fatal(err)
	}
	prep, err := page.PrepareSend(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if err := page.ConfirmSend(ctx, prep.ProposalID); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB(t, dir).Exec("UPDATE deliveries SET status = 'UNKNOWN', attempts = 1, version = version + 1"); err != nil {
		t.Fatal(err)
	}

	ds, err := page.Deliveries(ctx, "a1")
	if err != nil || len(ds) != 1 {
		t.Fatalf("Deliveries = %+v, %v", ds, err)
	}
	u := ds[0]
	if u.Status != "UNKNOWN" || !u.CanMarkSent || !u.CanSendAgain ||
		u.Notice != "Письмо отправлено, но подтверждение не получено: проверьте сами или отправьте ещё раз под свою ответственность" {
		t.Fatalf("unknown delivery = %+v", u)
	}
	if again, err := page.PrepareSend(ctx, "a1"); err != nil {
		t.Fatal(err)
	} else if err := page.ConfirmSend(ctx, again.ProposalID); !errors.Is(err, wails.ErrSendUnresolved) {
		t.Fatalf("a new send while one is unknown = %v, want ErrSendUnresolved", err)
	}

	sent, err := page.MarkSent(ctx, u.ID)
	if err != nil || sent.Status != "SENT" || sent.CanMarkSent || sent.CanSendAgain || sent.Notice != "" {
		t.Fatalf("MarkSent = %+v, %v", sent, err)
	}
	if _, err := page.SendAgain(ctx, u.ID); !errors.Is(err, wails.ErrDeliveryActionInvalid) {
		t.Fatalf("SendAgain of a sent email = %v", err)
	}
	if deliveries, _, _ := sideEffects(t, dir); deliveries != 1 {
		t.Fatalf("%d deliveries, want 1", deliveries)
	}
}
