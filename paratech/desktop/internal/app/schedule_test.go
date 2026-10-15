package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/icsschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/xlsxschedule"
	"paratech/desktop/internal/input/wails"
)

type pathPicker string

func (p pathPicker) PickScheduleFile(context.Context) (string, error) { return string(p), nil }

// The page's binding on the wired core: a CSV file is previewed and committed into the real SQLite schedule.
func TestScheduleImportThroughThePageBinding(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	log := slog.New(slog.DiscardHandler)
	c, err := buildCore(ctx, t.TempDir(), e, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.close() })

	path := filepath.Join(t.TempDir(), "lessons.csv")
	csv := "title,date,start,end,timezone,uid\nMath,2026-10-14,09:00,10:30,Europe/Moscow,math-1\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	page := wails.NewScheduleImportService(c.ui.Schedule, c.ui.Files, pathPicker(path), log)

	p, err := page.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.CanCommit || len(p.Added) != 1 || p.Added[0].Start != "14.10.2026 09:00" {
		t.Fatalf("preview = %+v, want one committable lesson at 09:00 Moscow time", p)
	}
	res, err := page.Commit(ctx, p.SelectionID)
	if err != nil || res != (wails.ImportResult{Added: 1}) {
		t.Fatalf("Commit = %+v, %v", res, err)
	}

	live, err := sqlite.NewLessonRepository(c.db).ListLive(ctx)
	if err != nil || len(live) != 1 || live[0].Content().Title != "Math" || live[0].SourceUID() != "csv:id:math-1" {
		t.Fatalf("ListLive = %v, %v; want the imported lesson", live, err)
	}
}

// The page reads at most what the parsers accept.
func TestScheduleFileLimitMatchesTheParsers(t *testing.T) {
	for name, limit := range map[string]int{
		"csv": csvschedule.MaxFileBytes, "ics": icsschedule.MaxFileBytes, "xlsx": xlsxschedule.MaxFileBytes,
	} {
		if limit != wails.MaxScheduleFileBytes {
			t.Errorf("%s parser limit %d, page limit %d", name, limit, wails.MaxScheduleFileBytes)
		}
	}
}
