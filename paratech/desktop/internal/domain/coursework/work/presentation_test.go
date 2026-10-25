package work

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

// deckOutlineJSON is a talk of 10 slides, 940 planned words.
const deckOutlineJSON = `{"topic":"Реформы Петра I","slides":[` +
	`{"kind":"title","title":"Реформы Петра I","words":60},` +
	`{"kind":"bullets","title":"Предпосылки","words":100},` +
	`{"kind":"bullets","title":"Армия и флот","words":100},` +
	`{"kind":"bullets","title":"Государственное управление","words":100},` +
	`{"kind":"bullets","title":"Церковь","words":100},` +
	`{"kind":"two_columns","title":"Итоги: за и против","words":100},` +
	`{"kind":"bullets","title":"Экономика","words":100},` +
	`{"kind":"bullets","title":"Культура и быт","words":100},` +
	`{"kind":"bullets","title":"Образование","words":100},` +
	`{"kind":"conclusion","title":"Выводы","words":80}]}`

// slideAnswer is a valid answer for a slide of kind.
func slideAnswer(kind values.SlideKind) string {
	switch kind {
	case values.SlideTitle:
		return `{"bullets":[],"columns":[],"notes":["Здравствуйте. Тема доклада — реформы Петра I."]}`
	case values.SlideTwoColumns:
		return `{"bullets":[],"columns":[{"heading":"За","bullets":["Сильная армия"]},` +
			`{"heading":"Против","bullets":["Тяжёлые налоги"]}],"notes":["Оценки реформ расходятся.","Рассмотрим обе стороны."]}`
	case values.SlideConclusion:
		return `{"bullets":["Россия стала империей"],"columns":[],"notes":["Спасибо за внимание."]}`
	}
	return `{"bullets":["Первый тезис","Второй тезис"],"columns":[],"notes":["Текст выступления к слайду."]}`
}

func questionsJSON(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"question":"Вопрос %d?","answer":"Короткий ответ %d."}`, i+1, i+1)
	}
	return `{"questions":[` + strings.Join(items, ",") + `]}`
}

// confirmedDeck is a confirmed presentation work.
func confirmedDeck(t *testing.T) *Work {
	t.Helper()
	w := newWork(t)
	a := answerOf(FeasibleFull, 0)
	a.WorkType = TypePresentation
	mustRecord(t, w, a)
	if err := w.ConfirmRequirements(t0); err != nil {
		t.Fatal(err)
	}
	return w
}

// writtenSlides starts the writing of a confirmed presentation, stores and approves its outline and writes every slide.
func writtenSlides(t *testing.T, w *Work) {
	t.Helper()
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	o, err := ParseSlideOutline(deckOutlineJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetOutline(o, t0); err != nil {
		t.Fatal(err)
	}
	if err := w.ApproveOutline(t0); err != nil {
		t.Fatal(err)
	}
	writeDueSlides(t, w)
}

func writeDueSlides(t *testing.T, w *Work) {
	t.Helper()
	for w.Next().Kind == StepSection {
		st := w.Next()
		sec, _ := w.Section(st.Section)
		text, err := ParseSlide(slideAnswer(sec.Kind), sec.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteSection(st.Section, st.Revision, text, t0); err != nil {
			t.Fatal(err)
		}
	}
}

func mustQuestions(t *testing.T, w *Work) {
	t.Helper()
	st := w.Next()
	if st.Kind != StepQuestions {
		t.Fatalf("next = %+v", st)
	}
	qs, err := ParseQuestions(questionsJSON(TalkQuestionCount))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetQuestions(st.Revision, qs, t0); err != nil {
		t.Fatal(err)
	}
}

func talkFileOf(w *Work) File {
	return File{Material: "mat-2", Artifact: "materials/" + strings.Repeat("b", 64), Content: w.ContentKey()}
}

// TestPresentationIsWrittenAsSlides replaces the refusal of TASK-131: a confirmed presentation is written as slides,
// then its likely questions, then two files.
func TestPresentationIsWrittenAsSlides(t *testing.T) {
	w := confirmedDeck(t)
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	wr, _ := w.Writing()
	if !w.Presentation() || wr.Deck.Template != values.PresentationPaper || w.Next().Kind != StepOutline {
		t.Fatalf("start = %+v %+v", wr.Deck, w.Next())
	}
	if o, _ := ParseOutline(outlineJSON); w.SetOutline(o, t0) == nil {
		t.Fatal("a paper outline taken for a presentation")
	}
	o, err := ParseSlideOutline(deckOutlineJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetOutline(o, t0); err != nil || w.Next().Kind != StepApprove {
		t.Fatalf("outline: %v %+v", err, w.Next())
	}
	if err := w.ApproveOutline(t0); err != nil {
		t.Fatal(err)
	}

	// A slide answer of the wrong kind is refused when stored.
	st := w.Next()
	wrong, _ := ParseSlide(slideAnswer(values.SlideBullets), values.SlideBullets)
	if err := w.WriteSection(st.Section, st.Revision, wrong, t0); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("a bullets text on the title slide: %v", err)
	}
	data, _, err := w.StepData(st, Texts{Assignment: "Презентация"})
	if err != nil || !strings.Contains(data, `"task":"slide"`) || !strings.Contains(data, `"kind":"title"`) {
		t.Fatalf("slide data = %s, %v", data, err)
	}
	writeDueSlides(t, w)
	if done, total := w.Progress(); done != 10 || total != 10 {
		t.Fatalf("progress = %d/%d", done, total)
	}

	st = w.Next()
	if st.Kind != StepQuestions || st.Revision != 1 || st.Attempt != 1 {
		t.Fatalf("after the slides = %+v", st)
	}
	data, _, err = w.StepData(st, Texts{Assignment: "Презентация"})
	if err != nil || !strings.Contains(data, `"task":"questions"`) || !strings.Contains(data, "Спасибо за внимание.") {
		t.Fatalf("questions data = %s, %v", data, err)
	}
	if err := w.SetQuestions(st.Revision+1, nil, t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("stale questions: %v", err)
	}
	mustQuestions(t, w)
	if len(w.TalkQuestions()) != TalkQuestionCount || w.Next().Kind != StepAssemble {
		t.Fatalf("after the questions = %+v", w.Next())
	}

	if err := w.Finish(fileOf(w), t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("a presentation finished as a paper: %v", err)
	}
	stale := talkFileOf(w)
	stale.Content = "c:other"
	if err := w.FinishDeck(fileOf(w), stale, t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("a talk of other slides: %v", err)
	}
	if err := w.FinishDeck(fileOf(w), talkFileOf(w), t0); err != nil || w.Status() != StatusDone {
		t.Fatalf("finish: %v %s", err, w.Status())
	}
	wr, _ = w.Writing()
	if files := wr.Files(); len(files) != 2 || files[0].Material != "mat-1" || files[1].Material != "mat-2" {
		t.Fatalf("files = %+v", files)
	}

	// The slides make a valid presentation.
	p := values.Presentation{Template: wr.Deck.Template}
	for _, s := range wr.Sections {
		sl, ok := s.Slide()
		if !ok {
			t.Fatalf("slide %d not written", s.Number)
		}
		p.Slides = append(p.Slides, sl)
	}
	if err := p.Validate(); err != nil || p.Slides[5].Left.Heading != "За" || p.Slides[5].Notes != "Оценки реформ расходятся.\nРассмотрим обе стороны." {
		t.Fatalf("presentation = %+v, %v", p.Slides[5], err)
	}

	// The stored form is read back through the same checks.
	state := w.State()
	state.Version = 3
	back, err := ParseWriting(wr.Encode())
	if err != nil {
		t.Fatal(err)
	}
	state.Writing = &back
	got, err := Restore(state)
	if err != nil || got.ContentKey() != w.ContentKey() || !got.Presentation() || len(got.TalkQuestions()) != TalkQuestionCount {
		t.Fatalf("restore: %v", err)
	}
}

// TestPaperKeyUnchanged: a paper keeps the content key it had before presentations, so a stored done paper stays done.
func TestPaperKeyUnchanged(t *testing.T) {
	w := confirmedWork(t)
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, w)
	wr, _ := w.Writing()
	// The key as TASK-131 computed it.
	h := sha256.New()
	h.Write([]byte(wr.Topic))
	for _, s := range wr.Sections {
		h.Write(fmt.Appendf(nil, "\x00%d\x00%d\x00%s\x00%d", s.Number, s.Revision, s.Title, s.Words))
	}
	if want := fmt.Sprintf("c:%x", h.Sum(nil)[:16]); w.ContentKey() != want {
		t.Fatalf("content key = %s, want %s", w.ContentKey(), want)
	}
	if strings.Contains(wr.Encode(), "deck") || strings.Contains(wr.Encode(), "kind") {
		t.Fatalf("a paper stores presentation fields: %s", wr.Encode())
	}
}

func TestParseSlideOutline(t *testing.T) {
	o, err := ParseSlideOutline(deckOutlineJSON)
	if err != nil || len(o.Items) != 10 || o.Items[0].Kind != values.SlideTitle || o.Items[9].Kind != values.SlideConclusion {
		t.Fatalf("outline = %+v, %v", o, err)
	}
	slide := func(kind string, words int) string {
		return fmt.Sprintf(`{"kind":%q,"title":"Слайд","words":%d}`, kind, words)
	}
	deck := func(slides ...string) string {
		return `{"topic":"Тема","slides":[` + strings.Join(slides, ",") + `]}`
	}
	middle := func(n, words int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = slide("bullets", words)
		}
		return out
	}
	full := func(n, words int) string {
		return deck(append(append([]string{slide("title", words)}, middle(n-2, words)...), slide("conclusion", words))...)
	}

	// A title with a line break and bidi controls is made one clean line.
	clean := strings.Replace(deckOutlineJSON, `"Предпосылки"`, `"Пред\u202eпосылки\nвойны"`, 1)
	if o, err := ParseSlideOutline(clean); err != nil || o.Items[1].Title != "Предпосылки войны" {
		t.Fatalf("cleaned title = %q, %v", o.Items[1].Title, err)
	}

	for name, raw := range map[string]string{
		"seven slides":        full(7, 100),
		"thirteen slides":     full(13, 80),
		"no title first":      deck(append([]string{slide("bullets", 100)}, append(middle(8, 100), slide("conclusion", 100))...)...),
		"no conclusion last":  deck(append([]string{slide("title", 100)}, middle(9, 100)...)...),
		"title in the middle": deck(append(append([]string{slide("title", 100), slide("title", 100)}, middle(7, 100)...), slide("conclusion", 100))...),
		"unknown kind":        strings.Replace(deckOutlineJSON, `"kind":"bullets"`, `"kind":"image"`, 1),
		"talk too short":      full(10, 50),
		"talk too long":       full(12, 120),
		"slide too short":     strings.Replace(deckOutlineJSON, `"words":60`, `"words":5`, 1),
		"extra key":           strings.Replace(deckOutlineJSON, `{"topic"`, `{"send_to":"x@y.ru","topic"`, 1),
		"extra slide key":     strings.Replace(deckOutlineJSON, `"words":60}`, `"words":60,"image":"C:\\x.png"}`, 1),
		"repeated key":        strings.Replace(deckOutlineJSON, `{"topic":"Реформы Петра I"`, `{"topic":"A","topic":"B"`, 1),
		"missing kind":        strings.Replace(deckOutlineJSON, `{"kind":"title",`, `{`, 1),
		"fraction":            strings.Replace(deckOutlineJSON, `"words":60`, `"words":60.5`, 1),
		"long title":          strings.Replace(deckOutlineJSON, `"Предпосылки"`, `"`+strings.Repeat("я", values.MaxSlideTitleRunes+1)+`"`, 1),
		"blank title":         strings.Replace(deckOutlineJSON, `"Предпосылки"`, `"\u200b "`, 1),
		"paper outline":       outlineJSON,
		"text after":          deckOutlineJSON + " ok",
		"not json":            "Вот план презентации:",
	} {
		if _, err := ParseSlideOutline(raw); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseSlide(t *testing.T) {
	for _, kind := range []values.SlideKind{values.SlideTitle, values.SlideBullets, values.SlideTwoColumns, values.SlideConclusion} {
		if _, err := ParseSlide(slideAnswer(kind), kind); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}

	// Hostile text stays text: markup characters are kept for the renderer to escape, control and bidi characters go.
	raw := `{"bullets":["<a:t>Взлом</a:t> & \"кавычки\"","Тезис\u202e справа\u0007"],"columns":[],"notes":["Абзац\nс переносом"]}`
	got, err := ParseSlide(raw, values.SlideBullets)
	if err != nil || got.Bullets[0] != `<a:t>Взлом</a:t> & "кавычки"` || got.Bullets[1] != "Тезис справа" ||
		got.Paragraphs[0] != "Абзац с переносом" || len(got.Sources) != 0 {
		t.Fatalf("cleaned = %+v, %v", got, err)
	}

	seven := `["1","2","3","4","5","6","7"]`
	long := `"` + strings.Repeat("я", MaxNotesParagraphRunes) + `"`
	for name, tc := range map[string]struct {
		kind values.SlideKind
		raw  string
	}{
		"title with bullets":    {values.SlideTitle, `{"bullets":["x"],"columns":[],"notes":["n"]}`},
		"bullets without any":   {values.SlideBullets, `{"bullets":[],"columns":[],"notes":["n"]}`},
		"seven bullets":         {values.SlideBullets, `{"bullets":` + seven + `,"columns":[],"notes":["n"]}`},
		"bullets with columns":  {values.SlideBullets, `{"bullets":["x"],"columns":[{"heading":"","bullets":["y"]}],"notes":["n"]}`},
		"one column":            {values.SlideTwoColumns, `{"bullets":[],"columns":[{"heading":"","bullets":["y"]}],"notes":["n"]}`},
		"three columns":         {values.SlideTwoColumns, `{"bullets":[],"columns":[{"heading":"","bullets":["a"]},{"heading":"","bullets":["b"]},{"heading":"","bullets":["c"]}],"notes":["n"]}`},
		"empty column":          {values.SlideTwoColumns, `{"bullets":[],"columns":[{"heading":"A","bullets":[]},{"heading":"B","bullets":["b"]}],"notes":["n"]}`},
		"seven in a column":     {values.SlideTwoColumns, `{"bullets":[],"columns":[{"heading":"A","bullets":` + seven + `},{"heading":"B","bullets":["b"]}],"notes":["n"]}`},
		"column extra key":      {values.SlideTwoColumns, `{"bullets":[],"columns":[{"heading":"A","bullets":["a"],"url":"http://x"},{"heading":"B","bullets":["b"]}],"notes":["n"]}`},
		"seven in a conclusion": {values.SlideConclusion, `{"bullets":` + seven + `,"columns":[],"notes":["n"]}`},
		"extra key":             {values.SlideBullets, `{"bullets":["x"],"columns":[],"notes":["n"],"email":"x@y.ru"}`},
		"missing notes":         {values.SlideBullets, `{"bullets":["x"],"columns":[]}`},
		"no notes":              {values.SlideBullets, `{"bullets":["x"],"columns":[],"notes":[]}`},
		"notes over the slide":  {values.SlideBullets, `{"bullets":["x"],"columns":[],"notes":[` + long + `,` + long + `,` + long + `]}`},
		"long bullet":           {values.SlideBullets, `{"bullets":["` + strings.Repeat("я", values.MaxSlideTextRunes+1) + `"],"columns":[],"notes":["n"]}`},
		"blank bullet":          {values.SlideBullets, `{"bullets":["\u202e\u200b"],"columns":[],"notes":["n"]}`},
		"bullet not a string":   {values.SlideBullets, `{"bullets":[1],"columns":[],"notes":["n"]}`},
		"unknown kind":          {"image", slideAnswer(values.SlideBullets)},
	} {
		if _, err := ParseSlide(tc.raw, tc.kind); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseQuestions(t *testing.T) {
	qs, err := ParseQuestions(questionsJSON(TalkQuestionCount))
	if err != nil || len(qs) != TalkQuestionCount || qs[0].Answer != "Короткий ответ 1." {
		t.Fatalf("questions = %+v, %v", qs, err)
	}
	for name, raw := range map[string]string{
		"four":         questionsJSON(4),
		"six":          questionsJSON(6),
		"extra key":    `{"questions":[],"send":"x@y.ru"}`,
		"item key":     strings.Replace(questionsJSON(5), `"answer":"Короткий ответ 1."`, `"answer":"a","url":"http://x"`, 1),
		"no answer":    strings.Replace(questionsJSON(5), `,"answer":"Короткий ответ 1."`, ``, 1),
		"blank answer": strings.Replace(questionsJSON(5), `"Короткий ответ 1."`, `"\u2066 "`, 1),
		"long answer":  strings.Replace(questionsJSON(5), `"Короткий ответ 1."`, `"`+strings.Repeat("я", MaxQuestionAnswerRunes+1)+`"`, 1),
		"missing":      `{}`,
	} {
		if _, err := ParseQuestions(raw); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestQuestionsBlockAndRegenerate: refused questions use their attempts; then Regenerate(0, note) asks them again
// under new keys with the note, the slides stay written.
func TestQuestionsBlockAndRegenerate(t *testing.T) {
	w := confirmedDeck(t)
	writtenSlides(t, w)
	first := w.Next()
	firstData, _, _ := w.StepData(first, Texts{Assignment: "a"})
	for range MaxAttempts {
		if err := w.RefuseStep(w.Next(), t0); err != nil {
			t.Fatal(err)
		}
	}
	if st := w.Next(); st.Kind != StepBlocked || st.Section != 0 {
		t.Fatalf("questions not blocked: %+v", st)
	}
	if err := w.Regenerate(0, "  Вопросы про армию  ", t0); err != nil {
		t.Fatal(err)
	}
	st := w.Next()
	data, note, err := w.StepData(st, Texts{Assignment: "a"})
	if st.Kind != StepQuestions || st.Attempt != 1 || st.Revision != 2 || err != nil || note != "Вопросы про армию" {
		t.Fatalf("after the regeneration = %+v, note %q, %v", st, note, err)
	}
	if w.StepKey(st, "i", data) == w.StepKey(first, "i", firstData) {
		t.Fatal("the questions asked again have the key of a refused answer")
	}
	if done, total := w.Progress(); done != total {
		t.Fatalf("slides rewritten: %d/%d", done, total)
	}
	wr, _ := w.Writing()
	if wr.Generation != 1 {
		t.Fatalf("generation = %d", wr.Generation)
	}
}

// TestEditDoneDeck: the student's edit keeps the kinds of known slides, makes a new one a bullets slide and refuses an
// order that breaks the title-first, conclusion-last rule; the changed talk gets new questions.
func TestEditDoneDeck(t *testing.T) {
	w := confirmedDeck(t)
	writtenSlides(t, w)
	mustQuestions(t, w)
	if err := w.FinishDeck(fileOf(w), talkFileOf(w), t0); err != nil {
		t.Fatal(err)
	}
	wr, _ := w.Writing()
	items := make([]OutlineItem, 0, len(wr.Sections))
	for _, s := range wr.Sections {
		items = append(items, OutlineItem{Number: s.Number, Title: s.Title, Words: s.Words})
	}
	if err := w.EditOutline(items, t0); err != nil || w.Status() != StatusDone {
		t.Fatalf("an unchanged deck: %v %s", err, w.Status())
	}

	swapped := append([]OutlineItem{items[1], items[0]}, items[2:]...)
	if err := w.EditOutline(swapped, t0); !errors.Is(err, ErrInvalidReply) {
		t.Fatalf("the title slide moved: %v", err)
	}
	added := append(append(append([]OutlineItem{}, items[:9]...), OutlineItem{Title: "Новый\u202e слайд", Words: 50}), items[9])
	if err := w.EditOutline(added, t0); err != nil {
		t.Fatal(err)
	}
	wr, _ = w.Writing()
	if w.Status() != StatusWriting || wr.Generation != 2 || len(wr.Sections) != 11 || wr.Sections[9].Kind != values.SlideBullets ||
		wr.Sections[9].Title != "Новый слайд" || wr.Sections[0].Kind != values.SlideTitle || wr.Deck.QuestionsRevision != 2 {
		t.Fatalf("after the edit: %s %+v", w.Status(), wr.Sections[9])
	}
	writeDueSlides(t, w)
	if st := w.Next(); st.Kind != StepQuestions || st.Revision != 2 {
		t.Fatalf("questions of the new talk: %+v", st)
	}
}

func TestRestorePresentation(t *testing.T) {
	w := confirmedDeck(t)
	writtenSlides(t, w)
	mustQuestions(t, w)
	if err := w.FinishDeck(fileOf(w), talkFileOf(w), t0); err != nil {
		t.Fatal(err)
	}
	breakers := []struct {
		name string
		edit func(*State)
	}{
		{"no deck", func(s *State) { s.Writing.Deck = nil }},
		{"unknown template", func(s *State) { s.Writing.Deck.Template = "neon" }},
		{"done without a talk", func(s *State) { s.Writing.Deck.Talk = nil }},
		{"four questions", func(s *State) { s.Writing.Deck.Questions.Items = s.Writing.Deck.Questions.Items[:4] }},
		{"questions of a later revision", func(s *State) { s.Writing.Deck.Questions.Revision = 9 }},
		{"bidi in a bullet", func(s *State) { s.Writing.Sections[1].Text.Bullets[0] = "a\u202eb" }},
		{"seven bullets", func(s *State) {
			s.Writing.Sections[1].Text.Bullets = []string{"1", "2", "3", "4", "5", "6", "7"}
		}},
		{"slide kind dropped", func(s *State) { s.Writing.Sections[1].Kind = "" }},
		{"conclusion first", func(s *State) { s.Writing.Sections[0].Kind = values.SlideConclusion }},
		{"paper sources on a slide", func(s *State) {
			s.Writing.Sections[1].Text.Sources = []Reference{{Text: "x"}}
		}},
		{"bad talk file", func(s *State) { s.Writing.Deck.Talk.Artifact = "x" }},
	}
	for _, tt := range breakers {
		t.Run(tt.name, func(t *testing.T) {
			st := w.State()
			st.Version = 4
			tt.edit(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	// A paper with presentation fields is refused too.
	p := confirmedWork(t)
	if err := p.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, p)
	mustWriteDue(t, p)
	for name, edit := range map[string]func(*State){
		"deck on a paper": func(s *State) {
			s.Writing.Deck = &Deck{Template: values.PresentationPaper, QuestionsRevision: 1, QuestionsAttempt: 1}
		},
		"kind on a section":  func(s *State) { s.Writing.Sections[0].Kind = values.SlideBullets },
		"bullets in a paper": func(s *State) { s.Writing.Sections[0].Text.Bullets = []string{"x"} },
	} {
		st := p.State()
		st.Version = 2
		edit(&st)
		if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Unknown stored fields are refused by the strict reader.
	wr, _ := w.Writing()
	raw := strings.Replace(wr.Encode(), `"template":"paper"`, `"template":"paper","logo":"x"`, 1)
	if _, err := ParseWriting(raw); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("unknown deck field: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(wr.Encode()), &probe); err != nil || probe["deck"] == nil {
		t.Fatalf("stored = %v", err)
	}
}
