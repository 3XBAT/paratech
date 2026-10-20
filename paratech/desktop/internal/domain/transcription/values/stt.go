package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// STTJobType is the job type of transcribing a recording. The jobs context keeps job types an open set of names, and
// contexts do not import each other (ARCH-11), so the name is defined here and converted by the use cases.
const STTJobType = "stt"

// NoSpeechMessage is the fixed last error of an STT job whose transcript holds no speech, only whisper's marks such
// as "[BLANK_AUDIO]" (TASK-110): the recording is treated like a silent one and nothing is post-processed. The job may
// prefix it ("permanent: ...").
const NoSpeechMessage = "stt job: no speech in the recording"

// IsNoSpeech reports the last error of an STT job that failed with NoSpeechMessage.
func IsNoSpeech(lastError string) bool { return strings.HasSuffix(lastError, NoSpeechMessage) }

// STTEngineShare is the part of an STT job's progress percent that the engine run fills (0..STTEngineShare); storing
// the document fills the rest. The transcription use case reports by it, the time left reads it back (TASK-084).
const STTEngineShare = 90

// STTEnginePercent converts the progress percent of an STT job back to the engine's own progress, 0..100.
func STTEnginePercent(jobPercent int) int {
	return min(max(jobPercent, 0)*100/STTEngineShare, 100)
}

// STTJobKeyPrefix is the start of the idempotency key of every STT job of rec, "stt:<recording id>:"; the pinned
// model id follows it. The enqueue builds keys with it and readers find the jobs of a recording by it.
func STTJobKeyPrefix(rec shared.RecordingID) string { return STTJobType + ":" + string(rec) + ":" }

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
