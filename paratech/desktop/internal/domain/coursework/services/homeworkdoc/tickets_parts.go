package homeworkdoc

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Wording of a part of a tickets file too large for one document (TASK-402).
const (
	// TicketTruncated closes the answer of a ticket too large for a file even alone: its first claims are kept.
	TicketTruncated = "Ответ сокращён: не поместился в файл."

	partTickets = "билеты "
	partTicket  = "билет "
	rangeDash   = "–"

	// maxNameRunes is what shared.SafeFileName keeps of a name: the range of a part is kept by cutting the subject.
	maxNameRunes = 120
)

// TicketsPart is one file of the answers: the tickets From..To (their numbers), its file name with DocxExtension and
// its document, which passes values.Document.Validate.
type TicketsPart struct {
	From, To int
	Name     string
	Document values.Document
}

// BuildTicketParts returns the answers as files: one document as BuildTickets builds it when it passes
// values.Document.Validate; otherwise consecutive parts filled in order, a ticket never split between them, each
// headed and named with its tickets («… (билеты 1–50)», «Билеты Физика 1–50.docx»; one ticket: «(билет 7)»). Each
// part has its own table of contents, mark and coverage summary of its tickets. A ticket too large even alone keeps
// the longest prefix of its claims and missing parts that fits, then TicketTruncated. No tickets: one empty part.
func BuildTicketParts(in TicketsInput) []TicketsPart {
	tickets := prepareTickets(in.Tickets)
	whole := TicketsPart{Name: TicketsFileName(in.Subject), Document: ticketsDoc(ticketsTitle(in, 0, 0), tickets)}
	if len(tickets) > 0 {
		whole.From, whole.To = tickets[0].number, tickets[len(tickets)-1].number
	}
	if len(tickets) == 0 || whole.Document.Validate() == nil {
		return []TicketsPart{whole}
	}

	try := func(ts []preparedTicket) values.Document {
		return ticketsDoc(ticketsTitle(in, ts[0].number, ts[len(ts)-1].number), ts)
	}
	var parts []TicketsPart
	for i := 0; i < len(tickets); {
		// More tickets never fit when fewer do not, so the count of the part is searched for: n tickets fit, n+1 not.
		n := sort.Search(len(tickets)-i, func(n int) bool { return try(tickets[i:i+n+1]).Validate() != nil })
		var doc values.Document
		if n == 0 {
			doc, n = try([]preparedTicket{truncated(tickets[i], try)}), 1
		} else {
			doc = try(tickets[i : i+n])
		}
		j := i + n
		from, to := tickets[i].number, tickets[j-1].number
		parts = append(parts, TicketsPart{From: from, To: to, Name: ticketsFileName(in.Subject, ticketRange(from, to)), Document: doc})
		i = j
	}
	if len(parts) == 1 {
		// One ticket cut to fit: its file is the whole answers, so it carries no range (a shorter heading still fits).
		p := parts[0]
		p.Name, p.Document.Title = whole.Name, ticketsTitle(in, 0, 0)
		return []TicketsPart{p}
	}
	return parts
}

// truncated is t cut to the longest prefix of its answer blocks whose part still fits, then TicketTruncated; only the
// note when nothing fits. A longer prefix never fits when a shorter one does not, so the prefix is searched for.
func truncated(t preparedTicket, try func([]preparedTicket) values.Document) preparedTicket {
	with := func(k int) preparedTicket {
		c := t
		c.body = append(t.body[:k:k], paragraph(TicketTruncated))
		return c
	}
	// The first k in [0, len) whose prefix of k+1 blocks does not fit: k blocks are the most that do.
	k := sort.Search(len(t.body), func(k int) bool {
		return try([]preparedTicket{with(k + 1)}).Validate() != nil
	})
	return with(k)
}

// ticketRange is «from–to», or «from» for one ticket.
func ticketRange(from, to int) string {
	if from == to {
		return strconv.Itoa(from)
	}
	return strconv.Itoa(from) + rangeDash + strconv.Itoa(to)
}

// ticketsFileName is «Билеты <subject> <rng>» safe for Windows, with DocxExtension; a long subject is cut so the range
// stays and the name keeps within ticketset.MaxFileNameBytes.
func ticketsFileName(subject, rng string) string {
	suffix := ""
	if rng != "" {
		suffix = " " + rng
	}
	base := []rune(shared.SafeFileName(strings.TrimSpace(ticketsFile + " " + values.CleanName(subject))))
	limit := maxNameRunes - utf8.RuneCountInString(suffix)
	if len(base) > limit {
		base = base[:limit]
	}
	for len(string(base))+len(suffix)+len(DocxExtension) > ticketset.MaxFileNameBytes {
		base = base[:len(base)-1]
	}
	return FileName(strings.TrimSpace(string(base)) + suffix)
}
