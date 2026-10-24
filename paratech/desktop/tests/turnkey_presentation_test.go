package tests

// A presentation (TASK-106, ADR-0025 addendum) end to end: the agent's write_work on an approved plan, the student's
// approval of the slides, one slide per job step and the likely questions on the real job runner and SQLite, the .pptx
// by the real renderer and the speaker text by the real .docx renderer, both filed into the subject's «ДЗ» of the real
// library; only the Hub is a fake. The Undo of write_work takes both files back to «Входящие».

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// rlo is the bidi control RIGHT-TO-LEFT OVERRIDE.
var rlo = string(rune(0x202E))

const presentationOutline = `{"topic":"Реформы Петра I","slides":[` +
	`{"kind":"title","title":"Реформы Петра I","words":60},` +
	`{"kind":"bullets","title":"Предпосылки","words":100},{"kind":"bullets","title":"Армия","words":100},` +
	`{"kind":"bullets","title":"Флот","words":100},{"kind":"bullets","title":"Управление","words":100},` +
	`{"kind":"two_columns","title":"За и против","words":100},{"kind":"bullets","title":"Экономика","words":100},` +
	`{"kind":"bullets","title":"Культура","words":100},{"kind":"bullets","title":"Образование","words":100},` +
	`{"kind":"conclusion","title":"Выводы","words":80}]}`

// presentationAnswers are the writer's answers in the order of the calls: the slides, each slide, the questions. The
// second slide carries markup characters and a bidi control.
func presentationAnswers() []string {
	out := []string{presentationOutline, `{"bullets":[],"columns":[],"notes":["Здравствуйте, тема доклада — реформы."]}`,
		`{"bullets":["Война <со> Швецией & \"окно\"` + rlo + `"],"columns":[],"notes":["Россия нуждалась в выходе к морю."]}`}
	for range 3 {
		out = append(out, `{"bullets":["Тезис"],"columns":[],"notes":["Пояснение к тезису."]}`)
	}
	out = append(out, `{"bullets":[],"columns":[{"heading":"За","bullets":["Армия"]},{"heading":"Против","bullets":["Налоги"]}],"notes":["Сравним."]}`)
	for range 3 {
		out = append(out, `{"bullets":["Тезис"],"columns":[],"notes":["Пояснение к тезису."]}`)
	}
	out = append(out, `{"bullets":["Россия стала империей"],"columns":[],"notes":["Спасибо за внимание."]}`,
		`{"questions":[{"question":"Зачем нужен флот?","answer":"Для выхода к морю."},`+
			`{"question":"Q2?","answer":"A2."},{"question":"Q3?","answer":"A3."},{"question":"Q4?","answer":"A4."},`+
			`{"question":"Q5?","answer":"A5."}]}`)
	return out
}

// TestTurnkeyPresentationEndToEnd: the agent writes a confirmed presentation; the student approves the slides; the
// job writes ten slides and the questions (twelve calls, twelve keys) and files the .pptx and the speaker text into
// «ДЗ»; Undo of write_work takes both back to «Входящие», the files kept.
func TestTurnkeyPresentationEndToEnd(t *testing.T) {
	e := newTurnkeyEnv(t, "Подготовьте презентацию о реформах Петра I на 5–7 минут.")
	ctx := t.Context()
	e.reader.AnswerTriage(strings.Replace(triageOutput("", 0, work.FeasibleFull), `"referat"`, `"presentation"`, 1))
	wc := workCardOf(t, e.runTriage(t))
	if v, err := e.binding.ConfirmRequirements(ctx, wc.Work.ID()); err != nil || v.Card.WorkType != "presentation" {
		t.Fatalf("confirm = %+v, %v", v, err)
	}
	id := shared.WorkID(wc.Work.ID())
	sub := e.fileUnder(t, "История")
	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, presentationAnswers()...)
	e.startRunner(t)

	e.runWriteWork(t, wc.Work)
	slides := e.waitOutline(t, id, e.jobOfCall(t, "ww1"))
	if !slides.Writing.Presentation || slides.Writing.Total != 10 {
		t.Fatalf("slides = %+v", slides.Writing)
	}
	v, _ := e.approveAndWait(t, id)
	if v.Writing.Material == "" || v.Writing.Talk == "" || v.Writing.Subject != sub.ID() || len(v.Writing.Questions) != 5 {
		t.Fatalf("writing = %+v", v.Writing)
	}
	if specs, keys := e.writerRequests(); len(specs) != 12 || keys != 12 {
		t.Fatalf("writer calls %d, keys %d", len(specs), keys)
	}

	deck, deckText, err := e.lib.MaterialText(ctx, v.Writing.Material)
	if err != nil {
		t.Fatal(err)
	}
	talk, talkText, err := e.lib.MaterialText(ctx, v.Writing.Talk)
	if err != nil {
		t.Fatal(err)
	}
	homework := subject.SystemFolderID(sub.ID(), subject.SystemHomework)
	if deck.Type() != material.TypePPTX || talk.Type() != material.TypeDOCX || deck.Folder() != homework || talk.Folder() != homework ||
		!strings.HasSuffix(deck.Name(), homeworkdoc.PptxExtension) {
		t.Fatalf("files = %+v, %+v", deck.State(), talk.State())
	}
	for _, want := range []string{"Реформы Петра I", `Война <со> Швецией & "окно"`, "За и против", "Выводы"} {
		if !strings.Contains(deckText.String(), want) {
			t.Errorf("the slides have no %q", want)
		}
	}
	if strings.Contains(deckText.String(), rlo) {
		t.Error("a bidi control reached the slides")
	}
	for _, want := range []string{homeworkdoc.GeneratedMark, "Слайд 2. Предпосылки", "Россия нуждалась в выходе к морю.",
		homeworkdoc.QuestionsHeading, "Зачем нужен флот?", "Для выхода к морю."} {
		if !strings.Contains(talkText.String(), want) {
			t.Errorf("the speaker text has no %q", want)
		}
	}

	entry, err := e.journal.Find(ctx, "th1", 2, "ww1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := e.journal.Undo(ctx, entry.ID)
	if err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	for _, mid := range []shared.MaterialID{v.Writing.Material, v.Writing.Talk} {
		m, err := e.lib.Material(ctx, mid)
		if err != nil || !m.InInbox() || !m.Active() {
			t.Fatalf("after the Undo %s = %+v, %v", mid, m, err)
		}
	}
}
