package gateway

import (
	"log/slog"
	"time"

	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Deps are the ports and domain services of the gateway; all are required.
type Deps struct {
	Accounts Accounts
	Results  Results
	Provider Provider
	Limiter  RateLimiter
	Clock    Clock
	Random   Random
	Models   *modelallowlist.List
	Log      *slog.Logger
}

// Config is the gateway configuration: the quota policy (the same values as the quota use case, ADR-0008) and the
// bound of one provider call (HUB_LLM_TIMEOUT).
type Config struct {
	Unit           string
	Limit          int64
	ReservationTTL time.Duration
	KeyRetention   time.Duration
	CallTimeout    time.Duration
}

// Command is one Complete call of the authenticated tester Owner. Operation is a values.Operation string; texts are
// passed as received and validated by the domain.
type Command struct {
	Owner           shared.APIKeyID
	IdempotencyKey  string
	Operation       string
	ModelAlias      string
	Instructions    string
	UntrustedData   string
	MaxOutputTokens int64
}

// Snapshot is the quota of a tester at one moment; it does not renew in MVP (no period end).
type Snapshot struct {
	Unit      string
	Remaining int64
	Reserved  int64
	Limit     int64
}

// Result is the answer of Complete. Output is model text: UNTRUSTED.
type Result struct {
	Output        string
	InputTokens   int64
	OutputTokens  int64
	Quota         Snapshot
	ReservationID string
	// Replayed is true when the answer is the stored result of an earlier call with the same key.
	Replayed bool
}

func snapshotOf(s account.Snapshot) Snapshot {
	return Snapshot{Unit: string(s.Unit), Remaining: s.Remaining, Reserved: s.Reserved, Limit: s.Limit}
}
