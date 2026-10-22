package testenv

import (
	"context"
	"sync"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
)

// AgentTurns is an in-memory store of kept model turns for the agent step's AssistantTurns port, with the semantics
// of the postgres adapter. Err, when set, fails every call. Safe for concurrent use.
type AgentTurns struct {
	mu    sync.Mutex
	turns map[agentTurnKey]keptTurn
	err   error
}

type agentTurnKey struct {
	owner shared.APIKeyID
	key   string
}

type keptTurn struct {
	turn values.AssistantTurn
	at   time.Time
}

// NewAgentTurns returns an empty store.
func NewAgentTurns() *AgentTurns {
	return &AgentTurns{turns: map[agentTurnKey]keptTurn{}}
}

// Fail makes every later call return err (nil restores the store).
func (s *AgentTurns) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Keep stores turn for owner under key; an entry already kept stays as it is.
func (s *AgentTurns) Keep(_ context.Context, owner shared.APIKeyID, key string, turn values.AssistantTurn, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	k := agentTurnKey{owner: owner, key: key}
	if _, ok := s.turns[k]; !ok {
		s.turns[k] = keptTurn{turn: turn, at: now}
	}
	return nil
}

// Kept returns the turn kept for owner under key after since.
func (s *AgentTurns) Kept(_ context.Context, owner shared.APIKeyID, key string, since time.Time) (values.AssistantTurn, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return values.AssistantTurn{}, false, s.err
	}
	t, ok := s.turns[agentTurnKey{owner: owner, key: key}]
	if !ok || !t.at.After(since) {
		return values.AssistantTurn{}, false, nil
	}
	return t.turn, true, nil
}

// PurgeTurns deletes every entry kept at or before cutoff.
func (s *AgentTurns) PurgeTurns(_ context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	var n int64
	for k, t := range s.turns {
		if !t.at.After(cutoff) {
			delete(s.turns, k)
			n++
		}
	}
	return n, nil
}

// Len is the number of kept entries of owner.
func (s *AgentTurns) Len(owner shared.APIKeyID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.turns {
		if k.owner == owner {
			n++
		}
	}
	return n
}

// Drop deletes every entry of owner, as if they had expired.
func (s *AgentTurns) Drop(owner shared.APIKeyID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.turns {
		if k.owner == owner {
			delete(s.turns, k)
		}
	}
}
