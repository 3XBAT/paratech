package schedulesync

import (
	"fmt"
	"maps"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Service runs the schedule use cases (PRD section 11). This task adds the import (import.go); the scheduler
// (TASK-018) and the calendar synchronization (TASK-051) extend it.
type Service struct {
	lessons LessonRepository
	parsers map[importplan.Source]ScheduleParser
	clock   Clock
	ids     IDGenerator
	tx      Tx
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

	return &Service{lessons: d.Lessons, parsers: maps.Clone(d.Parsers), clock: d.Clock, ids: d.IDs, tx: d.Tx}, nil
}
