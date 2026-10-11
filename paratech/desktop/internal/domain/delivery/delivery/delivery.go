package delivery

import "paratech/desktop/internal/domain/shared"

// Status is the state of a delivery. The set of values is mirrored by the CHECK constraint of
// deliveries.status in docs/contracts/schema.sql; a test keeps both in sync.
//
// PRD section 10 and 14 (case 7) name only UNKNOWN. The rest is derived from the section 10 flow and from
// hard rule 7, and is a proposal to confirm: PENDING (delivery job created after the user's confirmation,
// nothing attempted), SENDING (an attempt is in flight; a crash in this state means the outcome is
// unknown), SENT (provider accepted), FAILED (permanent rejection, nothing was sent), UNKNOWN (ambiguous
// outcome, to be reconciled before any retry). SENDING is persisted before the message is handed to the
// provider, so a crash during an attempt leaves SENDING, which startup recovery turns into UNKNOWN. A transient
// failure (definitely not accepted) returns the delivery SENDING -> PENDING for a later attempt. The full
// transition table is TASK-044; the decision is recorded in ADR-0004.
//
// Values only: transition rules are added by TASK-044, not here.
type Status string

// Delivery statuses.
const (
	StatusPending Status = "PENDING"
	StatusSending Status = "SENDING"
	StatusSent    Status = "SENT"
	StatusFailed  Status = "FAILED"
	StatusUnknown Status = "UNKNOWN"
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{StatusPending, StatusSending, StatusSent, StatusFailed, StatusUnknown}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// Delivery is an attempt to send an artifact to a recipient (PRD section 10). Skeleton: see the package comment.
type Delivery struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.DeliveryID
	status  Status
}

// ID returns the delivery identifier.
func (d *Delivery) ID() shared.DeliveryID { return d.id }

// Status returns the current status.
func (d *Delivery) Status() Status { return d.status }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (d *Delivery) Version() int64 { return d.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (d *Delivery) Persisted(version int64) { d.version = version }
