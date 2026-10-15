package quota

import (
	"errors"

	"paratech/hub/internal/domain/quota/account"
)

// ErrInvalidConfig means New got a nil port or an invalid configuration.
var ErrInvalidConfig = errors.New("quota: invalid configuration")

// ExceededError is the account.ErrQuotaExceeded of Reserve with the quota at that moment, for the error detail the
// client shows (errors.proto, ErrorDetail.quota). errors.Is(err, account.ErrQuotaExceeded) matches it.
type ExceededError struct {
	Quota Snapshot
}

// Error implements error.
func (e *ExceededError) Error() string { return account.ErrQuotaExceeded.Error() }

// Unwrap lets errors.Is see account.ErrQuotaExceeded.
func (e *ExceededError) Unwrap() error { return account.ErrQuotaExceeded }
