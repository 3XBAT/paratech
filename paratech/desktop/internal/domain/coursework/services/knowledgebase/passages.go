package knowledgebase

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Bounds of the passages of one question (TASK-118). A question is answered from a few short texts, not from whole
// lessons: the reader is paid by the token, and a citation must point at a place the student can open.
const (
	// MaxPassages bounds the passages of one question.
	MaxPassages = 12
	// MaxPassageBytes bounds the text of one passage; a longer text is cut around its first match.
	MaxPassageBytes = 2 << 10
	// MaxTerms bounds the search terms taken from one question.
	MaxTerms = 12
	// transcriptWindow is how many consecutive transcript segments make one passage.
	transcriptWindow = 6
	// leadRunes is how much text before the first match a cut passage keeps.
	leadRunes    = 300
	minTermRunes = 3
)

// LessonText is a lesson with everything searched of it.
type LessonText struct {
	Lesson   Lesson
	Document Document
}

// Passage is a text of a lesson that may answer a question, with its place as a Hit counts it: Index is the summary
// text, the lecture text block, the first transcript segment or the fragment in Document.Notes; StartMs is 0 for the
// summary and a note. Note, Material and Locator name the fragment of SectionOwnNote, zero for the other sections;
// Heading is the first line of a note fragment of a section (its heading, one plain line of at most
// MaxHeadingRunes), "" otherwise. Text and
// Heading are untrusted.
type Passage struct {
	Lesson   Lesson
	Section  Section
	Index    int
	StartMs  int64
	Text     string
	Note     shared.NoteID
	Material shared.MaterialID
	Locator  Locator
	Heading  string
}

// Recording returns the recording of the passage; "" for a note-only lecture.
func (p Passage) Recording() shared.RecordingID { return p.Lesson.Recording }

// locatorSection is the Locator.Kind of a section of a note, whose first line is its heading.
const locatorSection = "section"

// stopWords are question words that say nothing of the topic (folded: lower case, «ё» as «е»).
var stopWords = map[string]bool{
	"что": true, "это": true, "как": true, "такое": true, "такой": true, "такая": true, "такие": true, "какой": true,
	"какая": true, "какое": true, "какие": true, "каков": true, "где": true, "когда": true, "почему": true,
	"зачем": true, "или": true, "для": true, "при": true, "про": true, "над": true, "под": true, "чем": true,
	"его": true, "она": true, "они": true, "оно": true, "был": true, "была": true, "было": true, "были": true,
	"есть": true, "нам": true, "нас": true, "мне": true, "все": true, "всех": true, "этот": true, "эта": true,
	"эти": true, "того": true, "тем": true, "том": true, "так": true, "там": true, "тут": true, "уже": true,
	"еще": true, "лекция": true, "лекции": true, "лекций": true, "лекциях": true, "занятие": true, "занятии": true,
	"расскажи": true, "объясни": true, "скажи": true, "говорили": true, "говорил": true, "сказал": true,
	"сказали": true, "нужно": true, "можно": true, "the": true, "and": true, "what": true, "how": true,
}

// Terms returns the search terms of a question: its words of letters and digits read case-insensitively with «ё» as
// «е», without question words and words shorter than 3 characters, each cut to a crude stem (a long word loses its
// last two characters, a word of 5-6 its last one) so that the forms of a Russian word match. At most MaxTerms, in
// the order of the question, each once.
func Terms(question string) []string {
	words := strings.FieldsFunc(strings.Map(fold, question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out []string
	for _, w := range words {
		n := utf8.RuneCountInString(w)
		if n < minTermRunes || stopWords[w] {
			continue
		}
		switch {
		case n >= 7:
			w = string([]rune(w)[:n-2])
		case n >= 5:
			w = string([]rune(w)[:n-1])
		}
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
		if len(out) == MaxTerms {
			break
		}
	}
	return out
}

// Passages picks the passages of the lessons that best match the question (ADR-0019: no index, a scan at query
// time). The candidates are every fragment of the student's notes (SectionOwnNote, ADR-0027), then every summary
// text, every lecture text block and, for a lesson without a lecture text (which is its full text), windows of
// consecutive transcript segments. A candidate matches when it holds a term of the question; the most terms win, then
// the newer lesson, then the earlier place: within a lesson the notes come before the sections of its recording, as
// the search scans them (ADR-0027 «Ranking»). At most MaxPassages; none when the question has no terms.
// Texts are cleaned (no control or format characters, lines kept) and cut to MaxPassageBytes.
//
// Lessons are told apart by Lesson.Key (a note-only lecture has no recording); a lesson without a key by its
// recording. A second lesson of the same key is left out.
func Passages(question string, lessons []LessonText) []Passage {
	terms := Terms(question)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		p     Passage
		score int
		rank  int // lesson order, newest first
		order int // place within the lesson
	}
	byKey := make(map[string]LessonText, len(lessons))
	ls := make([]Lesson, 0, len(lessons))
	for _, lt := range lessons {
		key := passageKey(lt.Lesson)
		if _, ok := byKey[key]; ok {
			continue
		}
		byKey[key] = lt
		ls = append(ls, lt.Lesson)
	}

	var found []scored
	for rank, l := range Newest(ls) {
		d := byKey[passageKey(l)].Document
		order := 0
		add := func(p Passage, text string) {
			order++
			text = cleanText(text)
			score, first := match(text, terms)
			if score == 0 {
				return
			}
			p.Lesson, p.Text = l, cut(text, first)
			found = append(found, scored{p: p, score: score, rank: rank, order: order})
		}
		for i, f := range d.Notes {
			p := Passage{Section: SectionOwnNote, Index: i, Note: f.Note, Material: f.Material, Locator: f.Locator}
			if f.Locator.Kind == locatorSection {
				p.Heading = headingOf(f.Text)
			}
			add(p, f.Text)
		}
		for i, text := range d.Summary {
			add(Passage{Section: SectionSummary, Index: i}, text)
		}
		if len(d.Blocks) > 0 {
			for i, b := range d.Blocks {
				add(Passage{Section: SectionLectureText, Index: i, StartMs: b.StartMs},
					strings.Join(append([]string{b.Heading}, b.Paragraphs...), "\n"))
			}
		} else {
			for i := 0; i < len(d.Segments); i += transcriptWindow {
				win := d.Segments[i:min(i+transcriptWindow, len(d.Segments))]
				texts := make([]string, len(win))
				for j, s := range win {
					texts[j] = s.Text
				}
				add(Passage{Section: SectionTranscript, Index: i, StartMs: win[0].StartMs}, strings.Join(texts, " "))
			}
		}
	}

	slices.SortStableFunc(found, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if c := cmp.Compare(a.rank, b.rank); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	out := make([]Passage, 0, min(len(found), MaxPassages))
	for _, s := range found[:min(len(found), MaxPassages)] {
		out = append(out, s.p)
	}
	return out
}

// passageKey tells the lessons of Passages apart: the lesson key, else the key of its recording.
func passageKey(l Lesson) string {
	if l.Key != "" {
		return l.Key
	}
	return RecordingKey(l.Recording)
}

// headingOf is the first line of a note fragment of a section, one plain line cut to MaxHeadingRunes.
func headingOf(text string) string {
	line, _, _ := strings.Cut(cleanText(text), "\n")
	return cutHeading(strings.TrimSpace(plain([]rune(line))))
}

// match returns how many terms text holds and the rune position of the first match (of any term), -1 for none.
func match(text string, terms []string) (int, int) {
	folded := strings.Map(fold, text)
	score, first := 0, -1
	for _, t := range terms {
		i := strings.Index(folded, t)
		if i < 0 {
			continue
		}
		score++
		if pos := utf8.RuneCountInString(folded[:i]); first < 0 || pos < first {
			first = pos
		}
	}
	return score, first
}

// cut returns text when it fits MaxPassageBytes, else a piece of it that starts at most leadRunes before the rune
// position first and fits.
func cut(text string, first int) string {
	if len(text) <= MaxPassageBytes {
		return text
	}
	runes := []rune(text)
	start := max(first-leadRunes, 0)
	n := 0
	end := start
	for end < len(runes) && n+utf8.RuneLen(runes[end]) <= MaxPassageBytes {
		n += utf8.RuneLen(runes[end])
		end++
	}
	return strings.TrimSpace(string(runes[start:end]))
}

// cleanText makes untrusted text safe to send as one fragment: valid UTF-8, format characters (bidi overrides,
// zero-width ones) removed, control characters but line breaks as spaces, the ends trimmed.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || r == 0x2028 || r == 0x2029:
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
