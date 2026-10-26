package app

// «Отправить преподавателю» of a finished turnkey work on the wired core (TASK-180): real SQLite, the library and the
// Storage Manager in a temporary directory; the core is opened but not started, so no job runs and nothing leaves the
// machine.

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// docxBytes is a minimal Word package: the library tells the type by its main part.
func docxBytes(t *testing.T, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<w:document>" + text + "</w:document>"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// doneWork files data as the paper «Реферат.docx» in «ДЗ» of a new subject and stores the done work id of src whose
// file it is.
func doneWork(t *testing.T, a *App, id shared.WorkID, src work.Source, data []byte) *material.Material {
	t.Helper()
	ctx := context.Background()
	lib := a.core.ui.Library
	sub, err := lib.CreateSubject(ctx, library.NewSubject{Name: "История " + string(id)})
	if err != nil {
		t.Fatal(err)
	}
	im, err := lib.Import(ctx, library.Upload{Name: "Реферат.docx", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	dz, _ := sub.SystemFolder(subject.SystemHomework)
	m, err := lib.Place(ctx, im.Material.ID(), sub.ID(), dz.ID())
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	w, err := work.New(id, src, now)
	if err != nil {
		t.Fatal(err)
	}
	o, err := work.ParseOutline(`{"topic":"Русь","sections":[{"title":"Введение","words":200}]}`)
	if err != nil {
		t.Fatal(err)
	}
	text, err := work.ParseSection(`{"paragraphs":["Абзац."],"sources":[]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	answer := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: work.FeasibleFull,
		Questions: []work.Question{}, Assumptions: []string{"Объём по умолчанию"},
	}
	blob, err := material.BlobRef(m.Digest())
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return w.Record(1, answer, now) },
		func() error { return w.ConfirmRequirements(now) },
		func() error { return w.StartWriting(work.Snapshot{Subject: sub.ID(), SubjectName: "История", Title: "Реферат"}, now) },
		func() error { return w.SetOutline(o, now) },
		func() error { return w.ApproveOutline(now) },
		func() error { return w.WriteSection(1, 1, text, now) },
		func() error {
			return w.Finish(work.File{Material: m.ID(), Artifact: blob.String(), Content: w.ContentKey()}, now)
		},
		func() error { return sqlite.NewWorkRepository(a.core.db).Save(ctx, w) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func confirmCards(a *App) *wails.AgentConfirmService {
	return wails.NewAgentConfirmService(a.core.ui.AgentConfirm, slog.New(slog.DiscardHandler))
}

// The card asks, «Да» sends the copy of the work's file under its name, once; a second «Да» sends nothing more.
func TestWiredSendWorkCreatesOneDelivery(t *testing.T) {
	ctx := context.Background()
	a, dir := proposalCore(t, teacher)
	data := docxBytes(t, "Реферат о Руси")
	doneWork(t, a, "w1", work.Source{Assignment: "a1"}, data)
	cards := confirmCards(a)

	card, err := cards.ProposeSendWork(ctx, "w1")
	if err != nil || card.State != "waiting" || card.Kind != "send" || card.Recipient != teacher ||
		card.AttachmentName != "Реферат.docx" || card.AttachmentFormat != "docx" || card.AttachmentSize != int64(len(data)) {
		t.Fatalf("card = %+v, %v", card, err)
	}
	if d, j, _ := sideEffects(t, dir); d != 0 || j != 0 {
		t.Fatalf("%d deliveries, %d jobs before «Да»", d, j)
	}
	for click := range 2 {
		if done, err := cards.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat); err != nil || done.State != "done" {
			t.Fatalf("click %d = %+v, %v", click+1, done, err)
		}
	}

	ref := "exports/sent/" + card.TargetID + "/Реферат.docx"
	raw := rawDB(t, dir)
	if n := count(t, raw, `SELECT count(*) FROM deliveries WHERE idempotency_key = ? AND recipient = ? AND artifact_ref = ?
		AND status = 'PENDING'`, "proposal:"+card.TargetID, teacher, ref); n != 1 {
		t.Fatalf("%d deliveries of the copy, want 1", n)
	}
	if d, j, _ := sideEffects(t, dir); d != 1 || j != 1 {
		t.Fatalf("%d deliveries, %d jobs; want 1 and 1", d, j)
	}
	copyPath := filepath.Join(dir, artifactsDir, "exports", "sent", card.TargetID, "Реферат.docx")
	if got, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("copy = %d bytes, %v", len(got), err)
	}
}

// Hard rule 7: the app stopped after the delivery was created but before the proposal was marked EXECUTED, and the
// outcome of the email is UNKNOWN. «Да» again finds the delivery by the proposal's key and sends nothing.
func TestWiredSendWorkUnknownOutcomeIsNotResent(t *testing.T) {
	ctx := context.Background()
	a, dir := proposalCore(t, teacher)
	doneWork(t, a, "w1", work.Source{Assignment: "a1"}, docxBytes(t, "Реферат"))
	cards := confirmCards(a)
	card, err := cards.ProposeSendWork(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cards.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat); err != nil {
		t.Fatal(err)
	}
	raw := rawDB(t, dir)
	if _, err := raw.Exec("UPDATE deliveries SET status = 'UNKNOWN', attempts = 1, version = version + 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE proposals SET status = 'CONFIRMED', executed_at = NULL, version = version + 1 WHERE id = ?", card.TargetID); err != nil {
		t.Fatal(err)
	}

	if done, err := cards.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat); err != nil || done.State != "done" {
		t.Fatalf("«Да» after the crash = %+v, %v", done, err)
	}
	if n := count(t, raw, "SELECT count(*) FROM deliveries WHERE status = 'UNKNOWN' AND attempts = 1"); n != 1 {
		t.Fatalf("%d UNKNOWN deliveries with one attempt, want the one, unresolved", n)
	}
	if d, j, _ := sideEffects(t, dir); d != 1 || j != 1 {
		t.Fatalf("%d deliveries, %d jobs; want 1 and 1", d, j)
	}
}

// A work without an assignment is refused (no_assignment); a file moved out of «ДЗ» makes the confirmed send stale.
func TestWiredSendWorkRefusals(t *testing.T) {
	ctx := context.Background()
	a, dir := proposalCore(t, teacher)
	cards := confirmCards(a)

	upload, err := a.core.ui.Library.Import(ctx, library.Upload{Name: "задание.txt", Data: []byte("Реферат о Руси")})
	if err != nil {
		t.Fatal(err)
	}
	doneWork(t, a, "w-upload", work.Source{Material: upload.Material.ID()}, docxBytes(t, "Работа по файлу"))
	if _, err := cards.ProposeSendWork(ctx, "w-upload"); !errors.Is(err, wails.ErrAgentConfirmNoAssignment) {
		t.Fatalf("work without an assignment = %v", err)
	}
	if n := count(t, rawDB(t, dir), "SELECT count(*) FROM proposals"); n != 0 {
		t.Fatalf("%d proposals stored", n)
	}

	m := doneWork(t, a, "w1", work.Source{Assignment: "a1"}, docxBytes(t, "Реферат"))
	card, err := cards.ProposeSendWork(ctx, "w1")
	if err != nil || card.State != "waiting" {
		t.Fatalf("card = %+v, %v", card, err)
	}
	if _, err := a.core.ui.Library.MoveToInbox(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := cards.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat); !errors.Is(err, wails.ErrSendStale) {
		t.Fatalf("«Да» after the move = %v", err)
	}
	if got, err := a.core.proposals.Get(ctx, proposal.ID(card.TargetID)); err != nil || got.Reason != proposal.ReasonFileMoved {
		t.Fatalf("proposal = %+v, %v", got, err)
	}
	if d, j, _ := sideEffects(t, dir); d != 0 || j != 0 {
		t.Fatalf("%d deliveries, %d jobs", d, j)
	}
}
