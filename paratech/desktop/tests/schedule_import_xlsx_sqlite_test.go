package tests

// Scenario tests of the XLSX schedule import (PRD section 11.2, AC-16) on the real SQLite adapter, the real XLSX, CSV
// and ICS parsers and the real use case; the clock and the ids are fakes (ARCH-61). The workbooks are written here
// with excelize, so no binary test data is needed.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/icsschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/xlsxschedule"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/testenv"
)

var _ schedulesync.ScheduleParser = (*xlsxschedule.Parser)(nil)

func newXLSXScheduleEnv(t *testing.T) *scheduleEnv {
	t.Helper()
	db, err := openSQLite(context.Background(), t, sqlite.Options{Path: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &scheduleEnv{t: t, repo: sqlite.NewLessonRepository(db), clock: testenv.NewFakeClock(schedT0)}
	ics, err := icsschedule.New(e.clock)
	if err != nil {
		t.Fatal(err)
	}
	e.svc, err = schedulesync.New(schedulesync.Deps{
		Lessons: e.repo,
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{
			importplan.SourceCSV: csvschedule.New(), importplan.SourceICS: ics, importplan.SourceXLSX: xlsxschedule.New(),
		},
		Clock: e.clock, IDs: &lessonIDGen{}, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *scheduleEnv) previewAs(src importplan.Source, data []byte) schedulesync.Preview {
	e.t.Helper()
	p, err := e.svc.PreviewImport(context.Background(), schedulesync.ImportRequest{Source: src, Data: data})
	if err != nil {
		e.t.Fatalf("PreviewImport: %v", err)
	}
	return p
}

// xlsxBook writes the rows from A1 of the first sheet.
func xlsxBook(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	for i, r := range rows {
		if err := f.SetSheetRow("Sheet1", fmt.Sprintf("A%d", i+1), &r); err != nil {
			t.Fatal(err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Excel stores typed dates and times as numbers with a date or time format; excelize writes them the same way.
func xlsxDay(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func xlsxClock(h, m int) time.Duration {
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
}

var xlsxHeader = []any{"title", "date", "start", "end", "timezone", "uid"}

// semesterXLSX is the CSV semester as a workbook with typed cells and a text cell, plus an ignored column.
func semesterXLSX(t *testing.T, mathEnd time.Duration, withHistory bool) []byte {
	rows := [][]any{
		append(append([]any{}, xlsxHeader...), "room"),
		{"Math", xlsxDay(10, 5), xlsxClock(9, 0), mathEnd, "Europe/Moscow", "math-1", 101},
		{"Physics", "2026-10-05", "10:45", xlsxClock(12, 15), "Europe/Moscow", "phys-1"},
	}
	if withHistory {
		rows = append(rows, []any{"History", xlsxDay(10, 6), xlsxClock(9, 0), xlsxClock(10, 30), "Europe/Moscow", "hist-1"})
	}
	return xlsxBook(t, rows)
}

// Test step 2: an XLSX and the equivalent CSV give the same parsed rows, the same row errors at the same rows and
// the same preview, and commit the same lessons; only the source namespace of the uids differs.
func TestScheduleImportXLSXMatchesCSV(t *testing.T) {
	const csvFile = "title,date,start,end,timezone,uid\n" +
		"Math,2026-10-05,09:00,10:30,Europe/Moscow,math-1\n" +
		"Physics,2026-10-05,10:00,11:15,Europe/Moscow,\n" +
		"Seminar,2026-10-06,23:15,23:59,UTC,\n"
	xlsxFile := xlsxBook(t, [][]any{
		xlsxHeader,
		{"Math", xlsxDay(10, 5), xlsxClock(9, 0), xlsxClock(10, 30), "Europe/Moscow", "math-1"},
		{"Physics", "2026-10-05", xlsxClock(10, 0), "11:15", "Europe/Moscow"},
		{"Seminar", xlsxDay(10, 6), xlsxClock(23, 15), xlsxClock(23, 59), "UTC"},
	})
	ctx := context.Background()
	c, err := csvschedule.New().Parse(ctx, []byte(csvFile))
	if err != nil {
		t.Fatal(err)
	}
	x, err := xlsxschedule.New().Parse(ctx, xlsxFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Rows) != 3 || len(x.Rows) != len(c.Rows) || len(c.Errors)+len(x.Errors) != 0 {
		t.Fatalf("parsed: csv %+v, xlsx %+v", c, x)
	}
	for i := range c.Rows {
		if a, b := c.Rows[i], x.Rows[i]; a.Line != b.Line || a.Key != b.Key || !a.Content.Equal(b.Content) {
			t.Errorf("row %d: csv %+v, xlsx %+v", i, a, b)
		}
	}

	ce, xe := newXLSXScheduleEnv(t), newXLSXScheduleEnv(t)
	pc, px := ce.previewAs(importplan.SourceCSV, []byte(csvFile)), xe.previewAs(importplan.SourceXLSX, xlsxFile)
	if !pc.Plan.Valid() || !px.Plan.Valid() || len(pc.Plan.Added) != 3 || len(px.Plan.Added) != len(pc.Plan.Added) ||
		len(pc.Plan.Conflicts) != 1 || len(px.Plan.Conflicts) != len(pc.Plan.Conflicts) {
		t.Fatalf("previews: csv %+v, xlsx %+v", pc.Plan, px.Plan)
	}
	for i := range pc.Plan.Added {
		a, b := pc.Plan.Added[i], px.Plan.Added[i]
		if a.Line != b.Line || !strings.HasPrefix(b.UID, "xlsx:") || strings.TrimPrefix(a.UID, "csv:") != strings.TrimPrefix(b.UID, "xlsx:") ||
			!a.Content.Equal(b.Content) {
			t.Errorf("added %d: csv %+v, xlsx %+v", i, a, b)
		}
	}
	if a, b := pc.Plan.Conflicts[0], px.Plan.Conflicts[0]; a.Line != b.Line || a.WithLine != b.WithLine {
		t.Errorf("conflicts: csv %+v, xlsx %+v", a, b)
	}

	ce.importAs(importplan.SourceCSV, csvFile)
	xe.importAs(importplan.SourceXLSX, string(xlsxFile))
	cl, xl := ce.live("csv:"), xe.live("xlsx:")
	if len(cl) != 3 || len(xl) != len(cl) {
		t.Fatalf("stored: csv %+v, xlsx %+v", cl, xl)
	}
	for uid, a := range cl {
		b, ok := xl["xlsx:"+strings.TrimPrefix(uid, "csv:")]
		if !ok || a.Title != b.Title || !a.Start.Equal(b.Start) || !a.End.Equal(b.End) || a.Zone != b.Zone {
			t.Errorf("lesson %s: csv %+v, xlsx %+v (found %v)", uid, a, b, ok)
		}
	}
}

// The same row problems in both formats block the preview at the same rows and fields; XLSX adds the column letter.
func TestScheduleImportXLSXRowErrorsMatchCSV(t *testing.T) {
	const csvFile = "title,date,start,end,timezone\n" +
		"Ok,2026-10-05,08:00,08:30,UTC\n" +
		"Broken,2026-10-05,11:00,10:00,UTC\n" +
		"Zone,2026-10-05,11:00,12:00,Mars/Base\n"
	xlsxFile := xlsxBook(t, [][]any{
		xlsxHeader[:5],
		{"Ok", xlsxDay(10, 5), xlsxClock(8, 0), xlsxClock(8, 30), "UTC"},
		{"Broken", xlsxDay(10, 5), xlsxClock(11, 0), xlsxClock(10, 0), "UTC"},
		{"Zone", xlsxDay(10, 5), xlsxClock(11, 0), xlsxClock(12, 0), "Mars/Base"},
	})
	e := newXLSXScheduleEnv(t)
	pc, px := e.previewAs(importplan.SourceCSV, []byte(csvFile)), e.previewAs(importplan.SourceXLSX, xlsxFile)
	if pc.Plan.Valid() || px.Plan.Valid() || px.ConfirmToken != "" || len(px.Plan.Errors) != len(pc.Plan.Errors) {
		t.Fatalf("previews: csv %+v, xlsx %+v", pc.Plan.Errors, px.Plan.Errors)
	}
	wantField, wantLetter := []string{"end", "timezone"}, []string{"D", "E"}
	for i, a := range pc.Plan.Errors {
		b := px.Plan.Errors[i]
		if a.Line != b.Line || a.Field != wantField[i] || b.Field != a.Field || !strings.HasPrefix(b.Reason, "column "+wantLetter[i]+": ") {
			t.Errorf("error %d: csv %+v, xlsx %+v", i, a, b)
		}
	}
	if got := e.schedule(); len(got) != 0 {
		t.Fatalf("a preview with errors stored lessons: %+v", got)
	}
}

// Re-imports of a workbook: the same file changes nothing, an edited one changes and removes only XLSX lessons.
func TestScheduleImportXLSXReimport(t *testing.T) {
	e := newXLSXScheduleEnv(t)
	e.importAs(importplan.SourceCSV, semester) // the CSV lessons must stay untouched by every XLSX import

	_, res := e.importAs(importplan.SourceXLSX, string(semesterXLSX(t, xlsxClock(10, 30), true)))
	if res != (schedulesync.CommitResult{Added: 3}) {
		t.Fatalf("first import = %+v", res)
	}
	math := e.byUID("xlsx:id:math-1")
	wantStart := time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC) // 09:00 Moscow
	if math.Title != "Math" || !math.Start.Equal(wantStart) || !math.End.Equal(wantStart.Add(90*time.Minute)) || math.Zone != "Europe/Moscow" {
		t.Fatalf("stored lesson = %+v", math)
	}

	before := e.schedule()
	p, res := e.importAs(importplan.SourceXLSX, string(semesterXLSX(t, xlsxClock(10, 30), true)))
	if res != (schedulesync.CommitResult{}) || len(p.Plan.Unchanged) != 3 {
		t.Fatalf("re-import = %+v, plan %+v", res, p.Plan)
	}
	if after := e.schedule(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("re-importing the same workbook changed the schedule")
	}

	_, res = e.importAs(importplan.SourceXLSX, string(semesterXLSX(t, xlsxClock(10, 45), false)))
	if res != (schedulesync.CommitResult{Changed: 1, Removed: 1}) {
		t.Fatalf("edited import = %+v", res)
	}
	if m := e.byUID("xlsx:id:math-1"); m.ID != math.ID || !m.End.Equal(wantStart.Add(105*time.Minute)) || m.Revision != 2 {
		t.Errorf("changed lesson = %+v", m)
	}
	if _, ok := e.live("xlsx:")["xlsx:id:hist-1"]; ok {
		t.Error("the removed row is still a live lesson")
	}
	if csv := e.live("csv:"); len(csv) != 3 {
		t.Errorf("CSV lessons = %+v", csv)
	}
}
