package thread

import "strings"

// CardKind is the card a line of a reply line came from.
type CardKind string

// Card kinds of a reply line, by their labels.
const (
	CardReply    CardKind = "reply"
	CardPlan     CardKind = "plan"
	CardCannotDo CardKind = "cannot_do"
	CardAsked    CardKind = "asked"
)

// The parts of a cannot_do line, as Reply writes them.
const (
	partYours   = "your_part: "
	partOurs    = "our_part: "
	partsSep    = "; "
	partTextSep = " | "
)

// CardLine is one card of a reply line: the text of a reply or the summary of a plan; the student's and paratech's
// parts of a cannot_do; nothing for asked.
type CardLine struct {
	Kind  CardKind
	Text  string
	Yours []string
	Ours  []string
}

// CardLines reads the cards back from a reply line (Reply, a frozen line), in order; the actions line, NoReply and
// lines with no known label (a line cut at the size limit) give none. The line holds each card on one line
// (line breaks of a text became spaces), and a part text holding a separator of its own is split there: it is a view
// of what the student was shown, never exact.
func CardLines(line string) []CardLine {
	var out []CardLine
	for l := range strings.SplitSeq(line, "\n") {
		switch {
		case strings.HasPrefix(l, labelReply):
			out = append(out, CardLine{Kind: CardReply, Text: strings.TrimPrefix(l, labelReply)})
		case strings.HasPrefix(l, labelPlan):
			out = append(out, CardLine{Kind: CardPlan, Text: strings.TrimPrefix(l, labelPlan)})
		case strings.HasPrefix(l, labelCannotDo):
			out = append(out, cannotDoLine(strings.TrimPrefix(l, labelCannotDo)))
		case l == labelAsked+askedText:
			out = append(out, CardLine{Kind: CardAsked})
		}
	}
	return out
}

// cannotDoLine splits "your_part: a | b; our_part: c" (either part may be missing) at the first our_part.
func cannotDoLine(s string) CardLine {
	c := CardLine{Kind: CardCannotDo}
	ours := ""
	switch {
	case strings.HasPrefix(s, partOurs):
		ours, s = strings.TrimPrefix(s, partOurs), ""
	case strings.HasPrefix(s, partYours):
		s = strings.TrimPrefix(s, partYours)
		if i := strings.Index(s, partsSep+partOurs); i >= 0 {
			s, ours = s[:i], s[i+len(partsSep+partOurs):]
		}
		c.Yours = strings.Split(s, partTextSep)
	}
	if ours != "" {
		c.Ours = strings.Split(ours, partTextSep)
	}
	return c
}
