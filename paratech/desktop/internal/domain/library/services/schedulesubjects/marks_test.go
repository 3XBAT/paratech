package schedulesubjects

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestEveryMark checks each mark of the table in every place a schedule writes it.
func TestEveryMark(t *testing.T) {
	for _, m := range marks {
		r, size := utf8.DecodeRuneInString(m.text)
		upper := strings.ToUpper(string(r)) + m.text[size:]
		cut := []string{
			"Физика (" + m.text + ")",
			"Физика (" + upper + ".)",
			"Физика [" + m.text + "]",
			"Физика, " + m.text,
			"Физика - " + m.text,
			"Физика – " + upper,
			"Физика-" + m.text,
			"(" + upper + ") Физика",
			upper + ": Физика",
			upper + " - Физика",
		}
		kept := []string{"Физика" + m.text, m.text + "физика"}
		if m.short {
			cut = append(cut, "Физика "+m.text+".", upper+". Физика")
			if utf8.RuneCountInString(m.text) >= 3 {
				cut = append(cut, "Физика "+m.text)
			} else {
				kept = append(kept, "Физика "+m.text)
			}
		} else {
			kept = append(kept, "Физика "+m.text, upper+" Физика")
		}

		for _, s := range cut {
			base, k, ok := cutMarks(s)
			if !ok || base != "Физика" || k != m.kind {
				t.Errorf("cutMarks(%q) = %q, %d, %v; want Физика, %d", s, base, k, ok, m.kind)
			}
		}
		for _, s := range kept {
			if base, _, ok := cutMarks(s); ok || base != s {
				t.Errorf("cutMarks(%q) = %q, %v; want it kept", s, base, ok)
			}
		}
	}
}

func TestCutMarks(t *testing.T) {
	tests := []struct {
		in, base string
		kind     kind
	}{
		{"Математический анализ (лек.)", "Математический анализ", kindLecture},
		{"Матанализ, сем.", "Матанализ", kindSeminar},
		{"Физика - лабораторная", "Физика", kindLab},
		{"Иностранный язык (практика)", "Иностранный язык", kindSeminar},
		{"Информатика лаб.", "Информатика", kindLab},
		{"  Лек.  История  России ", "История России", kindLecture},
		{"Лекция: Философия", "Философия", kindLecture},
		{"[Лаб] Химия", "Химия", kindLab},
		{"Ёлки-палки (сем.)", "Ёлки-палки", kindSeminar},
		{"Физика (лек.) (сем.)", "Физика", kindLecture},
		{"Ин. яз. (пр.)", "Ин. яз.", kindSeminar},
		{"Физика (лабораторная работа)", "Физика", kindLab},

		// Kept: no mark, or a word that only looks like one.
		{"Учебная практика", "Учебная практика", 0},
		{"Производственная практика", "Производственная практика", 0},
		{"Семинар по физике", "Семинар по физике", 0},
		{"Научно-исследовательский семинар", "Научно-исследовательский семинар", 0},
		{"Практика", "Практика", 0},
		{"(лек.)", "(лек.)", 0},
		{"Лекция", "Лекция", 0},
		{"Семантика", "Семантика", 0},
		{"Лекционный курс", "Лекционный курс", 0},
		{"Практикум по физике", "Практикум по физике", 0},
		{"Физика (углублённый курс)", "Физика (углублённый курс)", 0},
		{"Физика пр", "Физика пр", 0},
		{"Сопромат", "Сопромат", 0},
		{"", "", 0},
	}
	for _, tt := range tests {
		base, k, ok := cutMarks(tt.in)
		if base != tt.base || k != tt.kind || ok != (tt.kind != 0) {
			t.Errorf("cutMarks(%q) = %q, %d, %v; want %q, %d", tt.in, base, k, ok, tt.base, tt.kind)
		}
	}
}
