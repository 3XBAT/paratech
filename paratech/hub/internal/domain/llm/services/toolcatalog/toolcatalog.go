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
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

// One-of groups (notes.one_of, for example "triage_assignment (assignment|upload|topic)"): optional arguments of which
// a call gives exactly one. The Hub enforces them from catalog oneOfFrom on; catalogs 1-3 were released leaving them
// to the desktop, whose refusal is an is_error result the planner can correct, not a failed step.
const (
	noteOneOf = "one_of"
	oneOfFrom = 4
)

var oneOfGroup = regexp.MustCompile(`([a-z][a-z0-9_]*) \(([a-z][a-z0-9_]*(?:\|[a-z][a-z0-9_]*)+)\)`)

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
	oneOf   map[string][]string // tool -> its one-of group, enforced only from catalog oneOfFrom
	specs   []values.ToolSpec
}

// Parse reads a catalog text and checks it: known fields only, a version above 0, unique snake_case tool and argument
// names, known classes, argument types allowed for the class (UI tools only card types, among them the IDs a card
// lists; other tools only IDs, enums, dates, labels and queries), enum values only on enums; from catalog oneOfFrom on,
// a one_of note whose groups name optional arguments of declared tools. The deeper policy checks of the golden file
// live in the contract test.
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
	if c.version >= oneOfFrom {
		groups, err := parseOneOf(doc.Notes[noteOneOf], c.tools)
		if err != nil {
			return nil, err
		}
		c.oneOf = groups
	}
	for _, name := range c.order {
		spec, err := values.NewToolSpec(name, c.description(c.tools[name]), c.schema(c.tools[name], doc.Types))
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

// parseOneOf reads the groups of the one_of note: at least one, each of a declared tool, at most one per tool, of 2+
// distinct optional arguments of that tool.
func parseOneOf(note string, tools map[string]tool) (map[string][]string, error) {
	matches := oneOfGroup.FindAllStringSubmatch(note, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: the one_of note names no groups", ErrInvalidCatalog)
	}
	groups := make(map[string][]string, len(matches))
	for _, m := range matches {
		name, args := m[1], strings.Split(m[2], "|")
		t, ok := tools[name]
		if _, dup := groups[name]; !ok || dup {
			return nil, fmt.Errorf("%w: one_of: %s is not a tool or has two groups", ErrInvalidCatalog, name)
		}
		for i, a := range args {
			k := slices.IndexFunc(t.Arguments, func(d argument) bool { return d.Name == a })
			if k < 0 || t.Arguments[k].Required || slices.Contains(args[:i], a) {
				return nil, fmt.Errorf("%w: one_of: %s.%s is not a distinct optional argument", ErrInvalidCatalog, name, a)
			}
		}
		groups[name] = args
	}
	return groups, nil
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
// each of its type (see schema), exactly one of its one-of group. UI card texts are at most MaxTextRunes characters.
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
	if group := c.oneOf[t.Name]; len(group) > 0 {
		given := 0
		for _, name := range group {
			if _, present := args[name]; present {
				given++
			}
		}
		if given != 1 {
			return fmt.Errorf("%w: %s: exactly one of %s is needed", ErrInvalidCall, t.Name, strings.Join(group, ", "))
		}
	}
	return nil
}

// The card a plain text answer of the planner becomes, and its text argument (TASK-200).
const (
	toolReply    = "reply"
	argReplyText = "text"
)

// TextReply turns the plain text of a planner answer without tool calls into the call reply{text} with callID, checked
// as a returned reply (CheckReturned: the text type, at most MaxTextRunes characters) and as the desktop checks card
// text (displayText). It is a display-only card and nothing else: the text never becomes another tool, an argument or
// an action. A catalog without a UI reply tool, or a text either check refuses, is ErrInvalidCall; its texts never
// quote the text.
func (c *Catalog) TextReply(callID, text string) (values.ToolCall, error) {
	if t, ok := c.tools[toolReply]; !ok || t.Class != classUI {
		return values.ToolCall{}, fmt.Errorf("%w: the catalog has no reply card", ErrInvalidCall)
	}
	if !utf8.ValidString(text) {
		return values.ToolCall{}, fmt.Errorf("%w: %s: the text is not UTF-8", ErrInvalidCall, toolReply)
	}
	if !displayText(text) {
		return values.ToolCall{}, fmt.Errorf("%w: %s: the text has a character the card does not show", ErrInvalidCall, toolReply)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // the canonical form of returned arguments (the anthropic adapter)
	if err := enc.Encode(map[string]string{argReplyText: text}); err != nil {
		return values.ToolCall{}, fmt.Errorf("%w: %s: the text cannot be encoded", ErrInvalidCall, toolReply)
	}
	call, err := values.NewToolCall(callID, toolReply, strings.TrimSuffix(b.String(), "\n"))
	if err != nil {
		return values.ToolCall{}, fmt.Errorf("%w: %s: %w", ErrInvalidCall, toolReply, err)
	}
	if err := c.CheckReturned([]values.ToolCall{call}); err != nil {
		return values.ToolCall{}, err
	}
	return call, nil
}

// displayText is the stricter check of the desktop's card text (desktop values.Text): printable characters and line
// breaks only, no tab, no U+FFFD, no angle brackets. Only TextReply applies it: a text the desktop refuses would leave
// the turn with a Hub-made call the desktop cannot answer, so such a text stays the old failure instead.
func displayText(s string) bool {
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if !unicode.IsPrint(r) || r == utf8.RuneError || r == '<' || r == '>' {
			return false
		}
	}
	return true
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

func (c *Catalog) description(t tool) string {
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
	// JSON Schema at the top of a tool input cannot say it for the provider, so the description does.
	if group := c.oneOf[t.Name]; len(group) > 0 {
		d += " Give exactly one of: " + strings.Join(group, ", ") + "."
	}
	return d
}
