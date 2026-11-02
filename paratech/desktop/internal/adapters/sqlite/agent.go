package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	opCurrentThread     = "sqlite: get current agent chat"
	opRequestScope      = "sqlite: get agent request scope"
	opRequestMentions   = "sqlite: get agent request mentions"
	opSaveAgentThread   = "sqlite: save agent thread"
	opListAgentThreads  = "sqlite: list running agent threads"
	opListAgentChats    = "sqlite: list agent chats"
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
	mentionWire struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Label string `json:"label"`
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

// Current loads the current chat (ADR-0028): the unarchived one active last. None wraps shared.ErrNotFound.
func (r *AgentThreadRepository) Current(ctx context.Context) (*thread.Thread, error) {
	row, err := r.db.Queries(ctx).GetCurrentAgentThread(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opCurrentThread, MapError(ctx, err))
	}
	return r.restore(ctx, row)
}

// List returns every chat: the unarchived ones by last activity, newest first, then the archived ones in the same
// order.
func (r *AgentThreadRepository) List(ctx context.Context) ([]*thread.Thread, error) {
	ids, err := r.db.Queries(ctx).ListAgentThreadIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAgentChats, MapError(ctx, err))
	}
	return r.getAll(ctx, ids)
}

// RequestScope returns the scope stored with request turn of a thread, the subject of that request (ADR-0028); a
// missing request wraps shared.ErrNotFound.
func (r *AgentThreadRepository) RequestScope(ctx context.Context, id shared.ThreadID, turn int) (values.Scope, error) {
	row, err := r.db.Queries(ctx).GetAgentTurn(ctx, sqlcgen.GetAgentTurnParams{ThreadID: string(id), Number: int64(turn)})
	if err != nil {
		return values.Scope{}, fmt.Errorf("%s %s/%d: %w", opRequestScope, id, turn, MapError(ctx, err))
	}
	var w scopeWire
	if err := strictJSON(row.Scope, &w); err != nil {
		return values.Scope{}, fmt.Errorf("stored agent request %s/%d: scope: %w", id, turn, err)
	}
	sc, err := scopeFrom(w)
	if err != nil {
		return values.Scope{}, fmt.Errorf("stored agent request %s/%d: scope: %w", id, turn, err)
	}
	return sc, nil
}

// RequestMentions returns the mentions stored with request turn of a thread, with the names shown at mention time
// (ADR-0028); a missing request wraps shared.ErrNotFound.
func (r *AgentThreadRepository) RequestMentions(ctx context.Context, id shared.ThreadID, turn int) ([]values.Mention, error) {
	row, err := r.db.Queries(ctx).GetAgentTurn(ctx, sqlcgen.GetAgentTurnParams{ThreadID: string(id), Number: int64(turn)})
	if err != nil {
		return nil, fmt.Errorf("%s %s/%d: %w", opRequestMentions, id, turn, MapError(ctx, err))
	}
	var ws []mentionWire
	if err := strictJSON(row.Mentions, &ws); err != nil {
		return nil, fmt.Errorf("stored agent request %s/%d: mentions: %w", id, turn, err)
	}
	ms, err := mentionsFrom(ws)
	if err != nil {
		return nil, fmt.Errorf("stored agent request %s/%d: mentions: %w", id, turn, err)
	}
	return ms, nil
}

// ListRunning returns the threads whose current request is PLANNING or EXECUTING.
func (r *AgentThreadRepository) ListRunning(ctx context.Context) ([]*thread.Thread, error) {
	ids, err := r.db.Queries(ctx).ListRunningAgentThreadIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAgentThreads, MapError(ctx, err))
	}
	return r.getAll(ctx, ids)
}

func (r *AgentThreadRepository) getAll(ctx context.Context, ids []string) ([]*thread.Thread, error) {
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
// compare-and-swap (ADR-0004(d)). A stale version or an id stored first is shared.ErrVersionConflict. Inside a
// caller's transaction the new version is set when this write succeeds, before that transaction commits; a caller
// whose transaction rolls back restores the old version.
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

	// The summary text is untrusted model output: stored as a parameter, never logged or put into SQL.
	sum := summaryColumns(st)
	next := st.Version + 1
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			err := DuplicateAsVersionConflict(MapError(ctx, q.InsertAgentThread(ctx, sqlcgen.InsertAgentThreadParams{
				ID: string(st.ID), Title: st.Title, Turns: int64(st.Turns), HistoryFrom: int64(st.HistoryFrom),
				Seen: string(seen), ArchivedAt: jobNullMs(st.ArchivedAt), LastActiveAt: jobMs(st.LastActive),
				SummaryText: sum.text, SummaryThrough: sum.through, SummaryAt: sum.at,
				ForkedFrom: jobNullString(string(st.ForkedFrom)), CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
			if err != nil {
				return err
			}
		} else {
			n, err := q.UpdateAgentThread(ctx, sqlcgen.UpdateAgentThreadParams{
				Title: st.Title, Turns: int64(st.Turns), HistoryFrom: int64(st.HistoryFrom), Seen: string(seen),
				ArchivedAt: jobNullMs(st.ArchivedAt), LastActiveAt: jobMs(st.LastActive),
				SummaryText: sum.text, SummaryThrough: sum.through, SummaryAt: sum.at,
				ForkedFrom: jobNullString(string(st.ForkedFrom)), UpdatedAt: jobMs(st.UpdatedAt),
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
	mentions := make([]mentionWire, len(t.Mentions))
	for i, m := range t.Mentions {
		mentions[i] = mentionWire{Kind: string(m.Ref().Kind()), ID: m.Ref().ID(), Label: m.Label()}
	}
	enc := map[string]any{"scope": scope, "mentions": mentions, "used": used, "open": open, "created": created}
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
		StopReason: string(t.StopReason), Scope: text["scope"], Mentions: text["mentions"], Steps: int64(t.Budget.Steps),
		Writes: int64(t.Budget.Writes), PaidUnplanned: int64(t.Budget.PaidUnplanned), PlannerInput: int64(t.Budget.InputTokens),
		QuestionRounds: int64(t.Budget.QuestionRounds), StepOpen: stepOpen, Retry: int64(t.Retry), ProposedPlan: proposed,
		ApprovedPlan: approved, Used: text["used"], OpenCalls: text["open"], CreatedIds: text["created"],
		// Written on every save as the aggregate holds it: a stopped request that goes on clears its line.
		ReplyLine: jobNullString(t.ReplyLine), StartedAt: jobMs(t.StartedAt), UpdatedAt: jobMs(t.UpdatedAt),
	}, nil
}

// summaryCols are the three summary columns of agent_threads: all NULL without a summary.
type summaryCols struct {
	text        sql.NullString
	through, at sql.NullInt64
}

func summaryColumns(st thread.State) summaryCols {
	if st.Summary.IsZero() {
		return summaryCols{}
	}
	return summaryCols{
		text:    sql.NullString{String: st.Summary.Text(), Valid: true},
		through: sql.NullInt64{Int64: int64(st.Summary.Through()), Valid: true},
		at:      jobNullMs(st.SummaryAt),
	}
}

// summaryFrom rebuilds the stored summary with the value checks; a partial or changed one is an error.
func summaryFrom(row sqlcgen.AgentThread) (values.HistorySummary, time.Time, error) {
	if !row.SummaryText.Valid && !row.SummaryThrough.Valid && !row.SummaryAt.Valid {
		return values.HistorySummary{}, time.Time{}, nil
	}
	if !row.SummaryText.Valid || !row.SummaryThrough.Valid || !row.SummaryAt.Valid {
		return values.HistorySummary{}, time.Time{}, errors.New("only some of the summary columns are set")
	}
	s, err := values.ParseHistorySummary(int(row.SummaryThrough.Int64), row.SummaryText.String)
	if err != nil {
		return values.HistorySummary{}, time.Time{}, err
	}
	if s.Text() != row.SummaryText.String {
		return values.HistorySummary{}, time.Time{}, errors.New("the summary text is not in its stored form")
	}
	return s, jobFromMs(row.SummaryAt.Int64), nil
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
		Version: row.Version, ID: shared.ThreadID(row.ID), Title: row.Title, Turns: int(row.Turns),
		HistoryFrom: int(row.HistoryFrom), ArchivedAt: jobFromNullMs(row.ArchivedAt), LastActive: jobFromMs(row.LastActiveAt),
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	var err error
	if st.Summary, st.SummaryAt, err = summaryFrom(row); err != nil {
		return bad("summary", err)
	}
	if row.ForkedFrom.Valid {
		if row.ForkedFrom.String == "" {
			return bad("fork source", errors.New("empty"))
		}
		st.ForkedFrom = shared.ThreadID(row.ForkedFrom.String)
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
		scope    scopeWire
		mentions []mentionWire
		used     []bool
		open     []openCallWire
		created  []createdWire
	)
	for _, col := range []struct {
		name string
		text string
		v    any
	}{
		{"scope", row.Scope, &scope}, {"mentions", row.Mentions, &mentions}, {"used", row.Used, &used},
		{"open calls", row.OpenCalls, &open}, {"created ids", row.CreatedIds, &created},
	} {
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
		DroppedBy: int(row.DroppedBy), ReplyLine: row.ReplyLine.String, StartedAt: jobFromMs(row.StartedAt),
		UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	if row.ReplyLine.Valid && row.ReplyLine.String == "" {
		return thread.TurnState{}, errors.New("reply line: empty")
	}
	if ts.Mentions, err = mentionsFrom(mentions); err != nil {
		return thread.TurnState{}, fmt.Errorf("mentions: %w", err)
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

func mentionsFrom(ws []mentionWire) ([]values.Mention, error) {
	var out []values.Mention
	for _, w := range ws {
		ref, err := values.NewRef(values.IDKind(w.Kind), w.ID)
		if err != nil {
			return nil, err
		}
		m, err := values.NewMention(ref, w.Label)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
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

// Since returns the last limit entries of the requests numbered from on, oldest first, each with the frozen reply line
// of its request ("" when it has none) and the IDs its request mentioned (one read for all requests, TASK-245).
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
	mentions := map[int64][]values.Ref{} // the column of a request is decoded once
	for _, row := range rows {
		e, err := entryFrom(row)
		if err != nil {
			return nil, fmt.Errorf("stored agent history %s/%d: %w", id, row.Seq, err)
		}
		refs, ok := mentions[row.Turn]
		if !ok {
			if refs, err = mentionRefsFrom(row.Mentions); err != nil {
				return nil, fmt.Errorf("stored agent history %s/%d: mentions of request %d: %w", id, row.Seq, row.Turn, err)
			}
			mentions[row.Turn] = refs
		}
		e.Mentions = slices.Clone(refs)
		out = append(out, e)
	}
	return out, nil
}

// mentionRefsFrom decodes the mentions column of a request into IDs; the labels stay here. No stored request (an
// entry without its row) has none.
func mentionRefsFrom(col sql.NullString) ([]values.Ref, error) {
	if !col.Valid {
		return nil, nil
	}
	var ws []mentionWire
	if err := strictJSON(col.String, &ws); err != nil {
		return nil, err
	}
	ms, err := mentionsFrom(ws)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	return values.MentionRefs(ms), nil
}

func entryFrom(row sqlcgen.ListAgentStepsRow) (values.HistoryEntry, error) {
	if row.ReplyLine.Valid {
		if err := (values.HistoryTurn{AgentReply: row.ReplyLine.String}).Validate(); err != nil {
			return values.HistoryEntry{}, fmt.Errorf("reply line of request %d: %w", row.Turn, err)
		}
	}
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
	return values.HistoryEntry{Turn: int(row.Turn), Entry: entry, ReplyLine: row.ReplyLine.String}, nil
}
