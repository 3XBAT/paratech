package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the lecture text repository, used in its error messages.
const (
	opGetLectureText  = "sqlite: get lecture text"
	opSaveLectureText = "sqlite: save lecture text"
)

// LectureTextRepository stores the lecture texts of transcripts (the homeworkflow.LectureTextRepository port,
// TASK-090). Validation stays in lecturetext.LectureText: the repository only converts rows; the blocks and the
// finished parts are JSON columns, read strictly and checked again by Restore.
type LectureTextRepository struct{ db *DB }

// NewLectureTextRepository returns the lecture text repository of db.
func NewLectureTextRepository(db *DB) *LectureTextRepository { return &LectureTextRepository{db: db} }

// blockWire is the stored JSON form of a block.
type blockWire struct {
	Heading    string   `json:"heading"`
	StartMs    int64    `json:"start_ms"`
	Paragraphs []string `json:"paragraphs"`
}

// Get loads a lecture text; a missing one wraps shared.ErrNotFound.
func (r *LectureTextRepository) Get(ctx context.Context, id lecturetext.ID) (*lecturetext.LectureText, error) {
	row, err := r.db.Queries(ctx).GetLectureText(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetLectureText, id, MapError(ctx, err))
	}
	return lectureTextFromRow(row)
}

// Save writes the lecture text: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id
// that exists already, or a stale version, yields shared.ErrVersionConflict; a transcript or lesson that does not
// exist yields an error wrapping shared.ErrConflict (foreign key).
func (r *LectureTextRepository) Save(ctx context.Context, l *lecturetext.LectureText) error {
	if l.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveLectureText, l.Version())
	}

	st := l.State()
	blocks, err := json.Marshal(blocksWire(st.Blocks))
	if err != nil {
		return fmt.Errorf("%s %s: encode blocks: %w", opSaveLectureText, st.ID, err)
	}
	parts := make([][]blockWire, len(st.Parts))
	for i, p := range st.Parts {
		parts[i] = blocksWire(p)
	}
	partsJSON, err := json.Marshal(parts)
	if err != nil {
		return fmt.Errorf("%s %s: encode parts: %w", opSaveLectureText, st.ID, err)
	}

	next := st.Version + 1
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertLectureText(ctx, sqlcgen.InsertLectureTextParams{
				ID: string(st.ID), TranscriptID: jobNullString(string(st.Transcript)), LessonID: jobNullString(string(st.Lesson)),
				Blocks: string(blocks), PromptVersion: int64(st.PromptVersion), Run: st.Run, RunParts: int64(st.RunParts),
				Parts: string(partsJSON), CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateLectureText(ctx, sqlcgen.UpdateLectureTextParams{
			Blocks: string(blocks), PromptVersion: int64(st.PromptVersion), Run: st.Run, RunParts: int64(st.RunParts),
			Parts: string(partsJSON), UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveLectureText, st.ID, err)
	}

	l.Persisted(next)
	return nil
}

func blocksWire(states []lecturetext.BlockState) []blockWire {
	out := make([]blockWire, len(states))
	for i, s := range states {
		out[i] = blockWire{Heading: s.Heading, StartMs: s.StartMs, Paragraphs: s.Paragraphs}
	}
	return out
}

func blockStates(wire []blockWire) []lecturetext.BlockState {
	out := make([]lecturetext.BlockState, len(wire))
	for i, w := range wire {
		out[i] = lecturetext.BlockState{Heading: w.Heading, StartMs: w.StartMs, Paragraphs: w.Paragraphs}
	}
	return out
}

func lectureTextFromRow(row sqlcgen.LectureText) (*lecturetext.LectureText, error) {
	var blocks []blockWire
	if err := strictJSON(row.Blocks, &blocks); err != nil {
		return nil, fmt.Errorf("stored lecture text %s: blocks are not a JSON array of blocks", row.ID)
	}
	var parts [][]blockWire
	if err := strictJSON(row.Parts, &parts); err != nil {
		return nil, fmt.Errorf("stored lecture text %s: parts are not a JSON array of block arrays", row.ID)
	}
	st := lecturetext.State{
		Version: row.Version, ID: lecturetext.ID(row.ID), Transcript: shared.TranscriptID(row.TranscriptID.String),
		Lesson: shared.LessonID(row.LessonID.String), Blocks: blockStates(blocks), PromptVersion: int(row.PromptVersion),
		Run: row.Run, RunParts: int(row.RunParts), CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	for _, p := range parts {
		st.Parts = append(st.Parts, blockStates(p))
	}
	l, err := lecturetext.Restore(st)
	if err != nil {
		return nil, fmt.Errorf("stored lecture text %s: %w", row.ID, err)
	}
	return l, nil
}

// strictJSON decodes one JSON value without unknown fields or trailing data.
func strictJSON(s string, v any) error {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}
