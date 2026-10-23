package library

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/library/subject"
)

// TestFileHomework: a paper the app made lands in «ДЗ» of its subject, once for the same bytes; without a subject (or
// with one that is gone) it stays in «Входящие».
func TestFileHomework(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := createSubject(t, e, "История")
	dz, _ := sub.SystemFolder(subject.SystemHomework)

	m, err := e.svc.FileHomework(ctx, sub.ID(), "Реферат.docx", docx(t))
	if err != nil || m.Subject() != sub.ID() || m.Folder() != dz.ID() || m.Name() != "Реферат.docx" {
		t.Fatalf("FileHomework = %+v, %v", m, err)
	}
	again, err := e.svc.FileHomework(ctx, sub.ID(), "Реферат.docx", docx(t))
	if err != nil || again.ID() != m.ID() || again.Folder() != dz.ID() {
		t.Fatalf("again = %+v, %v", again, err)
	}

	moved, err := e.svc.MoveToInbox(ctx, m.ID())
	if err != nil || !moved.InInbox() {
		t.Fatal(err)
	}
	inbox, err := e.svc.FileHomework(ctx, "", "Реферат.docx", docx(t))
	if err != nil || !inbox.InInbox() {
		t.Fatalf("no subject = %+v, %v", inbox, err)
	}
	gone, err := e.svc.FileHomework(ctx, "missing", "Реферат.docx", docx(t))
	if err != nil || !gone.InInbox() {
		t.Fatalf("a gone subject = %+v, %v", gone, err)
	}
}
