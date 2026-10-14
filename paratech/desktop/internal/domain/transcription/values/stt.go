package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// STTJobType is the job type of transcribing a recording. The jobs context keeps job types an open set of names, and
// contexts do not import each other (ARCH-11), so the name is defined here and converted by the use cases.
const STTJobType = "stt"

// STTRequest is the payload of an STT job: ids only (no audio, no paths, no settings). The model is not part of
// it: the job pins it in its own column (AC-08).
type STTRequest struct {
	recording shared.RecordingID
}

// NewSTTRequest validates the recording id.
func NewSTTRequest(rec shared.RecordingID) (STTRequest, error) {
	if err := rec.Validate(); err != nil {
		return STTRequest{}, fmt.Errorf("%w: %w", ErrInvalidSTTRequest, err)
	}
	return STTRequest{recording: rec}, nil
}

// Recording returns the recording to transcribe.
func (r STTRequest) Recording() shared.RecordingID { return r.recording }

type wireSTTRequest struct {
	RecordingID *string `json:"recording_id"`
}

// Payload renders the job payload, for example {"recording_id":"r1"}.
func (r STTRequest) Payload() string {
	id := string(r.recording)
	b, _ := json.Marshal(wireSTTRequest{RecordingID: &id}) // a struct with one string field always encodes
	return string(b)
}

// ParseSTTPayload reads a job payload strictly: exactly one object with recording_id and nothing else.
func ParseSTTPayload(s string) (STTRequest, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()

	var w wireSTTRequest
	if err := dec.Decode(&w); err != nil {
		return STTRequest{}, fmt.Errorf("%w: %w", ErrInvalidSTTRequest, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return STTRequest{}, fmt.Errorf("%w: data after the payload", ErrInvalidSTTRequest)
	}
	if w.RecordingID == nil {
		return STTRequest{}, fmt.Errorf("%w: no recording_id", ErrInvalidSTTRequest)
	}
	return NewSTTRequest(shared.RecordingID(*w.RecordingID))
}
