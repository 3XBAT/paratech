package schedulesubjects

import (
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/values"
)

func TestRelated(t *testing.T) {
	tests := []struct {
		a, b string
		link Link
		ok   bool
		why  string
	}{
		{"Матан", "Математический анализ", LinkAbbreviation, true, "prefixes of both words"},
		{"Матанализ", "Математический анализ", LinkAbbreviation, true, "prefix and a whole word"},
		{"Мат. анализ", "Математический анализ", LinkAbbreviation, true, "punctuation is no word"},
		{"ТВиМС", "Теория вероятностей и математическая статистика", LinkAbbreviation, true, "initials with «и»"},
		{"ТВМС", "Теория вероятностей и математическая статистика", LinkAbbreviation, true, "initials without «и»"},
		{"ИЯ", "Иностранный язык", LinkAbbreviation, true, "initials of two words"},
		{"Ин. яз.", "Иностранный язык", LinkAbbreviation, true, "prefixes with dots"},
		{"Физкультура", "Физическая культура", LinkAbbreviation, true, "a prefix and a whole word"},
		{"Математический анализ", "Матан", LinkAbbreviation, true, "either direction"},
		{"Программирование", "Програмирование", LinkTypo, true, "one letter missing"},
		{"Физика", "Физики", LinkTypo, true, "one letter, six letters"},
		{"Теория вероятностей", "Теория вероятности", LinkTypo, true, "two edits in a long key"},
		{"Мат анализ", "Мат-анализ", LinkTypo, true, "only punctuation differs"},
		{"Матанализ", "Матонализ", LinkTypo, true, "one substitution in nine letters"},
		{"Математика", "Матемтаика", LinkTypo, true, "two neighbours swapped"},
		{"Электротехника", "Электротехнка", LinkTypo, true, "one letter missing in a long key"},
		{"Ёлки-палки", "Елки-палки", LinkSame, false, "one key: not a doubtful link (related is called on keys)"},

		// Consciously separate.
		{"Физика", "Физкультура", 0, false, "different subjects with a common start"},
		{"Математика", "Математический анализ", 0, false, "«анализ» has no prefix in «математика»"},
		{"Микроэкономика", "Макроэкономика", 0, false, "a typo never touches the first three letters"},
		{"Органическая химия", "Неорганическая химия", 0, false, "a different start is a different word"},
		{"Физика 1", "Физика 2", 0, false, "different numbers"},
		{"История II", "История I", 0, false, "different Roman numbers"},
		{"Физ", "Физика", 0, false, "a short key of one word is no abbreviation, and too short for a typo"},
		{"Химия", "Хиппи", 0, false, "unrelated short words"},
		{"Право", "Правда", 0, false, "two edits in a short key"},
		{"ОБЖ", "Основы бухгалтерии", 0, false, "initials must cover every word and nothing more"},
		{"Матем", "Математика", 0, false, "a truncated word: one word, not a typo"},
		{"Физика", "Философия", 0, false, "unrelated"},
		{"Философия", "Филология", 0, false, "two plain substitutions: two disciplines"},
		{"Этология", "Этнология", 0, false, "a typo never touches the first three letters"},
		{"Электротехника", "Электротахнико", 0, false, "two plain substitutions, even in a long key"},
		{"Химия", "Химик", 0, false, "one edit needs six letters"},
	}
	for _, tt := range tests {
		a, b := values.Key(tt.a), values.Key(tt.b)
		if a == b {
			if tt.ok {
				t.Errorf("%q, %q: one key", tt.a, tt.b)
			}
			continue
		}
		link, ok := related(a, b)
		if ok != tt.ok || link != tt.link {
			t.Errorf("related(%q, %q) = %d, %v; want %d, %v (%s)", tt.a, tt.b, link, ok, tt.link, tt.ok, tt.why)
		}
	}
}

// TestShortFormIsPolynomial feeds crafted schedule strings that made a backtracking matcher try about C(98, 49)
// splits: each must finish well within the bound, by related, by Propose and against an existing subject.
func TestShortFormIsPolynomial(t *testing.T) {
	conj := "А" + strings.Repeat(" и", 98) + " Б" // 199 runes: a valid name
	words := strings.TrimSpace(strings.Repeat("аааа ", 40))
	tests := []struct {
		name, long, short string
		want              bool
	}{
		{"initials never match the last letter", conj, "А" + strings.Repeat("и", 49) + "В", false},
		{"initials skip half of «и»", conj, "А" + strings.Repeat("и", 49) + "Б", true},
		{"prefixes never match the last letter", words, strings.Repeat("а", 99) + "б", false},
		{"prefixes of two to four letters", words, strings.Repeat("а", 99), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				long, short := values.Key(tt.long), values.Key(tt.short)
				if got := isShortForm(short, long); got != tt.want {
					t.Errorf("isShortForm = %v, want %v", got, tt.want)
				}
				for _, pair := range [][2]string{{short, long}, {long, short}} {
					if _, ok := related(pair[0], pair[1]); ok != tt.want {
						t.Errorf("related = %v, want %v", ok, tt.want)
					}
				}
				rows := []Row{{Name: tt.long, Lessons: lec(2)}, {Name: tt.short, Lessons: lec(1)}}
				if p := Propose(rows, nil); (len(p.Groups) == 1) != tt.want {
					t.Errorf("Propose made %d groups", len(p.Groups))
				}
				Propose(rows[1:], []subjectmatch.Entry{{ID: "s1", Name: tt.long}})
				Propose(rows[:1], []subjectmatch.Entry{{ID: "s1", Name: tt.short}})
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("short form matching takes exponential time")
			}
		})
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b  string
		want  int
		plain bool
	}{
		{"", "", 0, true}, {"физика", "", 6, false}, {"физика", "физики", 1, true},
		{"программирование", "програмирование", 1, false}, {"право", "правда", 2, false}, {"ёлка", "елка", 1, true},
		{"философия", "филология", 2, true}, {"математика", "матемтаика", 1, false}, {"абвг", "абгв", 1, false},
		{"абвг", "бавг", 1, false}, {"абвг", "аббб", 2, true},
	}
	for _, tt := range tests {
		got, plain := editDistance([]rune(tt.a), []rune(tt.b))
		if got != tt.want || plain != tt.plain {
			t.Errorf("editDistance(%q, %q) = %d, %v; want %d, %v", tt.a, tt.b, got, plain, tt.want, tt.plain)
		}
	}
}
