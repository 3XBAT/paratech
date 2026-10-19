package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// Job types of the post-processing of a transcript (PRD sections 7.1 and 9). The jobs context keeps job types an open
// set of names and contexts do not import each other (ARCH-11), so the names are defined here.
const (
	SummarizeJobType       = "summarize"
	ExtractHomeworkJobType = "extract_homework"
	LectureTextJobType     = "lecture_text" // the full readable text of the lesson (TASK-090)
)

// LLMFailedMessage is the fixed last error of a post-processing job whose LLM call failed: the connection, the Hub
// key, the quota or the provider. The recordings screen offers the connections page for it and «Повторить» for the
// other failures (TASK-086); the job may prefix it ("attempts exhausted: ...").
const LLMFailedMessage = "post-processing: the LLM call failed"

// PostProcessRequest is the payload of a summarize, extract_homework or lecture_text job: the transcript id only. The lesson
// context and the transcript text are read when the job runs, never copied into the job.
type PostProcessRequest struct {
	transcript shared.TranscriptID
}

// NewPostProcessRequest validates the transcript id.
func NewPostProcessRequest(t shared.TranscriptID) (PostProcessRequest, error) {
	if err := t.Validate(); err != nil {
		return PostProcessRequest{}, fmt.Errorf("%w: %w", ErrInvalidPostProcessRequest, err)
	}
	return PostProcessRequest{transcript: t}, nil
}

// Transcript returns the transcript to process.
func (r PostProcessRequest) Transcript() shared.TranscriptID { return r.transcript }

type wirePostProcessRequest struct {
	TranscriptID *string `json:"transcript_id"`
}

// Payload renders the job payload, for example {"transcript_id":"t1"}.
func (r PostProcessRequest) Payload() string {
	id := string(r.transcript)
	b, _ := json.Marshal(wirePostProcessRequest{TranscriptID: &id}) // a struct with one string field always encodes
	return string(b)
}

// ParsePostProcessPayload reads a job payload strictly: exactly one object with transcript_id and nothing else.
func ParsePostProcessPayload(s string) (PostProcessRequest, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()

	var w wirePostProcessRequest
	if err := dec.Decode(&w); err != nil {
		return PostProcessRequest{}, fmt.Errorf("%w: not a payload object", ErrInvalidPostProcessRequest)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return PostProcessRequest{}, fmt.Errorf("%w: data after the payload", ErrInvalidPostProcessRequest)
	}
	if w.TranscriptID == nil {
		return PostProcessRequest{}, fmt.Errorf("%w: no transcript_id", ErrInvalidPostProcessRequest)
	}
	return NewPostProcessRequest(shared.TranscriptID(*w.TranscriptID))
}
