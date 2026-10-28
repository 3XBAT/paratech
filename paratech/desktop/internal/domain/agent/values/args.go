package values

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Limits of a tool call (gateway.proto ToolCall and the catalog types).
const (
	MaxArgumentsBytes = 8 << 10 // arguments_json of one call
	// MaxPlanArgumentsBytes is arguments_json of show_plan, which carries up to MaxPlanSteps steps.
	MaxPlanArgumentsBytes = 32 << 10
	MaxCallIDBytes        = 64
	MaxIDsPerArg          = 20 // job_status.ids
	MaxCardIDs            = 10 // reply.assignments (card_ids, since catalog v3)
	MaxTextListItems      = 10 // 0..10 as the Hub; cannot_do needs one item in either list
	// MaxPlanSteps is a runaway guard, not a product limit: an approved plan has no step limit of its own besides the
	// quota (ADR-0033 §4). Before catalog 10 the Hub itself caps a plan at 8 steps.
	MaxPlanSteps = 200
	MaxQuestions = 3 // stricter than the Hub (5): the planner is told at most 3 per round
	MinOptions   = 2
	MaxOptions   = 6
)

// Call is a tool call of the planner after strict parsing: a catalog tool and arguments that match its schema.
type Call struct {
	id   string
	tool Tool
	args Args
}

// ParseCall checks a call proposed by the planner (untrusted, hard rule 3): the call ID, the tool name and the
// arguments JSON. The arguments must be one JSON object of at most MaxArgumentsBytes (MaxPlanArgumentsBytes for
// show_plan) with only catalog arguments, no duplicates, every required argument, exactly one of each one-of group,
// and values of the catalog type. A null optional argument counts as absent.
func ParseCall(callID, tool, argumentsJSON string) (Call, error) {
	if err := checkCallID(callID); err != nil {
		return Call{}, err
	}
	t, err := ParseTool(tool)
	if err != nil {
		return Call{}, err
	}
	limit := MaxArgumentsBytes
	if t == ToolShowPlan {
		limit = MaxPlanArgumentsBytes
	}
	if len(argumentsJSON) > limit {
		return Call{}, fmt.Errorf("%w: arguments are larger than %d bytes", ErrInvalidArguments, limit)
	}
	spec, _ := specOf(t)
	args, err := parseArgs(spec, []byte(argumentsJSON))
	if err != nil {
		return Call{}, err
	}
	if t == ToolCannotDo && len(args.TextList("your_part"))+len(args.TextList("our_part")) == 0 {
		return Call{}, fmt.Errorf("%w: cannot_do needs an item in your_part or our_part", ErrInvalidArguments)
	}
	return Call{id: callID, tool: t, args: args}, nil
}

// CheckCallID checks a call ID given by the Hub: 1..MaxCallIDBytes ASCII letters, digits, '-' or '_'.
func CheckCallID(id string) error { return checkCallID(id) }

func checkCallID(id string) error {
	if id == "" || len(id) > MaxCallIDBytes {
		return fmt.Errorf("%w: call id must have 1..%d bytes", ErrInvalidArguments, MaxCallIDBytes)
	}
	for i := range len(id) {
		if !asciiAlnum(id[i]) && id[i] != '-' && id[i] != '_' {
			return fmt.Errorf("%w: call id has a forbidden character", ErrInvalidArguments)
		}
	}
	return nil
}

func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ID returns the call ID given by the Hub.
func (c Call) ID() string { return c.id }

// Tool returns the tool.
func (c Call) Tool() Tool { return c.tool }

// Args returns the parsed arguments.
func (c Call) Args() Args { return c.args }

// Refs returns every ID in the arguments, including the IDs of plan steps, for the provenance check.
func (c Call) Refs() []Ref { return c.args.Refs() }

// argValue is one parsed argument. canon is a canonical form used to match a call against an approved plan step.
type argValue struct {
	typ       ArgType
	ref       Ref
	refs      []Ref
	date      Date
	tod       TimeOfDay
	str       string // enum, label, name or text
	texts     []Text
	steps     []PlanStep
	questions []Question
	canon     string
}

// Args are the parsed arguments of a call. Absent optional arguments are not present.
type Args struct {
	vals map[string]argValue
}

// Has reports whether the argument was given.
func (a Args) Has(name string) bool {
	_, ok := a.vals[name]
	return ok
}

// Names returns the given argument names, sorted.
func (a Args) Names() []string {
	out := make([]string, 0, len(a.vals))
	for n := range a.vals {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// ID returns an id argument.
func (a Args) ID(name string) (Ref, bool) {
	v, ok := a.vals[name]
	return v.ref, ok && v.typ == ArgID
}

// IDs returns an ids argument.
func (a Args) IDs(name string) []Ref {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgIDs {
		return nil
	}
	return slices.Clone(v.refs)
}

// CardIDs returns a card_ids argument in the planner's order, duplicates dropped; nil when absent or empty.
func (a Args) CardIDs(name string) []Ref {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgCardIDs {
		return nil
	}
	return slices.Clone(v.refs)
}

// Date returns a date argument.
func (a Args) Date(name string) (Date, bool) {
	v, ok := a.vals[name]
	return v.date, ok && v.typ == ArgDate
}

// Time returns a time argument.
func (a Args) Time(name string) (TimeOfDay, bool) {
	v, ok := a.vals[name]
	return v.tod, ok && v.typ == ArgTime
}

// Enum returns an enum argument.
func (a Args) Enum(name string) (string, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgEnum {
		return "", false
	}
	return v.str, true
}

// Label returns a label argument that is not persistent.
func (a Args) Label(name string) (Label, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgLabel {
		return "", false
	}
	return Label(v.str), true
}

// Query returns a query argument (the student's question or search text).
func (a Args) Query(name string) (Query, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgQuery {
		return "", false
	}
	return Query(v.str), true
}

// Name returns a persistent label argument (a subject, folder or semester name), checked as a Name.
func (a Args) Name(name string) (Name, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgLabel {
		return "", false
	}
	return Name(v.str), true
}

// Text returns a text or reply_text argument.
func (a Args) Text(name string) (Text, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgText && v.typ != ArgReplyText {
		return "", false
	}
	return Text(v.str), true
}

// TextList returns a text_list argument.
func (a Args) TextList(name string) []Text {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgTextList {
		return nil
	}
	return slices.Clone(v.texts)
}

// PlanSteps returns a plan_steps argument.
func (a Args) PlanSteps(name string) []PlanStep {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgPlanSteps {
		return nil
	}
	return slices.Clone(v.steps)
}

// Questions returns a questions argument.
func (a Args) Questions(name string) []Question {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgQuestions {
		return nil
	}
	out := make([]Question, len(v.questions))
	for i, q := range v.questions {
		q.Options = slices.Clone(q.Options)
		out[i] = q
	}
	return out
}

// Refs returns every ID in the arguments, plan steps included, sorted.
func (a Args) Refs() []Ref {
	set := RefSet{}
	for _, v := range a.vals {
		set.Add(v.ref)
		set.Add(v.refs...)
		for _, s := range v.steps {
			set.Add(s.args.Refs()...)
		}
	}
	return set.Sorted()
}

func parseArgs(spec ToolSpec, raw []byte) (Args, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return Args{}, err
	}
	vals := make(map[string]argValue, len(fields))
	for name, rv := range fields {
		as, ok := spec.arg(name)
		if !ok {
			return Args{}, fmt.Errorf("%w: unknown argument %q", ErrInvalidArguments, name)
		}
		if isNull(rv) {
			if as.Required {
				return Args{}, fmt.Errorf("%w: %s is required", ErrInvalidArguments, name)
			}
			continue
		}
		v, err := parseValue(as, rv)
		if err != nil {
			return Args{}, fmt.Errorf("%w: %s: %w", ErrInvalidArguments, name, err)
		}
		vals[name] = v
	}
	for _, as := range spec.args {
		if _, ok := vals[as.Name]; as.Required && !ok {
			return Args{}, fmt.Errorf("%w: %s is required", ErrInvalidArguments, as.Name)
		}
	}
	for _, group := range spec.oneOf {
		if given(vals, group) != 1 {
			return Args{}, fmt.Errorf("%w: exactly one of %s is required", ErrInvalidArguments, strings.Join(group, ", "))
		}
	}
	if from, ok := vals["from"]; ok {
		if to, ok := vals["to"]; ok && to.date.Before(from.date) {
			return Args{}, fmt.Errorf("%w: to is before from", ErrInvalidArguments)
		}
	}
	if err := checkTimeWithDue(vals); err != nil {
		return Args{}, err
	}
	return Args{vals: vals}, nil
}

// dueArg is the date argument a time argument belongs to (create_assignment.due, catalog v6).
const dueArg = "due"

// checkTimeWithDue refuses a time of day without its date: the Hub does not check it (TASK-216), the planner gets
// is_error and corrects the call.
func checkTimeWithDue(vals map[string]argValue) error {
	for name, v := range vals {
		if v.typ != ArgTime {
			continue
		}
		if due, ok := vals[dueArg]; !ok || due.typ != ArgDate {
			return fmt.Errorf("%w: %s needs %s", ErrInvalidArguments, name, dueArg)
		}
	}
	return nil
}

// given counts the arguments of names present in vals.
func given(vals map[string]argValue, names []string) int {
	n := 0
	for _, name := range names {
		if _, ok := vals[name]; ok {
			n++
		}
	}
	return n
}

func parseValue(as ArgSpec, raw json.RawMessage) (argValue, error) {
	v := argValue{typ: as.Type}
	switch as.Type {
	case ArgID:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		if v.ref, err = NewRef(as.Kind, s); err != nil {
			return v, err
		}
		v.canon = v.ref.String()
	case ArgIDs:
		items, err := decodeArray(raw, 1, MaxIDsPerArg)
		if err != nil {
			return v, err
		}
		set := RefSet{}
		for _, it := range items {
			s, err := decodeString(it)
			if err != nil {
				return v, err
			}
			r, err := NewRef(as.Kind, s)
			if err != nil {
				return v, err
			}
			if set.Has(r) {
				return v, fmt.Errorf("%w: duplicate id", ErrInvalidArguments)
			}
			set.Add(r)
			v.refs = append(v.refs, r)
		}
		parts := make([]string, 0, len(set))
		for _, r := range set.Sorted() {
			parts = append(parts, r.String())
		}
		v.canon = strings.Join(parts, ",")
	case ArgCardIDs:
		// A repeated ID is harmless in a list the card shows (the Hub accepts it): it is shown once.
		items, err := decodeArray(raw, 0, MaxCardIDs)
		if err != nil {
			return v, err
		}
		set := RefSet{}
		for _, it := range items {
			s, err := decodeString(it)
			if err != nil {
				return v, err
			}
			r, err := NewRef(as.Kind, s)
			if err != nil {
				return v, err
			}
			if !set.Has(r) {
				set.Add(r)
				v.refs = append(v.refs, r)
			}
		}
	case ArgDate:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		if v.date, err = ParseDate(s); err != nil {
			return v, err
		}
		v.canon = v.date.String()
	case ArgTime:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		if v.tod, err = ParseTimeOfDay(s); err != nil {
			return v, err
		}
		v.canon = v.tod.String()
	case ArgEnum:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		if !slices.Contains(as.Values, s) {
			return v, fmt.Errorf("%w: value outside the enum", ErrInvalidArguments)
		}
		v.str, v.canon = s, s
	case ArgLabel:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		if as.Persistent {
			n, err := ParseName(s)
			if err != nil {
				return v, err
			}
			v.str = string(n)
		} else {
			l, err := ParseLabel(s)
			if err != nil {
				return v, err
			}
			v.str = string(l)
		}
		v.canon = v.str
	case ArgQuery:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		q, err := ParseQuery(s)
		if err != nil {
			return v, err
		}
		v.str, v.canon = string(q), string(q)
	case ArgText:
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		t, err := ParseText(s)
		if err != nil {
			return v, err
		}
		v.str = string(t)
	case ArgReplyText:
		// The Hub cuts a longer text at a sentence (catalog v7); one over the limit here is still invalid.
		s, err := decodeString(raw)
		if err != nil {
			return v, err
		}
		t, err := ParseReplyText(s)
		if err != nil {
			return v, err
		}
		v.str = string(t)
	case ArgTextList:
		texts, err := parseTextList(raw)
		if err != nil {
			return v, err
		}
		v.texts = texts
	case ArgPlanSteps:
		steps, err := parsePlanSteps(raw)
		if err != nil {
			return v, err
		}
		v.steps = steps
	case ArgQuestions:
		qs, err := parseQuestions(raw)
		if err != nil {
			return v, err
		}
		v.questions = qs
	default:
		return v, fmt.Errorf("%w: unsupported type", ErrInvalidArguments)
	}
	return v, nil
}

func parseTextList(raw json.RawMessage) ([]Text, error) {
	items, err := decodeArray(raw, 0, MaxTextListItems)
	if err != nil {
		return nil, err
	}
	out := make([]Text, 0, len(items))
	for _, it := range items {
		s, err := decodeString(it)
		if err != nil {
			return nil, err
		}
		t, err := ParseText(s)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// --- strict JSON helpers ---------------------------------------------------------------------------

// decodeObject reads exactly one JSON object and returns its members. Duplicate keys and trailing data are refused,
// so the planner cannot hide a second value behind the first.
func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("%w: not a JSON object", ErrInvalidArguments)
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidArguments)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidArguments)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrInvalidArguments, key)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidArguments)
		}
		out[key] = v
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidArguments)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data after the object", ErrInvalidArguments)
	}
	return out, nil
}

func decodeArray(raw json.RawMessage, minItems, maxItems int) ([]json.RawMessage, error) {
	if first(raw) != '[' {
		return nil, fmt.Errorf("%w: want an array", ErrInvalidArguments)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%w: malformed array", ErrInvalidArguments)
	}
	if len(items) < minItems || len(items) > maxItems {
		return nil, fmt.Errorf("%w: want %d..%d items", ErrInvalidArguments, minItems, maxItems)
	}
	return items, nil
}

func decodeString(raw json.RawMessage) (string, error) {
	if first(raw) != '"' {
		return "", fmt.Errorf("%w: want a string", ErrInvalidArguments)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%w: malformed string", ErrInvalidArguments)
	}
	return s, nil
}

func decodeInt(raw json.RawMessage) (int, error) {
	if c := first(raw); c != '-' && (c < '0' || c > '9') {
		return 0, fmt.Errorf("%w: want an integer", ErrInvalidArguments)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("%w: want an integer", ErrInvalidArguments)
	}
	return n, nil
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func first(raw json.RawMessage) byte {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return 0
	}
	return t[0]
}
