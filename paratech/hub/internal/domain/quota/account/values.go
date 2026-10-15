package account

import "fmt"

// Unit is the quota unit (PRD section 19, decision 4; ADR-0008). Only tokens exist for now.
type Unit string

// UnitTokens counts provider tokens, input and output together.
const UnitTokens Unit = "tokens"

// ParseUnit accepts a known unit.
func ParseUnit(s string) (Unit, error) {
	if Unit(s) != UnitTokens {
		return "", fmt.Errorf("%w: unknown quota unit (want %s)", ErrInvalidArgument, UnitTokens)
	}
	return UnitTokens, nil
}

// Operation is the LLM operation a reservation pays for; the set mirrors the contract's OperationType.
type Operation string

// Operations.
const (
	OperationSummary         Operation = "summary"
	OperationExtractHomework Operation = "extract_homework_deadlines"
	OperationHomeworkDraft   Operation = "homework_draft"
)

// Format limits of ADR-0005(b).
const (
	maxIdempotencyKeyLen          = 128
	maxModelAliasLen              = 64
	firstPrintable, lastPrintable = 0x21, 0x7e // printable ASCII without the space
)

// ParseOperation accepts a known operation.
func ParseOperation(s string) (Operation, error) {
	switch op := Operation(s); op {
	case OperationSummary, OperationExtractHomework, OperationHomeworkDraft:
		return op, nil
	default:
		return "", fmt.Errorf("%w: unknown operation type", ErrInvalidArgument)
	}
}

// ModelAlias is a Hub-side model name: 1-64 characters of [a-z0-9_-], starting with a letter or digit (ADR-0005(b)).
type ModelAlias string

// ParseModelAlias checks the alias format; whether the alias is allowed is the gateway's allowlist, not this check.
func ParseModelAlias(s string) (ModelAlias, error) {
	if len(s) == 0 || len(s) > maxModelAliasLen || !isAlnum(s[0]) {
		return "", fmt.Errorf("%w: model alias must be 1-%d characters of a-z, 0-9, '_' and '-' starting with a letter or digit",
			ErrInvalidArgument, maxModelAliasLen)
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isAlnum(c) && c != '_' && c != '-' {
			return "", fmt.Errorf("%w: model alias has a character outside a-z, 0-9, '_' and '-'", ErrInvalidArgument)
		}
	}
	return ModelAlias(s), nil
}

func isAlnum(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') }

// IdempotencyKey is a client key: 1-128 printable ASCII characters without spaces (ADR-0005(b)). It is namespaced per
// API key: the same text of two testers names two unrelated keys.
type IdempotencyKey string

// ParseIdempotencyKey checks the key format. The error never quotes the key.
func ParseIdempotencyKey(s string) (IdempotencyKey, error) {
	if len(s) == 0 || len(s) > maxIdempotencyKeyLen {
		return "", fmt.Errorf("%w: idempotency key must be 1-%d characters", ErrInvalidArgument, maxIdempotencyKeyLen)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < firstPrintable || s[i] > lastPrintable {
			return "", fmt.Errorf("%w: idempotency key must be printable ASCII without spaces", ErrInvalidArgument)
		}
	}
	return IdempotencyKey(s), nil
}

// Fingerprint is what an idempotency key was first used with. A Reserve with the same key and another fingerprint
// is a conflict. It holds no request text: RequestHash is the gateway's hash of the whole request (empty for a plain
// Reserve), opaque to this context.
type Fingerprint struct {
	Operation   Operation
	Alias       ModelAlias
	Amount      int64
	RequestHash RequestHash
}

// RequestHash is a lower-case hex SHA-256 of a gateway request (64 characters), or empty when there is none.
type RequestHash string

const requestHashLen = 64

// ParseRequestHash accepts 64 lower-case hex characters. The error never quotes the value.
func ParseRequestHash(s string) (RequestHash, error) {
	if len(s) != requestHashLen {
		return "", fmt.Errorf("%w: request hash must be %d lower-case hex characters", ErrInvalidArgument, requestHashLen)
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("%w: request hash must be %d lower-case hex characters", ErrInvalidArgument, requestHashLen)
		}
	}
	return RequestHash(s), nil
}

// ReserveRequest is a validated Reserve: the key, its fingerprint and the unit of the amount.
type ReserveRequest struct {
	key         IdempotencyKey
	fingerprint Fingerprint
	unit        Unit
}

// NewReserveRequest validates every argument of a Reserve. The amount must be positive; whether it fits the
// account is decided by Account.Reserve.
func NewReserveRequest(key, operation, alias, unit string, amount int64) (ReserveRequest, error) {
	k, err := ParseIdempotencyKey(key)
	if err != nil {
		return ReserveRequest{}, err
	}
	op, err := ParseOperation(operation)
	if err != nil {
		return ReserveRequest{}, err
	}
	a, err := ParseModelAlias(alias)
	if err != nil {
		return ReserveRequest{}, err
	}
	u, err := ParseUnit(unit)
	if err != nil {
		return ReserveRequest{}, err
	}
	if amount <= 0 {
		return ReserveRequest{}, fmt.Errorf("%w: amount must be positive", ErrInvalidArgument)
	}
	return ReserveRequest{key: k, fingerprint: Fingerprint{Operation: op, Alias: a, Amount: amount}, unit: u}, nil
}

// WithRequestHash returns the request with the gateway's request hash in its fingerprint (see Account.Start).
func (r ReserveRequest) WithRequestHash(hash string) (ReserveRequest, error) {
	h, err := ParseRequestHash(hash)
	if err != nil {
		return ReserveRequest{}, err
	}
	r.fingerprint.RequestHash = h
	return r, nil
}

// Key returns the idempotency key of the request.
func (r ReserveRequest) Key() IdempotencyKey { return r.key }

// Fingerprint returns the fingerprint of the request.
func (r ReserveRequest) Fingerprint() Fingerprint { return r.fingerprint }

// Usage is a validated Settle: the actual usage reported for a call and whether the call was cut short.
type Usage struct {
	actual  int64
	partial bool
	unit    Unit
}

// NewUsage validates the actual usage of a Settle: a known unit and a non-negative amount.
func NewUsage(unit string, actual int64, partial bool) (Usage, error) {
	u, err := ParseUnit(unit)
	if err != nil {
		return Usage{}, err
	}
	if actual < 0 {
		return Usage{}, fmt.Errorf("%w: actual usage must not be negative", ErrInvalidArgument)
	}
	return Usage{actual: actual, partial: partial, unit: u}, nil
}

// Actual returns the reported usage.
func (u Usage) Actual() int64 { return u.actual }

// Partial reports whether the call did not complete.
func (u Usage) Partial() bool { return u.partial }
