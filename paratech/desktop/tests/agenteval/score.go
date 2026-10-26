package agenteval

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ClassOf is the class of a run from the names of the tools the planner called, in order, until it stopped; classes
// are the catalog classes (R, U, C, UI). A card decides over actions, an irreversible action over a reversible one.
func ClassOf(tools []string, classes map[string]string) string {
	switch {
	case slices.Contains(tools, "show_plan"):
		return ClassPlan
	case slices.Contains(tools, "ask_questions"):
		return ClassQuestions
	case slices.Contains(tools, "cannot_do"):
		return ClassCannotDo
	}
	has := func(class string) bool {
		return slices.ContainsFunc(tools, func(t string) bool { return classes[t] == class })
	}
	switch {
	case has("C"):
		return ClassConfirm
	case has("U"):
		return ClassWrite
	case has("R"):
		return ClassRead
	}
	return ClassReply
}

// MatchTools reports whether want appears in got in order; other tools may come in between (an extra lookup is not
// a routing error).
func MatchTools(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// firstPerson are first-person words (singular and plural) the impersonal voice does not use (O3, system prompt).
var firstPerson = map[string]bool{
	"я": true, "меня": true, "мне": true, "мной": true, "мною": true,
	"мой": true, "моя": true, "моё": true, "мое": true, "мои": true, "моего": true, "моей": true, "моему": true,
	"моим": true, "моих": true, "моими": true, "моём": true, "моем": true, "мою": true,
	"мы": true, "нас": true, "нам": true, "нами": true,
	"i": true, "me": true, "my": true, "mine": true, "myself": true, "i'm": true, "i've": true, "i'll": true, "i'd": true,
	"we": true, "us": true, "our": true, "ours": true, "we're": true, "we've": true, "we'll": true,
}

// FirstPersonWords returns the first-person words of an agent text, lowercased, in order.
func FirstPersonWords(text string) []string {
	var out []string
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\'' && r != '’'
	})
	for _, w := range words {
		w = strings.Trim(strings.ReplaceAll(w, "’", "'"), "'")
		if firstPerson[w] {
			out = append(out, w)
		}
	}
	return out
}

// address are the words of addressing the student or greeting (system prompt v3, «Voice»): the panel speaks
// impersonally, with no «ты»/«вы», no «Привет».
var address = map[string]bool{
	"ты": true, "тебя": true, "тебе": true, "тобой": true, "твой": true, "твоя": true, "твоё": true, "твое": true,
	"твои": true, "твоего": true, "твоей": true, "твоему": true, "твоим": true, "твоих": true, "твою": true,
	"твоём": true, "твоем": true, "твоими": true,
	"вы": true, "вас": true, "вам": true, "вами": true, "ваш": true, "ваша": true, "ваше": true, "ваши": true,
	"вашего": true, "вашей": true, "вашему": true, "вашим": true, "ваших": true, "вашу": true, "вашем": true,
	"вашими": true,
	"привет": true, "здравствуй": true, "здравствуйте": true,
	"you": true, "your": true, "yours": true, "hello": true, "hi": true,
}

// AddressWords returns the words of an agent text that address or greet the student, lowercased, in order.
func AddressWords(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if address[w] {
			out = append(out, w)
		}
	}
	return out
}

// ReplyAssignments returns the assignment IDs a reply lists (catalog v3); other tools and unreadable arguments give
// none.
func ReplyAssignments(tool, argumentsJSON string) []string {
	if tool != "reply" {
		return nil
	}
	var args struct {
		Assignments []string `json:"assignments"`
	}
	if json.Unmarshal([]byte(argumentsJSON), &args) != nil {
		return nil
	}
	return args.Assignments
}

// triageTool is the tool whose source a case may check.
const triageTool = "triage_assignment"

// TriageSources are the arguments of triage_assignment of which exactly one is given (catalog v4).
func TriageSources() []string { return []string{"assignment", "upload", "topic"} }

// TriageSource returns the one source argument a triage_assignment call gives; other tools, unreadable arguments and
// a call with no or several sources give "".
func TriageSource(tool, argumentsJSON string) string {
	if tool != triageTool {
		return ""
	}
	var args map[string]json.RawMessage
	if json.Unmarshal([]byte(argumentsJSON), &args) != nil {
		return ""
	}
	found := ""
	for _, name := range TriageSources() {
		if v, ok := args[name]; ok && string(v) != "null" {
			if found != "" {
				return ""
			}
			found = name
		}
	}
	return found
}

// CardTexts returns the model texts of a card call that the student reads: reply text, plan summary and notes,
// cannot_do parts, question texts and options. Other tools and unreadable arguments give none.
func CardTexts(tool, argumentsJSON string) []string {
	var args struct {
		Text      string   `json:"text"`
		Summary   string   `json:"summary"`
		Notes     []string `json:"notes"`
		YourPart  []string `json:"your_part"`
		OurPart   []string `json:"our_part"`
		Questions []struct {
			Question string   `json:"question"`
			Text     string   `json:"text"`
			Options  []string `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal([]byte(argumentsJSON), &args) != nil {
		return nil
	}
	var out []string
	switch tool {
	case "reply":
		out = append(out, args.Text)
	case "show_plan":
		out = append(append(out, args.Summary), args.Notes...)
	case "cannot_do":
		out = append(append(out, args.YourPart...), args.OurPart...)
	case "ask_questions":
		for _, q := range args.Questions {
			out = append(append(out, q.Question, q.Text), q.Options...)
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return strings.TrimSpace(s) == "" })
}

// MinQuoteRunes is the shortest quote that counts as a citation.
const MinQuoteRunes = 8

// CitationValid reports whether the quote is at least MinQuoteRunes long and appears in the named chunk, ignoring
// case, «ё»/«е» and runs of spaces.
func CitationValid(c Citation, chunks []Chunk) bool {
	q := normalize(c.Quote)
	if utf8.RuneCountInString(q) < MinQuoteRunes {
		return false
	}
	for _, ch := range chunks {
		if ch.ID == c.Chunk {
			return strings.Contains(normalize(ch.Text), q)
		}
	}
	return false
}

// CitationShare counts the valid citations of an answer.
func CitationShare(cs []Citation, chunks []Chunk) (valid, total int) {
	for _, c := range cs {
		if CitationValid(c, chunks) {
			valid++
		}
	}
	return valid, len(cs)
}

func normalize(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.Join(strings.Fields(s), " ")
}
