package values

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// PlanStep is one planned call of a plan card: a tool and its arguments. Only ID, date, time, enum, label and query
// arguments may appear, so the desktop renders the step from templates. A step carries every required argument (and one of each
// one-of group) except an ID of a subject, folder or assignment that an earlier step of the plan creates; such IDs are
// deferred. A later call matches the step when it has the same tool and exactly the step's arguments with the same
// values, plus only deferred IDs, so an approved step is never a wildcard. The only exception is the unplannedArgs
// free text of a tool (the note of create_assignment), which matching ignores.
type PlanStep struct {
	tool     Tool
	args     Args
	deferred []string // ID arguments an earlier step creates; the call fills them in
}

// createdKinds maps a step tool to the ID kind it creates for later steps of the same plan.
var createdKinds = map[Tool]IDKind{
	ToolCreateSubject: KindSubject, ToolCreateFolder: KindFolder, ToolCreateAssignment: KindAssignment,
}

// unplannedArgs are the arguments a call may give, leave out or change against its approved step: free text the plan
// step cannot carry, as plan-step arguments are at most 80 characters on the Hub (TASK-216). The note of an assignment
// is its display text only; what the step fixes (title, subject, lesson, due, time) still has to match.
var unplannedArgs = map[Tool][]string{ToolCreateAssignment: {"note"}}

// IsCreatedKind reports whether a create tool of the catalog makes IDs of kind k for later plan steps.
func IsCreatedKind(k IDKind) bool {
	for _, c := range createdKinds {
		if c == k {
			return true
		}
	}
	return false
}

// Tool returns the planned tool.
func (s PlanStep) Tool() Tool { return s.tool }

// Args returns the planned arguments.
func (s PlanStep) Args() Args { return s.args }

// Deferred returns the ID arguments an earlier step of the plan creates; the call fills them in.
func (s PlanStep) Deferred() []string { return slices.Clone(s.deferred) }

// CreatedKind returns the ID kind a create_subject, create_folder or create_assignment call creates for later plan
// steps.
func CreatedKind(t Tool) (IDKind, bool) {
	k, ok := createdKinds[t]
	return k, ok
}

// Matches reports whether call is this planned step.
func (s PlanStep) Matches(call Call) bool {
	if call.tool != s.tool {
		return false
	}
	free := unplannedArgs[s.tool]
	for name, got := range call.args.vals {
		if slices.Contains(free, name) {
			continue
		}
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
		if _, ok := call.args.vals[name]; !ok && !slices.Contains(free, name) {
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

// JSON keys of plan_steps and questions, exactly as the Hub checks them (hub toolcatalog/schema.go).
const (
	keyTool      = "tool"
	keyArguments = "arguments"
	keyKind      = "kind"
	keyQuestion  = "question"
	keyOptions   = "options"
	keyDefault   = "default"
)

// EncodePlanSteps returns the steps as the JSON the planner sends (plan_steps), for storage.
func EncodePlanSteps(steps []PlanStep) (string, error) {
	type step struct {
		Tool Tool           `json:"tool"`
		Args map[string]any `json:"arguments,omitempty"`
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
			case ArgTime:
				args[name] = v.tod.String()
			case ArgEnum, ArgLabel, ArgQuery:
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

// DecodePlanSteps parses stored steps with the same strict rules as the planner's plan_steps, except that a step may
// name a retired tool: a plan stored under an older catalog still restores (its step can no longer be called).
func DecodePlanSteps(s string) ([]PlanStep, error) { return decodePlanSteps(json.RawMessage(s), true) }

// planStepArgTypes are the argument types a plan step may carry.
var planStepArgTypes = []ArgType{ArgID, ArgIDs, ArgDate, ArgTime, ArgEnum, ArgLabel, ArgQuery}

// parsePlanSteps parses the planner's plan_steps: current catalog tools only.
func parsePlanSteps(raw json.RawMessage) ([]PlanStep, error) { return decodePlanSteps(raw, false) }

func decodePlanSteps(raw json.RawMessage, stored bool) ([]PlanStep, error) {
	items, err := decodeArray(raw, 1, MaxPlanSteps)
	if err != nil {
		return nil, err
	}
	out := make([]PlanStep, 0, len(items))
	created := map[IDKind]bool{}
	for _, it := range items {
		s, err := parsePlanStep(it, created, stored)
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

// parsePlanStep parses one step; created holds the ID kinds that earlier steps create; stored allows a retired tool.
func parsePlanStep(raw json.RawMessage, created map[IDKind]bool, stored bool) (PlanStep, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return PlanStep{}, err
	}
	for k := range fields {
		if k != keyTool && k != keyArguments {
			return PlanStep{}, fmt.Errorf("%w: plan step has an unknown key %q", ErrInvalidArguments, k)
		}
	}
	name, err := decodeString(fields[keyTool])
	if err != nil {
		return PlanStep{}, fmt.Errorf("%w: plan step tool: %w", ErrInvalidArguments, err)
	}
	t := Tool(name)
	spec, ok := specOf(t)
	if !ok && stored {
		spec, ok = storedSpecOf(t)
	}
	if !ok {
		return PlanStep{}, fmt.Errorf("%w: %q", ErrUnknownTool, name)
	}
	vals := map[string]argValue{}
	if rawArgs, ok := fields[keyArguments]; ok {
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
	if err := checkTimeWithDue(vals); err != nil {
		return PlanStep{}, fmt.Errorf("%w: plan step %s: %w", ErrInvalidArguments, t, err)
	}
	var deferred []string
	for _, as := range spec.args {
		if _, ok := vals[as.Name]; ok {
			continue
		}
		// Only an ID the step needs is deferred: a required one, or the first of a one-of group that has no member
		// yet. An optional ID the step leaves out stays out of the call, as every deferred ID must be filled in.
		if as.Type == ArgID && created[as.Kind] && (as.Required || needsOneOf(spec, vals, deferred, as.Name)) {
			deferred = append(deferred, as.Name)
			continue
		}
		if as.Required {
			return PlanStep{}, fmt.Errorf("%w: plan step %s needs %s", ErrInvalidArguments, t, as.Name)
		}
	}
	for _, group := range spec.oneOf {
		n := given(vals, group)
		open := slices.ContainsFunc(group, func(name string) bool { return slices.Contains(deferred, name) })
		if n > 1 || n == 0 && !open {
			return PlanStep{}, fmt.Errorf("%w: plan step %s needs exactly one of %s", ErrInvalidArguments, t,
				strings.Join(group, ", "))
		}
	}
	return PlanStep{tool: t, args: Args{vals: vals}, deferred: deferred}, nil
}

// needsOneOf reports whether name is in a one-of group of spec of which neither vals nor deferred hold a member.
func needsOneOf(spec ToolSpec, vals map[string]argValue, deferred []string, name string) bool {
	group, ok := spec.oneOfGroup(name)
	if !ok || given(vals, group) > 0 {
		return false
	}
	return !slices.ContainsFunc(group, func(n string) bool { return slices.Contains(deferred, n) })
}

// QuestionKind is the kind of a question on a Questions card.
type QuestionKind string

// Question kinds.
const (
	QuestionChoice QuestionKind = "choice"
	QuestionText   QuestionKind = "text"
)

// NoDefault is Question.Default of a choice with no option preselected: the student has to pick one. Only the desktop
// sets it (the subject question); a planner default is always an option index.
const NoDefault = -1

// SubjectQuestion and NoSubjectOption are the subject question of panel prompt v8 (TASK-233), which pins this text
// and puts «Без предмета» last. A fast click must not file the assignment under the first subject, so this question
// gets NoDefault when the planner gives no default.
const (
	SubjectQuestion = "К какому предмету относится задание?"
	NoSubjectOption = "Без предмета"
)

// Question is one question of a Questions card. The planner gets back only {question index, option index} or the
// student's own typed text, never the question or option text (ADR-0022).
type Question struct {
	Kind    QuestionKind
	Text    Text
	Options []Text // choice only, MinOptions..MaxOptions
	Default int    // choice only: index of the default option (0 when the planner gave none) or NoDefault
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
		if q.Default != NoDefault && (q.Default < 0 || q.Default >= len(q.Options)) {
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

// isSubjectQuestion reports whether choice q is the subject question of prompt v8: its exact text, «Без предмета» last.
func isSubjectQuestion(q Question) bool {
	return q.Kind == QuestionChoice && string(q.Text) == SubjectQuestion && len(q.Options) > 0 &&
		string(q.Options[len(q.Options)-1]) == NoSubjectOption
}

func parseQuestion(raw json.RawMessage) (Question, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return Question{}, err
	}
	for k := range fields {
		if !slices.Contains([]string{keyKind, keyQuestion, keyOptions, keyDefault}, k) {
			return Question{}, fmt.Errorf("%w: question has an unknown key %q", ErrInvalidArguments, k)
		}
	}
	kind, err := decodeString(fields[keyKind])
	if err != nil {
		return Question{}, fmt.Errorf("%w: question kind: %w", ErrInvalidArguments, err)
	}
	text, err := decodeString(fields[keyQuestion])
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
		rawOpts, ok := fields[keyOptions]
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
		// The Hub makes default optional; without one the first option is preselected, except on the subject question.
		rawDef, ok := fields[keyDefault]
		switch {
		case ok:
			if q.Default, err = decodeInt(rawDef); err != nil {
				return Question{}, err
			}
			if q.Default < 0 {
				return Question{}, fmt.Errorf("%w: default option out of range", ErrInvalidArguments)
			}
		case isSubjectQuestion(q):
			q.Default = NoDefault
		}
	case QuestionText:
		// The Hub admits "options": [] on a text question; options with items or a default have no meaning here.
		if rawOpts, ok := fields[keyOptions]; ok {
			if _, err := decodeArray(rawOpts, 0, 0); err != nil {
				return Question{}, fmt.Errorf("%w: a text question has no options", ErrInvalidArguments)
			}
		}
		if _, ok := fields[keyDefault]; ok {
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
