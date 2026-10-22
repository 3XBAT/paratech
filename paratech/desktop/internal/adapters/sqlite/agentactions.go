package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the action journal repository, used in its error messages.
const (
	opGetAgentAction   = "sqlite: get agent action"
	opSaveAgentAction  = "sqlite: save agent action"
	opListAgentActions = "sqlite: list agent actions"
)

// AgentActionRepository stores the agent action journal (the agentjournal.ActionRepository port, TASK-116). The
// compensation is a JSON object of its kind and typed IDs; Restore checks it against the tool again.
type AgentActionRepository struct{ db *DB }

// NewAgentActionRepository returns the action journal repository of db.
func NewAgentActionRepository(db *DB) *AgentActionRepository { return &AgentActionRepository{db: db} }

// compensationWire is the stored form of every compensation kind; fields a kind does not use stay empty.
type compensationWire struct {
	Kind       string `json:"kind"`
	Subject    string `json:"subject,omitempty"`
	Folder     string `json:"folder,omitempty"`
	Material   string `json:"material,omitempty"`
	Lesson     string `json:"lesson,omitempty"`
	Recording  string `json:"recording,omitempty"`
	OldSubject string `json:"old_subject,omitempty"`
	OldKind    string `json:"old_kind,omitempty"`
	Assignment string `json:"assignment,omitempty"`
	OldDue     string `json:"old_due,omitempty"`
	Job        string `json:"job,omitempty"`
}

// Get loads an entry; a missing one wraps shared.ErrNotFound.
func (r *AgentActionRepository) Get(ctx context.Context, id shared.ActionID) (*action.Action, error) {
	row, err := r.db.Queries(ctx).GetAgentAction(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetAgentAction, id, MapError(ctx, err))
	}
	return actionFromRow(row)
}

// Save writes the entry: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). A second entry
// for the same call of a request is shared.ErrDuplicate; a stale version shared.ErrVersionConflict.
func (r *AgentActionRepository) Save(ctx context.Context, a *action.Action) error {
	st := a.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveAgentAction, st.Version)
	}
	comp, err := compensationText(st.Compensation)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveAgentAction, st.ID, err)
	}
	proposal := ""
	if !st.Proposal.IsZero() {
		proposal = st.Proposal.String()
	}

	next := st.Version + 1
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertAgentAction(ctx, sqlcgen.InsertAgentActionParams{
				ID: string(st.ID), ThreadID: string(st.Thread), Turn: int64(st.Turn), CallID: st.CallID, Tool: string(st.Tool),
				Status: string(st.Status), Compensation: comp, TargetVersion: st.TargetVersion, Proposal: proposal,
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateAgentAction(ctx, sqlcgen.UpdateAgentActionParams{
			Status: string(st.Status), Compensation: comp, TargetVersion: st.TargetVersion, UpdatedAt: jobMs(st.UpdatedAt),
			ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveAgentAction, st.ID, err)
	}
	a.Persisted(next)
	return nil
}

// ListByThread returns the entries of a thread, newest first, at most limit.
func (r *AgentActionRepository) ListByThread(ctx context.Context, th shared.ThreadID, limit int) ([]*action.Action, error) {
	rows, err := r.db.Queries(ctx).ListAgentActionsByThread(ctx, sqlcgen.ListAgentActionsByThreadParams{
		ThreadID: string(th), MaxRows: int64(max(limit, 0)),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListAgentActions, th, MapError(ctx, err))
	}
	return actionsFromRows(rows)
}

// ListUnfinished returns PENDING and UNDOING entries, oldest first, for startup recovery.
func (r *AgentActionRepository) ListUnfinished(ctx context.Context) ([]*action.Action, error) {
	rows, err := r.db.Queries(ctx).ListUnfinishedAgentActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s unfinished: %w", opListAgentActions, MapError(ctx, err))
	}
	return actionsFromRows(rows)
}

func actionsFromRows(rows []sqlcgen.AgentAction) ([]*action.Action, error) {
	out := make([]*action.Action, 0, len(rows))
	for _, row := range rows {
		a, err := actionFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func actionFromRow(row sqlcgen.AgentAction) (*action.Action, error) {
	bad := func(what string, err error) (*action.Action, error) {
		return nil, fmt.Errorf("stored agent action %s: %s: %w", row.ID, what, err)
	}
	comp, err := compensationFrom(row.Compensation)
	if err != nil {
		return bad("compensation", err)
	}
	var proposal values.Ref
	if row.Proposal != "" {
		kind, id, _ := strings.Cut(row.Proposal, ":")
		if proposal, err = values.NewRef(values.IDKind(kind), id); err != nil {
			return bad("proposal", err)
		}
	}
	a, err := action.Restore(action.State{
		Version: row.Version, ID: shared.ActionID(row.ID), Thread: shared.ThreadID(row.ThreadID), Turn: int(row.Turn),
		CallID: row.CallID, Tool: values.Tool(row.Tool), Status: action.Status(row.Status), Compensation: comp,
		TargetVersion: row.TargetVersion, Proposal: proposal, CreatedAt: jobFromMs(row.CreatedAt),
		UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return bad("restore", err)
	}
	return a, nil
}

func compensationText(c action.Compensation) (sql.NullString, error) {
	if c == nil {
		return sql.NullString{}, nil
	}
	w := compensationWire{Kind: string(c.Kind())}
	switch v := c.(type) {
	case action.RemoveSubject:
		w.Subject = v.Subject.ID()
	case action.RemoveFolder:
		w.Subject, w.Folder = v.Subject.ID(), v.Folder.ID()
	case action.ReturnToInbox:
		w.Material, w.Lesson = v.Material.ID(), v.Lesson.ID()
	case action.UnlinkMaterial:
		w.Material, w.Lesson = v.Material.ID(), v.Lesson.ID()
	case action.RestoreLessonInfo:
		w.Recording, w.OldSubject, w.OldKind = v.Recording.ID(), v.OldSubject, v.OldKind
	case action.RestoreDue:
		w.Assignment = v.Assignment.ID()
		if !v.OldDue.IsZero() {
			w.OldDue = v.OldDue.UTC().Format(time.RFC3339Nano)
		}
	case action.CancelJob:
		w.Job = v.Job.ID()
	default:
		return sql.NullString{}, fmt.Errorf("unknown compensation %s", c.Kind())
	}
	b, err := json.Marshal(w)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode compensation: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func compensationFrom(s sql.NullString) (action.Compensation, error) {
	if !s.Valid {
		return nil, nil
	}
	var w compensationWire
	if err := strictJSON(s.String, &w); err != nil {
		return nil, err
	}
	ref := func(kind values.IDKind, id string) values.Ref {
		r, _ := optionalRef(kind, id) // a malformed id stays zero and fails the compensation's check in Restore
		return r
	}
	switch action.CompensationKind(w.Kind) {
	case action.CompRemoveSubject:
		return action.RemoveSubject{Subject: ref(values.KindSubject, w.Subject)}, nil
	case action.CompRemoveFolder:
		return action.RemoveFolder{Subject: ref(values.KindSubject, w.Subject), Folder: ref(values.KindFolder, w.Folder)}, nil
	case action.CompReturnToInbox:
		lesson, err := optionalRef(values.KindLesson, w.Lesson)
		if err != nil {
			return nil, err
		}
		return action.ReturnToInbox{Material: ref(values.KindMaterial, w.Material), Lesson: lesson}, nil
	case action.CompUnlinkMaterial:
		return action.UnlinkMaterial{Material: ref(values.KindMaterial, w.Material), Lesson: ref(values.KindLesson, w.Lesson)}, nil
	case action.CompRestoreLessonInfo:
		return action.RestoreLessonInfo{Recording: ref(values.KindRecording, w.Recording), OldSubject: w.OldSubject, OldKind: w.OldKind}, nil
	case action.CompRestoreDue:
		var due time.Time
		if w.OldDue != "" {
			d, err := time.Parse(time.RFC3339Nano, w.OldDue)
			if err != nil {
				return nil, err
			}
			due = d.UTC()
		}
		return action.RestoreDue{Assignment: ref(values.KindAssignment, w.Assignment), OldDue: due}, nil
	case action.CompCancelJob:
		return action.CancelJob{Job: ref(values.KindJob, w.Job)}, nil
	}
	return nil, fmt.Errorf("unknown compensation kind %q", w.Kind)
}
