package app

import (
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/icsschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/userfile"
	"paratech/desktop/internal/adapters/xlsxschedule"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// newSchedule builds the schedule use case on the local schedule: the import of the schedule page (PRD section 11.2)
// and the schedule read of the agent. A commit that changed the schedule asks the calendar sync for a run
// (wireCalendar), so it needs c.calendarSync.
func (c *core) newSchedule(e env, log *slog.Logger) (*schedulesync.Service, error) {
	// The ICS import dates follow the same clock as the use case, so a preview and its commit agree on them.
	ics, err := icsschedule.New(e.clock)
	if err != nil {
		return nil, fmt.Errorf("app: ics parser: %w", err)
	}
	svc, err := schedulesync.New(schedulesync.Deps{
		Lessons: sqlite.NewLessonRepository(c.db),
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{
			importplan.SourceCSV:  csvschedule.New(),
			importplan.SourceICS:  ics,
			importplan.SourceXLSX: xlsxschedule.New(),
		},
		Clock: e.clock, IDs: e.ids, Tx: c.db, Sync: c.calendarSync, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: schedule import: %w", err)
	}
	return svc, nil
}

// wireSchedule gives the schedule page its use case and the reader of the file the user chooses.
func (c *core) wireSchedule(svc *schedulesync.Service) {
	c.ui.Schedule = svc
	c.ui.Files = userfile.New()
}
