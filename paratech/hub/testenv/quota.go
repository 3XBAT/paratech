package testenv

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// QuotaStore is an in-memory quota account repository with the semantics of the Postgres adapter: one lock
// serializes every command (the adapter locks per owner), the account is restored from stored records with the
// partial load of account.Scope, Changes are applied only when the command succeeds, and a reservation id is
// unique across all owners. Err, when set, fails every call.
type QuotaStore struct {
	mu       sync.Mutex
	accounts map[shared.APIKeyID]*quotaRows
	owners   map[shared.ReservationID]shared.APIKeyID
	Err      error
}

type quotaRows struct {
	used, held   int64
	reservations map[shared.ReservationID]account.ReservationRecord
	keys         map[account.IdempotencyKey]account.KeyRecord
	ledger       []account.Movement
}

// NewQuotaStore returns an empty store.
func NewQuotaStore() *QuotaStore {
	return &QuotaStore{accounts: map[shared.APIKeyID]*quotaRows{}, owners: map[shared.ReservationID]shared.APIKeyID{}}
}

// Modify runs fn on the account of scope.Owner and stores its changes if fn succeeds.
func (s *QuotaStore) Modify(_ context.Context, scope account.Scope, fn func(*account.Account) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	rows := s.accounts[scope.Owner]
	if rows == nil {
		rows = &quotaRows{reservations: map[shared.ReservationID]account.ReservationRecord{}, keys: map[account.IdempotencyKey]account.KeyRecord{}}
	}
	a, err := load(rows, scope, true)
	if err != nil {
		return err
	}
	if err := fn(a); err != nil {
		return err
	}

	ch := a.Changes()
	for _, r := range ch.Created {
		if _, dup := s.owners[r.ID]; dup {
			return fmt.Errorf("create reservation: %w", shared.ErrAlreadyExists)
		}
	}
	for _, r := range ch.Updated {
		if _, ok := rows.reservations[r.ID]; !ok {
			return fmt.Errorf("update reservation: %w", shared.ErrNotFound)
		}
	}
	for _, r := range append(ch.Created, ch.Updated...) {
		rows.reservations[r.ID] = r
		s.owners[r.ID] = scope.Owner
	}
	for _, k := range ch.Keys {
		rows.keys[k.Key] = k
	}
	rows.ledger = append(rows.ledger, ch.Movements...)
	rows.used, rows.held = ch.Used, ch.Held
	s.accounts[scope.Owner] = rows
	return nil
}

// Read loads the account of scope.Owner without changing anything.
func (s *QuotaStore) Read(_ context.Context, scope account.Scope) (*account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	rows := s.accounts[scope.Owner]
	if rows == nil {
		return account.New(scope.Owner, scope.Policy)
	}
	return load(rows, scope, false)
}

// PurgeResults clears the stored results of key records created at or before cutoff, like the Postgres adapter.
func (s *QuotaStore) PurgeResults(_ context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return 0, s.Err
	}
	var n int64
	for _, rows := range s.accounts {
		for key, k := range rows.keys {
			if k.Result.Stored && !k.CreatedAt.After(cutoff) {
				k.Result = account.Result{}
				rows.keys[key] = k
				n++
			}
		}
	}
	return n, nil
}

// Key returns the stored record of key of owner.
func (s *QuotaStore) Key(owner shared.APIKeyID, key account.IdempotencyKey) (account.KeyRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rows := s.accounts[owner]; rows != nil {
		k, ok := rows.keys[key]
		return k, ok
	}
	return account.KeyRecord{}, false
}

// Ledger returns the stored movements of owner, oldest first.
func (s *QuotaStore) Ledger(owner shared.APIKeyID) []account.Movement {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rows := s.accounts[owner]; rows != nil {
		return append([]account.Movement(nil), rows.ledger...)
	}
	return nil
}

// Totals returns the stored used and held totals of owner.
func (s *QuotaStore) Totals(owner shared.APIKeyID) (used, held int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rows := s.accounts[owner]; rows != nil {
		return rows.used, rows.held
	}
	return 0, 0
}

// Reservations returns the stored reservations of owner ordered by creation, then id.
func (s *QuotaStore) Reservations(owner shared.APIKeyID) []account.ReservationRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.accounts[owner]
	if rows == nil {
		return nil
	}
	out := make([]account.ReservationRecord, 0, len(rows.reservations))
	for _, r := range rows.reservations {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// load restores the account with the reservations scope asks for (see account.Scope).
func load(rows *quotaRows, scope account.Scope, withTargets bool) (*account.Account, error) {
	var overdue []account.ReservationRecord
	for _, r := range rows.reservations {
		if r.State == account.StateReserved && !scope.Now.Before(r.ExpiresAt) {
			overdue = append(overdue, r)
		}
	}
	sort.Slice(overdue, func(i, j int) bool {
		if !overdue[i].ExpiresAt.Equal(overdue[j].ExpiresAt) {
			return overdue[i].ExpiresAt.Before(overdue[j].ExpiresAt)
		}
		return overdue[i].ID < overdue[j].ID
	})
	if len(overdue) > account.MaxExpiredPerLoad {
		overdue = overdue[:account.MaxExpiredPerLoad]
	}

	seen := map[shared.ReservationID]bool{}
	var rs []account.ReservationRecord
	add := func(r account.ReservationRecord) {
		if !seen[r.ID] {
			seen[r.ID] = true
			rs = append(rs, r)
		}
	}
	for _, r := range overdue {
		add(r)
	}
	var keys []account.KeyRecord
	if withTargets && scope.Key != "" {
		if k, ok := rows.keys[scope.Key]; ok {
			keys = append(keys, k)
			add(rows.reservations[k.Latest])
		}
	}
	if withTargets && scope.Reservation != "" {
		if r, ok := rows.reservations[scope.Reservation]; ok {
			add(r)
		}
	}
	return account.Restore(scope.Owner, scope.Policy, rows.used, rows.held, rs, keys)
}
