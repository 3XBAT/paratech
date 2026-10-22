package values

import (
	"fmt"
	"strconv"
	"strings"
)

// versionPrefix starts the ID of a draft version ("v3" is version 3 of the assignment named next to it).
const versionPrefix = "v"

// VersionRef is the ID the planner sees for draft version n (n ≥ 1).
func VersionRef(n int) (Ref, error) {
	if n < 1 {
		return Ref{}, fmt.Errorf("%w: a draft version is at least 1", ErrInvalidRef)
	}
	return NewRef(KindVersion, versionPrefix+strconv.Itoa(n))
}

// VersionNumber is the draft version a version ref names.
func VersionNumber(r Ref) (int, error) {
	digits, ok := strings.CutPrefix(r.ID(), versionPrefix)
	if r.Kind() != KindVersion || !ok || digits == "" || digits[0] == '0' {
		return 0, fmt.Errorf("%w: not a draft version", ErrInvalidRef)
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: not a draft version", ErrInvalidRef)
	}
	return n, nil
}

// ProposalResult is the result of a proposal tool (class C): what was proposed waits for the student's «Да» on the
// confirm card; nothing ran. target is the proposal or the deletion preview the card is bound to.
func ProposalResult(callID string, target Ref) (ToolResult, []Ref, error) {
	body := map[string]any{"proposed": true, "waits_for": "student"}
	switch target.Kind() {
	case KindProposal:
		body["proposal"] = target.ID()
	case KindPreview:
		body["preview"] = target.ID()
	default:
		return ToolResult{}, nil, fmt.Errorf("%w: a proposal tool shows a proposal or a preview", ErrInvalidResult)
	}
	r := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := r.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return r, []Ref{target}, nil
}
