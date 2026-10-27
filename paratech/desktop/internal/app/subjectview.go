package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/visitfile"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/subjectview"
	"paratech/desktop/internal/domain/shared"
)

// visitsFile holds the last opening of each subject page, in the data directory (TASK-225): only the «новое» marks
// depend on it, so it is not in the database.
const visitsFile = "subject-visits.json"

// newSubjectView builds the page of a subject (TASK-225) over the local storage, the library (subject strings), the
// homework use case (the assignments as the homework screen shows them), the knowledge base (the recorded lessons)
// and the activity screen (failed chains). Days and weekdays in the student's zone.
func (c *core) newSubjectView(dataDir string, e env, lib *library.Service, knowledge *knowledgebase.Service,
	history *activity.Service, log *slog.Logger,
) (*subjectview.Service, error) {
	svc, err := subjectview.New(subjectview.Deps{
		Subjects: sqlite.NewSubjectRepository(c.db), Materials: sqlite.NewMaterialRepository(c.db), Resolver: lib,
		Assignments: todayAssignments{c.proposals}, Schedule: sqlite.NewLessonRepository(c.db), Knowledge: knowledge,
		Summaries: sqlite.NewSummaryRepository(c.db), Failures: chainFailures{history},
		Visits: visitfile.New(filepath.Join(dataDir, visitsFile)), Clock: e.local, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: subject page: %w", err)
	}
	return svc, nil
}

// chainFailures is the Failures port of the subject page over the activity screen; mapping only.
type chainFailures struct {
	activity *activity.Service
}

// FailedRecordings returns the recordings whose chain stopped on a failed step.
func (f chainFailures) FailedRecordings(ctx context.Context) ([]shared.RecordingID, error) {
	list, err := f.activity.ListRecordings(ctx)
	if err != nil {
		return nil, err
	}
	var out []shared.RecordingID
	for _, r := range list {
		if r.Chain.Stage == activity.StageFailed {
			out = append(out, r.ID)
		}
	}
	return out, nil
}
