package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// Summary eval (TASK-101): the summary of each case must have a section per expected section, in the order of the
// lesson. An expected section matches an answer section whose heading, text and points hold all its keywords (as
// evalExpected: case and "ё" ignored, "a|b" accepts either).

// evalSection is one section the summary of a case must have.
type evalSection struct {
	Keywords []string `json:"keywords"`
}

func (e evalSection) matches(s prompts.SummarySection) bool {
	text := normalizeEval(strings.Join(append([]string{s.Heading(), s.Text()}, s.Points()...), " "))
	for _, k := range e.Keywords {
		if !slices.ContainsFunc(strings.Split(k, "|"), func(alt string) bool {
			return strings.Contains(text, normalizeEval(strings.TrimSpace(alt)))
		}) {
			return false
		}
	}
	return true
}

// summaryRow is the score of the summary of one case: Ordered when the found sections come in the expected order.
type summaryRow struct {
	Name                            string
	Expected, Found, Missed, Answer int // Answer: sections in the answer
	KeyPoints                       int
	Ordered                         bool
	Note                            string
}

// scoreSummary matches each expected section to the first unmatched answer section with all its keywords.
func scoreSummary(c evalCase, s prompts.Summary) summaryRow {
	secs := s.Sections()
	row := summaryRow{Name: c.Name, Expected: len(c.Sections), Answer: len(secs), KeyPoints: len(s.KeyPoints()), Ordered: true}
	used := make([]bool, len(secs))
	last := -1
	for _, e := range c.Sections {
		i := -1
		for j, sec := range secs {
			if !used[j] && e.matches(sec) {
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
		if i < last {
			row.Ordered = false
		}
		last = i
	}
	return row
}

// runSummaryEval asks the summary of every case once and scores the checked answer; a failed call or an answer that
// fails the checks counts every expected section as missed, with the reason in Note.
func runSummaryEval(ctx context.Context, llm evalLLM, alias string, cases []evalCase) []summaryRow {
	rows := make([]summaryRow, 0, len(cases))
	for _, c := range cases {
		fail := func(note string) {
			rows = append(rows, summaryRow{Name: c.Name, Expected: len(c.Sections), Missed: len(c.Sections), Note: note})
		}
		req, err := c.requestFor(values.OperationSummary, alias)
		if err != nil {
			fail("request: " + err.Error())
			continue
		}
		res, err := llm.Complete(ctx, req)
		if err != nil {
			fail("hub: " + err.Error())
			continue
		}
		s, err := prompts.ParseSummary(res.Output)
		if err != nil {
			// The error names fields only, never model text.
			fail("answer: " + err.Error())
			continue
		}
		rows = append(rows, scoreSummary(c, s))
	}
	return rows
}

// summaryTable renders the summary rows with a total and the prompts version.
func summaryTable(alias string, rows []summaryRow) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "summary, prompts v%d, model alias %q, %d cases\n", prompts.Version, alias, len(rows))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	var werr error
	line := func(format string, a ...any) {
		if werr == nil {
			_, werr = fmt.Fprintf(w, format, a...)
		}
	}
	line("case\texpected\tfound\tmissed\tsections\tkey points\tin order\tnote\n")
	var total summaryRow
	for _, r := range rows {
		line("%s\t%d\t%d\t%d\t%d\t%d\t%v\t%s\n", r.Name, r.Expected, r.Found, r.Missed, r.Answer, r.KeyPoints, r.Ordered, r.Note)
		total.Expected += r.Expected
		total.Found += r.Found
		total.Missed += r.Missed
		total.Answer += r.Answer
		total.KeyPoints += r.KeyPoints
	}
	line("TOTAL\t%d\t%d\t%d\t%d\t%d\t\t\n", total.Expected, total.Found, total.Missed, total.Answer, total.KeyPoints)
	if err := w.Flush(); werr == nil {
		werr = err
	}
	if werr != nil {
		return "", fmt.Errorf("summary eval table: %w", werr)
	}
	return b.String(), nil
}

type evalSummaryWireSection struct {
	Heading string   `json:"heading"`
	Text    string   `json:"text"`
	Points  []string `json:"points"`
}

func evalSummaryAnswer(sections ...evalSummaryWireSection) string {
	if sections == nil {
		sections = []evalSummaryWireSection{}
	}
	b, _ := json.Marshal(map[string]any{"sections": sections, "key_points": []string{"Главное"}})
	return string(b)
}

// perfectSummary writes one section per expected section, named by the first alternative of its keywords.
func perfectSummary(c evalCase) string {
	var secs []evalSummaryWireSection
	for i, e := range c.Sections {
		var words []string
		for _, k := range e.Keywords {
			words = append(words, strings.Split(k, "|")[0])
		}
		secs = append(secs, evalSummaryWireSection{Heading: fmt.Sprintf("Раздел %d", i+1), Text: strings.Join(words, " "), Points: []string{}})
	}
	return evalSummaryAnswer(secs...)
}

// Every case builds a summary request with the lesson context, deterministically.
func TestPromptEvalSummaryCases(t *testing.T) {
	cases, err := loadEvalCases(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		req, err := c.requestFor(values.OperationSummary, evalDefaultModel)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if req.Operation() != values.OperationSummary || !strings.Contains(req.UntrustedData(), `"lesson_kind"`) ||
			!strings.HasPrefix(req.IdempotencyKey(), fmt.Sprintf("eval-summary:%s:v%d:", c.Name, prompts.Version)) {
			t.Errorf("%s: request %s %q", c.Name, req.Operation(), req.IdempotencyKey())
		}
		if again, _ := c.requestFor(values.OperationSummary, evalDefaultModel); again != req {
			t.Errorf("%s: the request is not deterministic", c.Name)
		}
		// The expected sections are checkable: the perfect answer passes the strict parser and scores fully.
		s, err := prompts.ParseSummary(values.NewUntrustedOutput(perfectSummary(c)))
		if err != nil {
			t.Fatalf("%s: perfect answer: %v", c.Name, err)
		}
		if r := scoreSummary(c, s); r.Found != len(c.Sections) || !r.Ordered {
			t.Errorf("%s: perfect answer scores %+v", c.Name, r)
		}
	}
}

func TestPromptEvalSummaryFakeHub(t *testing.T) {
	cases, err := loadEvalCases(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	f := &evalFakeHub{answers: map[string]string{}, summaries: map[string]string{}}
	byName := map[string]evalCase{}
	for _, c := range cases {
		byName[c.Name] = c
		f.summaries[c.Lesson.Title] = perfectSummary(c)
	}
	// Imperfect answers: sections in the wrong order, a missed section, an answer of the earlier prompts.
	f.summaries[byName["lecture-dates"].Lesson.Title] = evalSummaryAnswer(
		evalSummaryWireSection{Heading: "Армия", Text: "Вторая реформа: армия.", Points: []string{}},
		evalSummaryWireSection{Heading: "Налоги", Text: "Первая реформа: налоги.", Points: []string{}})
	f.summaries[byName["no-homework"].Lesson.Title] = evalSummaryAnswer(
		evalSummaryWireSection{Heading: "Другое", Text: "Ничего по теме.", Points: []string{}})
	f.summaries[byName["injection"].Lesson.Title] = `{"summary": "S", "key_points": []}`

	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := hubclient.New(hubclient.Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 5 * time.Second},
		envKey("la_eval_0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}

	rows := runSummaryEval(t.Context(), client, evalDefaultModel, cases)
	if len(rows) != len(cases) {
		t.Fatalf("%d rows for %d cases", len(rows), len(cases))
	}
	for _, r := range rows {
		c := byName[r.Name]
		n := len(c.Sections)
		want := summaryRow{Name: r.Name, Expected: n, Found: n, Answer: n, KeyPoints: 1, Ordered: true}
		switch r.Name {
		case "lecture-dates":
			want = summaryRow{Name: r.Name, Expected: n, Found: 2, Missed: n - 2, Answer: 2, KeyPoints: 1, Ordered: false}
		case "no-homework":
			want = summaryRow{Name: r.Name, Expected: n, Missed: n, Answer: 1, KeyPoints: 1, Ordered: true}
		case "injection":
			want = summaryRow{Name: r.Name, Expected: n, Missed: n}
		}
		note := r.Note
		r.Note = ""
		if r != want {
			t.Errorf("%s: row = %+v, want %+v", r.Name, r, want)
		}
		if (r.Name == "injection") != strings.HasPrefix(note, "answer: ") {
			t.Errorf("%s: note %q", r.Name, note)
		}
	}

	table, err := summaryTable(evalDefaultModel, rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprintf("prompts v%d", prompts.Version), "sections", "in order", "TOTAL", "lecture-dates"} {
		if !strings.Contains(table, s) {
			t.Errorf("table misses %q:\n%s", s, table)
		}
	}
	t.Log("\n" + table)
}
