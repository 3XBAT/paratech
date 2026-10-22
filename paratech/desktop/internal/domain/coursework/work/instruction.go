package work

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// typeNames are the Russian names of the work types, for the instruction text.
var typeNames = map[WorkType]string{
	TypeReferat: "реферат", TypeDoklad: "доклад", TypeEssay: "эссе", TypeAnswers: "ответы на вопросы",
	TypeProblems: "решение задач", TypeLabReport: "отчёт по лабораторной", TypePresentation: "презентация",
	TypeOther: "другое",
}

// fieldNames are the Russian names of the requirement fields, for the instruction text.
var fieldNames = map[Field]string{
	FieldVolume: "Объём", FieldFileFormat: "Формат файла", FieldFormatting: "Оформление", FieldVariant: "Вариант",
	FieldDeadline: "Срок", FieldSources: "Источники",
}

// Instruction is the student's channel of the writing (TASK-094, the instruction of a generation): the confirmed card,
// the answers to the questions and the stated assumptions, as plain lines. It is cut to values.MaxInstructionBytes
// and cleaned with values.CleanInstruction. Only a confirmed (or later) work has one; before it is "".
func (w *Work) Instruction() string {
	if w.status != StatusConfirmed && w.status != StatusWriting && w.status != StatusDone {
		return ""
	}
	u, _ := w.Understanding()
	var b strings.Builder
	fmt.Fprintf(&b, "Тип работы: %s\n", typeNames[u.WorkType])
	for _, f := range Fields()[1:] {
		if v := u.get(f); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", fieldNames[f], v)
		}
	}
	if prev := w.previous(); len(prev) > 0 {
		b.WriteString("Ответы на вопросы:\n")
		for _, p := range prev {
			answer := p.Answer
			if answer == "" {
				answer = "на усмотрение"
			}
			fmt.Fprintf(&b, "- %s — %s\n", p.Question, answer)
		}
	}
	if a, ok := w.Latest(); ok && len(a.Assumptions) > 0 {
		b.WriteString("Допущения:\n")
		for _, s := range a.Assumptions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	text, err := values.CleanInstruction(cutBytes(b.String(), values.MaxInstructionBytes))
	if err != nil {
		return "" // the parts are checked texts; a failure here means nothing safe to pass on
	}
	return text
}

// cutBytes returns the longest prefix of s of at most n bytes that ends on a whole character.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// MaxStepQuestionRunes bounds the student's question about a helper step.
const MaxStepQuestionRunes = 500

// stepKeyPrefix starts the key of a question about a step.
const stepKeyPrefix = "step:v1:"

// StepQuestion is «Спросить по шагу»: the student's question with the narrow context of one helper step and the
// work's requirements only, nothing else of the assignment (TASK-129). Data is the JSON the advisor reads as data.
type StepQuestion struct {
	data string
	key  string
}

// AskStep builds a question about helper step number step (from 0) of a work in the helper mode or a partial work.
// Errors: ErrInvalidTransition when the work has no helper plan, ErrInvalidReply for a step out of range or a
// question that is blank, longer than MaxStepQuestionRunes or holds control characters.
func (w *Work) AskStep(step int, question string) (StepQuestion, error) {
	a, ok := w.Latest()
	if !ok || a.Helper == nil {
		return StepQuestion{}, fmt.Errorf("%w: the work has no helper plan", ErrInvalidTransition)
	}
	question = strings.TrimSpace(question)
	switch {
	case step < 0 || step >= len(a.Helper.Steps):
		return StepQuestion{}, fmt.Errorf("%w: no such step", ErrInvalidReply)
	case !boundedText(question, MaxStepQuestionRunes, true, false):
		return StepQuestion{}, fmt.Errorf("%w: a question has 1..%d characters", ErrInvalidReply, MaxStepQuestionRunes)
	}
	u, _ := w.Understanding()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Strings and a struct of strings only: encoding cannot fail.
	_ = enc.Encode(struct {
		WorkType     WorkType     `json:"work_type"`
		Requirements Requirements `json:"requirements"`
		Step         string       `json:"step"`
		Question     string       `json:"question"`
	}{u.WorkType, u.Requirements, a.Helper.Steps[step], question})
	data := strings.TrimSuffix(b.String(), "\n")
	sum := sha256.Sum256([]byte(string(w.id) + "\x00" + data))
	return StepQuestion{data: data, key: fmt.Sprintf("%s%x", stepKeyPrefix, sum)}, nil
}

// Data is the JSON object the advisor reads as data: the work type, the requirements, the step and the question.
func (q StepQuestion) Data() string { return q.data }

// Key is the idempotency key: the same question about the same step is answered once.
func (q StepQuestion) Key() string { return q.key }

// IsZero reports whether q is the zero value.
func (q StepQuestion) IsZero() bool { return q.data == "" }
