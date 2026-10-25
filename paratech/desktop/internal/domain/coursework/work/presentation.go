package work

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// The writing of a presentation (TASK-106) reuses the writing of a paper: the outline is the list of slides (each
// section is a slide with its kind and the planned words of its speaker notes), the student approves it, then one slide
// per job step, then the five likely questions with short answers, then the files: the .pptx and the speaker text as a
// .docx. Every text is untrusted model output: it is cleaned of control and format characters (bidi, zero width) and
// checked here, also when it is read back from storage.
//
//	outline answer:   {"topic":"<line>","slides":[{"kind":"title|bullets|two_columns|conclusion","title":"<line>","words":<int>}]}
//	slide answer:     {"bullets":["<line>"],"columns":[{"heading":"<line or empty>","bullets":["<line>"]}],"notes":["<line>"]}
//	questions answer: {"questions":[{"question":"<line>","answer":"<line>"}]}

// Limits of a presentation.
const (
	MinSlides = 8
	MaxSlides = 12

	// The planned words of the notes: a talk of 5–7 minutes is about 700–1000 words; the plan may miss a little.
	MinSlideWords = 15
	MaxSlideWords = 250
	MinTalkWords  = 600
	MaxTalkWords  = 1200

	MaxNotesParagraphs     = 8
	MaxNotesParagraphRunes = 1500

	TalkQuestionCount      = 5
	MaxQuestionRunes       = 300
	MaxQuestionAnswerRunes = 1000

	slideColumns = 2
)

// Deck is the presentation part of a writing: the template of the slides, the likely questions of the current
// revision and the speaker text file. QuestionsRevision grows with every change of the slides, so the questions are
// asked again for the new talk; QuestionsAttempt is the attempt of that call.
type Deck struct {
	Template          values.PresentationTemplate `json:"template"`
	QuestionsRevision int                         `json:"questions_revision"`
	QuestionsAttempt  int                         `json:"questions_attempt"`
	QuestionsNote     string                      `json:"questions_note"`
	Questions         *QuestionSet                `json:"questions"`
	Talk              *File                       `json:"talk"` // the speaker text .docx, once done
}

// QuestionSet is the checked list of likely questions for one revision.
type QuestionSet struct {
	Revision int            `json:"revision"`
	Items    []TalkQuestion `json:"items"`
}

// TalkQuestion is a likely question of the audience with a short answer.
type TalkQuestion struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Column is a column of a two-column slide.
type Column struct {
	Heading string   `json:"heading"`
	Bullets []string `json:"bullets"`
}

// Presentation reports whether the work is written as a presentation.
func (w *Work) Presentation() bool { return w.writing != nil && w.writing.Deck != nil }

// Section returns the section (slide) number of the outline; ok is false for none.
func (w *Work) Section(number int) (Section, bool) {
	if w.writing == nil {
		return Section{}, false
	}
	for _, s := range w.writing.Sections {
		if s.Number == number {
			wr := Writing{Sections: []Section{s}}.clone()
			return wr.Sections[0], true
		}
	}
	return Section{}, false
}

// Slide returns the written slide of a presentation section with its title and notes (the paragraphs joined by line
// breaks); ok is false for a paper section or one without its text.
func (s Section) Slide() (values.Slide, bool) {
	if s.Kind == "" || !s.Written() {
		return values.Slide{}, false
	}
	return s.Text.slide(s.Kind, s.Title), true
}

func (t SectionText) slide(kind values.SlideKind, title string) values.Slide {
	sl := values.Slide{Kind: kind, Title: title, Bullets: slices.Clone(t.Bullets), Notes: strings.Join(t.Paragraphs, "\n")}
	if kind == values.SlideTwoColumns && len(t.Columns) == slideColumns {
		sl.Left = values.SlideColumn{Heading: t.Columns[0].Heading, Bullets: slices.Clone(t.Columns[0].Bullets)}
		sl.Right = values.SlideColumn{Heading: t.Columns[1].Heading, Bullets: slices.Clone(t.Columns[1].Bullets)}
	}
	return sl
}

// TalkQuestions returns the likely questions written for the current slides; nil before them.
func (w *Work) TalkQuestions() []TalkQuestion {
	if !w.Presentation() {
		return nil
	}
	d := w.writing.Deck
	if d.Questions == nil || d.Questions.Revision != d.QuestionsRevision {
		return nil
	}
	return slices.Clone(d.Questions.Items)
}

// SetQuestions stores the checked likely questions of the due questions step of revision; any other is ErrStaleStep.
func (w *Work) SetQuestions(revision int, items []TalkQuestion, now time.Time) error {
	st := w.Next()
	if st.Kind != StepQuestions || st.Revision != revision {
		return ErrStaleStep
	}
	if err := checkQuestions(items); err != nil {
		return err
	}
	next := w.clone()
	next.writing.Deck.Questions = &QuestionSet{Revision: revision, Items: slices.Clone(items)}
	return w.move(next, StatusWriting, now)
}

// FinishDeck keeps the files of a presentation built from its written slides and questions (both Content must be
// ContentKey) and marks the work done. Errors: ErrStaleStep when the files are not due or were built from other
// slides, ErrInvalidWork for a bad file.
func (w *Work) FinishDeck(slides, talk File, now time.Time) error {
	if !w.Presentation() || w.Next().Kind != StepAssemble || slides.Content != w.ContentKey() || talk.Content != w.ContentKey() {
		return ErrStaleStep
	}
	next := w.clone()
	s, t := slides, talk
	next.writing.File, next.writing.Deck.Talk = &s, &t
	return w.move(next, StatusDone, now)
}

// Files returns the files of the writing: the paper or the slides, then the speaker text of a presentation. A file of
// an earlier generation stays until the new one replaces it.
func (wr Writing) Files() []File {
	var out []File
	if wr.File != nil {
		out = append(out, *wr.File)
	}
	if wr.Deck != nil && wr.Deck.Talk != nil {
		out = append(out, *wr.Deck.Talk)
	}
	return out
}

// --- the answers ----------------------------------------------------------------------------------------

// ParseSlideOutline checks the outline answer of a presentation: exactly the keys topic and slides, MinSlides..MaxSlides
// slides, the title slide first, the conclusion last and bullets or two_columns between, a title of at most
// values.MaxSlideTitleRunes, MinSlideWords..MaxSlideWords planned words per slide and MinTalkWords..MaxTalkWords in
// all. Texts are made one clean line. Error: ErrInvalidAnswer.
func ParseSlideOutline(raw string) (Outline, error) {
	var o Outline
	var gotTopic, gotSlides bool
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		switch key {
		case "topic":
			s, ok := readLine(d, MaxTopicRunes)
			if !ok {
				return errShape
			}
			o.Topic, gotTopic = s, true
		case "slides":
			gotSlides = true
			return readArray(d, func(d *json.Decoder) error {
				if len(o.Items) == MaxSlides {
					return errShape
				}
				it, err := readSlideItem(d)
				o.Items = append(o.Items, it)
				return err
			})
		default:
			return errShape
		}
		return nil
	})
	if err == nil && (!gotTopic || !gotSlides) {
		err = errShape
	}
	if err == nil {
		err = o.check(formDeck)
	}
	if err != nil {
		return Outline{}, fmt.Errorf("%w: slide outline: %w", ErrInvalidAnswer, err)
	}
	return o, nil
}

func readSlideItem(d *json.Decoder) (OutlineItem, error) {
	var it OutlineItem
	var gotKind, gotTitle, gotWords bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		switch key {
		case "kind":
			s, ok := readString(d)
			if !ok || !knownSlideKind(values.SlideKind(s)) {
				return errShape
			}
			it.Kind, gotKind = values.SlideKind(s), true
		case "title":
			s, ok := readLine(d, values.MaxSlideTitleRunes)
			if !ok {
				return errShape
			}
			it.Title, gotTitle = s, true
		case "words":
			n, ok := readInt(d)
			if !ok || n < 0 || n > MaxTalkWords {
				return errShape
			}
			it.Words, gotWords = int(n), true
		default:
			return errShape
		}
		return nil
	})
	if err == nil && (!gotKind || !gotTitle || !gotWords) {
		err = errShape
	}
	return it, err
}

// ParseSlide checks the answer for one slide of kind: exactly the keys bullets, columns and notes; a title slide has
// no bullets and no columns, a bullets slide 1..values.MaxSlideBullets bullets, a two-column slide exactly two
// columns of 1..values.MaxSlideBullets bullets, a conclusion at most values.MaxSlideBullets bullets; notes are
// 1..MaxNotesParagraphs paragraphs of the speaker text. Texts are made one clean line; the slide must pass
// values.Slide.Validate. Error: ErrInvalidAnswer.
func ParseSlide(raw string, kind values.SlideKind) (SectionText, error) {
	t := SectionText{Sources: []Reference{}}
	var gotBullets, gotColumns, gotNotes bool
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		var ok bool
		switch key {
		case "bullets":
			t.Bullets, ok = readLines(d, 0, values.MaxSlideBullets, values.MaxSlideTextRunes)
			gotBullets = true
		case "columns":
			gotColumns, ok = true, true
			if err := readArray(d, func(d *json.Decoder) error {
				if len(t.Columns) == slideColumns {
					return errShape
				}
				c, err := readColumn(d)
				t.Columns = append(t.Columns, c)
				return err
			}); err != nil {
				return err
			}
		case "notes":
			t.Paragraphs, ok = readLines(d, 1, MaxNotesParagraphs, MaxNotesParagraphRunes)
			gotNotes = true
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err == nil && (!gotBullets || !gotColumns || !gotNotes) {
		err = errShape
	}
	if err == nil {
		t = t.normalized()
		err = t.checkSlide(kind)
	}
	if err != nil {
		return SectionText{}, fmt.Errorf("%w: slide: %w", ErrInvalidAnswer, err)
	}
	return t, nil
}

func readColumn(d *json.Decoder) (Column, error) {
	var c Column
	var gotHeading, gotBullets bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		var ok bool
		switch key {
		case "heading":
			var s string
			if s, ok = readString(d); ok && rawBounded(s, values.MaxSlideTextRunes) {
				c.Heading, gotHeading = oneLine(s), true
			} else {
				ok = false
			}
		case "bullets":
			c.Bullets, ok = readLines(d, 1, values.MaxSlideBullets, values.MaxSlideTextRunes)
			gotBullets = true
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err == nil && (!gotHeading || !gotBullets) {
		err = errShape
	}
	return c, err
}

// ParseQuestions checks the answer of likely questions: exactly the key questions with exactly TalkQuestionCount items
// of exactly the keys question and answer, one clean line each. Error: ErrInvalidAnswer.
func ParseQuestions(raw string) ([]TalkQuestion, error) {
	var items []TalkQuestion
	got := false
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		if key != "questions" {
			return errShape
		}
		got = true
		return readArray(d, func(d *json.Decoder) error {
			if len(items) == TalkQuestionCount {
				return errShape
			}
			q, err := readTalkQuestion(d)
			items = append(items, q)
			return err
		})
	})
	if err == nil && !got {
		err = errShape
	}
	if err == nil {
		err = checkQuestions(items)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: questions: %w", ErrInvalidAnswer, err)
	}
	return items, nil
}

func readTalkQuestion(d *json.Decoder) (TalkQuestion, error) {
	var q TalkQuestion
	var gotQ, gotA bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		var ok bool
		switch key {
		case "question":
			q.Question, ok = readLine(d, MaxQuestionRunes)
			gotQ = true
		case "answer":
			q.Answer, ok = readLine(d, MaxQuestionAnswerRunes)
			gotA = true
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err == nil && (!gotQ || !gotA) {
		err = errShape
	}
	return q, err
}

// --- checks ---------------------------------------------------------------------------------------------

// checkDeck checks the bounds of a slide outline.
func (o Outline) checkDeck() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswer, why) }
	if !cleanText(o.Topic, MaxTopicRunes, false) {
		return bad("topic")
	}
	n := len(o.Items)
	if n < MinSlides || n > MaxSlides {
		return bad("slides count")
	}
	total := 0
	for i, it := range o.Items {
		switch {
		case !slideKindAt(i, n, it.Kind) || it.Part != "":
			return bad("slide kind")
		case !cleanText(it.Title, values.MaxSlideTitleRunes, false):
			return bad("slide title")
		case it.Words < MinSlideWords || it.Words > MaxSlideWords:
			return bad("slide volume")
		}
		total += it.Words
	}
	if total < MinTalkWords || total > MaxTalkWords {
		return bad("talk volume")
	}
	return nil
}

// slideKindAt reports whether kind may stand at position i of n slides: the title first, the conclusion last.
func slideKindAt(i, n int, kind values.SlideKind) bool {
	switch i {
	case 0:
		return kind == values.SlideTitle
	case n - 1:
		return kind == values.SlideConclusion
	}
	return kind == values.SlideBullets || kind == values.SlideTwoColumns
}

func knownSlideKind(k values.SlideKind) bool {
	switch k {
	case values.SlideTitle, values.SlideBullets, values.SlideTwoColumns, values.SlideConclusion:
		return true
	}
	return false
}

// checkSlide checks a slide text of kind: the shape its kind needs, clean bounded lines and values.Slide.Validate.
func (t SectionText) checkSlide(kind values.SlideKind) error {
	bad := func(why string) error { return fmt.Errorf("%w: slide %s", ErrInvalidAnswer, why) }
	if len(t.Sources) > 0 {
		return bad("sources")
	}
	if len(t.Paragraphs) == 0 || len(t.Paragraphs) > MaxNotesParagraphs {
		return bad("notes count")
	}
	for _, p := range t.Paragraphs {
		if !cleanText(p, MaxNotesParagraphRunes, false) {
			return bad("notes")
		}
	}
	if !cleanLines(t.Bullets, values.MaxSlideTextRunes) {
		return bad("bullets")
	}
	for _, c := range t.Columns {
		if !cleanText(c.Heading, values.MaxSlideTextRunes, true) || len(c.Bullets) == 0 || !cleanLines(c.Bullets, values.MaxSlideTextRunes) {
			return bad("column")
		}
	}
	bullets, columns := len(t.Bullets), len(t.Columns)
	var shape bool
	switch kind {
	case values.SlideTitle:
		shape = bullets == 0 && columns == 0
	case values.SlideBullets:
		shape = bullets >= 1 && bullets <= values.MaxSlideBullets && columns == 0
	case values.SlideTwoColumns:
		shape = bullets == 0 && columns == slideColumns
	case values.SlideConclusion:
		shape = bullets <= values.MaxSlideBullets && columns == 0
	}
	if !shape {
		return bad("shape")
	}
	// The title is checked with the outline; a placeholder stands for it here.
	if err := t.slide(kind, "-").Validate(); err != nil {
		return bad("content")
	}
	return nil
}

// normalized returns t with nil lists as empty ones, as the stored form keeps them.
func (t SectionText) normalized() SectionText {
	if len(t.Columns) == 0 {
		t.Columns = nil
	}
	if len(t.Bullets) == 0 {
		t.Bullets = nil
	}
	return t
}

func checkQuestions(items []TalkQuestion) error {
	if len(items) != TalkQuestionCount {
		return fmt.Errorf("%w: %d questions, want %d", ErrInvalidAnswer, len(items), TalkQuestionCount)
	}
	for _, q := range items {
		if !cleanText(q.Question, MaxQuestionRunes, false) || !cleanText(q.Answer, MaxQuestionAnswerRunes, false) {
			return fmt.Errorf("%w: a question", ErrInvalidAnswer)
		}
	}
	return nil
}

// validate checks the invariants of the presentation part of a writing.
func (d Deck) validate() error {
	bad := func(why string) error { return fmt.Errorf("%w: writing: deck %s", ErrInvalidWork, why) }
	switch {
	case !slices.Contains(values.PresentationTemplates(), d.Template):
		return bad("template")
	case d.QuestionsRevision < 1 || d.QuestionsAttempt < 1 || d.QuestionsAttempt > MaxAttempts+1:
		return bad("questions revision or attempt")
	case !boundedText(d.QuestionsNote, MaxNoteRunes, true, true):
		return bad("questions note")
	}
	if q := d.Questions; q != nil && (q.Revision < 1 || q.Revision > d.QuestionsRevision || checkQuestions(q.Items) != nil) {
		return bad("questions")
	}
	if f := d.Talk; f != nil && f.validate() != nil {
		return bad("talk file")
	}
	return nil
}

func (d Deck) clone() Deck {
	c := d
	if d.Questions != nil {
		q := *d.Questions
		q.Items = slices.Clone(q.Items)
		c.Questions = &q
	}
	if d.Talk != nil {
		t := *d.Talk
		c.Talk = &t
	}
	return c
}

// --- clean text -----------------------------------------------------------------------------------------

// readLine reads a string of at most maxRunes characters and makes it one clean line; ok is false for another value or
// a text that is blank once clean.
func readLine(d *json.Decoder, maxRunes int) (string, bool) {
	s, ok := readString(d)
	if !ok || !rawBounded(s, maxRunes) {
		return "", false
	}
	s = oneLine(s)
	return s, s != ""
}

// readLines reads an array of minItems..maxItems strings made one clean line each, none blank.
func readLines(d *json.Decoder, minItems, maxItems, maxRunes int) ([]string, bool) {
	out := []string{}
	err := readArray(d, func(d *json.Decoder) error {
		if len(out) == maxItems {
			return errShape
		}
		s, ok := readLine(d, maxRunes)
		if !ok {
			return errShape
		}
		out = append(out, s)
		return nil
	})
	return out, err == nil && len(out) >= minItems
}

// rawBounded bounds a text before it is cleaned: valid UTF-8 of at most maxRunes characters.
func rawBounded(s string, maxRunes int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxRunes
}

// oneLine makes untrusted text one plain line: format characters (bidi controls, zero width) are dropped, control
// characters and line breaks become spaces, spaces are collapsed.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r) || r == 0x2028 || r == 0x2029:
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

// cleanText reports whether s is a clean line (oneLine leaves it as it is) of at most maxRunes characters, non-blank
// unless empty is allowed.
func cleanText(s string, maxRunes int, empty bool) bool {
	if s == "" {
		return empty
	}
	return utf8.RuneCountInString(s) <= maxRunes && oneLine(s) == s
}

func cleanLines(list []string, maxRunes int) bool {
	for _, s := range list {
		if !cleanText(s, maxRunes, false) {
			return false
		}
	}
	return true
}
