package agenteval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Plan classes: what the planner decided for a request, from the tools it called until it stopped.
const (
	ClassRead      = "read"      // R tools, then a reply
	ClassWrite     = "write"     // a reversible action at once
	ClassConfirm   = "confirm"   // an irreversible action: a proposal card
	ClassPlan      = "plan"      // show_plan
	ClassQuestions = "questions" // ask_questions
	ClassCannotDo  = "cannot_do" // cannot_do
	ClassReply     = "reply"     // only a reply card
)

// Classes returns every class.
func Classes() []string {
	return []string{ClassRead, ClassWrite, ClassConfirm, ClassPlan, ClassQuestions, ClassCannotDo, ClassReply}
}

// Size bounds of the case set (TASK-135).
const (
	MinRequests  = 40
	MaxRequests  = 60
	NumQuestions = 20
	NumCannotDo  = 5
)

// Subject is a subject of the student's base (fake data).
type Subject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Request is a student request and the expected routing: the class and the non-card tools in order (others may
// come in between).
type Request struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Subject    string   `json:"subject,omitempty"`
	Assignment string   `json:"assignment,omitempty"`
	Uploads    []string `json:"uploads,omitempty"`
	Class      string   `json:"class"`
	Tools      []string `json:"tools"`
}

// Chunk is a fragment of a lecture or a material.
type Chunk struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Citation is a quote from a chunk.
type Citation struct {
	Chunk string `json:"chunk"`
	Quote string `json:"quote"`
}

// Question is a question on lectures with the fragments an answer may cite; Reference is one valid citation.
type Question struct {
	ID        string   `json:"id"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
	Chunks    []Chunk  `json:"chunks"`
	Reference Citation `json:"reference"`
}

// CaseSet is the live case file.
type CaseSet struct {
	Version   int        `json:"version"`
	Subjects  []Subject  `json:"subjects"`
	Requests  []Request  `json:"requests"`
	CannotDo  []Request  `json:"cannot_do"`
	Questions []Question `json:"questions"`
}

// CatalogClasses reads the tool classes (tool -> R, U, C, UI) of a tool catalog file
// (shared/testdata/tool_catalog_v2.json).
func CatalogClasses(raw []byte) (map[string]string, error) {
	var doc struct {
		Tools []struct {
			Name  string `json:"name"`
			Class string `json:"class"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: catalog: %w", ErrInvalidCaseSet, err)
	}
	out := make(map[string]string, len(doc.Tools))
	for _, t := range doc.Tools {
		out[t.Name] = t.Class
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: catalog has no tools", ErrInvalidCaseSet)
	}
	return out, nil
}

// ErrInvalidCaseSet means the case file breaks a rule.
var ErrInvalidCaseSet = errors.New("invalid agent case set")

// ParseCaseSet reads a case file strictly and checks it against the tool classes of the catalog (tool -> R, U, C,
// UI): the set sizes, unique IDs, known subjects, classes and tools, and a valid reference citation per question.
func ParseCaseSet(raw []byte, toolClasses map[string]string) (CaseSet, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cs CaseSet
	if err := dec.Decode(&cs); err != nil {
		return CaseSet{}, fmt.Errorf("%w: %w", ErrInvalidCaseSet, err)
	}
	bad := func(format string, a ...any) (CaseSet, error) {
		return CaseSet{}, fmt.Errorf("%w: %s", ErrInvalidCaseSet, fmt.Sprintf(format, a...))
	}
	if n := len(cs.Requests); n < MinRequests || n > MaxRequests {
		return bad("%d requests, want %d..%d", n, MinRequests, MaxRequests)
	}
	if len(cs.Questions) != NumQuestions || len(cs.CannotDo) != NumCannotDo {
		return bad("%d questions and %d cannot_do cases, want %d and %d", len(cs.Questions), len(cs.CannotDo), NumQuestions, NumCannotDo)
	}
	subjects := map[string]bool{}
	for _, s := range cs.Subjects {
		if s.ID == "" || s.Name == "" || subjects[s.ID] {
			return bad("subjects need unique ids and names")
		}
		subjects[s.ID] = true
	}
	ids := map[string]bool{}
	for _, r := range slices.Concat(cs.Requests, cs.CannotDo) {
		if r.ID == "" || ids[r.ID] || r.Text == "" {
			return bad("request %q: needs a unique id and a text", r.ID)
		}
		ids[r.ID] = true
		if r.Subject != "" && !subjects[r.Subject] {
			return bad("request %s: unknown subject %s", r.ID, r.Subject)
		}
		if !slices.Contains(Classes(), r.Class) {
			return bad("request %s: unknown class %q", r.ID, r.Class)
		}
		for _, tool := range r.Tools {
			if c, ok := toolClasses[tool]; !ok || c == "UI" {
				return bad("request %s: %q is not a non-card tool of the catalog", r.ID, tool)
			}
		}
	}
	for _, r := range cs.CannotDo {
		if r.Class != ClassCannotDo {
			return bad("cannot_do case %s has class %s", r.ID, r.Class)
		}
	}
	for _, q := range cs.Questions {
		if q.ID == "" || ids[q.ID] || q.Text == "" || !subjects[q.Subject] || len(q.Chunks) == 0 {
			return bad("question %q: needs a unique id, a text, a known subject and chunks", q.ID)
		}
		ids[q.ID] = true
		if !CitationValid(q.Reference, q.Chunks) {
			return bad("question %s: the reference citation is not valid", q.ID)
		}
	}
	return cs, nil
}
