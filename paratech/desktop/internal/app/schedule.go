package app

import (
	"fmt"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/icsschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/userfile"
	"paratech/desktop/internal/adapters/xlsxschedule"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// wireSchedule builds the schedule import of the schedule page (PRD section 11.2) on the local schedule, and the reader
// of the file the user chooses. The calendar synchronization is not wired yet (TASK-051).
func (c *core) wireSchedule(e env) error {
	// The ICS import dates follow the same clock as the use case, so a preview and its commit agree on them.
	ics, err := icsschedule.New(e.clock)
	if err != nil {
		return fmt.Errorf("app: ics parser: %w", err)
	}
	svc, err := schedulesync.New(schedulesync.Deps{
		Lessons: sqlite.NewLessonRepository(c.db),
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{
			importplan.SourceCSV:  csvschedule.New(),
			importplan.SourceICS:  ics,
			importplan.SourceXLSX: xlsxschedule.New(),
		},
		Clock: e.clock, IDs: e.ids, Tx: c.db,
	})
	if err != nil {
		return fmt.Errorf("app: schedule import: %w", err)
	}

	c.ui.Schedule = svc
	c.ui.Files = userfile.New()
	return nil
}
