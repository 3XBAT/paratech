package schedulesubjects

import (
	"testing"

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

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"физика", "", 6}, {"физика", "физики", 1}, {"программирование", "програмирование", 1},
		{"право", "правда", 2}, {"ёлка", "елка", 1},
	}
	for _, tt := range tests {
		if got := levenshtein([]rune(tt.a), []rune(tt.b)); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
