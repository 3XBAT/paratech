package work

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// The writing of a lab report (TASK-107) reuses the writing of a paper: the outline follows the structure of the lab
// manual when the teacher's material has one (else цель, оборудование, теория, ход работы, результаты, выводы), each
// section with its part; the student approves it; then one section per job step; then the .docx. The model never
// writes the student's data: wherever it is needed a paragraph is a placeholder «▢ Вставьте …», which the paper shows
// highlighted and lists under «Что доделать самому». A results section without a placeholder is refused. Every text is
// untrusted model output, cleaned and checked here, also when read back from storage.
//
//	outline answer: {"topic":"<line>","sections":[{"title":"<line>","words":<int>,"part":"<part>"}]}
//	section answer: the answer of a paper section, its paragraphs one line each.

// Limits of a lab report.
const (
	MaxPlaceholderRunes    = 200 // a placeholder line, the mark included
	MaxSectionPlaceholders = 20  // placeholders of one section
)

// PlaceholderPrompt starts the prompt of every placeholder; the paper puts values.PlaceholderMark before it.
const PlaceholderPrompt = "Вставьте "

// LabPart is the part of a lab report a section is.
type LabPart string

// Parts of a lab report.
const (
	LabGoal        LabPart = "goal"
	LabEquipment   LabPart = "equipment"
	LabTheory      LabPart = "theory"
	LabProcedure   LabPart = "procedure"
	LabResults     LabPart = "results"
	LabConclusions LabPart = "conclusions"
	LabOther       LabPart = "other"
)

// LabParts returns every part in the order of the standard structure.
func LabParts() []LabPart {
	return []LabPart{LabGoal, LabEquipment, LabTheory, LabProcedure, LabResults, LabConclusions, LabOther}
}

// Manual is what became of the teacher's material (the lab manual) when the outline was asked.
type Manual string

// States of the manual.
const (
	ManualNone    Manual = "none"     // no material is attached
	ManualRead    Manual = "read"     // its text reached the writer
	ManualNotRead Manual = "not_read" // a PDF: its text is not read (ADR-0027)
	ManualNoText  Manual = "no_text"  // a scan, an empty, damaged or unreadable file
)

// Unread reports whether a manual was attached but its text did not reach the writer.
func (m Manual) Unread() bool { return m == ManualNotRead || m == ManualNoText }

func knownManual(m Manual) bool {
	return m == ManualNone || m == ManualRead || m == ManualNotRead || m == ManualNoText
}

// Lab is the lab-report part of a writing; Manual is set with the outline ("" before it).
type Lab struct {
	Manual Manual `json:"manual"`
}

// Placeholder is a gap of a written section for the student's data: the section and its prompt («Вставьте …»).
type Placeholder struct {
	Section int
	Title   string
	Prompt  string
}

// LabReport reports whether the work is written as a lab report.
func (w *Work) LabReport() bool { return w.writing != nil && w.writing.Lab != nil }

// Manual returns the state of the lab manual of a lab report; "" for another work or before the outline.
func (w *Work) Manual() Manual {
	if !w.LabReport() {
		return ""
	}
	return w.writing.Lab.Manual
}

// Placeholders returns the placeholders of the written sections in their order: «Что доделать самому».
func (w *Work) Placeholders() []Placeholder {
	if !w.LabReport() {
		return nil
	}
	var out []Placeholder
	for _, s := range w.writing.Sections {
		if !s.Written() {
			continue
		}
		for _, p := range s.Text.Paragraphs {
			if prompt, ok := PromptOf(p); ok {
				out = append(out, Placeholder{Section: s.Number, Title: s.Title, Prompt: prompt})
			}
		}
	}
	return out
}

// PromptOf returns the prompt («Вставьте …») of a paragraph that is a placeholder of a lab report.
func PromptOf(paragraph string) (string, bool) {
	rest, ok := strings.CutPrefix(paragraph, values.PlaceholderMark+" ")
	if !ok || !strings.HasPrefix(rest, PlaceholderPrompt) {
		return "", false
	}
	return rest, true
}

// --- the answers ----------------------------------------------------------------------------------------

// ParseLabOutline checks the outline answer of a lab report: exactly the keys topic and sections, 1..MaxSections
// sections of exactly the keys title, words and part, the bounds of a paper outline, a known part each, at least one
// results and one conclusions section. Texts are made one clean line. Error: ErrInvalidAnswer.
func ParseLabOutline(raw string) (Outline, error) {
	var o Outline
	var gotTopic, gotSections bool
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		switch key {
		case "topic":
			s, ok := readLine(d, MaxTopicRunes)
			if !ok {
				return errShape
			}
			o.Topic, gotTopic = s, true
		case "sections":
			gotSections = true
			return readArray(d, func(d *json.Decoder) error {
				if len(o.Items) == MaxSections {
					return errShape
				}
				it, err := readLabItem(d)
				o.Items = append(o.Items, it)
				return err
			})
		default:
			return errShape
		}
		return nil
	})
	if err == nil && (!gotTopic || !gotSections) {
		err = errShape
	}
	if err == nil {
		err = o.check(formLab)
	}
	if err != nil {
		return Outline{}, fmt.Errorf("%w: lab outline: %w", ErrInvalidAnswer, err)
	}
	return o, nil
}

func readLabItem(d *json.Decoder) (OutlineItem, error) {
	var it OutlineItem
	var gotTitle, gotWords, gotPart bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		var ok bool
		switch key {
		case "title":
			it.Title, ok = readLine(d, MaxSectionTitleRunes)
			gotTitle = true
		case "words":
			var n int64
			n, ok = readInt(d)
			ok = ok && n >= 0 && n <= MaxTotalWords
			it.Words, gotWords = int(n), true
		case "part":
			var s string
			s, ok = readString(d)
			ok = ok && slices.Contains(LabParts(), LabPart(s))
			it.Part, gotPart = LabPart(s), true
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err == nil && (!gotTitle || !gotWords || !gotPart) {
		err = errShape
	}
	return it, err
}

// ParseLabSection checks the answer for a section of part of a lab report: the answer of a paper section
// (ParseSection) whose paragraphs are made one clean line each; a paragraph with values.PlaceholderMark must be a
// placeholder «▢ Вставьте …» of at most MaxPlaceholderRunes, at most MaxSectionPlaceholders per section, and a results
// section has at least one. Error: ErrInvalidAnswer.
func ParseLabSection(raw string, materials []MaterialRef, part LabPart) (SectionText, error) {
	t, err := ParseSection(raw, materials)
	if err != nil {
		return SectionText{}, err
	}
	for i, p := range t.Paragraphs {
		t.Paragraphs[i] = labParagraph(p)
	}
	if err := t.checkLab(part); err != nil {
		return SectionText{}, fmt.Errorf("%w: lab section: %w", ErrInvalidAnswer, err)
	}
	return t, nil
}

// labParagraph makes a paragraph one clean line; a placeholder gets its one form «▢ Вставьте …» (the model may drop the
// space after the mark).
func labParagraph(p string) string {
	p = oneLine(p)
	if rest, ok := strings.CutPrefix(p, values.PlaceholderMark); ok {
		if rest = strings.TrimSpace(rest); strings.HasPrefix(rest, PlaceholderPrompt) {
			return values.PlaceholderText(rest)
		}
	}
	return p
}

// --- checks ---------------------------------------------------------------------------------------------

// checkLab checks a section text of part: clean lines, well-formed placeholders and the placeholder of a results
// section.
func (t SectionText) checkLab(part LabPart) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswer, why) }
	if len(t.Bullets) > 0 || len(t.Columns) > 0 {
		return bad("a lab section has no bullets")
	}
	if err := t.checkContent(); err != nil {
		return err
	}
	n := 0
	for _, p := range t.Paragraphs {
		if !cleanText(p, MaxParagraphRunes, false) {
			return bad("a paragraph is not one clean line")
		}
		if !strings.Contains(p, values.PlaceholderMark) {
			continue
		}
		prompt, ok := PromptOf(p)
		if !ok || strings.Contains(prompt, values.PlaceholderMark) || strings.TrimSpace(strings.TrimPrefix(prompt, PlaceholderPrompt)) == "" ||
			utf8.RuneCountInString(p) > MaxPlaceholderRunes {
			return bad("a placeholder")
		}
		n++
	}
	switch {
	case n > MaxSectionPlaceholders:
		return bad("too many placeholders")
	case part == LabResults && n == 0:
		return bad("a results section without a placeholder for the student's data")
	}
	return nil
}

// checkLab checks the bounds of a lab outline: those of a paper, a known part each, a results and a conclusions section.
func (o Outline) checkLab() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswer, why) }
	parts := map[LabPart]bool{}
	for _, it := range o.Items {
		if it.Kind != "" || !slices.Contains(LabParts(), it.Part) {
			return bad("a section part")
		}
		parts[it.Part] = true
	}
	if !parts[LabResults] || !parts[LabConclusions] {
		return bad("a lab report needs a results and a conclusions section")
	}
	return o.checkPaper()
}

// labItems is the student's edit of a lab outline with the parts filled: an empty part keeps the part of a known
// section, a new section is LabOther.
func labItems(items []OutlineItem, old map[int]Section) []OutlineItem {
	out := slices.Clone(items)
	for i, it := range out {
		if it.Part != "" {
			continue
		}
		if s, ok := old[it.Number]; ok && it.Number != 0 {
			out[i].Part = s.Part
		} else {
			out[i].Part = LabOther
		}
	}
	return out
}

// validate checks the lab part of a writing.
func (l Lab) validate(outlined bool) error {
	if (l.Manual == "") == outlined || (l.Manual != "" && !knownManual(l.Manual)) {
		return fmt.Errorf("%w: writing: lab manual", ErrInvalidWork)
	}
	return nil
}
