package work

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// The reader TRIAGE_ASSIGNMENT (ADR-0022, ADR-0023; paratech/README.md). Its limits and schema are those the Hub
// checks (hub domain llm/services/triage); the desktop checks the answer again (defence in depth).
//
//	input:  {"round":1|2|3,"assignment_text":"<text>","material_text":"<text or empty>",
//	         "previous":[{"question":"<text>","answer":"<text>"}]}
//	answer: {"work_type":"<type>",
//	         "requirements":{"volume":"","file_format":"","formatting":"","variant":"","deadline":"","sources":""},
//	         "feasibility":"full"|"partial"|"impossible","feasibility_reason":"<text>",
//	         "questions":[{"kind":"choice","question":"<text>","options":["<option>"],"default":<index>,"why":"<text>"}
//	                     |{"kind":"text","question":"<text>","options":[],"default":"<text>","why":"<text>"}],
//	         "assumptions":["<text>"],
//	         "helper":null|{"steps":["<text>"],"submit":"<text>","pitfalls":["<text>"],"ready_parts":["<text>"],
//	                        "placeholders":["<text>"]}}

// Limits of the input. Sizes are bytes of UTF-8, lengths are characters (runes).
const (
	// MaxAssignmentBytes bounds the assignment text; the beginning of a longer one is sent.
	MaxAssignmentBytes = 32 << 10
	// MaxMaterialBytes bounds the teacher's material text; the beginning of a longer one is sent.
	MaxMaterialBytes = 128 << 10
	// QuestionRounds is how many rounds may ask the student questions.
	QuestionRounds = 2
	// FinalRound is the reader call after the last question round: it asks nothing.
	FinalRound = QuestionRounds + 1
	// MaxAnswerRunes bounds a student's typed answer.
	MaxAnswerRunes = 1000
	// MaxAttempts bounds the reader calls of one round: each refused answer is charged.
	MaxAttempts = 3
	// MaxOutputTokens is the output cap asked for; the Hub clamps it to its alias cap.
	MaxOutputTokens = 4096

	// keyPrefix starts every idempotency key of the reader; the version changes with the request shape.
	keyPrefix = "tri:v1:"
)

// Limits of the answer.
const (
	MaxQuestions                              = 5
	MaxTextRunes                              = 300
	MaxRequirementRunes                       = 200
	MaxOptionRunes                            = 100
	MinOptions, MaxOptions                    = 2, 6
	MaxAssumptions                            = 5
	MaxSteps, MaxHelperItems, MaxPlaceholders = 10, 6, 10
)

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

// WorkType is the kind of work an assignment asks for. Closed set.
type WorkType string

// Work types.
const (
	TypeReferat      WorkType = "referat"
	TypeDoklad       WorkType = "doklad"
	TypeEssay        WorkType = "essay"
	TypeAnswers      WorkType = "answers"
	TypeProblems     WorkType = "problems"
	TypeLabReport    WorkType = "lab_report"
	TypePresentation WorkType = "presentation"
	TypeOther        WorkType = "other"
)

// WorkTypes returns every work type, in a fixed order.
func WorkTypes() []WorkType {
	return []WorkType{TypeReferat, TypeDoklad, TypeEssay, TypeAnswers, TypeProblems, TypeLabReport, TypePresentation, TypeOther}
}

// Valid reports whether t is one of the work types.
func (t WorkType) Valid() bool { return slices.Contains(WorkTypes(), t) }

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

// Kind is how the student answers a question. Closed set.
type Kind string

// Answer kinds.
const (
	KindChoice Kind = "choice"
	KindText   Kind = "text"
)

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
	Kind          Kind
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

// Previous is a question of an earlier round with the student's answer (the chosen option or the typed text; empty
// means skipped).
type Previous struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Request is a checked reader request: its round, untrusted data and idempotency key. Build it with Work.Request.
type Request struct {
	round int
	data  string
	key   string
}

// newRequest checks the input as the Hub does and builds the data and the key. The texts are cut to their limits
// before (Head of the material text); a longer one here is ErrInvalidRequest.
func newRequest(id shared.WorkID, round, attempt int, assignment, material string, previous []Previous) (Request, error) {
	bad := func(why string) (Request, error) { return Request{}, fmt.Errorf("%w: %s", ErrInvalidRequest, why) }
	switch {
	case id.Validate() != nil:
		return bad("work id")
	case round < 1 || round > FinalRound:
		return bad("round")
	case attempt < 1 || attempt > MaxAttempts:
		return bad("attempt")
	case len(assignment) > MaxAssignmentBytes || !utf8.ValidString(assignment) || strings.TrimSpace(assignment) == "" ||
		strings.ContainsRune(assignment, 0):
		return bad("assignment text must be 1-32 KB of UTF-8, not blank, without NUL")
	case len(material) > MaxMaterialBytes || !utf8.ValidString(material) || strings.ContainsRune(material, 0):
		return bad("material text must be at most 128 KB of UTF-8 without NUL")
	case (round == 1) != (len(previous) == 0) || len(previous) > (round-1)*MaxQuestions:
		return bad("previous answers do not fit the round")
	}
	for _, p := range previous {
		if !boundedText(p.Question, MaxTextRunes, true, false) || !boundedText(p.Answer, MaxAnswerRunes, true, true) {
			return bad("a previous question or answer")
		}
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// An int, strings and a slice of string pairs only: encoding cannot fail.
	_ = enc.Encode(struct {
		Round          int        `json:"round"`
		AssignmentText string     `json:"assignment_text"`
		MaterialText   string     `json:"material_text"`
		Previous       []Previous `json:"previous"`
	}{round, assignment, material, append([]Previous{}, previous...)})
	data := strings.TrimSuffix(b.String(), "\n")

	sum := sha256.New()
	for _, part := range []string{string(id), data, strconv.Itoa(attempt)} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return Request{round: round, data: data, key: fmt.Sprintf("%s%x", keyPrefix, sum.Sum(nil))}, nil
}

// Round is the round of the request, 1..FinalRound.
func (r Request) Round() int { return r.round }

// UntrustedData is the JSON object the reader reads as data.
func (r Request) UntrustedData() string { return r.data }

// Key is the idempotency key: a digest of the work, the data (with the round) and the attempt, so a retry of the same
// call is replayed by the Hub without a second charge and any change is a new key.
func (r Request) Key() string { return r.key }

// IsZero reports whether r is the zero value.
func (r Request) IsZero() bool { return r.data == "" }

// Parse checks the reader's output for this request (ParseAnswer of its round).
func (r Request) Parse(output string) (Answer, error) { return ParseAnswer(r.round, output) }

// ParseAnswer checks an answer of round against the schema: exactly the seven keys once each, enums of the closed
// sets, every text bounded and without control characters, at most MaxQuestions questions (a choice with
// MinOptions..MaxOptions distinct options and a default index among them, a text with no options and a default
// text), a non-blank reason and a helper unless the work is feasible in full, no helper when it is, and no questions
// in the final round. Anything else (another key, free text in an enum, a second object, a code fence) refuses the
// whole answer with ErrInvalidAnswer; the error never quotes the output. Stored answers are read back with it too.
func ParseAnswer(round int, output string) (Answer, error) {
	if round < 1 || round > FinalRound {
		return Answer{}, fmt.Errorf("%w: round", ErrInvalidAnswer)
	}
	if !utf8.ValidString(output) {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		a    Answer
		seen = map[string]bool{}
	)
	err := decodeObject(output, func(key string, d *json.Decoder) error {
		seen[key] = true
		ok := false
		switch key {
		case keyWorkType:
			var s string
			s, ok = readString(d)
			a.WorkType = WorkType(s)
			ok = ok && a.WorkType.Valid()
		case keyRequirements:
			var err error
			a.Requirements, err = readRequirements(d)
			ok = err == nil
		case keyFeasibility:
			var s string
			s, ok = readString(d)
			a.Feasibility = Feasibility(s)
			ok = ok && slices.Contains(Feasibilities(), a.Feasibility)
		case keyFeasibilityReason:
			a.FeasibilityReason, ok = readString(d)
			ok = ok && boundedText(a.FeasibilityReason, MaxTextRunes, true, true)
		case keyQuestions:
			a.Questions = []Question{}
			ok = readArray(d, func(d *json.Decoder) error {
				q, err := readQuestion(d)
				if err != nil || len(a.Questions) == MaxQuestions {
					return errShape
				}
				a.Questions = append(a.Questions, q)
				return nil
			}) == nil
		case keyAssumptions:
			a.Assumptions, ok = readTexts(d, 0, MaxAssumptions, MaxTextRunes, true)
		case keyHelper:
			raw, null, read := readRaw(d)
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
	if err := a.check(round); err != nil {
		return Answer{}, err
	}
	return a, nil
}

// check holds the rules across fields of an answer of round.
func (a Answer) check(round int) error {
	full := a.Feasibility == FeasibleFull
	switch {
	case full && a.Helper != nil:
		return fmt.Errorf("%w: a work feasible in full has no helper", ErrInvalidAnswer)
	case !full && a.Helper == nil:
		return fmt.Errorf("%w: a partial or impossible work needs a helper", ErrInvalidAnswer)
	case !full && strings.TrimSpace(a.FeasibilityReason) == "":
		return fmt.Errorf("%w: a partial or impossible work needs a reason", ErrInvalidAnswer)
	case round == FinalRound && len(a.Questions) > 0:
		return fmt.Errorf("%w: the final round asks nothing", ErrInvalidAnswer)
	}
	return nil
}

// Encode is the canonical form of the answer, the Hub's: the keys in the order of the wire shape, empty lists as [],
// helper null when there is none, a default of the type its kind takes. The repository stores it and reads it back
// with ParseAnswer.
func (a Answer) Encode() string {
	type question struct {
		Kind     Kind     `json:"kind"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Default  any      `json:"default"`
		Why      string   `json:"why"`
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
	fields := r.fields()
	seen := 0
	err := readObject(d, func(key string, d *json.Decoder) error {
		dst, known := fields[key]
		if !known {
			return errShape
		}
		s, ok := readString(d)
		if !ok || !boundedText(s, MaxRequirementRunes, false, true) {
			return errShape
		}
		*dst = s
		seen++
		return nil
	})
	if err != nil || seen != len(fields) {
		return Requirements{}, errShape
	}
	return r, nil
}

// fields maps the wire keys of the requirements to their fields.
func (r *Requirements) fields() map[string]*string {
	return map[string]*string{
		string(FieldVolume): &r.Volume, string(FieldFileFormat): &r.FileFormat, string(FieldFormatting): &r.Formatting,
		string(FieldVariant): &r.Variant, string(FieldDeadline): &r.Deadline, string(FieldSources): &r.Sources,
	}
}

func readQuestion(d *json.Decoder) (Question, error) {
	var (
		q          Question
		seen       = map[string]bool{}
		rawDefault string
	)
	err := readObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyKind:
			var s string
			s, ok = readString(d)
			q.Kind = Kind(s)
			ok = ok && (q.Kind == KindChoice || q.Kind == KindText)
		case keyQuestion:
			q.Text, ok = readString(d)
			ok = ok && boundedText(q.Text, MaxTextRunes, true, false)
		case keyOptions:
			q.Options, ok = readTexts(d, 0, MaxOptions, MaxOptionRunes, false)
		case keyDefault:
			// Its type depends on the kind, which may come later: checked below.
			rawDefault, _, ok = readRaw(d)
		case keyWhy:
			q.Why, ok = readString(d)
			ok = ok && boundedText(q.Why, MaxTextRunes, true, false)
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || len(seen) != 5 || !seen[keyKind] || !seen[keyQuestion] || !seen[keyOptions] || !seen[keyDefault] || !seen[keyWhy] {
		return Question{}, errShape
	}
	if q.Kind == KindText {
		s, ok := readString(rawDecoder(rawDefault))
		if len(q.Options) != 0 || !ok || !boundedText(s, MaxTextRunes, true, true) {
			return Question{}, errShape
		}
		q.DefaultText = s
		return q, nil
	}
	n, ok := readInt(rawDecoder(rawDefault))
	if len(q.Options) < MinOptions || hasDuplicate(q.Options) || !ok || n < 0 || n >= int64(len(q.Options)) {
		return Question{}, errShape
	}
	q.DefaultOption = int(n)
	return q, nil
}

// rawDecoder reads one value that readRaw has already checked as a whole JSON value.
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
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keySteps:
			h.Steps, ok = readTexts(d, 1, MaxSteps, MaxTextRunes, true)
		case keySubmit:
			h.Submit, ok = readString(d)
			ok = ok && boundedText(h.Submit, MaxTextRunes, true, false)
		case keyPitfalls:
			h.Pitfalls, ok = readTexts(d, 0, MaxHelperItems, MaxTextRunes, true)
		case keyReadyParts:
			h.ReadyParts, ok = readTexts(d, 0, MaxHelperItems, MaxTextRunes, true)
		case keyPlaceholders:
			h.Placeholders, ok = readTexts(d, 0, MaxPlaceholders, MaxRequirementRunes, false)
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || len(seen) != 5 {
		return Helper{}, errShape
	}
	return h, nil
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
