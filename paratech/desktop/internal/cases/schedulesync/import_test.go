package schedulesync

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func at(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

func content(title string, start time.Time) lesson.Content {
	return lesson.Content{Title: title, Start: start, End: start.Add(time.Hour), TimeZone: "UTC"}
}

type env struct {
	store  *lessonStore
	parser *fakeParser
	clock  *testenv.FakeClock
	svc    *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{store: newLessonStore(t), parser: &fakeParser{files: map[string]importplan.Parsed{}}, clock: testenv.NewFakeClock(t0)}
	svc, err := New(Deps{
		Lessons: e.store, Parsers: map[importplan.Source]ScheduleParser{importplan.SourceCSV: e.parser},
		Clock: e.clock, IDs: &seqIDs{}, Tx: e.store,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

func (e *env) seed(t *testing.T, id, uid string, c lesson.Content) {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: shared.LessonID(id), Content: c, SourceUID: uid}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.store.put(l)
}

func rows(rs ...importplan.Row) importplan.Parsed { return importplan.Parsed{Rows: rs} }

func row(line int, key string, c lesson.Content) importplan.Row {
	return importplan.Row{Line: line, Key: key, Content: c}
}

func TestNewRequiresPorts(t *testing.T) {
	s := newLessonStore(t)
	p := map[importplan.Source]ScheduleParser{importplan.SourceCSV: &fakeParser{}}
	tests := []struct {
		name string
		deps Deps
	}{
		{"no lessons", Deps{Parsers: p, Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}, Tx: s}},
		{"no parsers", Deps{Lessons: s, Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}, Tx: s}},
		{"unknown source", Deps{Lessons: s, Parsers: map[importplan.Source]ScheduleParser{"pdf": &fakeParser{}}, Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}, Tx: s}},
		{"nil parser", Deps{Lessons: s, Parsers: map[importplan.Source]ScheduleParser{importplan.SourceCSV: nil}, Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}, Tx: s}},
		{"no clock", Deps{Lessons: s, Parsers: p, IDs: &seqIDs{}, Tx: s}},
		{"no ids", Deps{Lessons: s, Parsers: p, Clock: testenv.NewFakeClock(t0), Tx: s}},
		{"no tx", Deps{Lessons: s, Parsers: p, Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}}},
	}
	for _, tt := range tests {
		if _, err := New(tt.deps); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: New error = %v, want ErrInvalidConfig", tt.name, err)
		}
	}
}

func TestPreviewStoresNothing(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, "l-old", "csv:old", content("Old", at(0)))
	e.parser.files["f"] = rows(row(2, "new", content("New", at(2))))
	before := e.store.snapshot()

	p, err := e.svc.PreviewImport(ctx, ImportRequest{Source: importplan.SourceCSV, Data: []byte("f")})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Plan.Added) != 1 || len(p.Plan.Removed) != 1 || p.ConfirmToken == "" || p.ConfirmToken != p.Plan.Fingerprint {
		t.Fatalf("preview = %+v", p)
	}
	if !reflect.DeepEqual(before, e.store.snapshot()) || e.store.applies != 0 {
		t.Fatal("preview changed the schedule")
	}
}

func TestPreviewErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	if _, err := e.svc.PreviewImport(ctx, ImportRequest{Source: importplan.SourceICS, Data: []byte("f")}); !errors.Is(err, ErrUnsupportedSource) {
		t.Errorf("ics without a parser: %v", err)
	}
	if _, err := e.svc.PreviewImport(ctx, ImportRequest{Source: importplan.SourceCSV, Data: []byte("unknown")}); !errors.Is(err, importplan.ErrUnreadableFile) {
		t.Errorf("unreadable file: %v", err)
	}

	e.parser.files["bad"] = importplan.Parsed{
		Rows:   []importplan.Row{row(2, "a", content("A", at(0)))},
		Errors: []importplan.RowError{{Line: 3, Field: "date", Reason: "not a date"}},
	}
	p, err := e.svc.PreviewImport(ctx, ImportRequest{Source: importplan.SourceCSV, Data: []byte("bad")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Valid() || p.ConfirmToken != "" || p.Plan.Errors[0].Line != 3 {
		t.Fatalf("preview of a bad file = %+v", p)
	}
}

func TestCommitAppliesConfirmedPlan(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, "l-keep", "csv:keep", content("Keep", at(0)))
	e.seed(t, "l-move", "csv:move", content("Move", at(2)))
	e.seed(t, "l-gone", "csv:gone", content("Gone", at(4)))
	e.seed(t, "l-manual", "", content("Manual", at(6)))
	file := rows(
		row(2, "keep", content("Keep", at(0))),
		row(3, "move", content("Move", at(3))),
		row(4, "new", content("New", at(8))),
	)
	e.parser.files["f"] = file
	req := ImportRequest{Source: importplan.SourceCSV, Data: []byte("f")}

	p, err := e.svc.PreviewImport(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	res, err := e.svc.CommitImport(ctx, CommitRequest{Source: req.Source, Data: req.Data, ConfirmToken: p.ConfirmToken})
	if err != nil {
		t.Fatal(err)
	}
	if res != (CommitResult{Added: 1, Changed: 1, Removed: 1}) || e.store.applies != 1 {
		t.Fatalf("result %+v, %d ApplyChanges calls", res, e.store.applies)
	}

	rows := e.store.snapshot()
	now := t0.Add(time.Minute)
	if st := rows["l-move"]; !st.Content.Start.Equal(at(3)) || st.ContentRevision != 2 || st.Version != 2 || !st.UpdatedAt.Equal(now) {
		t.Errorf("moved lesson = %+v", st)
	}
	if st := rows["l-gone"]; !st.DeletedAt.Equal(now) || st.ContentRevision != 2 {
		t.Errorf("removed lesson = %+v", st)
	}
	if st := rows["l-keep"]; st.Version != 1 || st.ContentRevision != 1 {
		t.Errorf("unchanged lesson was written: %+v", st)
	}
	if st := rows["l-manual"]; st.Version != 1 || !st.DeletedAt.IsZero() {
		t.Errorf("manual lesson touched: %+v", st)
	}
	if st, ok := rows["lesson-1"]; !ok || st.SourceUID != "csv:new" || st.Version != 1 || !st.CreatedAt.Equal(now) {
		t.Errorf("added lesson = %+v (found %v)", st, ok)
	}

	// The same file again is a no-op: nothing is written.
	p2, err := e.svc.PreviewImport(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Plan.HasChanges() || len(p2.Plan.Unchanged) != 3 {
		t.Fatalf("re-import preview = %+v", p2.Plan)
	}
	res, err = e.svc.CommitImport(ctx, CommitRequest{Source: req.Source, Data: req.Data, ConfirmToken: p2.ConfirmToken})
	if err != nil || res != (CommitResult{}) || e.store.applies != 1 {
		t.Fatalf("re-import commit: %+v, %v, %d ApplyChanges calls", res, err, e.store.applies)
	}
}

func TestCommitRefusals(t *testing.T) {
	ctx := context.Background()
	csv := importplan.SourceCSV

	t.Run("without confirmation", func(t *testing.T) {
		e := newEnv(t)
		e.parser.files["f"] = rows(row(2, "a", content("A", at(0))))
		if _, err := e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("f")}); !errors.Is(err, ErrNotConfirmed) {
			t.Fatalf("err = %v, want ErrNotConfirmed", err)
		}
		if e.parser.calls != 0 || e.store.applies != 0 {
			t.Fatal("an unconfirmed commit did work")
		}
	})

	t.Run("file with errors", func(t *testing.T) {
		e := newEnv(t)
		e.seed(t, "l1", "csv:a", content("A", at(0)))
		e.parser.files["bad"] = importplan.Parsed{Errors: []importplan.RowError{{Line: 3, Reason: "x"}}}
		before := e.store.snapshot()
		_, err := e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("bad"), ConfirmToken: "anything"})
		if !errors.Is(err, ErrInvalidImport) {
			t.Fatalf("err = %v, want ErrInvalidImport", err)
		}
		if !reflect.DeepEqual(before, e.store.snapshot()) || e.store.applies != 0 {
			t.Fatal("a file with errors changed the schedule")
		}
	})

	t.Run("token of another preview", func(t *testing.T) {
		e := newEnv(t)
		e.parser.files["f"] = rows(row(2, "a", content("A", at(0))))
		e.parser.files["g"] = rows(row(2, "a", content("A", at(5))))
		p, err := e.svc.PreviewImport(ctx, ImportRequest{Source: csv, Data: []byte("f")})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("g"), ConfirmToken: p.ConfirmToken})
		if !errors.Is(err, ErrStalePreview) || e.store.applies != 0 {
			t.Fatalf("err = %v (%d applies), want ErrStalePreview and nothing written", err, e.store.applies)
		}
	})

	t.Run("schedule changed after the preview", func(t *testing.T) {
		e := newEnv(t)
		e.parser.files["f"] = rows(row(2, "a", content("A", at(0))))
		p, err := e.svc.PreviewImport(ctx, ImportRequest{Source: csv, Data: []byte("f")})
		if err != nil {
			t.Fatal(err)
		}
		e.seed(t, "l-manual", "", content("Manual", at(0))) // a new conflict the user has not seen
		_, err = e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("f"), ConfirmToken: p.ConfirmToken})
		if !errors.Is(err, ErrStalePreview) || e.store.applies != 0 {
			t.Fatalf("err = %v (%d applies), want ErrStalePreview and nothing written", err, e.store.applies)
		}
	})

	t.Run("version conflict in the write", func(t *testing.T) {
		e := newEnv(t)
		e.parser.files["f"] = rows(row(2, "a", content("A", at(0))))
		p, err := e.svc.PreviewImport(ctx, ImportRequest{Source: csv, Data: []byte("f")})
		if err != nil {
			t.Fatal(err)
		}
		e.store.applyErr = shared.ErrVersionConflict
		_, err = e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("f"), ConfirmToken: p.ConfirmToken})
		if !errors.Is(err, ErrStalePreview) || !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("err = %v, want ErrStalePreview wrapping ErrVersionConflict", err)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		e := newEnv(t)
		e.parser.err = importplan.ErrUnreadableFile
		_, err := e.svc.CommitImport(ctx, CommitRequest{Source: csv, Data: []byte("f"), ConfirmToken: "t"})
		if !errors.Is(err, importplan.ErrUnreadableFile) || e.store.applies != 0 {
			t.Fatalf("err = %v, want ErrUnreadableFile", err)
		}
	})
}
