package values

import (
	"errors"
	"strings"
	"testing"
)

// The question of answer_question and the query of search are queries of up to 500 characters on one line (catalog
// v2); other labels stay at 80.
func TestQueryArguments(t *testing.T) {
	long := strings.Repeat("я", MaxQueryRunes)
	for _, tc := range []struct {
		tool Tool
		arg  string
	}{{ToolAnswerQuestion, "question"}, {ToolSearch, "query"}} {
		c, err := ParseCall("c1", string(tc.tool), `{"`+tc.arg+`":"  `+long+`  "}`)
		if err != nil {
			t.Fatalf("%s of %d characters: %v", tc.tool, MaxQueryRunes, err)
		}
		if q, ok := c.Args().Query(tc.arg); !ok || string(q) != long {
			t.Fatalf("%s: Query = %q, %v", tc.tool, q, ok)
		}
		if _, ok := c.Args().Label(tc.arg); ok {
			t.Fatalf("%s: a query read as a label", tc.tool)
		}
		for name, v := range map[string]string{
			"too long":   long + "я",
			"blank":      "   ",
			"two lines":  `a\nb`,
			"bidi":       "a‮b",
			"not string": "",
		} {
			raw := `{"` + tc.arg + `":"` + v + `"}`
			if name == "not string" {
				raw = `{"` + tc.arg + `":5}`
			}
			if _, err := ParseCall("c1", string(tc.tool), raw); !errors.Is(err, ErrInvalidArguments) {
				t.Errorf("%s %s: %v", tc.tool, name, err)
			}
		}
	}
	// As a label: no HTML angle brackets.
	if _, err := ParseCall("c1", string(ToolAnswerQuestion), `{"question":"<b>Что</b>"}`); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("a question with markup: %v", err)
	}
	if _, err := ParseCall("c1", string(ToolGenerateDraft), `{"assignment":"a1","instruction":"`+strings.Repeat("я", MaxLabelRunes+1)+`"}`); err == nil {
		t.Fatal("a label of 81 characters is accepted")
	}

	// A planned answer_question keeps its question; the call matches it exactly.
	steps, err := parsePlanSteps([]byte(`[{"tool":"answer_question","arguments":{"question":"Что такое ряд?","scope":"all"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := EncodePlanSteps(steps)
	if err != nil || !strings.Contains(enc, `"question":"Что такое ряд?"`) {
		t.Fatalf("EncodePlanSteps = %s, %v", enc, err)
	}
	same, _ := ParseCall("c2", string(ToolAnswerQuestion), `{"question":"Что такое ряд?","scope":"all"}`)
	other, _ := ParseCall("c3", string(ToolAnswerQuestion), `{"question":"Что такое ряды?","scope":"all"}`)
	if !steps[0].Matches(same) || steps[0].Matches(other) {
		t.Fatal("a planned question does not match exactly")
	}
}

func TestAnswerResult(t *testing.T) {
	an := mustRef(t, KindAnswer, "an1")
	res, refs, err := AnswerResult("c1", AnswerRow{Answer: an, Fragments: 5, Claims: 3, Supported: 2, Unsupported: 1, Missing: 1, Sources: 2})
	want := `{"answer":"an1","claims":3,"fragments":5,"missing":1,"sources":2,"supported":2,"unsupported":1}`
	if err != nil || res.ResultJSON != want || len(refs) != 1 || refs[0] != an || res.IsError {
		t.Fatalf("AnswerResult = %s, %v, %v", res.ResultJSON, refs, err)
	}
	if res, refs, err := AnswerResult("c1", AnswerRow{}); err != nil || res.ResultJSON != `{"fragments":0}` || refs != nil {
		t.Fatalf("no fragments = %s, %v, %v", res.ResultJSON, refs, err)
	}
	for name, r := range map[string]AnswerRow{
		"wrong kind":          {Answer: mustRef(t, KindJob, "j1"), Fragments: 1},
		"answer without data": {Answer: an},
		"data without answer": {Fragments: 2},
		"counts disagree":     {Answer: an, Fragments: 1, Claims: 2, Supported: 1},
		"negative":            {Answer: an, Fragments: 1, Missing: -1},
	} {
		if _, _, err := AnswerResult("c1", r); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
