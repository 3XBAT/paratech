package sqlite

import (
	"context"

	"paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// KnowledgeBase reads the lessons of the knowledge base (the knowledgebase.LessonStore port, TASK-091): the completed
// recordings that are neither silent nor marked for deletion, and what is stored for their newest transcript. It
// keeps no index, so a purged recording is simply gone (ADR-0019).
type KnowledgeBase struct{ db *DB }

// NewKnowledgeBase returns the knowledge base reads of db.
func NewKnowledgeBase(db *DB) *KnowledgeBase { return &KnowledgeBase{db: db} }

// ListLessons returns the recordings of the knowledge base, newest start first. A row that cannot be read or restored
// is left out and its id goes to unreadable.
func (k *KnowledgeBase) ListLessons(ctx context.Context) ([]*recording.Recording, []shared.RecordingID, error) {
	q := k.db.Queries(ctx)
	ids, err := q.ListKnowledgeBaseRecordingIDs(ctx, string(recording.StatusCompleted))
	if err != nil {
		return nil, nil, MapError(ctx, err)
	}
	return loadRecordings(ctx, q, ids)
}

// ListReadiness returns, per recording of the knowledge base, its newest transcript and whether that has a summary
// and a finished lecture text.
func (k *KnowledgeBase) ListReadiness(ctx context.Context) (map[shared.RecordingID]knowledgebase.Readiness, error) {
	rows, err := k.db.Queries(ctx).ListKnowledgeBaseReadiness(ctx, string(recording.StatusCompleted))
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make(map[shared.RecordingID]knowledgebase.Readiness, len(rows))
	for _, r := range rows {
		out[shared.RecordingID(r.RecordingID)] = knowledgebase.Readiness{
			Transcript: shared.TranscriptID(r.TranscriptID), Summary: r.HasSummary != 0, LectureText: r.HasLectureText != 0,
		}
	}
	return out, nil
}
