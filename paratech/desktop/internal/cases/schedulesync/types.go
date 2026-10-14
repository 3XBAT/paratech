package schedulesync

import "paratech/desktop/internal/domain/schedule/services/importplan"

// Deps are the ports the Service needs; all are required, and Parsers needs at least one entry.
type Deps struct {
	Lessons LessonRepository
	Parsers map[importplan.Source]ScheduleParser
	Clock   Clock
	IDs     IDGenerator
	Tx      Tx
}

// ImportRequest is a file to preview. Data is the whole file as read by the input layer.
type ImportRequest struct {
	Source importplan.Source
	Data   []byte
}

// Preview is what the user sees before the commit: errors, or the added, changed, unchanged and removed lessons
// and the conflicts. ConfirmToken is empty when the plan has errors.
type Preview struct {
	Plan         importplan.Plan
	ConfirmToken string
}

// CommitRequest commits a previewed file. ConfirmToken is the token of the preview the user confirmed, and Data the
// same bytes.
type CommitRequest struct {
	Source       importplan.Source
	Data         []byte
	ConfirmToken string
}

// CommitResult counts what the commit wrote.
type CommitResult struct {
	Added   int
	Changed int
	Removed int
}
