package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the summary repository, used in its error messages.
const (
	opGetSummary  = "sqlite: get summary"
	opSaveSummary = "sqlite: save summary"
)

// SummaryRepository stores the summaries of transcripts (the homeworkflow.SummaryRepository port). Validation stays in
// summary.Summary: the repository only converts rows; the key points are a JSON array of strings, the sections
// (TASK-101) a JSON array of sectionWire, NULL for a plain summary made before.
type SummaryRepository struct{ db *DB }

// NewSummaryRepository returns the summary repository of db.
func NewSummaryRepository(db *DB) *SummaryRepository { return &SummaryRepository{db: db} }

// Get loads a summary; a missing one wraps shared.ErrNotFound.
func (r *SummaryRepository) Get(ctx context.Context, id summary.ID) (*summary.Summary, error) {
	row, err := r.db.Queries(ctx).GetSummary(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetSummary, id, MapError(ctx, err))
	}
	return summaryFromRow(row)
}

// Save writes the summary: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id that
// exists already, or a stale version, yields shared.ErrVersionConflict; a transcript or lesson that does not exist
// yields an error wrapping shared.ErrConflict (foreign key).
func (r *SummaryRepository) Save(ctx context.Context, s *summary.Summary) error {
	if s.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveSummary, s.Version())
	}

	st := s.State()
	points, err := json.Marshal(st.KeyPoints)
	if err != nil {
		return fmt.Errorf("%s %s: encode key points: %w", opSaveSummary, st.ID, err)
	}
	if st.KeyPoints == nil {
		points = []byte("[]")
	}
	sections, err := encodeSections(st.Sections)
	if err != nil {
		return fmt.Errorf("%s %s: encode sections: %w", opSaveSummary, st.ID, err)
	}

	next := st.Version + 1
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertSummary(ctx, sqlcgen.InsertSummaryParams{
				ID: string(st.ID), TranscriptID: jobNullString(string(st.Transcript)), LessonID: jobNullString(string(st.Lesson)),
				Body: st.Text, Sections: sections, KeyPoints: string(points), PromptVersion: int64(st.PromptVersion),
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateSummary(ctx, sqlcgen.UpdateSummaryParams{
			Body: st.Text, Sections: sections, KeyPoints: string(points), PromptVersion: int64(st.PromptVersion),
			UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveSummary, st.ID, err)
	}

	s.Persisted(next)
	return nil
}

// sectionWire is the stored form of one summary section.
type sectionWire struct {
	Heading string   `json:"heading"`
	Text    string   `json:"text"`
	Points  []string `json:"points"`
}

// encodeSections returns the sections column: NULL for a plain summary (nil sections).
func encodeSections(sections []summary.Section) (sql.NullString, error) {
	if sections == nil {
		return sql.NullString{}, nil
	}
	wire := make([]sectionWire, len(sections))
	for i, s := range sections {
		wire[i] = sectionWire{Heading: s.Heading, Text: s.Text, Points: s.Points}
		if wire[i].Points == nil {
			wire[i].Points = []string{}
		}
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func summaryFromRow(row sqlcgen.Summary) (*summary.Summary, error) {
	var points []string
	if err := json.Unmarshal([]byte(row.KeyPoints), &points); err != nil {
		return nil, fmt.Errorf("stored summary %s: key points are not a JSON array of strings", row.ID)
	}
	var sections []summary.Section
	if row.Sections.Valid {
		var wire []sectionWire
		if err := strictJSON(row.Sections.String, &wire); err != nil || wire == nil {
			return nil, fmt.Errorf("stored summary %s: sections are not a JSON array of sections", row.ID)
		}
		// Not nil even when empty: an empty array is a sectioned summary without sections, which Restore refuses.
		sections = make([]summary.Section, len(wire))
		for i, w := range wire {
			sections[i] = summary.Section{Heading: w.Heading, Text: w.Text, Points: w.Points}
		}
	}
	s, err := summary.Restore(summary.State{
		Version: row.Version, ID: summary.ID(row.ID), Transcript: shared.TranscriptID(row.TranscriptID.String),
		Lesson: shared.LessonID(row.LessonID.String), PromptVersion: int(row.PromptVersion), Text: row.Body,
		Sections: sections, KeyPoints: points, CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored summary %s: %w", row.ID, err)
	}
	return s, nil
}
