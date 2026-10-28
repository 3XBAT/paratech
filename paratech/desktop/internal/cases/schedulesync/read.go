package schedulesync

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
)

// LessonsStartingBetween returns the live lessons of the local schedule whose start lies in [from, to), ordered by
// start: the schedule as the agent reads it (find_lessons).
func (s *Service) LessonsStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	ls, err := s.lessons.ListStartingBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("schedulesync: lessons between: %w", err)
	}
	return ls, nil
}
