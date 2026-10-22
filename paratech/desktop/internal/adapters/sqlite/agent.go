package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the agent repositories, used in their error messages.
const (
	opGetAgentThread    = "sqlite: get agent thread"
	opSaveAgentThread   = "sqlite: save agent thread"
	opListAgentThreads  = "sqlite: list running agent threads"
	opAppendAgentStep   = "sqlite: append agent history"
	opListAgentSteps    = "sqlite: read agent history"
	maxAgentHistoryRows = 1024
)

// History entry kinds of agent_steps.entry.
const (
	entryStudent = "student"
	entryCalls   = "calls"
	entryResults = "results"
)

// AgentThreadRepository stores agent threads with their current request (the agentturn.ThreadRepository port,
// TASK-116). Validation stays in thread.Thread: the repository only converts rows; JSON columns are read strictly
// and checked again by Restore.
type AgentThreadRepository struct{ db *DB }

// NewAgentThreadRepository returns the agent thread repository of db.
func NewAgentThreadRepository(db *DB) *AgentThreadRepository { return &AgentThreadRepository{db: db} }

// Wire forms of the JSON columns.
type (
	refWire struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	scopeWire struct {
		Subject    string `json:"subject,omitempty"`
		Screen     string `json:"screen,omitempty"`
		Lesson     string `json:"lesson,omitempty"`
		Assignment string `json:"assignment,omitempty"`
	}
	resultWire struct {
		CallID     string `json:"call_id"`
		ResultJSON string `json:"result_json"`
		IsError    bool   `json:"is_error,omitempty"`
	}
	openCallWire struct {
		ID       string      `json:"id"`
		Tool     string      `json:"tool,omitempty"`
		PlanStep int         `json:"plan_step"`
		Admitted bool        `json:"admitted,omitempty"`
		Result   *resultWire `json:"result,omitempty"`
	}
	createdWire struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		Step int    `json:"step"`
	}
	callWire struct {
		CallID        string `json:"call_id"`
		Tool          string `json:"tool"`
		ArgumentsJSON string `json:"arguments_json"`
	}
	stepBodyWire struct {
		Text    string       `json:"text,omitempty"`
		Calls   []callWire   `json:"calls,omitempty"`
		Seal    []byte       `json:"seal,omitempty"`
		Results []resultWire `json:"results,omitempty"`
	}
)

// Get loads a thread; a missing one wraps shared.ErrNotFound.
func (r *AgentThreadRepository) Get(ctx context.Context, id shared.ThreadID) (*thread.Thread, error) {
	row, err := r.db.Queries(ctx).GetAgentThread(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetAgentThread, id, MapError(ctx, err))
	}
	return r.restore(ctx, row)
}

// GetByKey loads the thread of a subject key; a missing one wraps shared.ErrNotFound.
func (r *AgentThreadRepository) GetByKey(ctx context.Context, key thread.Key) (*thread.Thread, error) {
	row, err := r.db.Queries(ctx).GetAgentThreadByKey(ctx, key.String())
	if err != nil {
		return nil, fmt.Errorf("%s by key: %w", opGetAgentThread, MapError(ctx, err))
	}
	return r.restore(ctx, row)
}

// ListRunning returns the threads whose current request is PLANNING or EXECUTING.
func (r *AgentThreadRepository) ListRunning(ctx context.Context) ([]*thread.Thread, error) {
	ids, err := r.db.Queries(ctx).ListRunningAgentThreadIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAgentThreads, MapError(ctx, err))
	}
	out := make([]*thread.Thread, 0, len(ids))
	for _, id := range ids {
		th, err := r.Get(ctx, shared.ThreadID(id))
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, nil
}

// Save writes the thread and its current request in one transaction: an INSERT for version 0, otherwise a version
// compare-and-swap (ADR-0004(d)). A stale version or an id stored first is shared.ErrVersionConflict; a second
// thread for the same key is shared.ErrDuplicate. Inside a caller's transaction the new version is set when this
// write succeeds, before that transaction commits; a caller whose transaction rolls back restores the old version.
func (r *AgentThreadRepository) Save(ctx context.Context, th *thread.Thread) error {
	st := th.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveAgentThread, st.Version)
	}
	seen, err := json.Marshal(refsWire(st.Seen))
	if err != nil {
		return fmt.Errorf("%s %s: encode seen ids: %w", opSaveAgentThread, st.ID, err)
	}
	var turn *sqlcgen.InsertAgentTurnParams
	if st.Current != nil {
		p, err := turnParams(st.ID, *st.Current)
		if err != nil {
			return fmt.Errorf("%s %s: %w", opSaveAgentThread, st.ID, err)
		}
		turn = &p
	}

	next := st.Version + 1
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			err := DuplicateAsVersionConflict(MapError(ctx, q.InsertAgentThread(ctx, sqlcgen.InsertAgentThreadParams{
				ID: string(st.ID), SubjectKey: st.Key.String(), Turns: int64(st.Turns), HistoryFrom: int64(st.HistoryFrom),
				Seen: string(seen), CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
			if err != nil {
				return err
			}
		} else {
			n, err := q.UpdateAgentThread(ctx, sqlcgen.UpdateAgentThreadParams{
				Turns: int64(st.Turns), HistoryFrom: int64(st.HistoryFrom), Seen: string(seen), UpdatedAt: jobMs(st.UpdatedAt),
				ID: string(st.ID), ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, n, err); err != nil {
				return err
			}
		}
		if turn == nil {
			return nil
		}
		if err := q.CancelEarlierAgentTurns(ctx, sqlcgen.CancelEarlierAgentTurnsParams{
			UpdatedAt: turn.UpdatedAt, ThreadID: turn.ThreadID, Number: turn.Number,
		}); err != nil {
			return MapError(ctx, err)
		}
		if err := q.DeleteAgentTurn(ctx, sqlcgen.DeleteAgentTurnParams{ThreadID: turn.ThreadID, Number: turn.Number}); err != nil {
			return MapError(ctx, err)
		}
		return MapError(ctx, q.InsertAgentTurn(ctx, *turn))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveAgentThread, st.ID, err)
	}
	th.Persisted(next)
	return nil
}

func turnParams(id shared.ThreadID, t thread.TurnState) (sqlcgen.InsertAgentTurnParams, error) {
	proposed, err := plansWire(t.ProposedPlan)
	if err != nil {
		return sqlcgen.InsertAgentTurnParams{}, fmt.Errorf("encode proposed plan: %w", err)
	}
	approved, err := plansWire(t.ApprovedPlan)
	if err != nil {
		return sqlcgen.InsertAgentTurnParams{}, fmt.Errorf("encode approved plan: %w", err)
	}
	scope := scopeWire{
		Subject: t.Scope.Subject().ID(), Screen: string(t.Scope.Screen()), Lesson: t.Scope.Lesson().ID(),
		Assignment: t.Scope.Assignment().ID(),
	}
	open := make([]openCallWire, len(t.Open))
	for i, c := range t.Open {
		open[i] = openCallWire{ID: c.ID, Tool: string(c.Tool), PlanStep: c.PlanStep, Admitted: c.Admitted}
		if c.Answered {
			open[i].Result = &resultWire{CallID: c.Result.CallID, ResultJSON: c.Result.ResultJSON, IsError: c.Result.IsError}
		}
	}
	created := make([]createdWire, len(t.Created))
	for i, c := range t.Created {
		created[i] = createdWire{Kind: string(c.Ref.Kind()), ID: c.Ref.ID(), Step: c.Step}
	}
	used := t.Used
	if used == nil {
		used = []bool{}
	}
	enc := map[string]any{"scope": scope, "used": used, "open": open, "created": created}
	text := map[string]string{}
	for k, v := range enc {
		b, err := json.Marshal(v)
		if err != nil {
			return sqlcgen.InsertAgentTurnParams{}, fmt.Errorf("encode %s: %w", k, err)
		}
		text[k] = string(b)
	}
	stepOpen := int64(0)
	if t.StepOpen {
		stepOpen = 1
	}
	return sqlcgen.InsertAgentTurnParams{
		ThreadID: string(id), Number: int64(t.Number), Status: string(t.Status), ResumeStatus: string(t.Resume),
		StopReason: string(t.StopReason), Scope: text["scope"], Steps: int64(t.Budget.Steps), Writes: int64(t.Budget.Writes),
		PaidUnplanned: int64(t.Budget.PaidUnplanned), PlannerInput: int64(t.Budget.InputTokens),
		QuestionRounds: int64(t.Budget.QuestionRounds), StepOpen: stepOpen, Retry: int64(t.Retry), ProposedPlan: proposed,
		ApprovedPlan: approved, Used: text["used"], OpenCalls: text["open"], CreatedIds: text["created"],
		StartedAt: jobMs(t.StartedAt), UpdatedAt: jobMs(t.UpdatedAt),
	}, nil
}

func plansWire(steps []values.PlanStep) (string, error) {
	if len(steps) == 0 {
		return "[]", nil
	}
	return values.EncodePlanSteps(steps)
}

func plansFrom(s string) ([]values.PlanStep, error) {
	if s == "[]" {
		return nil, nil
	}
	return values.DecodePlanSteps(s)
}

func refsWire(refs []values.Ref) []refWire {
	out := make([]refWire, len(refs))
	for i, r := range refs {
		out[i] = refWire{Kind: string(r.Kind()), ID: r.ID()}
	}
	return out
}

func optionalRef(kind values.IDKind, id string) (values.Ref, error) {
	if id == "" {
		return values.Ref{}, nil
	}
	return values.NewRef(kind, id)
}

func (r *AgentThreadRepository) restore(ctx context.Context, row sqlcgen.AgentThread) (*thread.Thread, error) {
	bad := func(what string, err error) (*thread.Thread, error) {
		return nil, fmt.Errorf("stored agent thread %s: %s: %w", row.ID, what, err)
	}
	var seenWire []refWire
	if err := strictJSON(row.Seen, &seenWire); err != nil {
		return bad("seen ids", err)
	}
	st := thread.State{
		Version: row.Version, ID: shared.ThreadID(row.ID), Key: thread.Key(row.SubjectKey), Turns: int(row.Turns),
		HistoryFrom: int(row.HistoryFrom), CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	for _, w := range seenWire {
		ref, err := values.NewRef(values.IDKind(w.Kind), w.ID)
		if err != nil {
			return bad("seen ids", err)
		}
		st.Seen = append(st.Seen, ref)
	}
	if row.Turns > 0 {
		turn, err := r.db.Queries(ctx).GetAgentTurn(ctx, sqlcgen.GetAgentTurnParams{ThreadID: row.ID, Number: row.Turns})
		if err != nil {
			return nil, fmt.Errorf("%s %s: request %d: %w", opGetAgentThread, row.ID, row.Turns, MapError(ctx, err))
		}
		ts, err := turnState(turn)
		if err != nil {
			return bad(fmt.Sprintf("request %d", row.Turns), err)
		}
		st.Current = &ts
	}
	th, err := thread.Restore(st)
	if err != nil {
		return bad("restore", err)
	}
	return th, nil
}

func turnState(row sqlcgen.AgentTurn) (thread.TurnState, error) {
	var (
		scope   scopeWire
		used    []bool
		open    []openCallWire
		created []createdWire
	)
	for _, col := range []struct {
		name string
		text string
		v    any
	}{{"scope", row.Scope, &scope}, {"used", row.Used, &used}, {"open calls", row.OpenCalls, &open}, {"created ids", row.CreatedIds, &created}} {
		if err := strictJSON(col.text, col.v); err != nil {
			return thread.TurnState{}, fmt.Errorf("%s: %w", col.name, err)
		}
	}
	sc, err := scopeFrom(scope)
	if err != nil {
		return thread.TurnState{}, fmt.Errorf("scope: %w", err)
	}
	proposed, err := plansFrom(row.ProposedPlan)
	if err != nil {
		return thread.TurnState{}, fmt.Errorf("proposed plan: %w", err)
	}
	approved, err := plansFrom(row.ApprovedPlan)
	if err != nil {
		return thread.TurnState{}, fmt.Errorf("approved plan: %w", err)
	}
	ts := thread.TurnState{
		Number: int(row.Number), Scope: sc, Status: thread.Status(row.Status), Resume: thread.Status(row.ResumeStatus),
		StopReason: values.StopReason(row.StopReason),
		Budget: thread.Budget{
			Steps: int(row.Steps), Writes: int(row.Writes), PaidUnplanned: int(row.PaidUnplanned),
			InputTokens: int(row.PlannerInput), QuestionRounds: int(row.QuestionRounds),
		},
		StepOpen: row.StepOpen == 1, ProposedPlan: proposed, ApprovedPlan: approved, Used: used, Retry: int(row.Retry),
		StartedAt: jobFromMs(row.StartedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	if len(ts.Used) == 0 {
		ts.Used = nil
	}
	for _, w := range open {
		oc := thread.OpenCall{ID: w.ID, Tool: values.Tool(w.Tool), PlanStep: w.PlanStep, Admitted: w.Admitted}
		if w.Result != nil {
			oc.Answered = true
			oc.Result = values.ToolResult{CallID: w.Result.CallID, ResultJSON: w.Result.ResultJSON, IsError: w.Result.IsError}
		}
		ts.Open = append(ts.Open, oc)
	}
	for _, w := range created {
		ref, err := values.NewRef(values.IDKind(w.Kind), w.ID)
		if err != nil {
			return thread.TurnState{}, fmt.Errorf("created ids: %w", err)
		}
		ts.Created = append(ts.Created, thread.Created{Ref: ref, Step: w.Step})
	}
	return ts, nil
}

func scopeFrom(w scopeWire) (values.Scope, error) {
	subject, err := optionalRef(values.KindSubject, w.Subject)
	if err != nil {
		return values.Scope{}, err
	}
	lesson, err := optionalRef(values.KindLesson, w.Lesson)
	if err != nil {
		return values.Scope{}, err
	}
	assignment, err := optionalRef(values.KindAssignment, w.Assignment)
	if err != nil {
		return values.Scope{}, err
	}
	return values.NewScope(subject, values.Screen(w.Screen), lesson, assignment)
}

// AgentHistory stores the history entries of agent threads (the agentturn.History port). Entries are appended in
// order and never changed; created_at serves the 90-day retention.
type AgentHistory struct {
	db    *DB
	clock interface{ Now() time.Time }
}

// NewAgentHistory returns the agent history of db; clock stamps the entries.
func NewAgentHistory(db *DB, clock interface{ Now() time.Time }) *AgentHistory {
	return &AgentHistory{db: db, clock: clock}
}

// Append adds an entry after the last one of the thread. Called inside the transaction that saves the thread, it
// takes part in it.
func (h *AgentHistory) Append(ctx context.Context, id shared.ThreadID, turn int, entry values.HistoryTurn) error {
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("%s %s: %w", opAppendAgentStep, id, err)
	}
	kind, body := entryStudent, stepBodyWire{Text: entry.StudentText}
	switch {
	case entry.Calls != nil:
		kind, body = entryCalls, stepBodyWire{Seal: entry.Calls.Seal}
		for _, c := range entry.Calls.Calls {
			body.Calls = append(body.Calls, callWire{CallID: c.CallID, Tool: c.Tool, ArgumentsJSON: c.ArgumentsJSON})
		}
	case len(entry.Results) > 0:
		kind, body = entryResults, stepBodyWire{}
		for _, r := range entry.Results {
			body.Results = append(body.Results, resultWire{CallID: r.CallID, ResultJSON: r.ResultJSON, IsError: r.IsError})
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s %s: encode: %w", opAppendAgentStep, id, err)
	}
	err = h.db.Do(ctx, func(ctx context.Context) error {
		q := h.db.Queries(ctx)
		last, err := q.LastAgentStepSeq(ctx, string(id))
		if err != nil {
			return MapError(ctx, err)
		}
		return MapError(ctx, q.InsertAgentStep(ctx, sqlcgen.InsertAgentStepParams{
			ThreadID: string(id), Seq: last + 1, Turn: int64(turn), Entry: kind, Body: string(raw),
			CreatedAt: jobMs(h.clock.Now()),
		}))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opAppendAgentStep, id, err)
	}
	return nil
}

// Since returns the last limit entries of the requests numbered from on, oldest first.
func (h *AgentHistory) Since(ctx context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	limit = min(max(limit, 0), maxAgentHistoryRows)
	rows, err := h.db.Queries(ctx).ListAgentSteps(ctx, sqlcgen.ListAgentStepsParams{
		ThreadID: string(id), FromTurn: int64(from), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListAgentSteps, id, MapError(ctx, err))
	}
	slices.Reverse(rows)
	out := make([]values.HistoryEntry, 0, len(rows))
	for _, row := range rows {
		e, err := entryFrom(row)
		if err != nil {
			return nil, fmt.Errorf("stored agent history %s/%d: %w", id, row.Seq, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func entryFrom(row sqlcgen.AgentStep) (values.HistoryEntry, error) {
	var body stepBodyWire
	if err := strictJSON(row.Body, &body); err != nil {
		return values.HistoryEntry{}, err
	}
	var entry values.HistoryTurn
	switch row.Entry {
	case entryStudent:
		entry.StudentText = body.Text
	case entryCalls:
		sealed := &values.SealedCalls{Seal: body.Seal}
		for _, c := range body.Calls {
			sealed.Calls = append(sealed.Calls, values.RawCall{CallID: c.CallID, Tool: c.Tool, ArgumentsJSON: c.ArgumentsJSON})
		}
		entry.Calls = sealed
	case entryResults:
		for _, r := range body.Results {
			entry.Results = append(entry.Results, values.ToolResult{CallID: r.CallID, ResultJSON: r.ResultJSON, IsError: r.IsError})
		}
	default:
		return values.HistoryEntry{}, fmt.Errorf("unknown entry %q", row.Entry)
	}
	if err := entry.Validate(); err != nil {
		return values.HistoryEntry{}, err
	}
	return values.HistoryEntry{Turn: int(row.Turn), Entry: entry}, nil
}
