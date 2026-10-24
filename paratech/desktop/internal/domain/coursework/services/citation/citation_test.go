package citation

import "testing"

const lesson = "Производная функции — это предел отношения приращения функции к приращению аргумента.\n" +
	"Её обозначают f'(x). Ёмкость конденсатора   измеряется в фарадах."

func TestMatches(t *testing.T) {
	tests := []struct {
		name  string
		quote string
		want  bool
	}{
		{"exact", "предел отношения приращения функции", true},
		{"case", "ПРОИЗВОДНАЯ ФУНКЦИИ", true},
		{"ё as е", "емкость конденсатора", true},
		{"е in the quote, ё in the text", "Ее обозначают f'(x)", true},
		{"spaces collapsed", "конденсатора измеряется", true},
		{"line break as a space", "аргумента. Её обозначают", true},
		{"spaces and tabs in the quote", "предел\t отношения\n приращения", true},
		{"dash variants", "Производная функции - это предел", true},
		{"ellipsis and quotation marks at the ends", "«…предел отношения приращения…»", true},
		{"final period the text lacks there", "предел отношения.", true},
		{"zero-width character in the quote", "пре​дел отношения", true},

		{"paraphrase", "производная есть предел отношения", false},
		{"another word inside", "предел большого отношения", false},
		{"not in the text", "интеграл Римана по отрезку", false},
		{"too short", "предел", false},
		{"only punctuation", "…………………", false},
		{"empty", "", false},
		{"invented continuation", "измеряется в фарадах и генри", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(tt.quote, lesson); got != tt.want {
				t.Fatalf("Matches(%q) = %v, want %v", tt.quote, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  Ёлка \n\t ЁЖ  ", "елка еж"},
		{"a​‮b", "ab"},
		{"x — y – z", "x - y - z"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := Quote(" «Цитата из лекции»... "); got != "цитата из лекции" {
		t.Errorf("Quote = %q", got)
	}
}
