package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// Job types of the work on a material of the library (TASK-122, ADR-0024): its summary and the homework search in
// it. They run the summary and extract_homework_deadlines operations on the material text instead of a transcript.
const (
	MaterialSummaryJobType  = "material_summary"
	MaterialHomeworkJobType = "material_homework"
)

// MaterialJobKeyPrefix is the start of the idempotency key of every job of type typ for material m,
// "<type>:<material id>:"; the prompts version follows it. Readers find the jobs of a material by it.
func MaterialJobKeyPrefix(typ string, m shared.MaterialID) string {
	return typ + ":" + string(m) + ":"
}

// MaterialRequest is the payload of a material job: the material id only. The text is read when the job runs,
// never copied into the job.
type MaterialRequest struct {
	material shared.MaterialID
}

// NewMaterialRequest validates the material id.
func NewMaterialRequest(m shared.MaterialID) (MaterialRequest, error) {
	if err := m.Validate(); err != nil {
		return MaterialRequest{}, fmt.Errorf("%w: %w", ErrInvalidPostProcessRequest, err)
	}
	return MaterialRequest{material: m}, nil
}

// Material returns the material to process.
func (r MaterialRequest) Material() shared.MaterialID { return r.material }

type wireMaterialRequest struct {
	MaterialID *string `json:"material_id"`
}

// Payload renders the job payload, for example {"material_id":"m1"}.
func (r MaterialRequest) Payload() string {
	id := string(r.material)
	b, _ := json.Marshal(wireMaterialRequest{MaterialID: &id}) // a struct with one string field always encodes
	return string(b)
}

// ParseMaterialPayload reads a job payload strictly: exactly one object with material_id and nothing else.
func ParseMaterialPayload(s string) (MaterialRequest, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()

	var w wireMaterialRequest
	if err := dec.Decode(&w); err != nil {
		return MaterialRequest{}, fmt.Errorf("%w: not a payload object", ErrInvalidPostProcessRequest)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return MaterialRequest{}, fmt.Errorf("%w: data after the payload", ErrInvalidPostProcessRequest)
	}
	if w.MaterialID == nil {
		return MaterialRequest{}, fmt.Errorf("%w: no material_id", ErrInvalidPostProcessRequest)
	}
	return NewMaterialRequest(shared.MaterialID(*w.MaterialID))
}
