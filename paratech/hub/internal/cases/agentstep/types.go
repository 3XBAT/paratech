package agentstep

import (
	"log/slog"
	"time"

	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Deps are the ports and domain services of the step; all are required.
type Deps struct {
	Accounts Accounts
	Provider Provider
	Limiter  RateLimiter
	Clock    Clock
	Random   Random
	Models   *modelallowlist.List
	Sealer   *seal.Sealer
	Log      *slog.Logger
}

// Config is the step configuration: the quota policy (the same values as the gateway, ADR-0008), the bound of one
// provider call and the server profiles.
type Config struct {
	Unit           string
	Limit          int64
	ReservationTTL time.Duration
	KeyRetention   time.Duration
	CallTimeout    time.Duration
	Profiles       []Profile
}

// Profile is what the Hub uses for one agent profile: the model alias from the server configuration (the client
// sends none) and the server prompt with the tool catalog of every version. MinOutput is the least output cap a
// request gets (0: DefaultMinOutput): a smaller one is raised to it, so a tiny cap cannot make a step that bills its
// whole input end at the cap with no usable call.
type Profile struct {
	Name      values.Profile
	Alias     string
	Versions  []Version
	MinOutput int64
}

// DefaultMinOutput is the output floor of a profile without MinOutput.
const DefaultMinOutput = 256

// Version is the server prompt and the tool catalog of one catalog version (hub/pkg/prompts agent/<profile>/v<N>).
type Version struct {
	System  string
	Catalog *toolcatalog.Catalog
}

// Command is one AgentStep call of the authenticated tester Owner, as received; the domain validates it.
type Command struct {
	Owner           shared.APIKeyID
	IdempotencyKey  string
	ThreadID        string
	Profile         string
	CatalogVersion  uint32
	Base            Base
	Scope           Scope
	Turns           []Turn
	MaxOutputTokens int64
}

// Base is the shared student context.
type Base struct {
	Subjects []values.Subject
	Today    string
	TimeZone string
}

// Scope is the context of the current thread.
type Scope struct {
	SubjectID    string
	Screen       values.Screen
	LessonID     string
	AssignmentID string
}

// Turn is one history turn: StudentText for values.TurnStudentText, Calls and Seal for values.TurnCalls, Results for
// values.TurnResults.
type Turn struct {
	Kind        values.TurnKind
	StudentText string
	Calls       []Call
	Seal        []byte
	Results     []ToolResult
}

// Call is one tool call. Arguments is a JSON object; UNTRUSTED when it comes from the model.
type Call struct {
	ID        string
	Tool      string
	Arguments string
}

// ToolResult is the desktop's result of one echoed call.
type ToolResult struct {
	CallID  string
	JSON    string
	IsError bool
}

// Snapshot is the quota of a tester at one moment.
type Snapshot struct {
	Unit      string
	Remaining int64
	Reserved  int64
	Limit     int64
}

// Result is the answer of Step: 1..4 calls proposed by the model (UNTRUSTED) with the Hub's seal over them.
type Result struct {
	Calls         []Call
	Seal          []byte
	InputTokens   int64
	OutputTokens  int64
	Quota         Snapshot
	ReservationID string
	// Replayed is true when the calls are the stored result of an earlier call with the same key.
	Replayed bool
}

func snapshotOf(s account.Snapshot) Snapshot {
	return Snapshot{Unit: string(s.Unit), Remaining: s.Remaining, Reserved: s.Reserved, Limit: s.Limit}
}
