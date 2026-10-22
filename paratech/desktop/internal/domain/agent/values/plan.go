package values

import (
	"encoding/json"
	"fmt"
	"slices"
)

// PlanStep is one planned call of a plan card: a tool and its arguments. Only ID, date, enum and label arguments may
// appear, so the desktop renders the step from templates. A step carries every required argument (and one of each
// one-of pair) except an ID of a subject or folder that an earlier step of the plan creates; such IDs are deferred. A
// later call matches the step when it has the same tool and exactly the step's arguments with the same values, plus
// only deferred IDs, so an approved step is never a wildcard.
type PlanStep struct {
	tool     Tool
	args     Args
	deferred []string // ID arguments an earlier step creates; the call fills them in
}

// createdKinds maps a step tool to the ID kind it creates for later steps of the same plan.
var createdKinds = map[Tool]IDKind{ToolCreateSubject: KindSubject, ToolCreateFolder: KindFolder}

// Tool returns the planned tool.
func (s PlanStep) Tool() Tool { return s.tool }

// Args returns the planned arguments.
func (s PlanStep) Args() Args { return s.args }

// Deferred returns the ID arguments an earlier step of the plan creates; the call fills them in.
func (s PlanStep) Deferred() []string { return slices.Clone(s.deferred) }

// CreatedKind returns the ID kind a create_subject or create_folder call creates for later plan steps.
func CreatedKind(t Tool) (IDKind, bool) {
	k, ok := createdKinds[t]
	return k, ok
}

// Matches reports whether call is this planned step.
func (s PlanStep) Matches(call Call) bool {
	if call.tool != s.tool {
		return false
	}
	for name, got := range call.args.vals {
		want, ok := s.args.vals[name]
		if !ok {
			if !slices.Contains(s.deferred, name) {
				return false
			}
			continue
		}
		if got.canon != want.canon {
			return false
		}
	}
	for name := range s.args.vals {
		if _, ok := call.args.vals[name]; !ok {
			return false
		}
	}
	// A deferred ID is part of the step: a call that leaves it out does something else than planned.
	for _, name := range s.deferred {
		if _, ok := call.args.vals[name]; !ok {
			return false
		}
	}
	return true
}

// EncodePlanSteps returns the steps as the JSON the planner sends (plan_steps), for storage.
func EncodePlanSteps(steps []PlanStep) (string, error) {
	type step struct {
		Tool Tool           `json:"tool"`
		Args map[string]any `json:"args,omitempty"`
	}
	out := make([]step, 0, len(steps))
	for _, s := range steps {
		args := map[string]any{}
		for name, v := range s.args.vals {
			switch v.typ {
			case ArgID:
				args[name] = v.ref.ID()
			case ArgIDs:
				ids := make([]string, len(v.refs))
				for i, r := range v.refs {
					ids[i] = r.ID()
				}
				args[name] = ids
			case ArgDate:
				args[name] = v.date.String()
			case ArgEnum, ArgLabel:
				args[name] = v.str
			default:
				return "", fmt.Errorf("%w: plan step argument %q cannot be stored", ErrInvalidArguments, name)
			}
		}
		out = append(out, step{Tool: s.tool, Args: args})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	return string(b), nil
}

// DecodePlanSteps parses stored steps with the same strict rules as the planner's plan_steps.
func DecodePlanSteps(s string) ([]PlanStep, error) { return parsePlanSteps(json.RawMessage(s)) }

// planStepArgTypes are the argument types a plan step may carry.
var planStepArgTypes = []ArgType{ArgID, ArgIDs, ArgDate, ArgEnum, ArgLabel}

func parsePlanSteps(raw json.RawMessage) ([]PlanStep, error) {
	items, err := decodeArray(raw, 1, MaxPlanSteps)
	if err != nil {
		return nil, err
	}
	out := make([]PlanStep, 0, len(items))
	created := map[IDKind]bool{}
	for _, it := range items {
		s, err := parsePlanStep(it, created)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if k, ok := createdKinds[s.tool]; ok {
			created[k] = true
		}
	}
	return out, nil
}

// parsePlanStep parses one step; created holds the ID kinds that earlier steps create.
func parsePlanStep(raw json.RawMessage, created map[IDKind]bool) (PlanStep, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return PlanStep{}, err
	}
	for k := range fields {
		if k != "tool" && k != "args" {
			return PlanStep{}, fmt.Errorf("%w: plan step has an unknown key %q", ErrInvalidArguments, k)
		}
	}
	name, err := decodeString(fields["tool"])
	if err != nil {
		return PlanStep{}, fmt.Errorf("%w: plan step tool: %w", ErrInvalidArguments, err)
	}
	t, err := ParseTool(name)
	if err != nil {
		return PlanStep{}, err
	}
	spec, _ := specOf(t)
	vals := map[string]argValue{}
	if rawArgs, ok := fields["args"]; ok {
		members, err := decodeObject(rawArgs)
		if err != nil {
			return PlanStep{}, err
		}
		for n, rv := range members {
			as, ok := spec.arg(n)
			if !ok || !slices.Contains(planStepArgTypes, as.Type) {
				return PlanStep{}, fmt.Errorf("%w: plan step argument %q is not allowed", ErrInvalidArguments, n)
			}
			v, err := parseValue(as, rv)
			if err != nil {
				return PlanStep{}, fmt.Errorf("%w: plan step %s: %w", ErrInvalidArguments, n, err)
			}
			vals[n] = v
		}
	}
	var deferred []string
	for _, as := range spec.args {
		if _, ok := vals[as.Name]; ok {
			continue
		}
		// Only an ID the step needs is deferred: a required one, or one of a one-of pair whose other member is
		// absent. An optional ID the step leaves out stays out of the call, as every deferred ID must be filled in.
		if as.Type == ArgID && created[as.Kind] && (as.Required || needsOneOf(spec, vals, as.Name)) {
			deferred = append(deferred, as.Name)
			continue
		}
		if as.Required {
			return PlanStep{}, fmt.Errorf("%w: plan step %s needs %s", ErrInvalidArguments, t, as.Name)
		}
	}
	for _, pair := range spec.oneOf {
		_, a := vals[pair[0]]
		_, b := vals[pair[1]]
		open := slices.Contains(deferred, pair[0]) || slices.Contains(deferred, pair[1])
		if a && b || !a && !b && !open {
			return PlanStep{}, fmt.Errorf("%w: plan step %s needs exactly one of %s and %s", ErrInvalidArguments, t,
				pair[0], pair[1])
		}
	}
	return PlanStep{tool: t, args: Args{vals: vals}, deferred: deferred}, nil
}

// needsOneOf reports whether name is in a one-of pair of spec whose other member vals lack.
func needsOneOf(spec ToolSpec, vals map[string]argValue, name string) bool {
	for _, pair := range spec.oneOf {
		other := ""
		switch name {
		case pair[0]:
			other = pair[1]
		case pair[1]:
			other = pair[0]
		default:
			continue
		}
		if _, ok := vals[other]; !ok {
			return true
		}
	}
	return false
}

// QuestionKind is the kind of a question on a Questions card.
type QuestionKind string

// Question kinds.
const (
	QuestionChoice QuestionKind = "choice"
	QuestionText   QuestionKind = "text"
)

// Question is one question of a Questions card. The planner gets back only {question index, option index} or the
// student's own typed text, never the question or option text (ADR-0022).
type Question struct {
	Kind    QuestionKind
	Text    Text
	Options []Text // choice only, MinOptions..MaxOptions
	Default int    // choice only: index of the default option
	CiteRef string // optional reference to the place in a source, set by the desktop (triage questions), never by the planner
}

// Validate checks the invariants of a question.
func (q Question) Validate() error {
	if _, err := ParseText(string(q.Text)); err != nil {
		return fmt.Errorf("%w: question text: %w", ErrInvalidCard, err)
	}
	switch q.Kind {
	case QuestionChoice:
		if len(q.Options) < MinOptions || len(q.Options) > MaxOptions {
			return fmt.Errorf("%w: a choice has %d..%d options", ErrInvalidCard, MinOptions, MaxOptions)
		}
		for _, o := range q.Options {
			if _, err := ParseText(string(o)); err != nil {
				return fmt.Errorf("%w: option: %w", ErrInvalidCard, err)
			}
		}
		if q.Default < 0 || q.Default >= len(q.Options) {
			return fmt.Errorf("%w: default option out of range", ErrInvalidCard)
		}
	case QuestionText:
		if len(q.Options) != 0 || q.Default != 0 {
			return fmt.Errorf("%w: a text question has no options", ErrInvalidCard)
		}
	default:
		return fmt.Errorf("%w: unknown question kind", ErrInvalidCard)
	}
	if len(q.CiteRef) > MaxLabelRunes {
		return fmt.Errorf("%w: cite reference too long", ErrInvalidCard)
	}
	return nil
}

func parseQuestions(raw json.RawMessage) ([]Question, error) {
	items, err := decodeArray(raw, 1, MaxQuestions)
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(items))
	for _, it := range items {
		q, err := parseQuestion(it)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, nil
}

func parseQuestion(raw json.RawMessage) (Question, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return Question{}, err
	}
	for k := range fields {
		if !slices.Contains([]string{"kind", "text", "options", "default"}, k) {
			return Question{}, fmt.Errorf("%w: question has an unknown key %q", ErrInvalidArguments, k)
		}
	}
	kind, err := decodeString(fields["kind"])
	if err != nil {
		return Question{}, fmt.Errorf("%w: question kind: %w", ErrInvalidArguments, err)
	}
	text, err := decodeString(fields["text"])
	if err != nil {
		return Question{}, fmt.Errorf("%w: question text: %w", ErrInvalidArguments, err)
	}
	t, err := ParseText(text)
	if err != nil {
		return Question{}, err
	}
	q := Question{Kind: QuestionKind(kind), Text: t}
	switch q.Kind {
	case QuestionChoice:
		rawOpts, ok := fields["options"]
		if !ok {
			return Question{}, fmt.Errorf("%w: a choice needs options", ErrInvalidArguments)
		}
		items, err := decodeArray(rawOpts, MinOptions, MaxOptions)
		if err != nil {
			return Question{}, err
		}
		for _, it := range items {
			s, err := decodeString(it)
			if err != nil {
				return Question{}, err
			}
			o, err := ParseText(s)
			if err != nil {
				return Question{}, err
			}
			q.Options = append(q.Options, o)
		}
		rawDef, ok := fields["default"]
		if !ok {
			return Question{}, fmt.Errorf("%w: a choice needs a default option", ErrInvalidArguments)
		}
		if q.Default, err = decodeInt(rawDef); err != nil {
			return Question{}, err
		}
	case QuestionText:
		if _, ok := fields["options"]; ok {
			return Question{}, fmt.Errorf("%w: a text question has no options", ErrInvalidArguments)
		}
		if _, ok := fields["default"]; ok {
			return Question{}, fmt.Errorf("%w: a text question has no default", ErrInvalidArguments)
		}
	default:
		return Question{}, fmt.Errorf("%w: question kind is choice or text", ErrInvalidArguments)
	}
	if err := q.Validate(); err != nil {
		return Question{}, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	return q, nil
}
