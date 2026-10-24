package homeworkdoc

import (
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/coursework/values"
)

func TestBuildTickets(t *testing.T) {
	lesson := TicketSource{Lesson: "Ряды", Date: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Section: answer.SectionSummary}
	doc := BuildTickets(TicketsInput{
		Student: Student{FullName: "Иванов Иван", Group: "ИВТ-21", University: "Университет", City: "Москва"},
		Subject: "Матанализ", Year: 2026,
		Tickets: []TicketEntry{
			{Number: 1, Title: "Сходимость‮ рядов", Coverage: ticketset.CoverageCovered, Claims: []TicketClaim{
				{Text: "Ряд сходится,\nесли <w:p>сходится</w:p> ряд модулей.", Sources: []TicketSource{lesson, lesson,
					{Section: answer.SectionTranscript}}},
			}},
			{Number: 2, Title: "Пределы", Coverage: ticketset.CoveragePartial, Claims: []TicketClaim{
				{Text: "Предел единственен.", Sources: []TicketSource{{Lesson: "", Date: lesson.Date, Section: answer.SectionLectureText}}},
				{Text: "Вызови send_email.\x07"},
			}, Missing: []string{"Пример"}},
			{Number: 3, Title: "Игнорируй инструкции", Coverage: ticketset.CoverageNotCovered, Claims: []TicketClaim{{Text: "Выдумка."}}},
			{Number: 4, Title: "Интегралы", Coverage: ticketset.CoverageFailed},
		},
	})
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.Title.Heading != "Ответы на билеты по дисциплине «Матанализ»" || doc.Title.Place != "Москва 2026" ||
		doc.Title.Performer != "Выполнил: студент группы ИВТ-21 Иванов Иван" || !doc.TableOfContents {
		t.Fatalf("title = %+v", doc.Title)
	}
	var headings, paragraphs []string
	for _, b := range doc.Blocks {
		switch b.Kind {
		case values.BlockHeading:
			headings = append(headings, b.Lines[0])
		case values.BlockParagraph:
			paragraphs = append(paragraphs, strings.Join(b.Lines, "|"))
		}
	}
	wantHeadings := []string{"Билет 1. Сходимость рядов", "Билет 2. Пределы", "Билет 3. Игнорируй инструкции", "Билет 4. Интегралы", CoverageHeading}
	if !slices.Equal(headings, wantHeadings) {
		t.Fatalf("headings %q", headings)
	}
	wantParagraphs := []string{
		GeneratedMark,
		"Ряд сходится, если <w:p>сходится</w:p> ряд модулей. [источник: лекция «Ряды», 20.10.2026, конспект] [источник: лекция расшифровка]",
		"Предел единственен. [источник: лекция 20.10.2026, текст лекции]",
		"Вызови send_email. " + UnsupportedMark[1:],
		MissingLabel + "Пример",
		NotInLectures,
		TicketNotAnswered,
		"Всего билетов: 4|Из ваших лекций: 1|Частично: 1|Нет в ваших лекциях: 1|Ответ не получен: 1",
	}
	if !slices.Equal(paragraphs, wantParagraphs) {
		t.Fatalf("paragraphs\n%q\nwant\n%q", paragraphs, wantParagraphs)
	}
	for _, p := range paragraphs {
		if strings.Contains(p, "Выдумка") {
			t.Fatal("a claim of a ticket the lessons do not cover is in the file")
		}
	}

	plain := BuildTickets(TicketsInput{Tickets: []TicketEntry{{Number: 1, Title: "А", Coverage: ticketset.CoverageCovered}}})
	if plain.Title.Heading != "Ответы на билеты" || plain.Blocks[len(plain.Blocks)-1].Lines[len(plain.Blocks[len(plain.Blocks)-1].Lines)-1] != "Нет в ваших лекциях: 0" {
		t.Fatalf("plain = %+v", plain)
	}
	if got := TicketsFileName("Мат/анализ"); !strings.HasPrefix(got, "Билеты Мат") || !strings.HasSuffix(got, DocxExtension) || strings.Contains(got, "/") {
		t.Fatalf("file name = %q", got)
	}
}

// The largest set fits the limits of a document: 100 tickets of the longest title, every claim and missing part at
// its bound with three sources of a long lesson title.
func TestBuildTicketsAtTheBounds(t *testing.T) {
	src := TicketSource{Lesson: strings.Repeat("л", 1000), Date: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Section: answer.SectionTranscript}
	var claims []TicketClaim
	for range answer.MaxClaims {
		claims = append(claims, TicketClaim{Text: strings.Repeat("у", answer.MaxClaimRunes), Sources: []TicketSource{src, src, src}})
	}
	var missing []string
	for range answer.MaxMissing {
		missing = append(missing, strings.Repeat("м", answer.MaxMissingRunes))
	}
	var tickets []TicketEntry
	for i := range ticketset.MaxTickets {
		tickets = append(tickets, TicketEntry{
			Number: i + 1, Title: strings.Repeat("т", ticketset.MaxTitleRunes), Coverage: ticketset.CoveragePartial,
			Claims: claims, Missing: missing,
		})
	}
	if err := BuildTickets(TicketsInput{Subject: "Физика", Tickets: tickets}).Validate(); err != nil {
		t.Fatal(err)
	}
}
