package schedulesync

import (
	"fmt"
	"log/slog"
	"maps"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Service runs the schedule import (import.go, PRD section 11.2). The calendar synchronization is CalendarSync
// (calendar.go); the Service only asks it for a run after a commit.
type Service struct {
	lessons LessonRepository
	parsers map[importplan.Source]ScheduleParser
	clock   Clock
	ids     IDGenerator
	tx      Tx
	sync    SyncRequester // nil: no calendar synchronization
	log     *slog.Logger
}

// New builds the Service.
func New(d Deps) (*Service, error) {
	if d.Lessons == nil || len(d.Parsers) == 0 || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	for src, p := range d.Parsers {
		if err := src.Validate(); err != nil || p == nil {
			return nil, fmt.Errorf("%w: parser for %q", ErrInvalidConfig, string(src))
		}
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Service{
		lessons: d.Lessons, parsers: maps.Clone(d.Parsers), clock: d.Clock, ids: d.IDs, tx: d.Tx, sync: d.Sync, log: log,
	}, nil
}
