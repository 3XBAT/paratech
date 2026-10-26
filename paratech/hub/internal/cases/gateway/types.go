package gateway

import (
	"log/slog"
	"time"

	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Deps are the ports and domain services of the gateway; all are required, except Streamer, which is required only
// when Config.ProviderStream is on.
type Deps struct {
	Accounts Accounts
	Results  Results
	Provider Provider
	Streamer StreamingProvider
	Limiter  RateLimiter
	Clock    Clock
	Random   Random
	Models   *modelallowlist.List
	Log      *slog.Logger
}

// Config is the gateway configuration: the quota policy (the same values as the quota use case, ADR-0008), the
// bound of one provider call (HUB_LLM_TIMEOUT) and the server prompts of the operations that have one (ADR-0023).
type Config struct {
	Unit           string
	Limit          int64
	ReservationTTL time.Duration
	KeyRetention   time.Duration
	CallTimeout    time.Duration
	// ServerPrompts holds the prompt of each operation with a server prompt (values.Operation.ServerPrompt). Such an
	// operation without an entry is refused as unavailable.
	ServerPrompts map[values.Operation]ServerPrompt
	// ProviderStream makes CompleteStream call StreamingProvider.Stream (HUB_PROVIDER_STREAM, ADR-0026); off, it calls
	// Provider.Complete and sends the whole output as one chunk.
	ProviderStream bool
}

// ServerPrompt is the Hub's own prompt of an operation: the allowlist alias it runs on, the prompt version (in the
// fingerprint) and the text (hub/pkg/prompts ops/<op>/v<N>). Reask is the note of that version added to the text when
// the model is asked once more after a refusal it can fix (TRIAGE_ASSIGNMENT, ADR-0025); empty, it is never asked again.
type ServerPrompt struct {
	Alias   string
	Version string
	Text    string
	Reask   string
}

// Command is one Complete or CompleteStream call of the authenticated tester Owner. Operation is a values.Operation string; texts are
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
