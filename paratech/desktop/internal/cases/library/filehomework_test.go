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

// TestFileTickets: the answers to a ticket set land in «Билеты» of their subject (TASK-133), never in «ДЗ».
func TestFileTickets(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := createSubject(t, e, "Физика")
	tickets, _ := sub.SystemFolder(subject.SystemTickets)

	m, err := e.svc.FileTickets(ctx, sub.ID(), "Билеты Физика.docx", docx(t))
	if err != nil || m.Subject() != sub.ID() || m.Folder() != tickets.ID() {
		t.Fatalf("FileTickets = %+v, %v", m, err)
	}
	if again, err := e.svc.FileTickets(ctx, sub.ID(), "Билеты Физика.docx", docx(t)); err != nil || again.ID() != m.ID() {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if again, err := e.svc.FileHomework(ctx, sub.ID(), "Билеты Физика.docx", docx(t)); err != nil || again.Folder() == tickets.ID() {
		t.Fatalf("the same file as homework = %+v, %v", again, err)
	}
}
