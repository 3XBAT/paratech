package work

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// The writing of a confirmed work (TASK-131, absorbs TASK-105): an outline the model proposes (sections with their
// volume, the student edits and approves it), then the text of one section per job step, then the file. The texts the model
// wrote are untrusted data: they are checked here (ParseOutline, ParseSection) and only ever rendered into the paper.
//
//	outline answer: {"topic":"<line>","sections":[{"title":"<line>","words":<int>}]}
//	section answer: {"paragraphs":["<text>"],"sources":[{"material":"<ref or empty>","text":"<line>"}]}

// Limits of the writing.
const (
	MaxSections          = 12
	MaxSectionTitleRunes = 150
	MinSectionWords      = 50
	MaxSectionWords      = 3000
	MaxTotalWords        = 15000
	MaxTopicRunes        = 200
	MaxParagraphs        = 40
	MaxParagraphRunes    = 4000
	MaxSources           = 10
	MaxSourceRunes       = 300
	MaxNoteRunes         = 1000
	MaxSnapshotRunes     = 200

	// MaxWritingAssignmentBytes and MaxWritingMaterialBytes bound the texts sent with every writing call: each section
	// is a call of its own, so a long material is not paid for again and again.
	MaxWritingAssignmentBytes = 8 << 10
	MaxWritingMaterialBytes   = 24 << 10

	// MaxContextRunes bounds the end of the previous section sent with the next one, for continuity.
	MaxContextRunes = 1000

	writeKeyPrefix = "wrk:v1:"
)

// ErrStaleStep means a step's answer is for a section revision or a step that is not due any more (the student
// edited the outline or regenerated meanwhile); the answer is dropped.
var ErrStaleStep = errors.New("work: the writing step is not due any more")

// ErrNotWritable means the work cannot be written whole as a paper (a presentation, the helper mode).
var ErrNotWritable = errors.New("work: this work is not written as a paper")

// Snapshot is what the paper takes from the assignment's lesson when the writing starts: the library subject whose
// «ДЗ» gets the file ("" none: the file stays in «Входящие») and the names of the title page. Plain local data.
type Snapshot struct {
	Subject     shared.SubjectID `json:"subject"`
	SubjectName string           `json:"subject_name"`
	Teacher     string           `json:"teacher"`
	Title       string           `json:"title"` // the assignment as the homework list shows it
}

// Reference is one entry of the bibliography of a section: one of the student's materials (Material set, Text its
// file name) or a source the model named (Material empty), which the paper marks «проверить источник».
type Reference struct {
	Text     string            `json:"text"`
	Material shared.MaterialID `json:"material"`
}

// SectionText is the checked text of a section for one of its revisions.
type SectionText struct {
	Revision   int         `json:"revision"`
	Paragraphs []string    `json:"paragraphs"`
	Sources    []Reference `json:"sources"`
}

// Section is a section of the outline. Number is stable across edits; Revision grows with every edit or regeneration
// of the section, and the text counts only for the revision it was written for. Attempt is the attempt of the call of
// the current revision: a refused answer uses one.
type Section struct {
	Number   int          `json:"number"`
	Title    string       `json:"title"`
	Words    int          `json:"words"`
	Revision int          `json:"revision"`
	Attempt  int          `json:"attempt"`
	Note     string       `json:"note"` // the student's additional instructions of the last regeneration
	Text     *SectionText `json:"text"`
}

// Written reports whether the section has its text for the current revision.
func (s Section) Written() bool { return s.Text != nil && s.Text.Revision == s.Revision }

// File is the finished paper: the library material that holds it, its artifact and the content it was built from.
type File struct {
	Material shared.MaterialID `json:"material"`
	Artifact string            `json:"artifact"` // shared.ArtifactRef in its string form
	Content  string            `json:"content"`  // ContentKey of the sections the file holds
}

// Writing is the writing state of a work from StartWriting on. Generation grows each time a done work is written
// again (an edit or a regeneration), so its job is a new one. Approved is the student's «Утвердить план»: no section
// is written before it (TASK-105).
type Writing struct {
	Snapshot       Snapshot  `json:"snapshot"`
	Generation     int       `json:"generation"`
	OutlineAttempt int       `json:"outline_attempt"`
	Topic          string    `json:"topic"`
	Sections       []Section `json:"sections"` // empty until the outline is stored
	Approved       bool      `json:"approved"`
	File           *File     `json:"file"`
}

// OutlineItem is a section of an outline as the model or the student gives it; Number 0 is a new section.
type OutlineItem struct {
	Number int
	Title  string
	Words  int
}

// Outline is a checked outline answer.
type Outline struct {
	Topic string
	Items []OutlineItem
}

// StepKind is what the next step of the writing does.
type StepKind string

// Step kinds.
const (
	StepNone     StepKind = "none"     // nothing to do: not writing
	StepOutline  StepKind = "outline"  // ask for the outline
	StepApprove  StepKind = "approve"  // the outline waits for the student: edit it, then ApproveOutline
	StepSection  StepKind = "section"  // ask for the text of Section
	StepAssemble StepKind = "assemble" // build the file from the written sections
	StepBlocked  StepKind = "blocked"  // the attempts of the due call are used up: the student regenerates
)

// Step is the next step of the writing.
type Step struct {
	Kind     StepKind
	Section  int // the number of the section of StepSection
	Revision int
	Attempt  int
}

// --- moves ----------------------------------------------------------------------------------------------

// StartWriting starts the writing of a confirmed work with what the paper takes from its lesson: the outline call is
// due. Errors: ErrInvalidTransition, ErrNotWritable (a presentation), ErrInvalidWork for a snapshot out of bounds.
func (w *Work) StartWriting(snap Snapshot, now time.Time) error {
	if w.status != StatusConfirmed {
		return fmt.Errorf("%w: write a %s work", ErrInvalidTransition, w.status)
	}
	if u, _ := w.Understanding(); u.WorkType == TypePresentation {
		return fmt.Errorf("%w: a presentation", ErrNotWritable)
	}
	next := w.clone()
	next.writing = &Writing{Snapshot: snap, Generation: 1, OutlineAttempt: 1, Sections: []Section{}}
	return w.move(next, StatusWriting, now)
}

// Next returns the next step of the writing: the outline, the student's approval of it, the first section without its
// text, the file, or blocked when the due call used its attempts.
func (w *Work) Next() Step {
	if w.status != StatusWriting || w.writing == nil {
		return Step{Kind: StepNone}
	}
	wr := w.writing
	if len(wr.Sections) == 0 {
		if wr.OutlineAttempt > MaxAttempts {
			return Step{Kind: StepBlocked}
		}
		return Step{Kind: StepOutline, Attempt: wr.OutlineAttempt}
	}
	if !wr.Approved {
		return Step{Kind: StepApprove}
	}
	for _, s := range wr.Sections {
		if s.Written() {
			continue
		}
		if s.Attempt > MaxAttempts {
			return Step{Kind: StepBlocked, Section: s.Number, Revision: s.Revision}
		}
		return Step{Kind: StepSection, Section: s.Number, Revision: s.Revision, Attempt: s.Attempt}
	}
	return Step{Kind: StepAssemble}
}

// SetOutline stores the outline the model proposed: the sections get the numbers 1..n and wait for the student's
// approval. Errors: ErrStaleStep when no outline is due, ErrInvalidAnswer for an outline out of bounds.
func (w *Work) SetOutline(o Outline, now time.Time) error {
	if w.Next().Kind != StepOutline {
		return ErrStaleStep
	}
	if err := o.check(); err != nil {
		return err
	}
	next := w.clone()
	next.writing.Topic = o.Topic
	for i, it := range o.Items {
		next.writing.Sections = append(next.writing.Sections, Section{Number: i + 1, Title: it.Title, Words: it.Words, Revision: 1, Attempt: 1})
	}
	return w.move(next, StatusWriting, now)
}

// ApproveOutline is the student's «Утвердить план»: the sections are written from now on. Error: ErrInvalidTransition
// when no outline waits for the approval.
func (w *Work) ApproveOutline(now time.Time) error {
	if w.Next().Kind != StepApprove {
		return fmt.Errorf("%w: no outline waits for the approval in %s", ErrInvalidTransition, w.status)
	}
	next := w.clone()
	next.writing.Approved = true
	return w.move(next, StatusWriting, now)
}

// EditOutline is the student's edit of the outline: the sections in the new order, a known Number keeps its text when
// its title and volume stay, a changed one is written again, Number 0 is a new section. A done work is written again
// (a new generation). The outline as it is changes nothing. An edit before the approval still waits for it. Errors:
// ErrInvalidTransition (no outline yet), ErrInvalidReply.
func (w *Work) EditOutline(items []OutlineItem, now time.Time) error {
	if err := w.outlined(); err != nil {
		return err
	}
	if err := (Outline{Topic: w.writing.Topic, Items: items}).check(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReply, err)
	}
	if w.sameOutline(items) {
		return nil
	}
	next := w.clone()
	old := map[int]Section{}
	top := 0
	for _, s := range w.writing.Sections {
		old[s.Number] = s
		top = max(top, s.Number)
	}
	seen := map[int]bool{}
	sections := make([]Section, 0, len(items))
	for _, it := range items {
		s, known := old[it.Number]
		switch {
		case it.Number != 0 && (!known || seen[it.Number]):
			return fmt.Errorf("%w: unknown or repeated section %d", ErrInvalidReply, it.Number)
		case !known:
			top++
			s = Section{Number: top, Revision: 1, Attempt: 1}
		case s.Title != it.Title || s.Words != it.Words:
			s.Revision, s.Attempt, s.Note = s.Revision+1, 1, ""
		}
		seen[s.Number] = true
		s.Title, s.Words = it.Title, it.Words
		sections = append(sections, s)
	}
	next.writing.Sections = sections
	return w.rewrite(next, now)
}

// Regenerate is «Дополнительные указания → Сгенерировать заново»: section number (0: every section) is written again
// with the student's note (may be empty). It is the student's own request to write, so it approves an outline that
// waits. Errors: ErrInvalidTransition, ErrInvalidReply.
func (w *Work) Regenerate(number int, note string, now time.Time) error {
	if err := w.outlined(); err != nil {
		return err
	}
	note = strings.TrimSpace(note)
	if !boundedText(note, MaxNoteRunes, true, true) {
		return fmt.Errorf("%w: a note has at most %d characters", ErrInvalidReply, MaxNoteRunes)
	}
	next := w.clone()
	found := false
	for i, s := range next.writing.Sections {
		if number == 0 || s.Number == number {
			next.writing.Sections[i].Revision, next.writing.Sections[i].Attempt, next.writing.Sections[i].Note = s.Revision+1, 1, note
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: no section %d", ErrInvalidReply, number)
	}
	next.writing.Approved = true
	return w.rewrite(next, now)
}

// WriteSection stores the checked text of the due section of revision; any other is ErrStaleStep.
func (w *Work) WriteSection(number, revision int, text SectionText, now time.Time) error {
	st := w.Next()
	if st.Kind != StepSection || st.Section != number || st.Revision != revision {
		return ErrStaleStep
	}
	text.Revision = revision
	if err := text.check(); err != nil {
		return err
	}
	next := w.clone()
	for i, s := range next.writing.Sections {
		if s.Number == number {
			t := text
			next.writing.Sections[i].Text = &t
		}
	}
	return w.move(next, StatusWriting, now)
}

// RefuseStep records a refused answer of the due step: the next call is a new attempt; after the last one the step
// is blocked until the student regenerates. A step that is not due any more is ErrStaleStep.
func (w *Work) RefuseStep(st Step, now time.Time) error {
	if cur := w.Next(); cur != st || (st.Kind != StepOutline && st.Kind != StepSection) {
		return ErrStaleStep
	}
	next := w.clone()
	if st.Kind == StepOutline {
		next.writing.OutlineAttempt++
	}
	for i, s := range next.writing.Sections {
		if st.Kind == StepSection && s.Number == st.Section {
			next.writing.Sections[i].Attempt++
		}
	}
	return w.move(next, StatusWriting, now)
}

// Finish keeps the file built from the written sections (f.Content must be ContentKey) and marks the work done.
// Errors: ErrStaleStep when the file is not due or was built from other sections, ErrInvalidWork for a bad file.
func (w *Work) Finish(f File, now time.Time) error {
	if w.Next().Kind != StepAssemble || f.Content != w.ContentKey() {
		return ErrStaleStep
	}
	next := w.clone()
	file := f
	next.writing.File = &file
	return w.move(next, StatusDone, now)
}

// outlined checks that the writing has its outline (writing or done).
func (w *Work) outlined() error {
	if (w.status != StatusWriting && w.status != StatusDone) || w.writing == nil || len(w.writing.Sections) == 0 {
		return fmt.Errorf("%w: no outline to change in %s", ErrInvalidTransition, w.status)
	}
	return nil
}

// sameOutline reports whether items are the sections as they are, in their order.
func (w *Work) sameOutline(items []OutlineItem) bool {
	if len(items) != len(w.writing.Sections) {
		return false
	}
	for i, s := range w.writing.Sections {
		if it := items[i]; it.Number != s.Number || it.Title != s.Title || it.Words != s.Words {
			return false
		}
	}
	return true
}

// rewrite takes next as the writing state: a done work is written again under a new generation.
func (w *Work) rewrite(next *Work, now time.Time) error {
	if w.status == StatusDone {
		next.writing.Generation++
	}
	return w.move(next, StatusWriting, now)
}

// --- reads ----------------------------------------------------------------------------------------------

// Writing returns a copy of the writing state; ok is false before StartWriting.
func (w *Work) Writing() (Writing, bool) {
	if w.writing == nil {
		return Writing{}, false
	}
	return w.writing.clone(), true
}

// Progress returns how many sections are written of how many; 0 of 0 before the outline.
func (w *Work) Progress() (done, total int) {
	if w.writing == nil {
		return 0, 0
	}
	for _, s := range w.writing.Sections {
		if s.Written() {
			done++
		}
	}
	return done, len(w.writing.Sections)
}

// ContentKey is a digest of what the paper is built from: the topic and every section with its revision. The file
// of another key is stale.
func (w *Work) ContentKey() string {
	if w.writing == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(w.writing.Topic))
	for _, s := range w.writing.Sections {
		h.Write(fmt.Appendf(nil, "\x00%d\x00%d\x00%s\x00%d", s.Number, s.Revision, s.Title, s.Words))
	}
	return fmt.Sprintf("c:%x", h.Sum(nil)[:16])
}

// StepKey is the idempotency key of the call of step st: a digest of the work, the step with its revision and
// attempt, the instructions and the data, so a repeat of the same call is replayed by the Hub for free and a stored
// section is never asked again.
func (w *Work) StepKey(st Step, instructions, data string) string {
	h := sha256.New()
	for _, part := range []string{
		string(w.id), string(st.Kind), strconv.Itoa(st.Section), strconv.Itoa(st.Revision), strconv.Itoa(st.Attempt),
		instructions, data,
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%s%x", writeKeyPrefix, h.Sum(nil))
}

// --- the data of a call ---------------------------------------------------------------------------------

// MaterialRef is one of the student's materials the writer may cite: the ref it is cited by and its file name.
type MaterialRef struct {
	ID   shared.MaterialID
	Name string
}

// Texts are the texts a writing call is made from: the assignment, the teacher's material and the student's
// materials that may be cited. They are cut to the writing limits.
type Texts struct {
	Assignment string
	Material   string
	Materials  []MaterialRef
}

// refOf is the ref the writer cites material i by.
func refOf(i int) string { return "m" + strconv.Itoa(i+1) }

type materialData struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

type outlineData struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Words  int    `json:"words"`
}

type stepData struct {
	Task           string         `json:"task"`
	WorkType       WorkType       `json:"work_type"`
	Brief          Brief          `json:"brief"`
	AssignmentText string         `json:"assignment_text"`
	MaterialText   string         `json:"material_text"`
	Materials      []materialData `json:"materials"`
	Topic          string         `json:"topic,omitempty"`
	Outline        []outlineData  `json:"outline,omitempty"`
	Section        *outlineData   `json:"section,omitempty"`
	PreviousEnd    string         `json:"previous_section_end,omitempty"`
}

// StepData builds the untrusted data of the call of step st (an outline or a section) as one JSON object: the brief,
// the texts, and for a section the topic, the outline, the section and the end of the previous one. The student's
// note of the section is returned apart: it is the student's own channel (TASK-094), not data. Errors:
// ErrStaleStep for a step that is not due, ErrInvalidRequest without an assignment text.
func (w *Work) StepData(st Step, t Texts) (data, note string, err error) {
	if cur := w.Next(); cur != st || (st.Kind != StepOutline && st.Kind != StepSection) {
		return "", "", ErrStaleStep
	}
	brief, _ := w.Brief()
	d := stepData{
		Task: string(st.Kind), WorkType: brief.WorkType, Brief: brief,
		AssignmentText: strings.TrimSpace(cutBytes(t.Assignment, MaxWritingAssignmentBytes)),
		MaterialText:   strings.TrimSpace(cutBytes(t.Material, MaxWritingMaterialBytes)),
		Materials:      make([]materialData, 0, len(t.Materials)),
	}
	if d.AssignmentText == "" {
		return "", "", fmt.Errorf("%w: no assignment text", ErrInvalidRequest)
	}
	for i, m := range t.Materials {
		d.Materials = append(d.Materials, materialData{Ref: refOf(i), Name: m.Name})
	}
	if st.Kind == StepSection {
		d.Topic = w.writing.Topic
		var prev *Section
		for i, s := range w.writing.Sections {
			d.Outline = append(d.Outline, outlineData{Number: s.Number, Title: s.Title, Words: s.Words})
			if s.Number == st.Section {
				d.Section, note = &d.Outline[len(d.Outline)-1], s.Note
				if i > 0 {
					prev = &w.writing.Sections[i-1]
				}
			}
		}
		if prev != nil && prev.Text != nil && len(prev.Text.Paragraphs) > 0 {
			d.PreviousEnd = lastRunes(prev.Text.Paragraphs[len(prev.Text.Paragraphs)-1], MaxContextRunes)
		}
	}

	// json.Marshal escapes '<' and '>', so no text can hold or close the markers of the data block.
	b, err := json.Marshal(d)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return string(b), note, nil
}

// lastRunes returns the last n characters of s.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// --- the answers ----------------------------------------------------------------------------------------

// ParseOutline checks an outline answer: exactly the keys topic and sections, a one-line topic, 1..MaxSections
// sections with a one-line title and MinSectionWords..MaxSectionWords words, at most MaxTotalWords in all. Error:
// ErrInvalidAnswer.
func ParseOutline(raw string) (Outline, error) {
	var o Outline
	var gotTopic, gotSections bool
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		switch key {
		case "topic":
			s, ok := readString(d)
			if !ok {
				return errShape
			}
			o.Topic, gotTopic = strings.TrimSpace(s), true
		case "sections":
			gotSections = true
			return readArray(d, func(d *json.Decoder) error {
				if len(o.Items) == MaxSections {
					return errShape
				}
				it, err := readOutlineItem(d)
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
		err = o.check()
	}
	if err != nil {
		return Outline{}, fmt.Errorf("%w: outline: %w", ErrInvalidAnswer, err)
	}
	return o, nil
}

func readOutlineItem(d *json.Decoder) (OutlineItem, error) {
	var it OutlineItem
	var gotTitle, gotWords bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		switch key {
		case "title":
			s, ok := readString(d)
			if !ok {
				return errShape
			}
			it.Title, gotTitle = strings.TrimSpace(s), true
		case "words":
			n, ok := readInt(d)
			if !ok || n < 0 || n > MaxTotalWords {
				return errShape
			}
			it.Words, gotWords = int(n), true
		default:
			return errShape
		}
		return nil
	})
	if err == nil && (!gotTitle || !gotWords) {
		err = errShape
	}
	return it, err
}

// check checks the bounds of an outline.
func (o Outline) check() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswer, why) }
	if !boundedText(o.Topic, MaxTopicRunes, false, false) {
		return bad("topic")
	}
	if len(o.Items) == 0 || len(o.Items) > MaxSections {
		return bad("sections count")
	}
	total := 0
	for _, it := range o.Items {
		if !boundedText(it.Title, MaxSectionTitleRunes, false, false) || it.Words < MinSectionWords || it.Words > MaxSectionWords {
			return bad("a section")
		}
		total += it.Words
	}
	if total > MaxTotalWords {
		return bad("total volume")
	}
	return nil
}

// ParseSection checks a section answer: exactly the keys paragraphs and sources, 1..MaxParagraphs paragraphs of one
// line each, at most MaxSources sources. A source that cites one of materials by its ref becomes that material
// (its file name, not the model's text); any other ref is refused; a source without a ref keeps the model's text and
// is marked «проверить источник» in the paper. Error: ErrInvalidAnswer.
func ParseSection(raw string, materials []MaterialRef) (SectionText, error) {
	var t SectionText
	var gotParagraphs, gotSources bool
	err := decodeObject(raw, func(key string, d *json.Decoder) error {
		switch key {
		case "paragraphs":
			ps, ok := readTexts(d, 1, MaxParagraphs, MaxParagraphRunes, false)
			if !ok {
				return errShape
			}
			t.Paragraphs, gotParagraphs = trimAll(ps), true
		case "sources":
			gotSources = true
			t.Sources = []Reference{}
			return readArray(d, func(d *json.Decoder) error {
				if len(t.Sources) == MaxSources {
					return errShape
				}
				s, err := readSource(d, materials)
				t.Sources = append(t.Sources, s)
				return err
			})
		default:
			return errShape
		}
		return nil
	})
	if err == nil && (!gotParagraphs || !gotSources) {
		err = errShape
	}
	if err == nil {
		err = t.checkContent()
	}
	if err != nil {
		return SectionText{}, fmt.Errorf("%w: section: %w", ErrInvalidAnswer, err)
	}
	return t, nil
}

func readSource(d *json.Decoder, materials []MaterialRef) (Reference, error) {
	var ref, text string
	var gotRef, gotText bool
	err := readObject(d, func(key string, d *json.Decoder) error {
		s, ok := readString(d)
		if !ok {
			return errShape
		}
		switch key {
		case "material":
			ref, gotRef = s, true
		case "text":
			text, gotText = strings.TrimSpace(s), true
		default:
			return errShape
		}
		return nil
	})
	if err != nil || !gotRef || !gotText {
		return Reference{}, errShape
	}
	if ref == "" {
		return Reference{Text: text}, nil
	}
	for i, m := range materials {
		if refOf(i) == ref {
			return Reference{Text: m.Name, Material: m.ID}, nil
		}
	}
	return Reference{}, errShape
}

func trimAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// check checks a stored section text: a revision and the bounds of its content.
func (t SectionText) check() error {
	if t.Revision < 1 {
		return fmt.Errorf("%w: revision", ErrInvalidAnswer)
	}
	return t.checkContent()
}

// checkContent checks the bounds of the paragraphs and the sources.
func (t SectionText) checkContent() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswer, why) }
	switch {
	case len(t.Paragraphs) == 0 || len(t.Paragraphs) > MaxParagraphs:
		return bad("paragraphs count")
	case len(t.Sources) > MaxSources:
		return bad("sources count")
	}
	for _, p := range t.Paragraphs {
		if !boundedText(p, MaxParagraphRunes, false, false) {
			return bad("a paragraph")
		}
	}
	for _, s := range t.Sources {
		if !boundedText(s.Text, MaxSourceRunes, false, false) || (s.Material != "" && s.Material.Validate() != nil) {
			return bad("a source")
		}
	}
	return nil
}

// --- stored form ----------------------------------------------------------------------------------------

// Encode returns the stored JSON of the writing state.
func (wr Writing) Encode() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(wr) // strings, ints, slices and pointers of them only: encoding cannot fail
	return strings.TrimSuffix(b.String(), "\n")
}

// ParseWriting reads a stored writing state strictly (no unknown field, nothing after it); Restore checks its bounds,
// so stored model text passes the same checks as a fresh answer. Error: ErrInvalidWork.
func ParseWriting(s string) (Writing, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	var wr Writing
	if err := d.Decode(&wr); err != nil {
		return Writing{}, fmt.Errorf("%w: writing: %w", ErrInvalidWork, err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return Writing{}, fmt.Errorf("%w: writing: data after the object", ErrInvalidWork)
	}
	return wr, nil
}

// validate checks the invariants of a writing state.
func (wr Writing) validate() error {
	bad := func(why string) error { return fmt.Errorf("%w: writing: %s", ErrInvalidWork, why) }
	sn := wr.Snapshot
	for _, s := range []string{sn.SubjectName, sn.Teacher, sn.Title} {
		if !boundedText(s, MaxSnapshotRunes, false, true) {
			return bad("snapshot text")
		}
	}
	switch {
	case sn.Subject != "" && sn.Subject.Validate() != nil:
		return bad("snapshot subject")
	case wr.Generation < 1:
		return bad("generation")
	case wr.OutlineAttempt < 1 || wr.OutlineAttempt > MaxAttempts+1:
		return bad("outline attempt")
	case len(wr.Sections) == 0 && (wr.Topic != "" || wr.File != nil || wr.Approved):
		return bad("a topic, a file or an approval without an outline")
	}
	if !wr.Approved && slices.ContainsFunc(wr.Sections, func(s Section) bool { return s.Text != nil }) {
		return bad("a section written before the approval")
	}
	if len(wr.Sections) > 0 {
		items := make([]OutlineItem, len(wr.Sections))
		numbers := map[int]bool{}
		for i, s := range wr.Sections {
			items[i] = OutlineItem{Number: s.Number, Title: s.Title, Words: s.Words}
			switch {
			case s.Number < 1 || numbers[s.Number]:
				return bad("section numbers")
			case s.Revision < 1 || s.Attempt < 1 || s.Attempt > MaxAttempts+1:
				return bad("section revision or attempt")
			case !boundedText(s.Note, MaxNoteRunes, true, true):
				return bad("section note")
			case s.Text != nil && (s.Text.Revision > s.Revision || s.Text.check() != nil):
				return bad("section text")
			}
			numbers[s.Number] = true
		}
		if err := (Outline{Topic: wr.Topic, Items: items}).check(); err != nil {
			return bad("outline")
		}
	}
	if f := wr.File; f != nil {
		if f.Material.Validate() != nil || f.Content == "" {
			return bad("file")
		}
		if _, err := shared.ParseArtifactRef(f.Artifact); err != nil {
			return bad("file artifact")
		}
	}
	return nil
}

func (wr Writing) clone() Writing {
	c := wr
	c.Sections = slices.Clone(wr.Sections)
	for i, s := range c.Sections {
		if s.Text != nil {
			t := *s.Text
			t.Paragraphs = slices.Clone(t.Paragraphs)
			t.Sources = slices.Clone(t.Sources)
			c.Sections[i].Text = &t
		}
	}
	if wr.File != nil {
		f := *wr.File
		c.File = &f
	}
	return c
}

// ArtifactRef returns the artifact of the file; zero for a malformed one.
func (f File) ArtifactRef() shared.ArtifactRef {
	r, _ := shared.ParseArtifactRef(f.Artifact)
	return r
}
