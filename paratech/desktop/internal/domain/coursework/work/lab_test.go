package work

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// labOutlineJSON is the standard structure of a lab report, 1100 planned words.
const labOutlineJSON = `{"topic":"Изучение математического маятника","sections":[` +
	`{"title":"Цель работы","words":50,"part":"goal"},` +
	`{"title":"Оборудование","words":50,"part":"equipment"},` +
	`{"title":"Теоретическая часть","words":400,"part":"theory"},` +
	`{"title":"Ход работы","words":250,"part":"procedure"},` +
	`{"title":"Результаты","words":200,"part":"results"},` +
	`{"title":"Выводы","words":150,"part":"conclusions"}]}`

// labSectionJSON is a valid answer for a section of part: a results section has its placeholders.
func labSectionJSON(part LabPart) string {
	switch part {
	case LabResults:
		return `{"paragraphs":["Результаты измерений сведены в таблицу.","▢ Вставьте таблицу измерений периода",` +
			`"▢Вставьте график зависимости T от l"],"sources":[]}`
	case LabConclusions:
		return `{"paragraphs":["Если измеренный период совпадает с расчётным, формула подтверждается."],"sources":[]}`
	}
	return `{"paragraphs":["Текст раздела."],"sources":[{"material":"m1","text":""}]}`
}

// confirmedLab is a confirmed lab report.
func confirmedLab(t *testing.T) *Work {
	t.Helper()
	w := newWork(t)
	a := answerOf(FeasibleFull, 0)
	a.WorkType = TypeLabReport
	mustRecord(t, w, a)
	if err := w.ConfirmRequirements(t0); err != nil {
		t.Fatal(err)
	}
	return w
}

// writtenLab starts, outlines with manual m, approves and writes every section of a confirmed lab report.
func writtenLab(t *testing.T, w *Work, m Manual) {
	t.Helper()
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	o, err := ParseLabOutline(labOutlineJSON)
	if err != nil {
		t.Fatal(err)
	}
	o.Manual = m
	if err := w.SetOutline(o, t0); err != nil {
		t.Fatal(err)
	}
	if err := w.ApproveOutline(t0); err != nil {
		t.Fatal(err)
	}
	for w.Next().Kind == StepSection {
		st := w.Next()
		sec, _ := w.Section(st.Section)
		text, err := ParseLabSection(labSectionJSON(sec.Part), mats, sec.Part)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteSection(st.Section, st.Revision, text, t0); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLabReportFlow(t *testing.T) {
	w := confirmedLab(t)
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	if !w.LabReport() || w.Presentation() || w.Manual() != "" {
		t.Fatalf("start: lab %v, manual %q", w.LabReport(), w.Manual())
	}
	if o, _ := ParseOutline(outlineJSON); w.SetOutline(o, t0) == nil {
		t.Fatal("a paper outline without parts taken for a lab report")
	}

	// The outline call tells the writer about the manual; the brief and the manual are data.
	data, _, err := w.StepData(w.Next(), Texts{Assignment: "Лабораторная работа 3", Manual: ManualNotRead})
	if err != nil || !strings.Contains(data, `"manual":"not_read"`) || !strings.Contains(data, `"work_type":"lab_report"`) {
		t.Fatalf("outline data = %s, %v", data, err)
	}
	o, err := ParseLabOutline(labOutlineJSON)
	if err != nil {
		t.Fatal(err)
	}
	o.Manual = ManualNotRead
	if err := w.SetOutline(o, t0); err != nil || w.Manual() != ManualNotRead || !w.Manual().Unread() {
		t.Fatalf("outline: %v, manual %q", err, w.Manual())
	}
	if err := w.ApproveOutline(t0); err != nil {
		t.Fatal(err)
	}

	// The section data names its part and keeps the manual of the outline.
	for w.Next().Kind == StepSection {
		st := w.Next()
		sec, _ := w.Section(st.Section)
		data, _, err := w.StepData(st, Texts{Assignment: "Лабораторная работа 3", Manual: ManualRead})
		if err != nil || !strings.Contains(data, `"part":"`+string(sec.Part)+`"`) || !strings.Contains(data, `"manual":"not_read"`) {
			t.Fatalf("section data = %s, %v", data, err)
		}
		if sec.Part == LabResults {
			// A results text without a place for the data is refused when stored too.
			plain, _ := ParseSection(labSectionJSON(LabGoal), mats)
			if err := w.WriteSection(st.Section, st.Revision, plain, t0); !errors.Is(err, ErrInvalidAnswer) {
				t.Fatalf("results without a placeholder: %v", err)
			}
		}
		text, err := ParseLabSection(labSectionJSON(sec.Part), mats, sec.Part)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteSection(st.Section, st.Revision, text, t0); err != nil {
			t.Fatal(err)
		}
	}

	got := w.Placeholders()
	want := []Placeholder{
		{Section: 5, Title: "Результаты", Prompt: "Вставьте таблицу измерений периода"},
		{Section: 5, Title: "Результаты", Prompt: "Вставьте график зависимости T от l"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("placeholders = %+v", got)
	}
	if w.Next().Kind != StepAssemble {
		t.Fatalf("next = %+v", w.Next())
	}
	if err := w.Finish(fileOf(w), t0); err != nil || w.Status() != StatusDone {
		t.Fatalf("finish: %v", err)
	}
}

func TestParseLabSection(t *testing.T) {
	rlo := string(rune(0x202E))
	for _, tt := range []struct {
		name, raw string
		part      LabPart
		ok        bool
		first     string // the first paragraph once clean
	}{
		{"results with placeholders", labSectionJSON(LabResults), LabResults, true, "Результаты измерений сведены в таблицу."},
		{"no space after the mark", `{"paragraphs":["▢Вставьте таблицу"],"sources":[]}`, LabResults, true, "▢ Вставьте таблицу"},
		{"bidi dropped", `{"paragraphs":["▢ Вставьте ` + rlo + `таблицу"],"sources":[]}`, LabResults, true, "▢ Вставьте таблицу"},
		{"results without a placeholder", `{"paragraphs":["Период равен 2,01 с."],"sources":[]}`, LabResults, false, ""},
		{"theory without a placeholder", `{"paragraphs":["Теория."],"sources":[]}`, LabTheory, true, "Теория."},
		{"mark inside a paragraph", `{"paragraphs":["Период ▢ Вставьте"],"sources":[]}`, LabResults, false, ""},
		{"mark without a prompt", `{"paragraphs":["▢ Здесь данные"],"sources":[]}`, LabResults, false, ""},
		{"empty prompt", `{"paragraphs":["▢ Вставьте "],"sources":[]}`, LabResults, false, ""},
		{"two marks", `{"paragraphs":["▢ Вставьте ▢ таблицу"],"sources":[]}`, LabResults, false, ""},
		{"long placeholder", `{"paragraphs":["▢ Вставьте ` + strings.Repeat("я", MaxPlaceholderRunes) + `"],"sources":[]}`, LabResults, false, ""},
		{"too many placeholders", `{"paragraphs":[` + strings.Repeat(`"▢ Вставьте x",`, MaxSectionPlaceholders) + `"▢ Вставьте y"],"sources":[]}`,
			LabResults, false, ""},
		{"extra key", `{"paragraphs":["▢ Вставьте таблицу"],"sources":[],"email":"x@y.ru"}`, LabResults, false, ""},
		{"line break refused", `{"paragraphs":["▢ Вставьте\nтаблицу"],"sources":[]}`, LabResults, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLabSection(tt.raw, mats, tt.part)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalidAnswer) {
					t.Fatalf("err = %v, want ErrInvalidAnswer", err)
				}
				return
			}
			if got.Paragraphs[0] != tt.first {
				t.Fatalf("first = %q", got.Paragraphs[0])
			}
		})
	}
}

func TestParseLabOutline(t *testing.T) {
	ok := func(s string) string { return strings.Replace(labOutlineJSON, `"words":50,"part":"goal"`, s, 1) }
	for _, tt := range []struct {
		name, raw string
		ok        bool
	}{
		{"standard", labOutlineJSON, true},
		{"manual's own part", ok(`"words":50,"part":"other"`), true},
		{"unknown part", ok(`"words":50,"part":"email"`), false},
		{"no part", ok(`"words":50`), false},
		{"no results", strings.Replace(labOutlineJSON, `"part":"results"`, `"part":"other"`, 1), false},
		{"no conclusions", strings.Replace(labOutlineJSON, `"part":"conclusions"`, `"part":"other"`, 1), false},
		{"too short", ok(`"words":10,"part":"goal"`), false},
		{"extra key", ok(`"words":50,"part":"goal","kind":"title"`), false},
		{"repeated key", ok(`"words":50,"part":"goal","part":"results"`), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, err := ParseLabOutline(tt.raw)
			if tt.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidAnswer)) {
				t.Fatalf("err = %v", err)
			}
			if err == nil && (len(o.Items) != 6 || o.Items[4].Part != LabResults || o.Manual != "") {
				t.Fatalf("outline = %+v", o)
			}
		})
	}
}

// TestLabEditKeepsParts: the student's edit keeps the parts of known sections, a new one is «other»; removing the
// results is refused.
func TestLabEditKeepsParts(t *testing.T) {
	w := confirmedLab(t)
	writtenLab(t, w, ManualRead)
	wr, _ := w.Writing()
	items := make([]OutlineItem, 0, len(wr.Sections)+1)
	for _, s := range wr.Sections {
		items = append(items, OutlineItem{Number: s.Number, Title: s.Title, Words: s.Words})
	}
	if err := w.EditOutline(items, t0); err != nil || w.Next().Kind != StepAssemble {
		t.Fatalf("the outline as it is: %v %+v", err, w.Next())
	}
	edited := append(items[:5:5], OutlineItem{Title: "Контрольные вопросы", Words: 100}, items[5])
	if err := w.EditOutline(edited, t0); err != nil {
		t.Fatal(err)
	}
	wr, _ = w.Writing()
	if wr.Sections[5].Part != LabOther || wr.Sections[4].Part != LabResults || !wr.Sections[4].Written() {
		t.Fatalf("sections = %+v", wr.Sections)
	}
	noResults := append(items[:4:4], items[5])
	if err := w.EditOutline(noResults, t0); !errors.Is(err, ErrInvalidReply) {
		t.Fatalf("an edit without the results: %v", err)
	}
}

// TestRestoreLab: a lab report reads back through the same checks; broken stored states are refused.
func TestRestoreLab(t *testing.T) {
	w := confirmedLab(t)
	writtenLab(t, w, ManualNoText)
	if err := w.Finish(fileOf(w), t0); err != nil {
		t.Fatal(err)
	}
	wr, _ := w.Writing()
	back, err := ParseWriting(wr.Encode())
	if err != nil {
		t.Fatal(err)
	}
	st := w.State()
	st.Version = 3
	st.Writing = &back
	got, err := Restore(st)
	if err != nil || got.ContentKey() != w.ContentKey() || !got.LabReport() || got.Manual() != ManualNoText ||
		len(got.Placeholders()) != 2 || got.Status() != StatusDone {
		t.Fatalf("restore: %v", err)
	}
	if !strings.Contains(wr.Encode(), `"lab":{"manual":"no_text"}`) || !strings.Contains(wr.Encode(), `"part":"results"`) {
		t.Fatalf("stored = %s", wr.Encode())
	}

	results := 4 // the index of the results section
	for _, tt := range []struct {
		name string
		edit func(*State)
	}{
		{"results without a placeholder", func(s *State) {
			s.Writing.Sections[results].Text.Paragraphs = []string{"Период равен 2,01 с."}
		}},
		{"bad placeholder", func(s *State) { s.Writing.Sections[results].Text.Paragraphs[1] = "▢ Здесь" }},
		{"unclean placeholder", func(s *State) {
			s.Writing.Sections[results].Text.Paragraphs[1] = "▢ Вставьте " + string(rune(0x202E)) + "таблицу"
		}},
		{"unknown part", func(s *State) { s.Writing.Sections[0].Part = "email" }},
		{"no part", func(s *State) { s.Writing.Sections[0].Part = "" }},
		{"unknown manual", func(s *State) { s.Writing.Lab.Manual = "maybe" }},
		{"no manual with an outline", func(s *State) { s.Writing.Lab.Manual = "" }},
		{"part of a paper", func(s *State) { s.Writing.Lab = nil }},
		{"lab of another type", func(s *State) {
			s.Rounds = slices.Clone(s.Rounds)
			s.Rounds[0].Answer.WorkType = TypeReferat
		}},
		{"other manual than the file", func(s *State) { s.Writing.Lab.Manual = ManualRead }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := w.State()
			st.Version = 3
			wr := st.Writing.clone()
			st.Writing = &wr
			tt.edit(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// TestLegacyLabIsAPaper: a lab report stored before TASK-107 (written as a paper, no lab part) still reads back and
// keeps its content key; a paper stores no lab field.
func TestLegacyLabIsAPaper(t *testing.T) {
	w := confirmedLab(t)
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	st := w.State()
	st.Version = 2
	wr := st.Writing.clone()
	wr.Lab = nil
	st.Writing = &wr
	legacy, err := Restore(st)
	if err != nil || legacy.LabReport() {
		t.Fatalf("legacy: %v", err)
	}
	mustOutline(t, legacy)
	if legacy.Placeholders() != nil || legacy.Manual() != "" {
		t.Fatal("a paper has placeholders")
	}

	paper := confirmedWork(t)
	if err := paper.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, paper)
	mustWriteDue(t, paper)
	pw, _ := paper.Writing()
	data, _, err := paper.StepData(paper.Next(), Texts{Assignment: "Реферат", Manual: ManualNotRead})
	if strings.Contains(pw.Encode(), "lab") || strings.Contains(pw.Encode(), "part") || err != nil ||
		strings.Contains(data, `"manual"`) || strings.Contains(data, `"part"`) {
		t.Fatalf("a paper has lab fields: %s / %s", pw.Encode(), data)
	}
}

func TestPromptOf(t *testing.T) {
	for _, tt := range []struct {
		in, want string
		ok       bool
	}{
		{"▢ Вставьте таблицу", "Вставьте таблицу", true},
		{"Вставьте таблицу", "", false},
		{"▢ Таблица", "", false},
		{"▢Вставьте таблицу", "", false}, // stored texts are in the one form
	} {
		if got, ok := PromptOf(tt.in); got != tt.want || ok != tt.ok {
			t.Errorf("PromptOf(%q) = %q, %v", tt.in, got, ok)
		}
	}
}
