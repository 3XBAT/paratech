package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/shared"
)

var tsT0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func newTicketSet(t *testing.T, source shared.MaterialID) *ticketset.Set {
	t.Helper()
	items, err := ticketset.ParseList("1. Сходимость рядов\n2. Пределы <script>\n3. Интегралы")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ticketset.New("ts1", "sub1", source, items, tsT0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTicketSetSaveGet(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewTicketSetRepository(db)
	if _, err := repo.Get(ctx, "ts1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of none = %v", err)
	}

	s := newTicketSet(t, "m1")
	if err := repo.Save(ctx, s); err != nil || s.Version() != 1 {
		t.Fatalf("insert = %v, version %d", err, s.Version())
	}
	stale, err := repo.Get(ctx, "ts1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(false, tsT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(tsT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(s.Next(), ticketset.Result{Answer: "an1", Coverage: ticketset.CoveragePartial}, tsT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil || s.Version() != 2 {
		t.Fatalf("update = %v, version %d", err, s.Version())
	}
	got, err := repo.Get(ctx, "ts1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State(), s.State()) {
		t.Fatalf("round trip\n got %+v\nwant %+v", got.State(), s.State())
	}

	if err := stale.Cancel(tsT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale save = %v", err)
	}
	if err := repo.Save(ctx, newTicketSet(t, "")); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("a second insert of the id = %v", err)
	}

	pasted, err := ticketset.New("ts2", "sub1", "", []ticketset.Item{{Number: 1, Title: "А"}}, tsT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, pasted); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "ts2"); err != nil || got.Source() != "" {
		t.Fatalf("pasted = %+v, %v", got, err)
	}
}

// A row edited outside the app is refused on load: stored titles and results pass the same checks as fresh ones.
func TestTicketSetCorruptRow(t *testing.T) {
	tests := map[string]string{
		"unknown field":   `UPDATE ticket_sets SET content = json_set(content, '$.send', 1)`,
		"title two lines": `UPDATE ticket_sets SET content = json_set(content, '$.tickets[0].title', 'а' || char(10) || 'б')`,
		"bad coverage":    `UPDATE ticket_sets SET content = json_set(content, '$.tickets[0].result', json('{"answer":"","coverage":"maybe"}'))`,
		"result too soon": `UPDATE ticket_sets SET content = json_set(content, '$.tickets[0].result', json('{"answer":"","coverage":"not_covered"}'))`,
		"no tickets":      `UPDATE ticket_sets SET content = json_set(content, '$.tickets', json('[]'))`,
		"done no file":    `UPDATE ticket_sets SET status = 'done'`,
	}
	for name, stmt := range tests {
		t.Run(name, func(t *testing.T) {
			db, ctx := openTest(t, nil), context.Background()
			repo := NewTicketSetRepository(db)
			if err := repo.Save(ctx, newTicketSet(t, "")); err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, stmt)
			_, err := repo.Get(ctx, "ts1")
			if !errors.Is(err, ticketset.ErrInvalidSet) || strings.Contains(err.Error(), "Пределы") {
				t.Fatalf("Get = %v", err)
			}
		})
	}
}
