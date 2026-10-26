package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the work repository, used in its error messages.
const (
	opGetWork     = "sqlite: get work"
	opSaveWork    = "sqlite: save work"
	opDeleteWork  = "sqlite: delete work"
	opRestoreWork = "stored work"
)

// WorkRepository stores the turnkey works (the turnkey.WorkRepository port, TASK-129). The reader's answer of a round
// is kept in its canonical JSON and read back through work.ParseAnswer, the writing (TASK-131) through
// work.ParseWriting: stored model output passes the same strict checks as a fresh one. Validation stays in the
// aggregate; the repository only converts rows.
type WorkRepository struct{ db *DB }

// NewWorkRepository returns the work repository of db.
func NewWorkRepository(db *DB) *WorkRepository { return &WorkRepository{db: db} }

// Get loads a work; a missing one wraps shared.ErrNotFound.
func (r *WorkRepository) Get(ctx context.Context, id shared.WorkID) (*work.Work, error) {
	row, err := r.db.Queries(ctx).GetWork(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetWork, id, MapError(ctx, err))
	}
	return r.restore(ctx, row)
}

// ActiveOf loads the work of a source that has not failed; none wraps shared.ErrNotFound.
func (r *WorkRepository) ActiveOf(ctx context.Context, src work.Source) (*work.Work, error) {
	q := r.db.Queries(ctx)
	var (
		row sqlcgen.Work
		err error
	)
	switch {
	case !src.Topic.IsZero():
		row, err = q.GetActiveWorkOfTopic(ctx, jobNullString(src.Topic.Request()))
	case src.Assignment != "":
		row, err = q.GetActiveWorkOfAssignment(ctx, jobNullString(string(src.Assignment)))
	default:
		row, err = q.GetActiveWorkOfMaterial(ctx, jobNullString(string(src.Material)))
	}
	if err != nil {
		return nil, fmt.Errorf("%s of a source: %w", opGetWork, MapError(ctx, err))
	}
	return r.restore(ctx, row)
}

// Save writes the work and its rounds in one transaction: an INSERT for version 0, otherwise a version
// compare-and-swap (ADR-0004(d)). A stale version wraps shared.ErrVersionConflict; a second active work for the same
// source shared.ErrDuplicate; a source that does not exist shared.ErrConflict.
func (r *WorkRepository) Save(ctx context.Context, w *work.Work) error {
	st := w.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveWork, st.Version)
	}
	edit := func(f work.Field) sql.NullString {
		v, ok := st.Edits[f]
		return sql.NullString{String: v, Valid: ok}
	}
	topic := st.Source.Topic
	var writing sql.NullString
	if st.Writing != nil {
		writing = sql.NullString{String: st.Writing.Encode(), Valid: true}
	}
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			err := q.InsertWork(ctx, sqlcgen.InsertWorkParams{
				ID: string(st.ID), AssignmentID: jobNullString(string(st.Source.Assignment)),
				MaterialID: jobNullString(string(st.Source.Material)), TopicRequest: jobNullString(topic.Request()),
				Topic: jobNullString(topic.Text()), TopicSubjectID: jobNullString(string(topic.Subject())),
				Status: string(st.Status), Round: int64(st.Round),
				Attempt: int64(st.Attempt), EditWorkType: edit(work.FieldWorkType), EditVolume: edit(work.FieldVolume),
				EditFileFormat: edit(work.FieldFileFormat), EditFormatting: edit(work.FieldFormatting),
				EditVariant: edit(work.FieldVariant), EditDeadline: edit(work.FieldDeadline),
				EditSources: edit(work.FieldSources), Writing: writing, CreatedAt: jobMs(st.CreatedAt),
				UpdatedAt: jobMs(st.UpdatedAt),
			})
			if err := DuplicateAsVersionConflict(MapError(ctx, err)); err != nil {
				return err
			}
		} else {
			n, err := q.UpdateWork(ctx, sqlcgen.UpdateWorkParams{
				Status: string(st.Status), Round: int64(st.Round), Attempt: int64(st.Attempt),
				EditWorkType: edit(work.FieldWorkType), EditVolume: edit(work.FieldVolume),
				EditFileFormat: edit(work.FieldFileFormat), EditFormatting: edit(work.FieldFormatting),
				EditVariant: edit(work.FieldVariant), EditDeadline: edit(work.FieldDeadline),
				EditSources: edit(work.FieldSources), Writing: writing, UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID),
				ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, n, err); err != nil {
				return err
			}
		}
		for _, round := range st.Rounds {
			replies, err := repliesText(round.Replies)
			if err != nil {
				return err
			}
			if err := q.UpsertWorkRound(ctx, sqlcgen.UpsertWorkRoundParams{
				WorkID: string(st.ID), Round: int64(round.Number), Answer: round.Answer.Encode(), Replies: replies,
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveWork, st.ID, err)
	}
	w.Persisted(st.Version + 1)
	return nil
}

// Delete removes a work with its rounds if it is still at version expected; a changed or missing one wraps
// shared.ErrVersionConflict.
func (r *WorkRepository) Delete(ctx context.Context, id shared.WorkID, expected int64) error {
	n, err := r.db.Queries(ctx).DeleteWork(ctx, sqlcgen.DeleteWorkParams{ID: string(id), ExpectedVersion: expected})
	if err := VersionCAS(ctx, n, err); err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteWork, id, err)
	}
	return nil
}

// replyWire is the stored form of a student's reply.
type replyWire struct {
	Question int    `json:"question"`
	Option   int    `json:"option"`
	Text     string `json:"text"`
	Default  bool   `json:"default"`
}

func repliesText(replies []work.Reply) (sql.NullString, error) {
	if replies == nil {
		return sql.NullString{}, nil
	}
	out := make([]replyWire, 0, len(replies))
	for _, r := range replies {
		out = append(out, replyWire(r))
	}
	b, err := json.Marshal(out)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode replies: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func repliesFrom(s sql.NullString) ([]work.Reply, error) {
	if !s.Valid {
		return nil, nil
	}
	var in []replyWire
	if err := strictJSON(s.String, &in); err != nil {
		return nil, err
	}
	out := make([]work.Reply, 0, len(in))
	for _, r := range in {
		out = append(out, work.Reply(r))
	}
	return out, nil
}

func (r *WorkRepository) restore(ctx context.Context, row sqlcgen.Work) (*work.Work, error) {
	bad := func(what string, err error) (*work.Work, error) {
		return nil, fmt.Errorf("%s %s: %s: %w", opRestoreWork, row.ID, what, err)
	}
	rows, err := r.db.Queries(ctx).ListWorkRounds(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetWork, row.ID, MapError(ctx, err))
	}
	rounds := make([]work.Round, 0, len(rows))
	for _, rr := range rows {
		a, err := work.ParseAnswer(int(rr.Round), rr.Answer)
		if err != nil {
			return bad("answer", err)
		}
		replies, err := repliesFrom(rr.Replies)
		if err != nil {
			return bad("replies", err)
		}
		rounds = append(rounds, work.Round{Number: int(rr.Round), Answer: a, Replies: replies})
	}
	edits := map[work.Field]string{}
	for f, v := range map[work.Field]sql.NullString{
		work.FieldWorkType: row.EditWorkType, work.FieldVolume: row.EditVolume, work.FieldFileFormat: row.EditFileFormat,
		work.FieldFormatting: row.EditFormatting, work.FieldVariant: row.EditVariant, work.FieldDeadline: row.EditDeadline,
		work.FieldSources: row.EditSources,
	} {
		if v.Valid {
			edits[f] = v.String
		}
	}
	var writing *work.Writing
	if row.Writing.Valid {
		wr, err := work.ParseWriting(row.Writing.String)
		if err != nil {
			return bad("writing", err)
		}
		writing = &wr
	}
	src := work.Source{Assignment: shared.AssignmentID(row.AssignmentID.String), Material: shared.MaterialID(row.MaterialID.String)}
	if row.Topic.Valid || row.TopicRequest.Valid {
		// The stored topic passes the same rules as a new one and is stored clean already.
		if src.Topic, err = work.NewTopic(row.TopicRequest.String, row.Topic.String, shared.SubjectID(row.TopicSubjectID.String)); err != nil {
			return bad("topic", err)
		}
		if src.Topic.Text() != row.Topic.String {
			return bad("topic", work.ErrInvalidTopic)
		}
	}
	w, err := work.Restore(work.State{
		Version: row.Version, ID: shared.WorkID(row.ID), Source: src,
		Status: work.Status(row.Status), Round: int(row.Round), Attempt: int(row.Attempt), Rounds: rounds, Edits: edits,
		Writing: writing, CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return bad("state", err)
	}
	return w, nil
}
