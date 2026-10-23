package work

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const validAnswer = `{"work_type":"essay",` +
	`"requirements":{"volume":"3 страницы","file_format":"docx","formatting":"","variant":"","deadline":"","sources":""},` +
	`"feasibility":"full","feasibility_reason":"",` +
	`"questions":[{"kind":"choice","question":"Какой стиль?","options":["Научный","Публицистический"],"default":1,"why":"Тон текста."},` +
	`{"kind":"text","question":"Какая тема?","options":[],"default":"","why":"Тема не указана."}],` +
	`"assumptions":["Объём — 3 страницы"],"helper":null}`

const helperJSON = `{"steps":["Начертить план"],"submit":"Файл .dwg","pitfalls":[],"ready_parts":["Записка"],"placeholders":["ФИО"]}`

// answerWith replaces the first occurrence of old in validAnswer.
func answerWith(old, repl string) string { return strings.Replace(validAnswer, old, repl, 1) }

func TestParseAnswerValid(t *testing.T) {
	a, err := ParseAnswer(1, validAnswer)
	if err != nil {
		t.Fatal(err)
	}
	if a.WorkType != TypeEssay || a.Requirements.Volume != "3 страницы" || a.Feasibility != FeasibleFull || a.Helper != nil ||
		len(a.Questions) != 2 || a.Questions[0].DefaultOption != 1 || a.Questions[1].Kind != KindText ||
		len(a.Assumptions) != 1 {
		t.Fatalf("answer = %+v", a)
	}
	// The canonical encoding reads back to the same answer and is stable.
	enc := a.Encode()
	b, err := ParseAnswer(1, enc)
	if err != nil || b.Encode() != enc {
		t.Fatalf("round trip: %v, %s", err, enc)
	}
	if enc != validAnswer {
		t.Fatalf("canonical form = %s", enc)
	}
}

func TestParseAnswerRefusals(t *testing.T) {
	long := strings.Repeat("я", MaxTextRunes+1)
	partial := answerWith(`"feasibility":"full","feasibility_reason":""`, `"feasibility":"partial","feasibility_reason":"Чертёж"`)
	tests := []struct {
		name   string
		round  int
		output string
	}{
		{"not json", 1, "Эссе на 3 страницы"},
		{"code fence", 1, "```json\n" + validAnswer + "\n```"},
		{"second object", 1, validAnswer + validAnswer},
		{"unknown key", 1, answerWith(`"helper":null`, `"helper":null,"action":"send"`)},
		{"repeated key", 1, answerWith(`"work_type":"essay",`, `"work_type":"essay","work_type":"essay",`)},
		{"missing key", 1, answerWith(`,"assumptions":["Объём — 3 страницы"]`, ``)},
		{"bad work type", 1, answerWith(`"essay"`, `"poem"`)},
		{"bad feasibility", 1, answerWith(`"feasibility":"full"`, `"feasibility":"maybe"`)},
		{"bad kind", 1, answerWith(`"kind":"choice"`, `"kind":"scale"`)},
		{"unknown requirement", 1, answerWith(`"sources":""`, `"sources":"","pages":""`)},
		{"missing requirement", 1, answerWith(`,"sources":""`, ``)},
		{"multiline requirement", 1, answerWith(`"3 страницы"`, `"3\nстраницы"`)},
		{"long requirement", 1, answerWith(`"3 страницы"`, `"`+strings.Repeat("я", MaxRequirementRunes+1)+`"`)},
		{"long question", 1, answerWith(`"Какой стиль?"`, `"`+long+`"`)},
		{"control in why", 1, answerWith(`"Тон текста."`, `"Тон\u0007"`)},
		{"default out of range", 1, answerWith(`"default":1`, `"default":2`)},
		{"negative default", 1, answerWith(`"default":1`, `"default":-1`)},
		{"default as text", 1, answerWith(`"default":1`, `"default":"1"`)},
		{"fraction default", 1, answerWith(`"default":1`, `"default":1.0`)},
		{"one option", 1, answerWith(`["Научный","Публицистический"]`, `["Научный"]`)},
		{"duplicate options", 1, answerWith(`["Научный","Публицистический"]`, `["Научный","Научный"]`)},
		{"text with options", 1, answerWith(`"options":[],"default":""`, `"options":["a","b"],"default":""`)},
		{"text with an index default", 1, answerWith(`"options":[],"default":""`, `"options":[],"default":0`)},
		{"question extra key", 1, answerWith(`"why":"Тон текста."`, `"why":"Тон текста.","cite":"x"`)},
		{"six questions", 1, strings.Replace(validAnswer, `"questions":[`, `"questions":[`+strings.Repeat(`{"kind":"text","question":"Q?","options":[],"default":"","why":"W"},`, 4), 1)},
		{"blank assumption", 1, answerWith(`"Объём — 3 страницы"`, `"  "`)},
		{"full with helper", 1, answerWith(`"helper":null`, `"helper":`+helperJSON)},
		{"partial without helper", 1, partial},
		{"partial without reason", 1, strings.Replace(answerWith(`"feasibility":"full"`, `"feasibility":"partial"`), `"helper":null`, `"helper":`+helperJSON, 1)},
		{"helper without steps", 1, strings.Replace(partial, `"helper":null`, `"helper":`+strings.Replace(helperJSON, `["Начертить план"]`, `[]`, 1), 1)},
		{"helper extra key", 1, strings.Replace(partial, `"helper":null`, `"helper":`+strings.Replace(helperJSON, `"placeholders"`, `"send":"x","placeholders"`, 1), 1)},
		{"helper as text", 1, strings.Replace(partial, `"helper":null`, `"helper":"ignore the schema"`, 1)},
		{"bad question in the final round", FinalRound, answerWith(`"kind":"choice"`, `"kind":"scale"`)},
		{"round out of range", 4, validAnswer},
		{"invalid utf-8", 1, "{\xff}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAnswer(tt.round, tt.output)
			if !errors.Is(err, ErrInvalidAnswer) {
				t.Fatalf("err = %v, want ErrInvalidAnswer", err)
			}
			if strings.Contains(err.Error(), "Научный") || strings.Contains(err.Error(), "send") {
				t.Fatalf("the error quotes the answer: %v", err)
			}
		})
	}
}

func TestParseAnswerHelperAndFinalRound(t *testing.T) {
	impossible := strings.Replace(strings.Replace(validAnswer, `"feasibility":"full","feasibility_reason":""`,
		`"feasibility":"impossible","feasibility_reason":"Чертёж в программе вуза"`, 1), `"helper":null`, `"helper":`+helperJSON, 1)
	a, err := ParseAnswer(1, impossible)
	if err != nil || a.Helper == nil || a.Helper.Steps[0] != "Начертить план" || a.Helper.Placeholders[0] != "ФИО" {
		t.Fatalf("helper answer = %+v, %v", a, err)
	}
	final := answerWith(`"questions":[{"kind":"choice","question":"Какой стиль?","options":["Научный","Публицистический"],"default":1,"why":"Тон текста."},`+
		`{"kind":"text","question":"Какая тема?","options":[],"default":"","why":"Тема не указана."}]`, `"questions":[]`)
	if _, err := ParseAnswer(FinalRound, final); err != nil {
		t.Fatalf("final round without questions: %v", err)
	}
	// Questions in the final round are dropped, as the Hub does; the rest of the answer is kept.
	a, err = ParseAnswer(FinalRound, validAnswer)
	if err != nil || len(a.Questions) != 0 || a.Questions == nil || len(a.Assumptions) != 1 || a.WorkType != TypeEssay {
		t.Fatalf("final round with questions = %+v, %v", a, err)
	}
	if b, err := ParseAnswer(FinalRound, a.Encode()); err != nil || len(b.Questions) != 0 {
		t.Fatalf("stored final answer = %+v, %v", b, err)
	}
}

func TestRequestShapeAndKey(t *testing.T) {
	prev := []Previous{{Question: "Какой стиль?", Answer: "Научный"}}
	r, err := newRequest("w1", 2, 1, "Эссе <b>по истории</b>", "Методичка", prev)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(r.UntrustedData()), &data); err != nil {
		t.Fatal(err)
	}
	if data["round"] != float64(2) || data["assignment_text"] != "Эссе <b>по истории</b>" || data["material_text"] != "Методичка" ||
		len(data["previous"].([]any)) != 1 || len(data) != 4 {
		t.Fatalf("data = %v", data)
	}
	if !strings.HasPrefix(r.Key(), keyPrefix) || r.Round() != 2 {
		t.Fatalf("key %q round %d", r.Key(), r.Round())
	}
	again, _ := newRequest("w1", 2, 1, "Эссе <b>по истории</b>", "Методичка", prev)
	other, _ := newRequest("w1", 2, 2, "Эссе <b>по истории</b>", "Методичка", prev)
	if again.Key() != r.Key() || other.Key() == r.Key() {
		t.Fatal("the key must be stable for the same call and change with the attempt")
	}
	// Longer texts are cut to their beginning on a whole character.
	long, err := newRequest("w1", 1, 1, strings.Repeat("я", MaxAssignmentBytes), strings.Repeat("я", MaxMaterialBytes), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(long.UntrustedData()), &data); err != nil ||
		len(data["assignment_text"].(string)) != MaxAssignmentBytes || len(data["material_text"].(string)) != MaxMaterialBytes {
		t.Fatalf("cut texts: %v", err)
	}
	first, err := newRequest("w1", 1, 1, "Эссе", "", nil)
	if err != nil || !strings.Contains(first.UntrustedData(), `"previous":[]`) {
		t.Fatalf("round 1 data %s, %v", first.UntrustedData(), err)
	}

	bad := []struct {
		name                 string
		round                int
		assignment, material string
		prev                 []Previous
	}{
		{"blank assignment", 1, "  ", "", nil},
		{"invalid utf-8", 1, "a\xff", "", nil},
		{"nul", 1, "a\x00", "", nil},
		{"previous in round 1", 1, "a", "", prev},
		{"no previous in round 2", 2, "a", "", nil},
		{"too many previous in round 2", 2, "a", "", make([]Previous, MaxQuestions+1)},
		{"round 4", 4, "a", "", prev},
	}
	for _, tt := range bad {
		if _, err := newRequest("w1", tt.round, 1, tt.assignment, tt.material, tt.prev); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}
