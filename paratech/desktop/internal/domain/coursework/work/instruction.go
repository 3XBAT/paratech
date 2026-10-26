package work

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// typeNames are the Russian names of the work types.
var typeNames = map[WorkType]string{
	TypeReferat: "реферат", TypeDoklad: "доклад", TypeEssay: "эссе", TypeAnswers: "ответы на вопросы",
	TypeProblems: "решение задач", TypeLabReport: "отчёт по лабораторной", TypePresentation: "презентация",
	TypeOther: "работа",
}

// TypeName returns the Russian name of a work type («реферат»); "" for an unknown one.
func TypeName(t WorkType) string { return typeNames[t] }

// Brief is what the writer is told about a confirmed work (TASK-131): the card «Как я понял задание» with the
// student's edits, the questions of the rounds with the student's answers and the reader's assumptions. The questions,
// the assumptions and the card fields the student left were written by the reader model, so the brief is DATA: it goes
// into the untrusted data block of a writing request as one JSON field, never into the instructions (ADR-0022).
type Brief struct {
	WorkType     WorkType     `json:"work_type"`
	Requirements Requirements `json:"requirements"`
	Answers      []Previous   `json:"answers"`     // "" answer: the student skipped the question
	Assumptions  []string     `json:"assumptions"` // stated by the reader, not confirmed one by one
	Partial      bool         `json:"partial"`     // only a part can be done (TASK-107)
}

// Brief returns the brief of a confirmed (or later) work; ok is false before the requirements are confirmed.
func (w *Work) Brief() (b Brief, ok bool) {
	if w.status != StatusConfirmed && w.status != StatusWriting && w.status != StatusDone {
		return Brief{}, false
	}
	u, _ := w.Understanding()
	a, _ := w.Latest()
	return Brief{
		WorkType: u.WorkType, Requirements: u.Requirements, Answers: append([]Previous{}, w.previous()...),
		Assumptions: nonNil(slices.Clone(a.Assumptions)), Partial: w.Partial(),
	}, true
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

// Attempt returns the question asked for the answer number n (1..MaxAdviceAttempts): the same data, and for n > 1 a
// key of its own. The Hub replays the answer it stored for a key, so an answer that fails CheckAdvice would come back
// for the same question forever; the next attempt asks anew. ok is false for another n or a zero q.
func (q StepQuestion) Attempt(n int) (StepQuestion, bool) {
	if q.IsZero() || n < 1 || n > MaxAdviceAttempts {
		return StepQuestion{}, false
	}
	if n > 1 {
		q.key += ":a" + strconv.Itoa(n)
	}
	return q, true
}

// Bounds of an answer about a helper step.
const (
	// MaxAdviceRunes bounds the text of an answer.
	MaxAdviceRunes = 2000
	// MaxAdviceAttempts bounds the answers paid for one question (Attempt).
	MaxAdviceAttempts = 3
)

// CheckAdvice checks an answer about a helper step before it is shown: untrusted text for a card only, trimmed,
// 1..MaxAdviceRunes characters without control characters other than line breaks and tabs. Error: ErrInvalidAnswer.
func CheckAdvice(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !boundedText(s, MaxAdviceRunes, true, false) {
		return "", fmt.Errorf("%w: an answer about a step has 1..%d characters", ErrInvalidAnswer, MaxAdviceRunes)
	}
	return s, nil
}
