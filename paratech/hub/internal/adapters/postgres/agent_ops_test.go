package postgres

import (
	"testing"

	"paratech/hub/internal/domain/quota/account"
)

// The agent operations (migration 0006) are stored and read back: the same request replays under each key.
func TestQuotaAccountsAgentOperations(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 10_000)
	for i, op := range []string{"agent_step", "inspect_material", "answer", "triage_assignment"} {
		scope := scopeOf(owner, p, keyT0)
		scope.Key = account.IdempotencyKey("job-1:" + op)
		req, err := account.NewReserveRequest(string(scope.Key), op, "planner", "tokens", 100)
		if err != nil {
			t.Fatal(err)
		}
		for attempt, want := range []bool{false, true} {
			err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
				_, replayed, err := a.Reserve(req, resID(10*i+attempt+1), keyT0)
				if err == nil && replayed != want {
					t.Errorf("%s attempt %d: replayed = %v, want %v", op, attempt, replayed, want)
				}
				return err
			})
			if err != nil {
				t.Fatalf("%s attempt %d: %v", op, attempt, err)
			}
		}
	}
}
