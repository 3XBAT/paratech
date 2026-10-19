package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Prompt quality eval (TASK-093, docs/refs/prompts.md): synthetic lessons in testdata/prompteval are sent through
// prompts.Build as extract_homework_deadlines requests and the answers are scored against the expected assignments.
// TestPromptEvalHub runs it against a real Hub only when both environment variables below are set; it is never run in
// CI. TestPromptEvalFakeHub checks the harness against a fake Hub.

const (
	evalHubURLEnv = "PARATECH_EVAL_HUB_URL"
	evalHubKeyEnv = "PARATECH_EVAL_HUB_KEY"
	evalModelEnv  = "PARATECH_EVAL_MODEL" // optional Hub model alias; default evalDefaultModel
	evalDir       = "testdata/prompteval"

	evalDefaultModel = "default"
)

type evalLesson struct {
	Title     string `json:"title"`
	Subject   string `json:"subject"`
	Kind      string `json:"kind"`
	Teacher   string `json:"teacher"`
	Start     string `json:"start"`
	NextStart string `json:"next_start"`
}

// evalExpected is one assignment the teacher gave. Every keyword must occur in the title or description (case and
// "ё" ignored); "a|b" accepts either. Due is RFC 3339 or null for "no deadline".
type evalExpected struct {
	Keywords []string `json:"keywords"`
	Due      *string  `json:"due"`
}

type evalCase struct {
	Name       string         `json:"name"`
	Lesson     evalLesson     `json:"lesson"`
	Transcript []string       `json:"transcript"`
	Expected   []evalExpected `json:"expected"`

	start time.Time
}

var evalName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// loadEvalCases reads every case strictly: unknown fields, bad times or kinds and cases without keywords are errors.
func loadEvalCases(dir string) ([]evalCase, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var out []evalCase
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		var c evalCase
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if err := c.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (c *evalCase) check() error {
	if !evalName.MatchString(c.Name) {
		return fmt.Errorf("name %q is not [a-z0-9-]", c.Name)
	}
	var err error
	if c.start, err = time.Parse(time.RFC3339, c.Lesson.Start); err != nil {
		return fmt.Errorf("lesson start: %w", err)
	}
	if c.Lesson.NextStart != "" {
		if _, err := time.Parse(time.RFC3339, c.Lesson.NextStart); err != nil {
			return fmt.Errorf("next lesson start: %w", err)
		}
	}
	if !values.LessonKind(c.Lesson.Kind).Valid() {
		return fmt.Errorf("kind %q", c.Lesson.Kind)
	}
	for i, e := range c.Expected {
		if len(e.Keywords) == 0 {
			return fmt.Errorf("expected[%d] has no keywords", i)
		}
		if e.Due != nil {
			if _, err := time.Parse(time.RFC3339, *e.Due); err != nil {
				return fmt.Errorf("expected[%d] due: %w", i, err)
			}
		}
	}
	return nil
}

// request builds the extract request of the case exactly as the app does. The idempotency key is derived from the
// request, so a changed prompt is asked anew while a repeat of the same prompt is replayed by the Hub for free.
func (c evalCase) request(alias string) (values.LLMRequest, error) {
	next, _ := time.Parse(time.RFC3339, c.Lesson.NextStart) // zero when absent; checked by check
	m := contextselection.Material{Lesson: contextselection.Lesson{
		Title: c.Lesson.Title, Subject: c.Lesson.Subject, Teacher: c.Lesson.Teacher, Kind: values.LessonKind(c.Lesson.Kind),
		Start: c.start, NextStart: next,
	}}
	for _, s := range c.Transcript {
		m.Transcript = append(m.Transcript, contextselection.Segment{Text: s})
	}
	sel, err := contextselection.Select(values.OperationExtractHomework, m)
	if err != nil {
		return values.LLMRequest{}, err
	}
	draft, err := prompts.Build(sel, alias, "pending")
	if err != nil {
		return values.LLMRequest{}, err
	}
	sum := sha256.Sum256([]byte(draft.Instructions() + "\x00" + draft.UntrustedData()))
	key := fmt.Sprintf("eval:%s:v%d:%x", c.Name, prompts.Version, sum[:12])
	return values.NewLLMRequest(draft.Operation(), alias, draft.Instructions(), draft.UntrustedData(), key)
}

// evalRow is the score of one case.
type evalRow struct {
	Name                                  string
	Expected, Found, Missed, Extra, DueOK int
	Note                                  string // why the case could not be scored
}

func normalizeEval(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }

func (e evalExpected) matches(h prompts.Homework) bool {
	text := normalizeEval(h.Title() + " " + h.Description())
	for _, k := range e.Keywords {
		if !slices.ContainsFunc(strings.Split(k, "|"), func(alt string) bool {
			return strings.Contains(text, normalizeEval(strings.TrimSpace(alt)))
		}) {
			return false
		}
	}
	return true
}

func (e evalExpected) dueOK(h prompts.Homework) bool {
	if e.Due == nil {
		return h.Due().IsZero()
	}
	want, _ := time.Parse(time.RFC3339, *e.Due)
	return h.Due().Equal(want)
}

// score matches each expected assignment to the first unmatched answer item with all its keywords; items left over
// are extra. The deadline counts only for found assignments.
func score(c evalCase, got []prompts.Homework) evalRow {
	row := evalRow{Name: c.Name, Expected: len(c.Expected)}
	used := make([]bool, len(got))
	for _, e := range c.Expected {
		i := -1
		for j, h := range got {
			if !used[j] && e.matches(h) {
				i = j
				break
			}
		}
		if i < 0 {
			row.Missed++
			continue
		}
		used[i] = true
		row.Found++
		if e.dueOK(got[i]) {
			row.DueOK++
		}
	}
	for _, u := range used {
		if !u {
			row.Extra++
		}
	}
	return row
}

type evalLLM interface {
	Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error)
}

// runEval asks every case once (no format reminder) and scores the checked answer; a failed call or an answer that
// fails the checks counts every expected assignment as missed, with the reason in Note.
func runEval(ctx context.Context, llm evalLLM, alias string, cases []evalCase) []evalRow {
	rows := make([]evalRow, 0, len(cases))
	for _, c := range cases {
		fail := func(note string) {
			rows = append(rows, evalRow{Name: c.Name, Expected: len(c.Expected), Missed: len(c.Expected), Note: note})
		}
		req, err := c.request(alias)
		if err != nil {
			fail("request: " + err.Error())
			continue
		}
		res, err := llm.Complete(ctx, req)
		if err != nil {
			fail("hub: " + err.Error())
			continue
		}
		x, err := prompts.ParseExtraction(res.Output, c.start)
		if err != nil {
			// The error names fields only, never model text.
			fail("answer: " + err.Error())
			continue
		}
		rows = append(rows, score(c, x.Homework()))
	}
	return rows
}

// evalTable renders the rows with a total and the prompts version, so that runs can be compared.
func evalTable(alias string, rows []evalRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "prompts v%d, model alias %q, %d cases\n", prompts.Version, alias, len(rows))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "case\texpected\tfound\tmissed\textra\tdue ok\tnote")
	var total evalRow
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%s\n", r.Name, r.Expected, r.Found, r.Missed, r.Extra, r.DueOK, r.Note)
		total.Expected += r.Expected
		total.Found += r.Found
		total.Missed += r.Missed
		total.Extra += r.Extra
		total.DueOK += r.DueOK
	}
	fmt.Fprintf(w, "TOTAL\t%d\t%d\t%d\t%d\t%d\t\n", total.Expected, total.Found, total.Missed, total.Extra, total.DueOK)
	_ = w.Flush()
	return b.String()
}

// envKey is the API key from the environment as the hubclient credential store: the eval never reads the Windows
// Credential Manager.
type envKey string

func (k envKey) Get(context.Context, string) (string, error) {
	if k == "" {
		return "", fmt.Errorf("eval key: %w", shared.ErrNotFound)
	}
	return string(k), nil
}
