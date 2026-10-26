// Package triage is the quarantined reader of a homework assignment (OPERATION_TYPE_TRIAGE_ASSIGNMENT, ADR-0022): the
// shape of its input and the strict check of its answer. The assignment and material texts are untrusted data; the
// answer is enums and short bounded texts for the desktop cards, never an instruction, an address or an action.
//
// Wire shape (CompletionSpec.untrusted_data, no client instructions; CompletionResult.output):
//
//	input:  {"round":1|2|3,"assignment_text":"<text>","material_text":"<text or empty>",
//	         "previous":[{"question":"<text>","answer":"<text>"}]}
//	answer: {"work_type":"<type>",
//	         "requirements":{"volume":"","file_format":"","formatting":"","variant":"","deadline":"","sources":""},
//	         "feasibility":"full"|"partial"|"impossible","feasibility_reason":"<text>",
//	         "questions":[{"kind":"choice","question":"<text>","options":["<option>"],"default":<option index>,"why":"<text>"}
//	                     |{"kind":"text","question":"<text>","options":[],"default":"<text>","why":"<text>"}],
//	         "assumptions":["<text>"],
//	         "helper":null|{"steps":["<text>"],"submit":"<text>","pitfalls":["<text>"],"ready_parts":["<text>"],
//	                        "placeholders":["<text>"]}}
//
// The student gets at most two question rounds. Round 1 has no previous answers; round 2 carries the answers of
// round 1; both may ask up to MaxQuestions questions. Round 3 is the final call (the student answered or skipped
// round 2) and carries the answers of rounds 1 and 2: there is no further round, so the Hub returns its questions
// empty (the prompt asks for stated assumptions instead).
package triage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/strictjson"
)

// Limits of the input. Sizes are bytes of UTF-8, lengths are characters (runes).
const (
	// MaxAssignmentBytes bounds the assignment text.
	MaxAssignmentBytes = 32 << 10
	// MaxMaterialBytes bounds the teacher's material text, as much as INSPECT_MATERIAL reads.
	MaxMaterialBytes = 128 << 10
	// QuestionRounds is how many rounds may ask the student questions.
	QuestionRounds = 2
	// FinalRound is the call after the last question round: it asks nothing. A round above it is refused.
	FinalRound = QuestionRounds + 1
	// MaxPrevious bounds the previous questions with answers: those of every question round.
	MaxPrevious = QuestionRounds * MaxQuestions
	// MaxAnswerRunes bounds a student's answer to a previous question.
	MaxAnswerRunes = 1000
)

// Limits of the answer.
const (
	// MaxQuestions bounds the questions of one round.
	MaxQuestions = 5
	// MaxTextRunes bounds every card text (as the cards of ADR-0021).
	MaxTextRunes = 300
	// MaxRequirementRunes bounds one requirement value.
	MaxRequirementRunes = 200
	// MaxOptionRunes bounds one choice option.
	MaxOptionRunes = 100
	// MinOptions and MaxOptions bound the options of a choice question.
	MinOptions, MaxOptions = 2, 6
	// MaxAssumptions bounds the stated assumptions.
	MaxAssumptions = 5
	// MaxSteps bounds the helper plan; MaxHelperItems bounds pitfalls and ready parts; MaxPlaceholders the
	// placeholders the student fills.
	MaxSteps, MaxHelperItems, MaxPlaceholders = 10, 6, 10
)

// FailedOutput is the stored result of a reader answer that was charged but refused, so a retry with the same key
// repeats the refusal instead of paying again. It never passes ParseAnswer.
const FailedOutput = "{}"

// Keys of the input and the answer.
const (
	keyRound          = "round"
	keyAssignmentText = "assignment_text"
	keyMaterialText   = "material_text"
	keyPrevious       = "previous"
	keyQuestion       = "question"
	keyAnswer         = "answer"

	keyWorkType          = "work_type"
	keyRequirements      = "requirements"
	keyFeasibility       = "feasibility"
	keyFeasibilityReason = "feasibility_reason"
	keyQuestions         = "questions"
	keyAssumptions       = "assumptions"
	keyHelper            = "helper"

	keyVolume     = "volume"
	keyFileFormat = "file_format"
	keyFormatting = "formatting"
	keyVariant    = "variant"
	keyDeadline   = "deadline"
	keySources    = "sources"

	keyKind    = "kind"
	keyOptions = "options"
	keyDefault = "default"
	keyWhy     = "why"

	keySteps        = "steps"
	keySubmit       = "submit"
	keyPitfalls     = "pitfalls"
	keyReadyParts   = "ready_parts"
	keyPlaceholders = "placeholders"
)

// Errors of the reader (ARCH-18). None of them quotes the assignment, the material or the model's answer.
var (
	// ErrInvalidInput means the untrusted data of a request is not a valid reader input.
	ErrInvalidInput = errors.New("invalid triage_assignment input")
	// ErrInvalidAnswer means the model's answer breaks the reader schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid triage_assignment answer")
	// ErrMissingHelper is the ErrInvalidAnswer of a partial or impossible work without its helper, an answer otherwise
	// within the schema: the one defect the Hub asks the model once more to fix, with the server's reask note of the
	// prompt version (hub/pkg/prompts, ADR-0025).
	ErrMissingHelper = fmt.Errorf("%w: a partial or impossible work needs a helper", ErrInvalidAnswer)
)

// WorkType is the kind of work an assignment asks for. Closed set.
type WorkType string

// Work types.
const (
	WorkReferat      WorkType = "referat"
	WorkDoklad       WorkType = "doklad"
	WorkEssay        WorkType = "essay"
	WorkAnswers      WorkType = "answers"
	WorkProblems     WorkType = "problems"
	WorkLabReport    WorkType = "lab_report"
	WorkPresentation WorkType = "presentation"
	WorkOther        WorkType = "other"
)

// WorkTypes returns every work type, in a fixed order.
func WorkTypes() []WorkType {
	return []WorkType{WorkReferat, WorkDoklad, WorkEssay, WorkAnswers, WorkProblems, WorkLabReport, WorkPresentation, WorkOther}
}

// Feasibility says how much of the work the app can do. Closed set.
type Feasibility string

// Feasibility outcomes.
const (
	FeasibleFull       Feasibility = "full"
	FeasiblePartial    Feasibility = "partial"
	FeasibleImpossible Feasibility = "impossible"
)

// Feasibilities returns every outcome, in a fixed order.
func Feasibilities() []Feasibility {
	return []Feasibility{FeasibleFull, FeasiblePartial, FeasibleImpossible}
}

// AnswerKind is how the student answers a question. Closed set.
type AnswerKind string

// Answer kinds.
const (
	KindChoice AnswerKind = "choice"
	KindText   AnswerKind = "text"
)

// Previous is a question of an earlier round with the student's answer (the chosen option or the typed text).
type Previous struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Input is a checked reader request. Build it with ParseInput.
type Input struct {
	round      int
	assignment string
	material   string
	previous   []Previous
}

// Round is the round of the request, 1..FinalRound.
func (in Input) Round() int { return in.round }

// ParseInput checks the untrusted data of a request: one JSON object with exactly the keys round (1..FinalRound),
// assignment_text (non-blank, at most MaxAssignmentBytes), material_text (at most MaxMaterialBytes, may be empty) and
// previous (empty in round 1; 1..MaxQuestions questions with answers in round 2; 1..MaxPrevious in the final round).
// No text may hold NUL.
func ParseInput(data string) (Input, error) {
	var (
		in   Input
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(data, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyRound:
			var n int64
			n, ok = strictjson.ReadInt(d)
			if !ok || n < 1 || n > FinalRound {
				return fmt.Errorf("%w: round must be 1..%d", ErrInvalidInput, FinalRound)
			}
			in.round = int(n)
		case keyAssignmentText:
			in.assignment, ok = strictjson.ReadString(d)
			if !ok || len(in.assignment) > MaxAssignmentBytes || strings.TrimSpace(in.assignment) == "" || strings.ContainsRune(in.assignment, 0) {
				return fmt.Errorf("%w: assignment text must be 1-%d bytes, not blank, without NUL", ErrInvalidInput, MaxAssignmentBytes)
			}
		case keyMaterialText:
			in.material, ok = strictjson.ReadString(d)
			if !ok || len(in.material) > MaxMaterialBytes || strings.ContainsRune(in.material, 0) {
				return fmt.Errorf("%w: material text must be at most %d bytes without NUL", ErrInvalidInput, MaxMaterialBytes)
			}
		case keyPrevious:
			in.previous = []Previous{}
			return strictjson.ReadArray(d, func(d *json.Decoder) error {
				p, err := readPrevious(d)
				if err != nil {
					return err
				}
				// The round may come after previous: the per-round bound is checked below.
				if len(in.previous) == MaxPrevious {
					return fmt.Errorf("%w: at most %d previous questions", ErrInvalidInput, MaxPrevious)
				}
				in.previous = append(in.previous, p)
				return nil
			})
		default:
			return fmt.Errorf("%w: unknown key", ErrInvalidInput)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			return Input{}, err
		}
		return Input{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if !seen[keyRound] || !seen[keyAssignmentText] || !seen[keyMaterialText] || !seen[keyPrevious] {
		return Input{}, fmt.Errorf("%w: round, assignment_text, material_text and previous are required", ErrInvalidInput)
	}
	if (in.round == 1) != (len(in.previous) == 0) {
		return Input{}, fmt.Errorf("%w: round 1 has no previous answers, a later round has the answers of the earlier ones", ErrInvalidInput)
	}
	// Round r carries the answers of rounds 1..r-1, at most MaxQuestions each.
	if len(in.previous) > (in.round-1)*MaxQuestions {
		return Input{}, fmt.Errorf("%w: round %d has at most %d previous questions", ErrInvalidInput, in.round, (in.round-1)*MaxQuestions)
	}
	return in, nil
}

func readPrevious(d *json.Decoder) (Previous, error) {
	var p Previous
	seen := map[string]bool{}
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyQuestion:
			p.Question, ok = strictjson.ReadString(d)
			ok = ok && boundedText(p.Question, MaxTextRunes, true, false)
		case keyAnswer:
			p.Answer, ok = strictjson.ReadString(d)
			ok = ok && boundedText(p.Answer, MaxAnswerRunes, true, true)
		}
		if !ok {
			return fmt.Errorf("%w: a previous question is {question: 1-%d characters, answer: 0-%d characters}",
				ErrInvalidInput, MaxTextRunes, MaxAnswerRunes)
		}
		return nil
	})
	if err != nil {
		return Previous{}, err
	}
	if !seen[keyQuestion] || !seen[keyAnswer] {
		return Previous{}, fmt.Errorf("%w: a previous question needs question and answer", ErrInvalidInput)
	}
	return p, nil
}

// ModelData is what the model reads: the checked input re-encoded as one JSON object, so every text is a JSON string
// value and cannot end the data or pose as another key. HTML characters are not escaped.
func (in Input) ModelData() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only an int, strings and a slice of string pairs: encoding cannot fail.
	_ = enc.Encode(struct {
		Round          int        `json:"round"`
		Previous       []Previous `json:"previous"`
		AssignmentText string     `json:"assignment_text"`
		MaterialText   string     `json:"material_text"`
	}{in.round, in.previous, in.assignment, in.material})
	return strings.TrimSuffix(b.String(), "\n")
}

// Requirements are the stated requirements of the work; an empty value is unknown.
type Requirements struct {
	Volume     string `json:"volume"`
	FileFormat string `json:"file_format"`
	Formatting string `json:"formatting"`
	Variant    string `json:"variant"`
	Deadline   string `json:"deadline"`
	Sources    string `json:"sources"`
}

// Question is one question to the student. Options and DefaultOption are set for a choice, DefaultText for a text.
type Question struct {
	Kind          AnswerKind
	Text          string
	Options       []string
	DefaultOption int
	DefaultText   string
	Why           string
}

// Helper is the helper mode of a work the app cannot do whole: steps, what to submit and how, typical pitfalls, the
// parts the app can make, and the placeholders the student fills.
type Helper struct {
	Steps        []string `json:"steps"`
	Submit       string   `json:"submit"`
	Pitfalls     []string `json:"pitfalls"`
	ReadyParts   []string `json:"ready_parts"`
	Placeholders []string `json:"placeholders"`
}

// Answer is a checked reader answer. Helper is nil exactly when Feasibility is FeasibleFull.
type Answer struct {
	WorkType          WorkType
	Requirements      Requirements
	Feasibility       Feasibility
	FeasibilityReason string
	Questions         []Question
	Assumptions       []string
	Helper            *Helper
}

// ParseAnswer checks the model's output against the schema: exactly the seven keys once each, enums of the closed
// sets, every text bounded and without control characters, at most MaxQuestions questions (a choice with
// MinOptions..MaxOptions distinct options and a default index among them, a text with no options and a default text),
// a non-blank reason and a helper unless the work is feasible in full, no helper when it is. Whitespace and one
// Markdown code fence around the object are tolerated; anything else (another key, free text, a second object)
// refuses the whole answer. In the final round the questions are cleared: there is no further round.
func (in Input) ParseAnswer(output string) (Answer, error) {
	a, err := parseAnswer(output)
	if err != nil {
		return Answer{}, err
	}
	if in.round == FinalRound {
		a.Questions = []Question{}
	}
	return a, nil
}

func parseAnswer(output string) (Answer, error) {
	body, ok := strictjson.Unfence(output)
	if !ok {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		a    Answer
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(body, func(key string, d *json.Decoder) error {
		seen[key] = true
		ok := false
		switch key {
		case keyWorkType:
			var s string
			s, ok = strictjson.ReadString(d)
			a.WorkType = WorkType(s)
			ok = ok && slices.Contains(WorkTypes(), a.WorkType)
		case keyRequirements:
			var err error
			a.Requirements, err = readRequirements(d)
			ok = err == nil
		case keyFeasibility:
			var s string
			s, ok = strictjson.ReadString(d)
			a.Feasibility = Feasibility(s)
			ok = ok && slices.Contains(Feasibilities(), a.Feasibility)
		case keyFeasibilityReason:
			a.FeasibilityReason, ok = strictjson.ReadString(d)
			ok = ok && boundedText(a.FeasibilityReason, MaxTextRunes, true, true)
		case keyQuestions:
			a.Questions = []Question{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				q, err := readQuestion(d)
				if err != nil || len(a.Questions) == MaxQuestions {
					return strictjson.ErrShape
				}
				a.Questions = append(a.Questions, q)
				return nil
			}) == nil
		case keyAssumptions:
			a.Assumptions, ok = readTexts(d, 0, MaxAssumptions, MaxTextRunes, true)
		case keyHelper:
			raw, null, read := strictjson.ReadRaw(d)
			ok = read
			if read && !null {
				h, err := parseHelper(raw)
				a.Helper, ok = &h, err == nil
			}
		}
		if !ok {
			return fmt.Errorf("%w: %s is not a value of the schema", ErrInvalidAnswer, safeKey(key))
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAnswer) {
			return Answer{}, err
		}
		return Answer{}, fmt.Errorf("%w: %w", ErrInvalidAnswer, err)
	}
	for _, k := range []string{keyWorkType, keyRequirements, keyFeasibility, keyFeasibilityReason, keyQuestions, keyAssumptions, keyHelper} {
		if !seen[k] {
			return Answer{}, fmt.Errorf("%w: %s is missing", ErrInvalidAnswer, k)
		}
	}
	full := a.Feasibility == FeasibleFull
	switch {
	case full && a.Helper != nil:
		return Answer{}, fmt.Errorf("%w: a work feasible in full has no helper", ErrInvalidAnswer)
	case !full && a.Helper == nil:
		return Answer{}, ErrMissingHelper
	case !full && strings.TrimSpace(a.FeasibilityReason) == "":
		return Answer{}, fmt.Errorf("%w: a partial or impossible work needs a reason", ErrInvalidAnswer)
	}
	return a, nil
}

// Encode is the canonical answer the Hub returns and stores: the keys in the order of the wire shape, empty lists as
// [], helper null when there is none, a default of the type its kind takes.
func (a Answer) Encode() string {
	type question struct {
		Kind     AnswerKind `json:"kind"`
		Question string     `json:"question"`
		Options  []string   `json:"options"`
		Default  any        `json:"default"`
		Why      string     `json:"why"`
	}
	questions := make([]question, 0, len(a.Questions))
	for _, q := range a.Questions {
		out := question{Kind: q.Kind, Question: q.Text, Options: nonNil(q.Options), Default: q.DefaultText, Why: q.Why}
		if q.Kind == KindChoice {
			out.Default = q.DefaultOption
		}
		questions = append(questions, out)
	}
	var helper *Helper
	if a.Helper != nil {
		h := Helper{
			Steps: nonNil(a.Helper.Steps), Submit: a.Helper.Submit, Pitfalls: nonNil(a.Helper.Pitfalls),
			ReadyParts: nonNil(a.Helper.ReadyParts), Placeholders: nonNil(a.Helper.Placeholders),
		}
		helper = &h
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings, ints, slices and a pointer to a struct of them: encoding cannot fail.
	_ = enc.Encode(struct {
		WorkType          WorkType     `json:"work_type"`
		Requirements      Requirements `json:"requirements"`
		Feasibility       Feasibility  `json:"feasibility"`
		FeasibilityReason string       `json:"feasibility_reason"`
		Questions         []question   `json:"questions"`
		Assumptions       []string     `json:"assumptions"`
		Helper            *Helper      `json:"helper"`
	}{a.WorkType, a.Requirements, a.Feasibility, a.FeasibilityReason, questions, nonNil(a.Assumptions), helper})
	return strings.TrimSuffix(b.String(), "\n")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func readRequirements(d *json.Decoder) (Requirements, error) {
	var r Requirements
	fields := map[string]*string{
		keyVolume: &r.Volume, keyFileFormat: &r.FileFormat, keyFormatting: &r.Formatting,
		keyVariant: &r.Variant, keyDeadline: &r.Deadline, keySources: &r.Sources,
	}
	seen := 0
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		dst, known := fields[key]
		if !known {
			return strictjson.ErrShape
		}
		s, ok := strictjson.ReadString(d)
		if !ok || !boundedText(s, MaxRequirementRunes, false, true) {
			return strictjson.ErrShape
		}
		*dst = s
		seen++
		return nil
	})
	if err != nil || seen != len(fields) {
		return Requirements{}, strictjson.ErrShape
	}
	return r, nil
}

func readQuestion(d *json.Decoder) (Question, error) {
	var (
		q          Question
		seen       = map[string]bool{}
		rawDefault string
	)
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyKind:
			var s string
			s, ok = strictjson.ReadString(d)
			q.Kind = AnswerKind(s)
			ok = ok && (q.Kind == KindChoice || q.Kind == KindText)
		case keyQuestion:
			q.Text, ok = strictjson.ReadString(d)
			ok = ok && boundedText(q.Text, MaxTextRunes, true, false)
		case keyOptions:
			q.Options, ok = readTexts(d, 0, MaxOptions, MaxOptionRunes, false)
		case keyDefault:
			// Its type depends on the kind, which may come later: checked below.
			rawDefault, _, ok = strictjson.ReadRaw(d)
		case keyWhy:
			q.Why, ok = strictjson.ReadString(d)
			ok = ok && boundedText(q.Why, MaxTextRunes, true, false)
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || len(seen) != 5 || !seen[keyKind] || !seen[keyQuestion] || !seen[keyOptions] || !seen[keyDefault] || !seen[keyWhy] {
		return Question{}, strictjson.ErrShape
	}
	if q.Kind == KindText {
		if len(q.Options) != 0 || !readDefaultText(rawDefault, &q.DefaultText) {
			return Question{}, strictjson.ErrShape
		}
		return q, nil
	}
	if len(q.Options) < MinOptions || hasDuplicate(q.Options) || !readDefaultOption(rawDefault, len(q.Options), &q.DefaultOption) {
		return Question{}, strictjson.ErrShape
	}
	return q, nil
}

// readDefaultText reads the raw default of a text question: a string, possibly empty.
func readDefaultText(raw string, dst *string) bool {
	s, ok := strictjson.ReadString(rawDecoder(raw))
	if !ok || !boundedText(s, MaxTextRunes, true, true) {
		return false
	}
	*dst = s
	return true
}

// readDefaultOption reads the raw default of a choice question: the index of one of its options.
func readDefaultOption(raw string, options int, dst *int) bool {
	n, ok := strictjson.ReadInt(rawDecoder(raw))
	if !ok || n < 0 || n >= int64(options) {
		return false
	}
	*dst = int(n)
	return true
}

// rawDecoder reads one value that strictjson.ReadRaw has already checked as a whole JSON value.
func rawDecoder(raw string) *json.Decoder {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	return d
}

func parseHelper(raw string) (Helper, error) {
	var (
		h    Helper
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(raw, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keySteps:
			h.Steps, ok = readTexts(d, 1, MaxSteps, MaxTextRunes, true)
		case keySubmit:
			h.Submit, ok = strictjson.ReadString(d)
			ok = ok && boundedText(h.Submit, MaxTextRunes, true, false)
		case keyPitfalls:
			h.Pitfalls, ok = readTexts(d, 0, MaxHelperItems, MaxTextRunes, true)
		case keyReadyParts:
			h.ReadyParts, ok = readTexts(d, 0, MaxHelperItems, MaxTextRunes, true)
		case keyPlaceholders:
			h.Placeholders, ok = readTexts(d, 0, MaxPlaceholders, MaxRequirementRunes, false)
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || len(seen) != 5 {
		return Helper{}, strictjson.ErrShape
	}
	return h, nil
}

// readTexts reads an array of minItems..maxItems non-blank texts of at most maxRunes each.
func readTexts(d *json.Decoder, minItems, maxItems, maxRunes int, multiline bool) ([]string, bool) {
	out := []string{}
	err := strictjson.ReadArray(d, func(d *json.Decoder) error {
		s, ok := strictjson.ReadString(d)
		if !ok || len(out) == maxItems || !boundedText(s, maxRunes, multiline, false) {
			return strictjson.ErrShape
		}
		out = append(out, s)
		return nil
	})
	return out, err == nil && len(out) >= minItems
}

// boundedText is valid UTF-8 of at most maxRunes characters without control characters (a multiline text may hold
// line breaks and tabs), non-blank unless empty is allowed; an allowed empty value is exactly "".
func boundedText(s string, maxRunes int, multiline, empty bool) bool {
	if s == "" {
		return empty
	}
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

func hasDuplicate(items []string) bool {
	seen := make(map[string]bool, len(items))
	for _, s := range items {
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}

// safeKey names a schema key in an error; any other key (model text) is not quoted.
func safeKey(key string) string {
	switch key {
	case keyWorkType, keyRequirements, keyFeasibility, keyFeasibilityReason, keyQuestions, keyAssumptions, keyHelper:
		return key
	}
	return "an unknown key"
}
