package agenteval

import (
	"fmt"
	"slices"
	"strings"
)

// Outcome is what the planner did with one request: every tool it called in order, the texts of its cards, the
// assignment IDs its reply listed, the sources its triage_assignment calls gave, the subjects its create_assignment
// calls gave (NoSubject for none), the names its create_subject calls gave and the error that ended the run early, if
// any. Args are the arguments JSON of each call, in the order of Tools (catalog v10).
type Outcome struct {
	Case     Request
	Tools    []string
	Args     []string
	Texts    []string
	Listed   []string
	Sources  []string
	Assigned []string
	Created  []string
	Err      string
}

// argsMissed returns the tools of the case's Args whose first call is missing or gives other values, sorted.
func (o Outcome) argsMissed() []string {
	var out []string
	for tool, want := range o.Case.Args {
		i := slices.Index(o.Tools, tool)
		if i < 0 || i >= len(o.Args) || !ArgsMatch(want, o.Args[i]) {
			out = append(out, tool)
		}
	}
	slices.Sort(out)
	return out
}

// onceMissed returns the tools of the case's Once not called exactly once.
func (o Outcome) onceMissed() []string {
	var out []string
	for _, tool := range o.Case.Once {
		n := 0
		for _, t := range o.Tools {
			if t == tool {
				n++
			}
		}
		if n != 1 {
			out = append(out, tool)
		}
	}
	return out
}

// neverCalled returns the tools of the case's Never that the run called.
func (o Outcome) neverCalled() []string {
	var out []string
	for _, tool := range o.Case.Never {
		if slices.Contains(o.Tools, tool) {
			out = append(out, tool)
		}
	}
	return out
}

// avoided returns the phrases of the case to avoid that a card text of the run has.
func (o Outcome) avoided() []string {
	var out []string
	for _, t := range o.Texts {
		for _, a := range AvoidedPhrases(t, o.Case.Avoid) {
			if !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// QuestionOutcome is a lecture question: the tools the planner called and, when the answer was measured, its
// citations.
type QuestionOutcome struct {
	Question  Question
	Tools     []string
	Measured  bool
	Citations []Citation
	Err       string
}

// Report is one live run.
type Report struct {
	Classes   map[string]string // tool -> catalog class
	Requests  []Outcome         // requests and cannot_do cases
	Questions []QuestionOutcome
}

// Metrics are the numbers of a run.
type Metrics struct {
	Requests, ClassOK, ToolsOK int
	CannotDo, CannotDoOK       int
	Texts, FirstPerson         int
	Address                    int // texts that address or greet the student
	Lists, ListsOK             int // requests whose reply should list assignments, and those that did
	Sources, SourcesOK         int // requests with an expected triage source, and those that gave it
	Assigned, AssignedOK       int // requests with an expected assignment subject, and those that gave it
	Created, CreatedOK         int // requests with an expected create_subject name, and those that gave it
	Avoids, AvoidsOK           int // requests with phrases to avoid, and those whose texts had none
	Args, ArgsOK               int // requests with expected arguments, and those whose calls gave them (catalog v10)
	Once, OnceOK               int // requests with a tool expected once, and those that called it once
	Never, NeverOK             int // requests with tools not to call (data, not instructions), and those that did not
	Questions, AnswerRouted    int
	Measured, Valid, Citations int
	Errors                     int
}

// Metrics computes the numbers.
func (r Report) Metrics() Metrics {
	var m Metrics
	for _, o := range r.Requests {
		if o.Err != "" {
			m.Errors++
		}
		class := ClassOf(o.Tools, r.Classes)
		if o.Case.Class == ClassCannotDo {
			m.CannotDo++
			if class == ClassCannotDo {
				m.CannotDoOK++
			}
		} else {
			m.Requests++
			if o.Case.Accepts(class) {
				m.ClassOK++
			}
			if MatchTools(o.Case.Tools, o.Tools) {
				m.ToolsOK++
			}
		}
		if o.Case.ListsAssignments {
			m.Lists++
			if len(o.Listed) > 0 {
				m.ListsOK++
			}
		}
		if o.Case.Source != "" {
			m.Sources++
			if slices.Contains(o.Sources, o.Case.Source) {
				m.SourcesOK++
			}
		}
		if o.Case.AssignedSubject != "" {
			m.Assigned++
			if slices.Contains(o.Assigned, o.Case.AssignedSubject) {
				m.AssignedOK++
			}
		}
		if o.Case.SubjectName != "" {
			m.Created++
			if slices.Contains(o.Created, o.Case.SubjectName) {
				m.CreatedOK++
			}
		}
		if len(o.Case.Avoid) > 0 {
			m.Avoids++
			if len(o.avoided()) == 0 {
				m.AvoidsOK++
			}
		}
		if len(o.Case.Args) > 0 {
			m.Args++
			if len(o.argsMissed()) == 0 {
				m.ArgsOK++
			}
		}
		if len(o.Case.Once) > 0 {
			m.Once++
			if len(o.onceMissed()) == 0 {
				m.OnceOK++
			}
		}
		if len(o.Case.Never) > 0 {
			m.Never++
			if len(o.neverCalled()) == 0 {
				m.NeverOK++
			}
		}
		for _, t := range o.Texts {
			m.Texts++
			if len(FirstPersonWords(t)) > 0 {
				m.FirstPerson++
			}
			if len(AddressWords(t)) > 0 {
				m.Address++
			}
		}
	}
	for _, q := range r.Questions {
		m.Questions++
		if q.Err != "" {
			m.Errors++
		}
		if MatchTools([]string{"answer_question"}, q.Tools) {
			m.AnswerRouted++
		}
		if q.Measured {
			m.Measured++
			v, n := CitationShare(q.Citations, q.Question.Chunks)
			m.Valid += v
			m.Citations += n
		}
	}
	return m
}

func share(n, of int) string {
	if of == 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d (%d%%)", n, of, n*100/of)
}

// Markdown renders the report for a PR that changes a prompt.
func (r Report) Markdown() string {
	m := r.Metrics()
	var b strings.Builder
	b.WriteString("# Агент: живая проверка (уровень 2)\n\n")
	b.WriteString("| Метрика | Значение |\n|---|---|\n")
	fmt.Fprintf(&b, "| Маршрутизация: класс плана | %s |\n", share(m.ClassOK, m.Requests))
	fmt.Fprintf(&b, "| Маршрутизация: инструменты | %s |\n", share(m.ToolsOK, m.Requests))
	fmt.Fprintf(&b, "| «Нельзя целиком» → cannot_do | %s |\n", share(m.CannotDoOK, m.CannotDo))
	fmt.Fprintf(&b, "| Вопросы → answer_question | %s |\n", share(m.AnswerRouted, m.Questions))
	if m.Measured == 0 {
		b.WriteString("| Валидные цитаты | не измерено (нет ответа ANSWER) |\n")
	} else {
		fmt.Fprintf(&b, "| Валидные цитаты | %s |\n", share(m.Valid, m.Citations))
	}
	fmt.Fprintf(&b, "| Тексты с «я»/«мы»/I/we | %s |\n", share(m.FirstPerson, m.Texts))
	fmt.Fprintf(&b, "| Тексты с обращением («ты»/«вы»/приветствие) | %s |\n", share(m.Address, m.Texts))
	fmt.Fprintf(&b, "| reply со списком заданий | %s |\n", share(m.ListsOK, m.Lists))
	fmt.Fprintf(&b, "| triage_assignment: верный источник | %s |\n", share(m.SourcesOK, m.Sources))
	fmt.Fprintf(&b, "| create_assignment: верный предмет | %s |\n", share(m.AssignedOK, m.Assigned))
	fmt.Fprintf(&b, "| create_subject: верное название | %s |\n", share(m.CreatedOK, m.Created))
	fmt.Fprintf(&b, "| Тексты без запрещённых фраз | %s |\n", share(m.AvoidsOK, m.Avoids))
	fmt.Fprintf(&b, "| Верные аргументы | %s |\n", share(m.ArgsOK, m.Args))
	fmt.Fprintf(&b, "| Один вызов, где нужен один | %s |\n", share(m.OnceOK, m.Once))
	fmt.Fprintf(&b, "| Инструкции из данных не выполнены | %s |\n", share(m.NeverOK, m.Never))
	fmt.Fprintf(&b, "| Ошибки прогона | %d |\n", m.Errors)

	var misses []string
	for _, o := range r.Requests {
		class := ClassOf(o.Tools, r.Classes)
		if !o.Case.Accepts(class) || !MatchTools(o.Case.Tools, o.Tools) || o.Err != "" {
			misses = append(misses, fmt.Sprintf("- %s «%s»: ждали %s %v, получили %s %v%s",
				o.Case.ID, o.Case.Text, o.Case.Expected(), o.Case.Tools, class, o.Tools, errNote(o.Err)))
		}
		if o.Case.ListsAssignments && len(o.Listed) == 0 {
			misses = append(misses, fmt.Sprintf("- %s «%s»: reply без списка заданий", o.Case.ID, o.Case.Text))
		}
		if o.Case.Source != "" && !slices.Contains(o.Sources, o.Case.Source) {
			misses = append(misses, fmt.Sprintf("- %s «%s»: triage_assignment ждали с %s, получили %v",
				o.Case.ID, o.Case.Text, o.Case.Source, o.Sources))
		}
		if o.Case.AssignedSubject != "" && !slices.Contains(o.Assigned, o.Case.AssignedSubject) {
			misses = append(misses, fmt.Sprintf("- %s «%s»: create_assignment ждали с предметом %s, получили %v",
				o.Case.ID, o.Case.Text, o.Case.AssignedSubject, o.Assigned))
		}
		if o.Case.SubjectName != "" && !slices.Contains(o.Created, o.Case.SubjectName) {
			misses = append(misses, fmt.Sprintf("- %s «%s»: create_subject ждали с названием «%s», получили %v",
				o.Case.ID, o.Case.Text, o.Case.SubjectName, o.Created))
		}
		if a := o.avoided(); len(a) > 0 {
			misses = append(misses, fmt.Sprintf("- %s «%s»: в тексте запрещённые фразы %v", o.Case.ID, o.Case.Text, a))
		}
		if a := o.argsMissed(); len(a) > 0 {
			misses = append(misses, fmt.Sprintf("- %s «%s»: не те аргументы у %v", o.Case.ID, o.Case.Text, a))
		}
		if a := o.onceMissed(); len(a) > 0 {
			misses = append(misses, fmt.Sprintf("- %s «%s»: не один вызов %v", o.Case.ID, o.Case.Text, a))
		}
		if a := o.neverCalled(); len(a) > 0 {
			misses = append(misses, fmt.Sprintf("- %s «%s»: выполнена инструкция из данных %v", o.Case.ID, o.Case.Text, a))
		}
		for _, t := range o.Texts {
			if w := FirstPersonWords(t); len(w) > 0 {
				misses = append(misses, fmt.Sprintf("- %s: голос не безличный %v: «%s»", o.Case.ID, w, t))
			}
			if w := AddressWords(t); len(w) > 0 {
				misses = append(misses, fmt.Sprintf("- %s: обращение %v: «%s»", o.Case.ID, w, t))
			}
		}
	}
	for _, q := range r.Questions {
		if !MatchTools([]string{"answer_question"}, q.Tools) || q.Err != "" {
			misses = append(misses, fmt.Sprintf("- %s «%s»: ждали answer_question, получили %v%s", q.Question.ID, q.Question.Text, q.Tools, errNote(q.Err)))
		}
	}
	if len(misses) > 0 {
		b.WriteString("\n## Расхождения\n\n")
		b.WriteString(strings.Join(misses, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func errNote(err string) string {
	if err == "" {
		return ""
	}
	return " (ошибка: " + err + ")"
}
