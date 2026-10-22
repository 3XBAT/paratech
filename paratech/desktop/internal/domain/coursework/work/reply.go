package work

import (
	"fmt"
	"strings"
)

// Reply is the student's answer to one question of a round: an option of a choice or the typed text of a text
// question ("" means skipped). Default marks a question left unanswered, which takes the question's default.
type Reply struct {
	Question int
	Option   int
	Text     string
	Default  bool
}

// Defaults are the replies of «Пропустить — сделай как считаешь»: the default of every question.
func Defaults(questions []Question) []Reply {
	out := make([]Reply, 0, len(questions))
	for i, q := range questions {
		r := Reply{Question: i, Default: true}
		if q.Kind == KindChoice {
			r.Option = q.DefaultOption
		} else {
			r.Text = q.DefaultText
		}
		out = append(out, r)
	}
	return out
}

// Resolve checks the student's replies to questions and fills every question not answered with its default. A reply
// names a question by index at most once; a choice takes an option of the question, a text question a typed text of
// at most MaxAnswerRunes (trimmed; "" is a skipped question). Error: ErrInvalidReply.
func Resolve(questions []Question, replies []Reply) ([]Reply, error) {
	out := Defaults(questions)
	given := map[int]bool{}
	for _, r := range replies {
		if r.Question < 0 || r.Question >= len(questions) || given[r.Question] {
			return nil, fmt.Errorf("%w: question index out of range or repeated", ErrInvalidReply)
		}
		given[r.Question] = true
		q := questions[r.Question]
		switch q.Kind {
		case KindChoice:
			if r.Text != "" || r.Option < 0 || r.Option >= len(q.Options) {
				return nil, fmt.Errorf("%w: a choice takes one of its options", ErrInvalidReply)
			}
			out[r.Question] = Reply{Question: r.Question, Option: r.Option}
		default:
			text := strings.TrimSpace(r.Text)
			if r.Option != 0 || !boundedText(text, MaxAnswerRunes, true, true) {
				return nil, fmt.Errorf("%w: a text answer has at most %d characters, no control characters", ErrInvalidReply, MaxAnswerRunes)
			}
			out[r.Question] = Reply{Question: r.Question, Text: text}
		}
	}
	return out, nil
}

// answerText is what the student answered to q, as the reader reads it in the next round.
func answerText(q Question, r Reply) string {
	if q.Kind == KindChoice {
		return q.Options[r.Option]
	}
	return r.Text
}

// checkReplies reports whether replies are a full, resolved answer to questions (the stored form).
func checkReplies(questions []Question, replies []Reply) bool {
	if len(replies) != len(questions) {
		return false
	}
	for i, r := range replies {
		q := questions[i]
		switch {
		case r.Question != i:
			return false
		case q.Kind == KindChoice && (r.Text != "" || r.Option < 0 || r.Option >= len(q.Options)):
			return false
		case q.Kind == KindText && (r.Option != 0 || !boundedText(r.Text, MaxAnswerRunes, true, true)):
			return false
		}
	}
	return true
}
