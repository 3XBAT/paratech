package answer

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// fullAnswer is a reader output in the schema; its claim texts are wantTexts.
const fullAnswer = `{"claims":[` +
	`{"text":"Производная — предел \"отношения\".","cites":[{"chunk_id":"f1","quote":"предел отношения"}]},` +
	`{"cites":[{"chunk_id":"f2","quote":"text"}],"text":"Ряд\nТейлора — многочлен 😀."}` +
	`],"missing":["Как считать производную"]}`

var wantTexts = []string{"Производная — предел \"отношения\".", "Ряд\nТейлора — многочлен 😀."}

func TestDraft(t *testing.T) {
	// The draft has no count or length limits, as the answer (TASK-366).
	long := strings.Repeat("я", 450)
	many := `{"claims":[` + strings.Repeat(`{"text":"x","cites":[]},`, 11) + `]}`
	tests := []struct {
		name    string
		partial string
		want    []string
	}{
		{"empty", "", nil},
		{"not JSON", "Ответ: производная", nil},
		{"only the start", `{"cla`, nil},
		{"claims opened", `{"claims":[{"te`, nil},
		{"text opened", `{"claims":[{"text":"`, nil},
		{"text cut", `{"claims":[{"text":"Произв`, []string{"Произв"}},
		{"cut inside an escape", `{"claims":[{"text":"а\`, []string{"а"}},
		{"cut inside a unicode escape", `{"claims":[{"text":"а\u20`, []string{"а"}},
		{"cut inside a surrogate pair", `{"claims":[{"text":"а\ud83d\ude`, []string{"а"}},
		{"cut inside a rune", `{"claims":[{"text":"аб` + "\xd0", []string{"аб"}},
		{"code fence and text before", "```json\n" + `{"claims":[{"text":"a"}`, []string{"a"}},
		{"key order", `{"missing":["m"],"claims":[{"cites":[{"quote":"q","chunk_id":"f1"}],"text":"t"}`, []string{"t"}},
		{"control characters", `{"claims":[{"text":"a\u0000b\rc\td` + "\\u202e\x01" + `e\u0007"}`, []string{"abc\td\u202ee"}},
		{"blank text skipped", `{"claims":[{"text":"  "},{"text":"b"}`, []string{"b"}},
		{"text of another key", `{"missing":["text"],"text":"no","claims":[{"quote":"no","text":"yes"}]}`, []string{"yes"}},
		{"nested text ignored", `{"claims":[{"cites":[{"text":"no"}],"text":"yes"}]}`, []string{"yes"}},
		{"second text of a claim ignored", `{"claims":[{"text":"a","text":"b"}]}`, []string{"a"}},
		{"after the answer", `{"claims":[]}{"claims":[{"text":"no"}]}`, nil},
		{"bad escape dropped", `{"claims":[{"text":"a\qb\uZZZZc\udc00d"}]}`, []string{"abZZZZcd"}},
		{"long text whole", `{"claims":[{"text":"` + long + `"}]}`, []string{long}},
		{"many claims", many, slices.Repeat([]string{"x"}, 11)},
		{"deep nesting", `{"claims":` + strings.Repeat("[", 10_000) + `{"text":"no"}`, nil},
		{"full answer", fullAnswer, wantTexts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Draft(tt.partial); !slices.Equal(got, tt.want) {
				t.Fatalf("Draft = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every prefix of a streamed answer gives the finished texts whole and the last one as a prefix of its final text.
func TestDraftPrefixes(t *testing.T) {
	r := request(t)
	full := `{"claims":[` +
		`{"text":"Производная — предел отношения \"приращений\".","cites":[{"chunk_id":"f1","quote":"предел отношения"}]},` +
		`{"text":"Касательная — прямая.","cites":[{"chunk_id":"f3","quote":"касательная"}]}` +
		`],"missing":["Как считать"]}`
	rep, err := r.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	var final []string
	for _, c := range rep.Claims {
		final = append(final, c.Text)
	}

	prev := 0
	for n := range len(full) + 1 {
		got := Draft(full[:n])
		if len(got) < prev || len(got) > len(final) {
			t.Fatalf("prefix %d: %d texts after %d", n, len(got), prev)
		}
		prev = len(got)
		for i, s := range got {
			if i < len(got)-1 && s != final[i] || !strings.HasPrefix(final[i], s) {
				t.Fatalf("prefix %d: text %d = %q, final %q", n, i, s, final[i])
			}
		}
	}
	if !slices.Equal(Draft(full), final) {
		t.Fatalf("Draft(full) = %q, want %q", Draft(full), final)
	}
}

func FuzzDraft(f *testing.F) {
	for _, s := range []string{"", "{", fullAnswer, fullAnswer[:40], `{"claims":[{"text":"\ud83d`, `{"claims":[{"text":"\u`,
		`{"claims":[[[{"text":"a"}]]]}`, "{\"claims\":[{\"text\":\"\xff\xfe\"}]}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, partial string) {
		got := Draft(partial)
		total := 0
		for _, s := range got {
			total += len(s)
		}
		if total > len(partial) {
			t.Fatalf("%d bytes of texts from %d bytes", total, len(partial))
		}
		for _, s := range got {
			if !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
				t.Fatalf("text %q", s)
			}
			for _, r := range s {
				if unicode.IsControl(r) && r != '\n' && r != '\t' || r == utf8.RuneError {
					t.Fatalf("text %q has %U", s, r)
				}
			}
		}
	})
}
