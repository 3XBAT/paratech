package senddelivery

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// JobType is the type of the delivery job. The app registers Service with the job runner for it as the handler and
// the reconciler, with RecoveryReconcile (an interrupted send is never rerun blindly), Uncancellable (a user
// cancellation in the middle of a send would only make the outcome unknown) and a Timeout of at least twice the SMTP
// timeout (the smtp adapter's deadline covers the session setup and then the transaction separately).
const JobType = job.Type(delivery.JobType)

// Defaults of Config.
const (
	DefaultSubject        = "Homework"
	DefaultCredentialName = "smtp-password" // the name the smtp adapter reads the password from (its default too)
	DefaultMaxAttempts    = 5
)

// MaxListedDeliveries bounds the deliveries of one assignment the screen shows (ListForAssignment).
const MaxListedDeliveries = 50

// resultSaveTimeout bounds saving the result of an attempt with a context that is no longer cancelled: the result
// must be stored even when the job's context ended during the send (timeout, shutdown).
const resultSaveTimeout = 10 * time.Second

// Deps are the ports the Service needs. Log is optional.
type Deps struct {
	Deliveries  DeliveryRepository
	History     DeliveryHistory
	Artifacts   ArtifactStore
	Email       EmailAdapter
	Jobs        JobEnqueuer
	Credentials CredentialStore
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
	Log         *slog.Logger
}

// Config holds the settings of the use case; zero fields take the defaults.
type Config struct {
	Subject        string // subject of a homework email; set by the app, never by the model
	CredentialName string // credential entry of the SMTP password
	MaxAttempts    int    // attempts of one delivery job; at most the job runner's backoff MaxAttempts, so the last one is known
}

// View is a delivery as the UI shows it (PRD section 10: the stored delivery result, AC-15).
type View struct {
	ID           shared.DeliveryID
	AssignmentID shared.AssignmentID
	Recipient    string
	Subject      string
	Status       delivery.Status
	Attempts     int
	LastError    string
	ConfirmedAt  time.Time
	SentAt       time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time

	// OutcomeUnknown: the message may or may not have been accepted; the user resolves it with MarkSent or SendAgain,
	// which the two flags offer (the aggregate decides).
	OutcomeUnknown bool
	CanMarkSent    bool
	CanSendAgain   bool
}

func viewOf(d *delivery.Delivery) View {
	return View{
		ID: d.ID(), AssignmentID: d.AssignmentID(), Recipient: d.Recipient(), Subject: d.Subject(), Status: d.Status(),
		Attempts: d.Attempts(), LastError: d.LastError(), ConfirmedAt: d.ConfirmedAt(), SentAt: d.SentAt(),
		CreatedAt: d.CreatedAt(), UpdatedAt: d.UpdatedAt(),
		// MarkSent and SendAgain both resolve an unknown outcome and nothing else.
		OutcomeUnknown: d.OutcomeUnknown(), CanMarkSent: d.OutcomeUnknown(), CanSendAgain: d.OutcomeUnknown(),
	}
}
