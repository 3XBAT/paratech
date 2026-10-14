package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// JobType is the type of the job that sends a delivery (PRD section 10 step 8). The jobs context keeps job types an
// open set of names and contexts do not import each other (ARCH-11), so the name is defined here.
const JobType = "send_email"

// JobKey is the idempotency key of the job of one round of a delivery. A round is the attempts made so far: a new
// job is created only after the delivery went back to PENDING outside a running job (send again), so a repeated
// request finds the job of the current round instead of adding one.
func (d *Delivery) JobKey() string { return fmt.Sprintf("%s:%s:%d", JobType, d.id, d.attempts) }

type wirePayload struct {
	DeliveryID *string `json:"delivery_id"`
}

// JobPayload renders the payload of the delivery job: the delivery id only, for example {"delivery_id":"d1"}.
func (d *Delivery) JobPayload() string {
	id := string(d.id)
	b, _ := json.Marshal(wirePayload{DeliveryID: &id}) // a struct with one string field always encodes
	return string(b)
}

// ParseJobPayload reads a job payload strictly: exactly one object with a valid delivery_id and nothing else.
func ParseJobPayload(s string) (shared.DeliveryID, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()

	var w wirePayload
	if err := dec.Decode(&w); err != nil {
		return "", fmt.Errorf("%w: not a payload object", ErrInvalidPayload)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%w: data after the payload", ErrInvalidPayload)
	}
	if w.DeliveryID == nil {
		return "", fmt.Errorf("%w: no delivery_id", ErrInvalidPayload)
	}
	id := shared.DeliveryID(*w.DeliveryID)
	if err := id.Validate(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	return id, nil
}
