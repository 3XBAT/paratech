package lesson

import (
	"time"

	"paratech/desktop/internal/domain/shared"
)

// StartAnnounced is the "lesson is starting" event (PRD section 6.1): the scheduler decided to offer the recording
// prompt for this lesson. It is returned by AnnounceStart and published by the use case (ARCH-16).
//
// Start and End are instants; TimeZone is the IANA zone of the source record, so a consumer shows the times in it.
type StartAnnounced struct {
	LessonID    shared.LessonID
	Title       string
	Start       time.Time
	End         time.Time
	TimeZone    string
	AnnouncedAt time.Time
}
