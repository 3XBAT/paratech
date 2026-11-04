package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the chat fork (ADR-0028 «Форк»).
const (
	opForkSource = "sqlite: read agent fork source"
	opSaveFork   = "sqlite: save agent fork"
)

// ForkSource reads requests 1…upTo of a chat that are not dropped, oldest first (the agentchats port): texts, scope,
// mentions, frozen line and times, and every entry of a request whose line is not frozen. One transaction, so a purge
// of the steps cannot fall between the lines and the entries; no row limit, so a long chat keeps its first requests.
// Stored values are checked as Get and Since check them.
func (r *AgentThreadRepository) ForkSource(ctx context.Context, id shared.ThreadID, upTo int) ([]thread.PastRequest, error) {
	var out []thread.PastRequest
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		turns, err := q.ListAgentForkTurns(ctx, sqlcgen.ListAgentForkTurnsParams{ThreadID: string(id), UpTo: int64(upTo)})
		if err != nil {
			return MapError(ctx, err)
		}
		rows, err := q.ListAgentForkTexts(ctx, sqlcgen.ListAgentForkTextsParams{ThreadID: string(id), UpTo: int64(upTo)})
		if err != nil {
			return MapError(ctx, err)
		}
		texts := map[int64][]string{}
		for _, row := range rows {
			e, err := storedEntry(id, row.Seq, row.Turn, row.Entry, row.Body)
			if err != nil {
				return err
			}
			texts[row.Turn] = append(texts[row.Turn], e.StudentText)
		}

		for _, row := range turns {
			ts, err := turnState(row)
			if err != nil {
				return fmt.Errorf("stored agent request %s/%d: %w", id, row.Number, err)
			}
			if ts.ReplyLine != "" {
				if err := (values.HistoryTurn{AgentReply: ts.ReplyLine}).Validate(); err != nil {
					return fmt.Errorf("stored agent request %s/%d: reply line: %w", id, row.Number, err)
				}
			}
			p := thread.PastRequest{
				Number: ts.Number, Scope: ts.Scope, Mentions: ts.Mentions, Texts: texts[row.Number], ReplyLine: ts.ReplyLine,
				StartedAt: ts.StartedAt, EndedAt: ts.UpdatedAt,
			}
			if !row.ReplyLine.Valid {
				if p.Entries, err = requestEntries(ctx, q, id, row.Number); err != nil {
					return err
				}
			}
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opForkSource, id, err)
	}
	return out, nil
}

func requestEntries(ctx context.Context, q *sqlcgen.Queries, id shared.ThreadID, turn int64) ([]values.HistoryTurn, error) {
	rows, err := q.ListAgentRequestEntries(ctx, sqlcgen.ListAgentRequestEntriesParams{ThreadID: string(id), Turn: turn})
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make([]values.HistoryTurn, 0, len(rows))
	for _, row := range rows {
		e, err := storedEntry(id, row.Seq, row.Turn, row.Entry, row.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// storedEntry decodes a step row with the checks of Since (entryFrom).
func storedEntry(id shared.ThreadID, seq, turn int64, entry, body string) (values.HistoryTurn, error) {
	e, err := entryFrom(sqlcgen.ListAgentStepsRow{ThreadID: string(id), Seq: seq, Turn: turn, Entry: entry, Body: body})
	if err != nil {
		return values.HistoryTurn{}, fmt.Errorf("stored agent history %s/%d: %w", id, seq, err)
	}
	return e.Entry, nil
}

// forkStep is a student text of a copy as agent_steps stores it.
type forkStep struct {
	turn int64
	body string
}

// SaveFork inserts a forked chat (the agentchats port) in one transaction: the thread with its current request (Save),
// the other copies as DONE requests and every copy's student texts as 'student' entries, in request order, stamped
// with the chat's creation. Nothing else is written: no other steps, actions or seen ids. An id stored first is
// shared.ErrVersionConflict; on any error nothing is written and the thread stays unsaved.
func (r *AgentThreadRepository) SaveFork(ctx context.Context, f thread.Fork) error {
	if f.Thread == nil {
		return fmt.Errorf("%s: no chat", opSaveFork)
	}
	st := f.Thread.State()
	if st.Version != 0 {
		return fmt.Errorf("%s %s: a fork is a new chat, version %d", opSaveFork, st.ID, st.Version)
	}
	n := len(f.Requests)
	if (n == 0) != (st.Current == nil) || (n > 0 && f.Requests[n-1].Number != st.Current.Number) {
		return fmt.Errorf("%s %s: the last copy is not the current request", opSaveFork, st.ID)
	}

	var others []sqlcgen.InsertAgentTurnParams
	var steps []forkStep
	for i, c := range f.Requests {
		if i < n-1 {
			p, err := turnParams(st.ID, c.State())
			if err != nil {
				return fmt.Errorf("%s %s: request %d: %w", opSaveFork, st.ID, c.Number, err)
			}
			others = append(others, p)
		}
		if len(c.Texts) == 0 {
			return fmt.Errorf("%s %s: request %d: no text", opSaveFork, st.ID, c.Number)
		}
		for _, text := range c.Texts {
			if err := (values.HistoryTurn{StudentText: text}).Validate(); err != nil {
				return fmt.Errorf("%s %s: request %d: %w", opSaveFork, st.ID, c.Number, err)
			}
			raw, err := json.Marshal(stepBodyWire{Text: text})
			if err != nil {
				return fmt.Errorf("%s %s: encode: %w", opSaveFork, st.ID, err)
			}
			steps = append(steps, forkStep{turn: int64(c.Number), body: string(raw)})
		}
	}

	err := r.db.Do(ctx, func(ctx context.Context) error {
		if err := r.Save(ctx, f.Thread); err != nil {
			return err
		}
		q := r.db.Queries(ctx)
		for _, p := range others {
			if err := q.InsertAgentTurn(ctx, p); err != nil {
				return MapError(ctx, err)
			}
		}
		last, err := q.LastAgentStepSeq(ctx, string(st.ID))
		if err != nil {
			return MapError(ctx, err)
		}
		for i, s := range steps {
			if err := q.InsertAgentStep(ctx, sqlcgen.InsertAgentStepParams{
				ThreadID: string(st.ID), Seq: last + 1 + int64(i), Turn: s.turn, Entry: entryStudent, Body: s.body,
				CreatedAt: jobMs(st.CreatedAt),
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		f.Thread.Persisted(st.Version) // Save set the new version before the transaction rolled back
		return fmt.Errorf("%s %s: %w", opSaveFork, st.ID, err)
	}
	return nil
}
