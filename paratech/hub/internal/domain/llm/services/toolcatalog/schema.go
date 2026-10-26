package toolcatalog

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"unicode"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/values"
)

// Structured UI argument types (the catalog names them; their shape is fixed here and mirrored by the desktop):
//
//	plan_steps: [{"tool": <non-UI tool of the catalog>, "arguments": {<snake_case>: <string of 1-80 characters>}}], 1-8 steps
//	questions:  [{"kind": "choice"|"text", "question": <text>, "options": [<text>], "default": <option index>}], 1-5
//	            questions; a choice has 2-6 options; default only with options
const (
	fieldTool      = "tool"
	fieldArguments = "arguments"
	fieldKind      = "kind"
	fieldQuestion  = "question"
	fieldOptions   = "options"
	fieldDefault   = "default"
	kindChoice     = "choice"
	minChoices     = 2
)

const (
	toolCreateAssignment = "create_assignment"
	argNote              = "note"
)

// datePattern is the JSON Schema form of values.ValidDate's shape; the check also rejects impossible dates.
const datePattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`

// timePattern is a time of day HH:MM, 24 h; the provider gets it as the schema and the check uses the same expression.
// The rule "time only together with due" is the desktop's (an is_error result the planner corrects), not the Hub's.
const timePattern = `^([01][0-9]|2[0-3]):[0-5][0-9]$`

var timeOfDay = regexp.MustCompile(timePattern)

type object = map[string]any

// schema is the JSON Schema of the arguments of t. Map keys are sorted by encoding/json, so the text is stable and
// the prompt prefix stays cacheable.
func (c *Catalog) schema(t tool, types map[string]string) string {
	props := object{}
	required := []string{}
	for _, a := range t.Arguments {
		s := c.typeSchema(a)
		if d := types[a.Type]; d != "" {
			s["description"] = d
		}
		props[a.Name] = s
		if a.Required {
			required = append(required, a.Name)
		}
	}
	s := object{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "" // NewToolSpec refuses it; cannot happen with these plain values
	}
	return string(b)
}

func (c *Catalog) typeSchema(a argument) object {
	switch a.Type {
	case typeID:
		return idSchema()
	case typeIDs:
		return object{"type": "array", "items": idSchema(), "minItems": 1, "maxItems": maxIDsItems}
	case typeDate:
		return object{"type": "string", "pattern": datePattern}
	case typeTime:
		return object{"type": "string", "pattern": timePattern}
	case typeEnum:
		return object{"type": "string", "enum": a.Values}
	case typeLabel:
		return stringSchema(MaxLabelRunes)
	case typeQuery:
		return stringSchema(MaxQueryRunes)
	case typeText:
		return stringSchema(MaxTextRunes)
	case typeTextList:
		return object{"type": "array", "items": stringSchema(MaxTextRunes), "maxItems": maxTextListItems}
	case typeCardIDs:
		return object{"type": "array", "items": idSchema(), "maxItems": MaxCardIDs}
	case typePlanSteps:
		step := object{
			"type": "object",
			"properties": object{
				fieldTool:      object{"type": "string", "enum": c.actionTools()},
				fieldArguments: object{"type": "object", "additionalProperties": stringSchema(MaxLabelRunes), "maxProperties": maxStepArguments},
			},
			"required":             []string{fieldTool},
			"additionalProperties": false,
		}
		return object{"type": "array", "items": step, "minItems": 1, "maxItems": maxPlanSteps}
	default: // typeQuestions; Parse admits no other type
		question := object{
			"type": "object",
			"properties": object{
				fieldKind:     object{"type": "string", "enum": questionKinds},
				fieldQuestion: stringSchema(MaxTextRunes),
				fieldOptions:  object{"type": "array", "items": stringSchema(MaxTextRunes), "maxItems": maxOptions},
				fieldDefault:  object{"type": "integer", "minimum": 0, "maximum": maxOptions - 1},
			},
			"required":             []string{fieldKind, fieldQuestion},
			"additionalProperties": false,
		}
		return object{"type": "array", "items": question, "minItems": 1, "maxItems": maxQuestions}
	}
}

func idSchema() object {
	return object{"type": "string", "minLength": 1, "maxLength": maxIDBytes}
}

func stringSchema(maxRunes int) object {
	return object{"type": "string", "minLength": 1, "maxLength": maxRunes}
}

// actionTools are the tools a plan step may name: every tool but the UI cards, in catalog order.
func (c *Catalog) actionTools() []string {
	var out []string
	for _, name := range c.order {
		if c.tools[name].Class != classUI {
			out = append(out, name)
		}
	}
	return out
}

// valid reports whether v (decoded with UseNumber) matches the type of a.
func (c *Catalog) valid(a argument, v any) bool {
	switch a.Type {
	case typeID:
		return id(v)
	case typeIDs:
		return eachOf(v, 1, maxIDsItems, id)
	case typeDate:
		s, ok := v.(string)
		return ok && values.ValidDate(s)
	case typeTime:
		s, ok := v.(string)
		return ok && timeOfDay.MatchString(s)
	case typeEnum:
		s, ok := v.(string)
		return ok && slices.Contains(a.Values, s)
	case typeLabel:
		return label(v)
	case typeQuery:
		s, ok := v.(string)
		return ok && boundedText(s, MaxQueryRunes, false)
	case typeText:
		return text(v)
	case typeTextList:
		return eachOf(v, 0, maxTextListItems, text)
	case typeCardIDs:
		return eachOf(v, 0, MaxCardIDs, id)
	case typePlanSteps:
		return eachOf(v, 1, maxPlanSteps, c.planStep)
	case typeQuestions:
		return eachOf(v, 1, maxQuestions, question)
	}
	return false
}

func (c *Catalog) planStep(v any) bool {
	step, ok := v.(map[string]any)
	if !ok {
		return false
	}
	name, ok := step[fieldTool].(string)
	if !ok || !slices.Contains(c.actionTools(), name) {
		return false
	}
	for k, field := range step {
		switch k {
		case fieldTool:
		case fieldArguments:
			args, ok := field.(map[string]any)
			if !ok || len(args) > maxStepArguments {
				return false
			}
			for argName, argValue := range args {
				if !values.ValidToolName(argName) || !c.stepArgument(name, argName, argValue) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// stepArgument checks one plan-step argument: a label, except the note of create_assignment, which keeps the catalog's
// own query limit (a planned note was otherwise refused above 80 characters). The provider schema keeps the 80-character
// bound for every step argument, because changing it would change the frozen catalog text; only this check is looser.
func (c *Catalog) stepArgument(toolName, argName string, v any) bool {
	if toolName == toolCreateAssignment && argName == argNote {
		if t, ok := c.tools[toolName]; ok && slices.ContainsFunc(t.Arguments, func(a argument) bool {
			return a.Name == argName && a.Type == typeQuery
		}) {
			s, ok := v.(string)
			return ok && boundedText(s, MaxQueryRunes, false)
		}
	}

	return label(v)
}

func question(v any) bool {
	q, ok := v.(map[string]any)
	if !ok {
		return false
	}
	kind, ok := q[fieldKind].(string)
	if !ok || !slices.Contains(questionKinds, kind) || !text(q[fieldQuestion]) {
		return false
	}
	options := 0
	if raw, present := q[fieldOptions]; present {
		if !eachOf(raw, 0, maxOptions, text) {
			return false
		}
		options = len(raw.([]any))
	}
	if kind == kindChoice && options < minChoices {
		return false
	}
	if raw, present := q[fieldDefault]; present {
		n, ok := raw.(json.Number)
		if !ok {
			return false
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil || i < 0 || i >= int64(options) {
			return false
		}
	}
	for k := range q {
		switch k {
		case fieldKind, fieldQuestion, fieldOptions, fieldDefault:
		default:
			return false
		}
	}
	return true
}

func eachOf(v any, minItems, maxItems int, ok func(any) bool) bool {
	items, isArray := v.([]any)
	if !isArray || len(items) < minItems || len(items) > maxItems {
		return false
	}
	for _, item := range items {
		if !ok(item) {
			return false
		}
	}
	return true
}

func id(v any) bool {
	s, ok := v.(string)
	return ok && values.ValidID(s)
}

// label is a short single-line text from the student's request.
func label(v any) bool {
	s, ok := v.(string)
	return ok && boundedText(s, MaxLabelRunes, false)
}

// text is a UI card text; line breaks and tabs are allowed.
func text(v any) bool {
	s, ok := v.(string)
	return ok && boundedText(s, MaxTextRunes, true)
}

func boundedText(s string, maxRunes int, multiline bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	blank := true
	for _, r := range s {
		lineBreak := multiline && (r == '\n' || r == '\t')
		if unicode.IsControl(r) && !lineBreak {
			return false
		}
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	return !blank
}
