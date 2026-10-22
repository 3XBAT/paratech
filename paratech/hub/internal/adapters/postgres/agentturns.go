package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in error messages.
const (
	opKeepTurn   = "keep agent turn"
	opReadTurn   = "read agent turn"
	opPurgeTurns = "purge agent turns"
)

// turnTimeout bounds one read or write of a kept turn.
const turnTimeout = 5 * time.Second

// AgentTurns keeps the model turns of AgentStep (table agent_turns of migration 0007). It implements the
// AssistantTurns port of the agent step use case. Every query is scoped by the owner; the content is never logged
// and never part of an error.
type AgentTurns struct {
	db *DB
}

// AgentTurns returns the kept turn repository of db.
func (db *DB) AgentTurns() *AgentTurns { return &AgentTurns{db: db} }

// Keep stores turn for owner under key at now; an entry already kept under the key stays as it is. An owner that is
// not a stored API key is shared.ErrNotFound.
func (r *AgentTurns) Keep(ctx context.Context, owner shared.APIKeyID, key string, turn values.AssistantTurn, now time.Time) error {
	if err := r.db.open(); err != nil {
		return fmt.Errorf("%s: %w", opKeepTurn, err)
	}
	ctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	// A non-nil slice, so that "no turn" is stored as '' and not as NULL.
	_, err := r.db.pool.Exec(ctx, "INSERT INTO agent_turns (api_key_id, call_set, content, created_at) VALUES ($1, $2, $3, $4) "+
		"ON CONFLICT (api_key_id, call_set) DO NOTHING", owner.String(), key, append([]byte{}, turn.Raw()...), now)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		return fmt.Errorf("%s: %w", opKeepTurn, shared.ErrNotFound)
	}
	return r.db.queryError(ctx, opKeepTurn, err)
}

// Kept returns the turn kept for owner under key after since; ok is false when there is none.
func (r *AgentTurns) Kept(ctx context.Context, owner shared.APIKeyID, key string, since time.Time) (values.AssistantTurn, bool, error) {
	if err := r.db.open(); err != nil {
		return values.AssistantTurn{}, false, fmt.Errorf("%s: %w", opReadTurn, err)
	}
	ctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	var content []byte
	err := r.db.pool.QueryRow(ctx, "SELECT content FROM agent_turns WHERE api_key_id = $1 AND call_set = $2 AND created_at > $3",
		owner.String(), key, since).Scan(&content)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return values.AssistantTurn{}, false, nil
	case err != nil:
		return values.AssistantTurn{}, false, r.db.queryError(ctx, opReadTurn, err)
	case len(content) == 0:
		return values.AssistantTurn{}, true, nil
	}
	turn, err := values.NewAssistantTurn(string(content))
	if err != nil {
		return values.AssistantTurn{}, false, fmt.Errorf("%s: %w", opReadTurn, &Error{Kind: ErrCorrupt, Detail: "a kept turn is not valid"})
	}
	return turn, true, nil
}

// PurgeTurns deletes every turn kept at or before cutoff and returns how many.
func (r *AgentTurns) PurgeTurns(ctx context.Context, cutoff time.Time) (int64, error) {
	if err := r.db.open(); err != nil {
		return 0, fmt.Errorf("%s: %w", opPurgeTurns, err)
	}
	ctx, cancel := context.WithTimeout(ctx, purgeTimeout)
	defer cancel()

	tag, err := r.db.pool.Exec(ctx, "DELETE FROM agent_turns WHERE created_at <= $1", cutoff)
	if err != nil {
		return 0, r.db.queryError(ctx, opPurgeTurns, err)
	}
	return tag.RowsAffected(), nil
}
