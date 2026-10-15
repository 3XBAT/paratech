package app

// Stand-ins for the ports that this build does not configure yet (Hub and SMTP settings, Credential Manager reads and
// the draft store come with TASK-057). Each one fails with a fixed text that wraps errNotConfigured and with the error
// class that keeps the jobs safe until the real adapter arrives.

import (
	"context"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// errNotConfigured marks a call into a part of the core that this build does not configure.
var errNotConfigured = errors.New("not configured in this build")

// hubNotConfigured is the LLM client without a Hub. The network-unavailable class parks the LLM jobs in
// WAITING_FOR_NETWORK without using attempts, so they run once a configured build starts (their keys stay usable).
type hubNotConfigured struct{}

func (hubNotConfigured) Complete(context.Context, values.LLMRequest) (values.LLMResult, error) {
	return values.LLMResult{}, fmt.Errorf("hub client: %w: %w", errNotConfigured, shared.ErrNetworkUnavailable)
}

// mailNotConfigured is the email adapter without an SMTP account. Send is a definite refusal (nothing was handed
// over, the delivery fails); an unclassified error would count as an unknown outcome.
type mailNotConfigured struct{}

func (mailNotConfigured) Send(context.Context, *delivery.Delivery, io.Reader) error {
	return fmt.Errorf("email: %w: %w", errNotConfigured, delivery.ErrRejected)
}

func (mailNotConfigured) Reconcile(context.Context, *delivery.Delivery) (bool, error) {
	return false, fmt.Errorf("email: %w: %w", errNotConfigured, delivery.ErrOutcomeUnknown)
}

// mailSecretsNotConfigured is the mail credential store before Credential Manager is wired.
type mailSecretsNotConfigured struct{}

func (mailSecretsNotConfigured) Set(context.Context, string, string) error { return errMailSecrets() }

func (mailSecretsNotConfigured) Exists(context.Context, string) (bool, error) {
	return false, errMailSecrets()
}

func (mailSecretsNotConfigured) Delete(context.Context, string) error { return errMailSecrets() }

func errMailSecrets() error { return fmt.Errorf("mail credentials: %w", errNotConfigured) }

// draftsNotConfigured is the draft store before its SQLite repository exists; a generation job fails at once.
type draftsNotConfigured struct{}

func (draftsNotConfigured) Get(context.Context, shared.AssignmentID) (*homeworkdraft.Draft, error) {
	return nil, errDrafts()
}

func (draftsNotConfigured) Save(context.Context, *homeworkdraft.Draft) error { return errDrafts() }

func errDrafts() error { return backoff.Permanent(fmt.Errorf("homework drafts: %w", errNotConfigured)) }

// sttNotConfigured is the speech recognition engine of a build whose whisper-cli cannot be used (missing next to the
// executable, or refused by the adapter's checks; the log says why). An STT job fails at once instead of burning CPU
// on retries.
type sttNotConfigured struct{}

func (sttNotConfigured) Transcribe(context.Context, shared.ArtifactRef, shared.ArtifactRef, shared.ArtifactRef, func(int)) error {
	return backoff.Permanent(fmt.Errorf("speech recognition: %w", errNotConfigured))
}
