package agenteval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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
// come in between). Also are other classes the prompt allows as well. ListsAssignments marks a request whose reply
// should list the assignments found (catalog v3). Source is the argument triage_assignment should be given (one of
// TriageSources, catalog v4). Answer is the option the student picks on the first question of the first
// ask_questions card: the run answers it by its index and goes on instead of stopping (the subject question, catalog
// v8). AssignedSubject is the subject create_assignment should get: a subject ID, or NoSubject for none (catalog v8).
// Chats (catalog v9): Earlier are earlier requests of the chat sent as pairs before the text, Summary the summary turn
// before them, Mentions the «@» objects of the request (IDs only; Earlier marks a mention of an earlier request).
type Request struct {
	ID               string     `json:"id"`
	Text             string     `json:"text"`
	Subject          string     `json:"subject,omitempty"`
	Assignment       string     `json:"assignment,omitempty"`
	Uploads          []string   `json:"uploads,omitempty"`
	Class            string     `json:"class"`
	Also             []string   `json:"also,omitempty"`
	Tools            []string   `json:"tools"`
	ListsAssignments bool       `json:"lists_assignments,omitempty"`
	Source           string     `json:"source,omitempty"`
	Answer           string     `json:"answer,omitempty"`
	AssignedSubject  string     `json:"assigned_subject,omitempty"`
	Summary          string     `json:"summary,omitempty"`
	Earlier          []Exchange `json:"earlier,omitempty"`
	Mentions         []Mention  `json:"mentions,omitempty"`
}

// Exchange is an earlier request of the chat: the student's text and the reply line the history carries.
type Exchange struct {
	Text  string `json:"text"`
	Reply string `json:"reply"`
}

// Mention is an object mentioned with «@»: its kind (MentionKinds) and fake ID, of the current request or, with
// Earlier, of an earlier one.
type Mention struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Earlier bool   `json:"earlier,omitempty"`
}

// MentionKinds are the kinds a case may mention (values.MentionKinds).
func MentionKinds() []string { return []string{"subject", "lesson", "material", "assignment"} }

// Mention limits of one step (values.MaxMentions, values.MaxStepMentions).
const (
	maxCurrentMentions = 8
	maxStepMentions    = 16
)

// NoSubject is the AssignedSubject of an assignment added without a subject («Без предмета»).
const NoSubject = "none"

// Accepts reports whether a run of class class routed the request right.
func (r Request) Accepts(class string) bool {
	return class == r.Class || slices.Contains(r.Also, class)
}

// Expected is the expected class for a report, alternatives joined with «/».
func (r Request) Expected() string {
	return strings.Join(append([]string{r.Class}, r.Also...), "/")
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

// CatalogFile is the golden catalog the run is scored against, in shared/testdata: the version the desktop sends.
const CatalogFile = "tool_catalog_v9.json"

// checkChat checks the chat fields of a case: earlier requests with a text and a reply, known mention kinds with an
// ID, each object once, within the step limits, and an earlier mention only after an earlier request.
func checkChat(r Request) error {
	for _, x := range r.Earlier {
		if strings.TrimSpace(x.Text) == "" || strings.TrimSpace(x.Reply) == "" {
			return errors.New("an earlier request needs a text and a reply")
		}
	}
	if r.Summary != "" && strings.TrimSpace(r.Summary) == "" {
		return errors.New("a blank summary")
	}
	current, seen := 0, map[Mention]bool{}
	for _, m := range r.Mentions {
		key := Mention{Kind: m.Kind, ID: m.ID}
		if !slices.Contains(MentionKinds(), m.Kind) || m.ID == "" || seen[key] {
			return fmt.Errorf("mention %s:%s: a known kind, an ID, each object once", m.Kind, m.ID)
		}
		seen[key] = true
		if m.Earlier && len(r.Earlier) == 0 {
			return fmt.Errorf("mention %s of an earlier request without one", m.ID)
		}
		if !m.Earlier {
			current++
		}
	}
	if current > maxCurrentMentions || len(r.Mentions) > maxStepMentions {
		return fmt.Errorf("at most %d current and %d mentions", maxCurrentMentions, maxStepMentions)
	}
	return nil
}

// CatalogClasses reads the tool classes (tool -> R, U, C, UI) of a tool catalog file
// (shared/testdata/tool_catalog_v9.json).
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
		for i, c := range r.Also {
			if !slices.Contains(Classes(), c) || c == r.Class || c == ClassCannotDo || r.Class == ClassCannotDo || slices.Contains(r.Also[:i], c) {
				return bad("request %s: alternative class %q", r.ID, c)
			}
		}
		for _, tool := range r.Tools {
			if c, ok := toolClasses[tool]; !ok || c == "UI" {
				return bad("request %s: %q is not a non-card tool of the catalog", r.ID, tool)
			}
		}
		if r.ListsAssignments && (r.Class != ClassRead || !slices.Contains(r.Tools, "list_assignments")) {
			return bad("request %s: a reply lists assignments only after list_assignments in a read", r.ID)
		}
		if r.Source != "" && (!slices.Contains(TriageSources(), r.Source) || !slices.Contains(r.Tools, triageTool)) {
			return bad("request %s: source %q needs triage_assignment and one of %v", r.ID, r.Source, TriageSources())
		}
		if r.Answer != "" && r.Class != ClassQuestions {
			return bad("request %s: an answer needs the class questions", r.ID)
		}
		if s := r.AssignedSubject; s != "" && (!slices.Contains(r.Tools, assignTool) || s != NoSubject && !subjects[s]) {
			return bad("request %s: assigned subject %q needs create_assignment and a known subject or %s", r.ID, s, NoSubject)
		}
		if err := checkChat(r); err != nil {
			return bad("request %s: %v", r.ID, err)
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
