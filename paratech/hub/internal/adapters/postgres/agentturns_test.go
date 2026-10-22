package postgres

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
)

var (
	callSetA = strings.Repeat("a1", 32)
	callSetB = strings.Repeat("b2", 32)
)

func assistantTurn(t *testing.T, raw string) values.AssistantTurn {
	t.Helper()
	turn, err := values.NewAssistantTurn(raw)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

// TestAgentTurnsRoundTrip: a kept turn reads back unchanged for its owner only, a second Keep under the key changes
// nothing, "no turn" is kept as such, and a turn kept at or before since is not returned.
func TestAgentTurnsRoundTrip(t *testing.T) {
	db := migratedDB(t)
	repo, alice, bob := db.AgentTurns(), storedOwner(t, db), storedOwner(t, db)
	raw := `[{"type":"thinking","thinking":"x <y> & ü","signature":"c2ln+/="},{"type":"tool_use","id":"toolu_01","name":"reply","input":{}}]`

	if err := repo.Keep(t.Context(), alice, callSetA, assistantTurn(t, raw), keyT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Keep(t.Context(), alice, callSetA, assistantTurn(t, `[]`), keyT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Keep(t.Context(), alice, callSetB, values.AssistantTurn{}, keyT0); err != nil {
		t.Fatal(err)
	}

	before := keyT0.Add(-time.Second)
	got, ok, err := repo.Kept(t.Context(), alice, callSetA, before)
	if err != nil || !ok || got.Raw() != raw {
		t.Fatalf("Kept = %q, %v, %v", got.Raw(), ok, err)
	}
	if got, ok, err := repo.Kept(t.Context(), alice, callSetB, before); err != nil || !ok || !got.IsZero() {
		t.Fatalf("no turn = %v, %v, %v", got, ok, err)
	}
	if _, ok, err := repo.Kept(t.Context(), bob, callSetA, before); err != nil || ok {
		t.Fatalf("bob reads alice's turn: %v, %v", ok, err)
	}
	if _, ok, err := repo.Kept(t.Context(), alice, callSetA, keyT0); err != nil || ok {
		t.Fatalf("a turn kept at since: %v, %v", ok, err)
	}
	if n := count(t, db, "agent_turns", alice); n != 2 {
		t.Fatalf("rows = %d", n)
	}
}

func TestAgentTurnsPurge(t *testing.T) {
	db := migratedDB(t)
	repo, owner := db.AgentTurns(), storedOwner(t, db)
	for i, at := range []time.Time{keyT0, keyT0.Add(time.Hour)} {
		if err := repo.Keep(t.Context(), owner, []string{callSetA, callSetB}[i], assistantTurn(t, `[]`), at); err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.PurgeTurns(t.Context(), keyT0)
	if err != nil || n < 1 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	if _, ok, _ := repo.Kept(t.Context(), owner, callSetA, keyT0.Add(-time.Hour)); ok {
		t.Fatal("a purged turn is still kept")
	}
	if _, ok, _ := repo.Kept(t.Context(), owner, callSetB, keyT0); !ok {
		t.Fatal("a newer turn was purged")
	}
}

// TestAgentTurnsConcurrentKeep: concurrent Keeps of one call set store exactly one row without errors.
func TestAgentTurnsConcurrentKeep(t *testing.T) {
	db := migratedDB(t)
	repo, owner := db.AgentTurns(), storedOwner(t, db)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- repo.Keep(t.Context(), owner, callSetA, assistantTurn(t, `[]`), keyT0) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, db, "agent_turns", owner); n != 1 {
		t.Fatalf("rows = %d", n)
	}
}

func TestAgentTurnsErrors(t *testing.T) {
	db := migratedDB(t)
	repo := db.AgentTurns()
	if err := repo.Keep(t.Context(), "0123456789abcdef", callSetA, values.AssistantTurn{}, keyT0); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown owner: %v", err)
	}
	owner := storedOwner(t, db)
	if err := repo.Keep(t.Context(), owner, "not-a-digest", values.AssistantTurn{}, keyT0); !errors.Is(err, ErrQuery) {
		t.Fatalf("bad call set: %v", err)
	}
	if _, err := db.pool.Exec(t.Context(), "INSERT INTO agent_turns (api_key_id, call_set, content, created_at) VALUES ($1, $2, $3, $4)",
		owner.String(), callSetB, []byte("[{"), keyT0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Kept(t.Context(), owner, callSetB, keyT0.Add(-time.Hour)); !errors.Is(err, ErrCorrupt) || strings.Contains(err.Error(), "[{") {
		t.Fatalf("corrupt row: %v", err)
	}

	db.Close()
	if err := repo.Keep(t.Context(), owner, callSetA, values.AssistantTurn{}, keyT0); !errors.Is(err, ErrClosed) {
		t.Errorf("Keep = %v", err)
	}
	if _, _, err := repo.Kept(t.Context(), owner, callSetA, keyT0); !errors.Is(err, ErrClosed) {
		t.Errorf("Kept = %v", err)
	}
	if _, err := repo.PurgeTurns(t.Context(), keyT0); !errors.Is(err, ErrClosed) {
		t.Errorf("PurgeTurns = %v", err)
	}
}
