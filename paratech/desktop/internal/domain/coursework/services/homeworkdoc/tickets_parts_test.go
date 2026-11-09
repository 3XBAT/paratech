package homeworkdoc

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/coursework/values"
)

// longTickets are n tickets of claims claims of runes characters each, every odd one covered, every even one partial.
func longTickets(n, claims, runes int) []TicketEntry {
	var out []TicketEntry
	for i := 1; i <= n; i++ {
		e := TicketEntry{Number: i, Title: "Вопрос " + strconv.Itoa(i), Coverage: ticketset.CoverageCovered}
		if i%2 == 0 {
			e.Coverage = ticketset.CoveragePartial
		}
		for range claims {
			e.Claims = append(e.Claims, TicketClaim{Text: strings.Repeat("у", runes)})
		}
		out = append(out, e)
	}
	return out
}

// ticketHeadings are the «Билет N.» headings of a document, by number.
func ticketHeadings(doc values.Document) []int {
	var out []int
	for _, b := range doc.Blocks {
		if b.Kind != values.BlockHeading || !strings.HasPrefix(b.Lines[0], ticketPrefix) {
			continue
		}
		n, _, _ := strings.Cut(strings.TrimPrefix(b.Lines[0], ticketPrefix), ".")
		v, err := strconv.Atoi(n)
		if err != nil {
			panic(err)
		}
		out = append(out, v)
	}
	return out
}

func summaryOf(doc values.Document) string {
	return strings.Join(doc.Blocks[len(doc.Blocks)-1].Lines, "|")
}

func TestBuildTicketPartsSplitsALargeSet(t *testing.T) {
	in := TicketsInput{Subject: "Физика", Year: 2026, Tickets: longTickets(ticketset.MaxTickets, 30, 400)}
	if BuildTickets(in).Validate() == nil {
		t.Fatal("the set fits one document: the test does not split")
	}
	parts := BuildTicketParts(in)
	if len(parts) < 2 {
		t.Fatalf("%d parts", len(parts))
	}
	next := 1
	for _, p := range parts {
		if err := p.Document.Validate(); err != nil {
			t.Fatalf("part %d–%d: %v", p.From, p.To, err)
		}
		if p.From != next || p.To < p.From {
			t.Fatalf("part %d–%d after %d", p.From, p.To, next-1)
		}
		next = p.To + 1
		rng := fmt.Sprintf("%d–%d", p.From, p.To)
		if p.Document.Title.Heading != "Ответы на билеты по дисциплине «Физика» (билеты "+rng+")" ||
			p.Name != "Билеты Физика "+rng+DocxExtension || !p.Document.TableOfContents ||
			p.Document.Blocks[0].Lines[0] != GeneratedMark {
			t.Fatalf("part %s: %q, %q", rng, p.Document.Title.Heading, p.Name)
		}
		var want []int
		covered := 0
		for n := p.From; n <= p.To; n++ {
			want = append(want, n)
			if n%2 == 1 {
				covered++
			}
		}
		if got := ticketHeadings(p.Document); !reflect.DeepEqual(got, want) {
			t.Fatalf("part %s holds %v", rng, got)
		}
		wantSummary := fmt.Sprintf("Всего билетов: %d|Из ваших лекций: %d|Частично: %d|Нет в ваших лекциях и конспектах: 0",
			len(want), covered, len(want)-covered)
		if got := summaryOf(p.Document); got != wantSummary {
			t.Fatalf("part %s summary %q", rng, got)
		}
		if strings.Contains(fmt.Sprint(p.Document.Blocks), TicketTruncated) {
			t.Fatalf("part %s cut a ticket that fits", rng)
		}
	}
	if next != ticketset.MaxTickets+1 {
		t.Fatalf("the parts end at %d", next-1)
	}
	// Greedy: a part with the first ticket of the next one does not fit.
	first := prepareTickets(in.Tickets)[:parts[0].To+1]
	if ticketsDoc(ticketsTitle(in, 1, parts[0].To+1), first).Validate() == nil {
		t.Fatalf("part 1 ends at %d, but one more ticket fits", parts[0].To)
	}
}

func TestBuildTicketPartsKeepsOneDocument(t *testing.T) {
	in := TicketsInput{Subject: "Физика", Tickets: longTickets(10, 2, 50)}
	parts := BuildTicketParts(in)
	if len(parts) != 1 || parts[0].From != 1 || parts[0].To != 10 || parts[0].Name != "Билеты Физика"+DocxExtension {
		t.Fatalf("parts %+v", parts)
	}
	if !reflect.DeepEqual(parts[0].Document, BuildTickets(in)) || strings.Contains(parts[0].Document.Title.Heading, "(") {
		t.Fatalf("the one part is not the whole file: %q", parts[0].Document.Title.Heading)
	}

	none := BuildTicketParts(TicketsInput{})
	if len(none) != 1 || none[0].From != 0 || none[0].To != 0 || none[0].Name != "Билеты"+DocxExtension ||
		none[0].Document.Validate() != nil || summaryOf(none[0].Document) != "Всего билетов: 0|Из ваших лекций: 0|Частично: 0|Нет в ваших лекциях и конспектах: 0" {
		t.Fatalf("no tickets = %+v", none)
	}
}

// A ticket larger than a document keeps its whole first claims, then the note; alone it is the whole file.
func TestBuildTicketPartsCutsATicketTooLarge(t *testing.T) {
	const runes = 15000
	in := TicketsInput{Tickets: longTickets(1, values.MaxDocumentRunes/runes+10, runes)}
	in.Tickets[0].Missing = []string{"Пример"}
	parts := BuildTicketParts(in)
	if len(parts) != 1 || parts[0].From != 1 || parts[0].To != 1 || parts[0].Name != "Билеты"+DocxExtension {
		t.Fatalf("parts %+v", parts)
	}
	doc := parts[0].Document
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.Title.Heading != "Ответы на билеты" {
		t.Fatalf("heading %q", doc.Title.Heading)
	}
	claim := strings.Repeat("у", runes) + UnsupportedMark
	kept := 0
	body := doc.Blocks[2 : len(doc.Blocks)-2] // after the mark and the ticket heading, before the summary
	for _, b := range body[:len(body)-1] {
		if b.Lines[0] != claim {
			t.Fatalf("a claim is cut: %d runes", utf8.RuneCountInString(b.Lines[0]))
		}
		kept++
	}
	if body[len(body)-1].Lines[0] != TicketTruncated || kept == 0 {
		t.Fatalf("kept %d claims, then %q", kept, body[len(body)-1].Lines[0])
	}
	// The longest prefix: one more claim does not fit.
	more := doc
	more.Blocks = append(append(append([]values.Block{}, doc.Blocks[:2+kept]...), paragraph(claim), paragraph(TicketTruncated)),
		doc.Blocks[len(doc.Blocks)-2:]...)
	if more.Validate() == nil {
		t.Fatalf("%d claims kept, one more fits", kept)
	}
}

// A claim longer than a line of a document is dropped with what follows it; a part of one ticket is named by its number.
func TestBuildTicketPartsDropsAClaimTooLong(t *testing.T) {
	in := TicketsInput{Subject: "Физика", Tickets: longTickets(2, 1, 10)}
	in.Tickets[0].Claims = []TicketClaim{{Text: strings.Repeat("у", values.MaxTextRunes+1)}, {Text: "Коротко."}}
	parts := BuildTicketParts(in)
	if len(parts) != 2 {
		t.Fatalf("parts %+v", parts)
	}
	for i, p := range parts {
		n := i + 1
		if err := p.Document.Validate(); err != nil {
			t.Fatal(err)
		}
		if p.From != n || p.To != n || p.Name != fmt.Sprintf("Билеты Физика %d%s", n, DocxExtension) ||
			p.Document.Title.Heading != fmt.Sprintf("Ответы на билеты по дисциплине «Физика» (билет %d)", n) {
			t.Fatalf("part %d = %d–%d %q %q", n, p.From, p.To, p.Name, p.Document.Title.Heading)
		}
	}
	if b := parts[0].Document.Blocks; len(b) != 5 || b[2].Lines[0] != TicketTruncated {
		t.Fatalf("ticket 1 = %+v", b)
	}
	if strings.Contains(fmt.Sprint(parts[1].Document.Blocks), TicketTruncated) {
		t.Fatal("ticket 2 is cut")
	}

	plain := BuildTicketParts(TicketsInput{Tickets: in.Tickets})
	if plain[0].Document.Title.Heading != "Ответы на билеты (билет 1)" || plain[0].Name != "Билеты 1"+DocxExtension {
		t.Fatalf("without a subject: %q %q", plain[0].Document.Title.Heading, plain[0].Name)
	}
}

// A long subject is cut so the name keeps its range and its bounds.
func TestTicketsFileNameKeepsTheRange(t *testing.T) {
	for _, subject := range []string{strings.Repeat("ф", 200), strings.Repeat("😀", 200)} {
		name := ticketsFileName(subject, "51–100")
		if !strings.HasSuffix(name, " 51–100"+DocxExtension) || len(name) > ticketset.MaxFileNameBytes ||
			!strings.HasPrefix(name, "Билеты ") {
			t.Fatalf("name %q (%d bytes)", name, len(name))
		}
		if whole := TicketsFileName(subject); len(whole) > ticketset.MaxFileNameBytes || !strings.HasSuffix(whole, DocxExtension) {
			t.Fatalf("whole name %q (%d bytes)", whole, len(whole))
		}
	}
	if got := TicketsFileName("Физика"); got != "Билеты Физика"+DocxExtension {
		t.Fatalf("name %q", got)
	}
}
