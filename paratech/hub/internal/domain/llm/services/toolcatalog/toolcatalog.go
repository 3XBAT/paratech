// Package toolcatalog is the agent tool catalog of one version (ADR-0021..0023, golden files
// shared/testdata/tool_catalog_v<N>.json): which tools the planner may call, the JSON Schema the provider gets for each,
// and the check of the calls the provider returns. The catalog text comes from the server (hub/pkg/prompts) and is
// passed in as bytes; the package has no I/O.
package toolcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"paratech/hub/internal/domain/llm/values"
)

// Errors of the service (ARCH-18).
var (
	// ErrInvalidCatalog means the catalog text breaks a rule; the Hub refuses to start with it.
	ErrInvalidCatalog = errors.New("invalid tool catalog")

	// ErrInvalidCall means a returned tool call does not match the catalog schema. Its texts never quote the call.
	ErrInvalidCall = errors.New("tool call does not match the catalog")
)

// Bounds of the argument types beyond the catalog text. The catalog fixes label (80), query (500, catalog v2), text
// (300) and card_ids (10, catalog v3); the other list sizes are the Hub's.
const (
	MaxLabelRunes    = values.MaxLabelRunes
	MaxQueryRunes    = 500
	MaxTextRunes     = 300
	MaxCardIDs       = 10
	maxIDsItems      = 20
	maxTextListItems = 10
	maxPlanSteps     = 8
	maxStepArguments = 8
	maxQuestions     = 5
	maxOptions       = 6
	maxIDBytes       = 128
)

// Argument types of the catalog.
const (
	typeID        = "id"
	typeIDs       = "ids"
	typeDate      = "date"
	typeEnum      = "enum"
	typeLabel     = "label"
	typeQuery     = "query"
	typeText      = "text"
	typeTextList  = "text_list"
	typeCardIDs   = "card_ids"
	typePlanSteps = "plan_steps"
	typeQuestions = "questions"
)

// Tool classes.
const (
	classRead    = "R"
	classUndo    = "U"
	classConfirm = "C"
	classUI      = "UI"
)

var (
	actionTypes = []string{typeID, typeIDs, typeDate, typeEnum, typeLabel, typeQuery}
	uiTypes     = []string{typeText, typeTextList, typeCardIDs, typePlanSteps, typeQuestions}
)

// Question kinds of ask_questions.
var questionKinds = []string{"choice", "text"}

type argument struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Values   []string `json:"values"`
}

type tool struct {
	Name            string     `json:"name"`
	Class           string     `json:"class"`
	Paid            bool       `json:"paid"`
	AgentCanExecute bool       `json:"agent_can_execute"`
	Arguments       []argument `json:"arguments"`
}

type document struct {
	CatalogVersion uint32            `json:"catalog_version"`
	Types          map[string]string `json:"types"`
	Notes          map[string]string `json:"notes"`
	Tools          []tool            `json:"tools"`
}

// Catalog is a parsed and checked catalog. It is immutable and safe for concurrent use.
type Catalog struct {
	version uint32
	tools   map[string]tool
	order   []string
	specs   []values.ToolSpec
}

// Parse reads a catalog text and checks it: known fields only, a version above 0, unique snake_case tool and argument
// names, known classes, argument types allowed for the class (UI tools only card types, among them the IDs a card
// lists; other tools only IDs, enums, dates, labels and queries), enum values only on enums. The deeper policy checks of the golden file live in the contract
// test.
func Parse(raw []byte) (*Catalog, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: not a catalog document", ErrInvalidCatalog)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: trailing data", ErrInvalidCatalog)
	}
	if doc.CatalogVersion == 0 {
		return nil, fmt.Errorf("%w: catalog_version is missing", ErrInvalidCatalog)
	}
	if len(doc.Tools) == 0 {
		return nil, fmt.Errorf("%w: no tools", ErrInvalidCatalog)
	}
	c := &Catalog{version: doc.CatalogVersion, tools: make(map[string]tool, len(doc.Tools))}
	for _, t := range doc.Tools {
		if err := checkTool(t); err != nil {
			return nil, err
		}
		if _, dup := c.tools[t.Name]; dup {
			return nil, fmt.Errorf("%w: tool %s is declared twice", ErrInvalidCatalog, t.Name)
		}
		c.tools[t.Name] = t
		c.order = append(c.order, t.Name)
	}
	for _, name := range c.order {
		spec, err := values.NewToolSpec(name, description(c.tools[name]), c.schema(c.tools[name], doc.Types))
		if err != nil {
			return nil, fmt.Errorf("%w: tool %s: %w", ErrInvalidCatalog, name, err)
		}
		c.specs = append(c.specs, spec)
	}
	return c, nil
}

func checkTool(t tool) error {
	if !values.ValidToolName(t.Name) {
		return fmt.Errorf("%w: a tool name is not snake_case", ErrInvalidCatalog)
	}
	allowed := actionTypes
	switch t.Class {
	case classRead, classUndo, classConfirm:
	case classUI:
		allowed = uiTypes
	default:
		return fmt.Errorf("%w: tool %s has an unknown class", ErrInvalidCatalog, t.Name)
	}
	seen := map[string]bool{}
	for _, a := range t.Arguments {
		if !values.ValidToolName(a.Name) || seen[a.Name] {
			return fmt.Errorf("%w: tool %s: argument names must be snake_case and unique", ErrInvalidCatalog, t.Name)
		}
		seen[a.Name] = true
		if !slices.Contains(allowed, a.Type) {
			return fmt.Errorf("%w: tool %s: argument %s has a type its class does not allow", ErrInvalidCatalog, t.Name, a.Name)
		}
		if (a.Type == typeEnum) != (len(a.Values) > 0) {
			return fmt.Errorf("%w: tool %s: argument %s: an enum needs values and only an enum has them", ErrInvalidCatalog, t.Name, a.Name)
		}
		for i, v := range a.Values {
			if !values.ValidToolName(v) || slices.Contains(a.Values[:i], v) {
				return fmt.Errorf("%w: tool %s: argument %s: enum values must be snake_case and unique", ErrInvalidCatalog, t.Name, a.Name)
			}
		}
	}
	return nil
}

// Version is the catalog version.
func (c *Catalog) Version() uint32 { return c.version }

// Has reports whether tool is in the catalog.
func (c *Catalog) Has(tool string) bool {
	_, ok := c.tools[tool]
	return ok
}

// Specs are the tools as the provider gets them, in catalog order (a copy).
func (c *Catalog) Specs() []values.ToolSpec { return append([]values.ToolSpec(nil), c.specs...) }

// CheckEcho checks calls a client echoed: every tool is in the catalog. Their arguments were checked when the Hub
// returned them, and the seal proves they are unchanged.
func (c *Catalog) CheckEcho(calls []values.ToolCall) error {
	for _, call := range calls {
		if !c.Has(call.Tool()) {
			return fmt.Errorf("%w: unknown tool", ErrInvalidCall)
		}
	}
	return nil
}

// CheckReturned checks the calls the provider returned: 1..MaxAgentCalls calls with distinct ids, each matching the
// schema of its tool (Check).
func (c *Catalog) CheckReturned(calls []values.ToolCall) error {
	if len(calls) == 0 || len(calls) > values.MaxAgentCalls {
		return fmt.Errorf("%w: a step returns 1-%d calls", ErrInvalidCall, values.MaxAgentCalls)
	}
	ids := map[string]bool{}
	for _, call := range calls {
		if ids[call.CallID()] {
			return fmt.Errorf("%w: call ids repeat", ErrInvalidCall)
		}
		ids[call.CallID()] = true
		if err := c.Check(call); err != nil {
			return err
		}
	}
	return nil
}

// Check checks one call against the schema of its tool: a known tool, only declared arguments, every required one,
// each of its type (see schema). UI card texts are at most MaxTextRunes characters.
func (c *Catalog) Check(call values.ToolCall) error {
	t, ok := c.tools[call.Tool()]
	if !ok {
		return fmt.Errorf("%w: unknown tool", ErrInvalidCall)
	}
	args, err := decodeObject(call.Arguments())
	if err != nil {
		return err
	}
	declared := make(map[string]argument, len(t.Arguments))
	for _, a := range t.Arguments {
		declared[a.Name] = a
		if _, present := args[a.Name]; a.Required && !present {
			return fmt.Errorf("%w: %s: a required argument is missing", ErrInvalidCall, t.Name)
		}
	}
	for name, v := range args {
		a, ok := declared[name]
		if !ok {
			return fmt.Errorf("%w: %s: an undeclared argument", ErrInvalidCall, t.Name)
		}
		if !c.valid(a, v) {
			return fmt.Errorf("%w: %s: argument %s does not match its type", ErrInvalidCall, t.Name, a.Name)
		}
	}
	return nil
}

// decodeObject reads a JSON object keeping numbers exact.
func decodeObject(s string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil || v == nil || dec.More() {
		return nil, fmt.Errorf("%w: arguments are not a JSON object", ErrInvalidCall)
	}
	return v, nil
}

func description(t tool) string {
	var d string
	switch t.Class {
	case classRead:
		d = "Read-only lookup; runs at once and its result comes back as a tool result."
	case classUndo:
		d = "Reversible local action; runs at once and the student can undo it."
	case classConfirm:
		d = "Irreversible action: the call only creates a confirmation card; nothing happens until the student clicks it."
	default:
		d = "Card shown to the student; it has no other effect."
	}
	if t.Paid {
		d += " Paid: it uses the student's quota."
	}
	return d
}
