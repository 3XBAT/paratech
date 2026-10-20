package summary

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Limits of the content. They are those of the summary answer (domain/coursework/services/prompts), so every valid
// answer fits.
const (
	MaxTextRunes     = 20000
	MaxKeyPoints     = 20
	MaxKeyPointRunes = 300
)

// ID identifies a summary. It is the id of the transcript it summarises (IDOf).
type ID string

// IDOf returns the id of the summary of transcript t.
func IDOf(t shared.TranscriptID) ID { return ID(t) }

// String returns the identifier as a plain string.
func (id ID) String() string { return string(id) }

// Validate applies the rules of a transcript id, which a summary id is.
func (id ID) Validate() error { return shared.TranscriptID(id).Validate() }

// Content is the summary and its key points. A summary is either sectioned (TASK-101: headed sections) or a plain
// text, the form of the summaries made before; a plain text reads as one section without heading. It is a validated
// value: build it with NewSectionedContent or NewContent.
type Content struct {
	text      string    // the plain text; of a sectioned summary its rendering (plainText)
	sections  []Section // nil: a plain text
	keyPoints []string
}

// NewContent validates and trims a plain summary text (1 to MaxTextRunes characters, line breaks and tabs allowed)
// and the key points (at most MaxKeyPoints lines of 1 to MaxKeyPointRunes characters).
func NewContent(text string, keyPoints []string) (Content, error) {
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxTextRunes ||
		strings.ContainsFunc(text, badControl) {
		return Content{}, fmt.Errorf("%w: text must be 1-%d characters without control characters", ErrInvalidContent, MaxTextRunes)
	}
	points, err := checkKeyPoints(keyPoints)
	if err != nil {
		return Content{}, err
	}
	return Content{text: text, keyPoints: points}, nil
}

// NewSectionedContent validates and trims the sections and key points of a summary: 1 to MaxSections sections, each
// with a one-line heading of 1 to MaxHeadingRunes characters, a text of 1 to MaxSectionTextRunes characters and at
// most MaxSectionPoints one-line points of 1 to MaxPointRunes; at most MaxTextRunes characters in all; key points as
// NewContent.
func NewSectionedContent(sections []Section, keyPoints []string) (Content, error) {
	if len(sections) == 0 || len(sections) > MaxSections {
		return Content{}, fmt.Errorf("%w: 1-%d sections", ErrInvalidContent, MaxSections)
	}
	out := make([]Section, 0, len(sections))
	total := 0
	for i, s := range sections {
		s, err := checkSection(i, s)
		if err != nil {
			return Content{}, err
		}
		total += s.runes()
		out = append(out, s)
	}
	if total > MaxTextRunes {
		return Content{}, fmt.Errorf("%w: sections hold more than %d characters", ErrInvalidContent, MaxTextRunes)
	}
	points, err := checkKeyPoints(keyPoints)
	if err != nil {
		return Content{}, err
	}
	return Content{text: plainText(out), sections: out, keyPoints: points}, nil
}

func checkKeyPoints(keyPoints []string) ([]string, error) {
	if len(keyPoints) > MaxKeyPoints {
		return nil, fmt.Errorf("%w: more than %d key points", ErrInvalidContent, MaxKeyPoints)
	}
	points := make([]string, 0, len(keyPoints))
	for i, p := range keyPoints {
		p = strings.TrimSpace(p)
		if p == "" || !utf8.ValidString(p) || utf8.RuneCountInString(p) > MaxKeyPointRunes || strings.ContainsFunc(p, notInLine) {
			return nil, fmt.Errorf("%w: key point %d must be one line of 1-%d characters", ErrInvalidContent, i, MaxKeyPointRunes)
		}
		points = append(points, p)
	}
	return points, nil
}

// Text returns the summary as plain text: the text of a plain summary, the rendering of the sections (headings,
// texts and points) of a sectioned one.
func (c Content) Text() string { return c.text }

// Sectioned reports whether the summary has headed sections; false for a plain text.
func (c Content) Sectioned() bool { return c.sections != nil }

// Sections returns a copy of the sections; a plain text is one section without heading and points. Empty for the
// zero value.
func (c Content) Sections() []Section {
	if c.IsZero() {
		return nil
	}
	if c.sections == nil {
		return []Section{{Text: c.text}}
	}
	out := make([]Section, len(c.sections))
	for i, s := range c.sections {
		out[i] = s.clone()
	}
	return out
}

// KeyPoints returns a copy of the key points.
func (c Content) KeyPoints() []string { return slices.Clone(c.keyPoints) }

// IsZero reports whether c is the invalid zero value.
func (c Content) IsZero() bool { return c.text == "" }

// Equal reports whether both contents hold the same text, sections and key points.
func (c Content) Equal(o Content) bool {
	return c.text == o.text && c.Sectioned() == o.Sectioned() &&
		slices.EqualFunc(c.sections, o.sections, Section.equal) && slices.Equal(c.keyPoints, o.keyPoints)
}

// Summary is the summary of one transcript (PRD section 9). Fields change only through the methods below.
type Summary struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      ID

	transcript    shared.TranscriptID // empty after the transcript was deleted (ON DELETE SET NULL)
	lesson        shared.LessonID     // optional
	promptVersion int                 // prompts version that produced the content

	content   Content
	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a summary provides to New.
type Spec struct {
	Transcript    shared.TranscriptID
	Lesson        shared.LessonID // optional
	PromptVersion int             // at least 1
	Content       Content
}

// New creates the summary of a transcript, never stored (Version() == 0). Its id is IDOf(spec.Transcript).
func New(spec Spec, now time.Time) (*Summary, error) {
	if err := spec.Transcript.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSummary, err)
	}
	s := &Summary{
		id: IDOf(spec.Transcript), transcript: spec.Transcript, lesson: spec.Lesson, promptVersion: spec.PromptVersion,
		content: spec.Content, createdAt: now, updatedAt: now,
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// State is the stored form of a summary, for Restore and the repositories.
type State struct {
	Version       int64
	ID            ID
	Transcript    shared.TranscriptID // empty: the transcript was deleted
	Lesson        shared.LessonID     // empty: none
	PromptVersion int
	Text          string    // the plain text; of a sectioned summary its rendering, which Restore derives again
	Sections      []Section // nil: a plain text (summaries made before TASK-101)
	KeyPoints     []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Restore rebuilds a stored summary without side effects; it only checks that the state is consistent.
func Restore(st State) (*Summary, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidSummary, st.Version)
	}
	c, err := restoreContent(st)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSummary, err)
	}
	if st.Transcript != "" && IDOf(st.Transcript) != st.ID {
		return nil, fmt.Errorf("%w: id differs from the transcript", ErrInvalidSummary)
	}
	s := &Summary{
		version: st.Version, id: st.ID, transcript: st.Transcript, lesson: st.Lesson, promptVersion: st.PromptVersion,
		content: c, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func restoreContent(st State) (Content, error) {
	if st.Sections != nil {
		return NewSectionedContent(st.Sections, st.KeyPoints)
	}
	return NewContent(st.Text, st.KeyPoints)
}

func (s *Summary) validate() error {
	if err := s.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSummary, err)
	}
	if s.lesson != "" {
		if err := s.lesson.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidSummary, err)
		}
	}
	switch {
	case s.content.IsZero():
		return fmt.Errorf("%w: no content", ErrInvalidSummary)
	case s.promptVersion < 1:
		return fmt.Errorf("%w: prompt version %d", ErrInvalidSummary, s.promptVersion)
	case s.createdAt.IsZero() || s.updatedAt.IsZero():
		return fmt.Errorf("%w: missing time", ErrInvalidSummary)
	}
	return nil
}

// Replace stores the content of a new summary answer for the same transcript (reprocessing). changed tells whether
// the summary must be saved: the same content from the same prompt version changes nothing.
func (s *Summary) Replace(c Content, promptVersion int, now time.Time) (changed bool, err error) {
	switch {
	case c.IsZero():
		return false, fmt.Errorf("%w: no content", ErrInvalidSummary)
	case promptVersion < 1:
		return false, fmt.Errorf("%w: prompt version %d", ErrInvalidSummary, promptVersion)
	case s.content.Equal(c) && s.promptVersion == promptVersion:
		return false, nil
	}
	s.content, s.promptVersion, s.updatedAt = c, promptVersion, now
	return true, nil
}

// ID returns the summary identifier.
func (s *Summary) ID() ID { return s.id }

// TranscriptID returns the summarised transcript, empty if it was deleted.
func (s *Summary) TranscriptID() shared.TranscriptID { return s.transcript }

// LessonID returns the lesson, empty if unknown.
func (s *Summary) LessonID() shared.LessonID { return s.lesson }

// PromptVersion returns the prompts version that produced the content.
func (s *Summary) PromptVersion() int { return s.promptVersion }

// Content returns the text and the key points.
func (s *Summary) Content() Content { return s.content }

// CreatedAt returns the creation time.
func (s *Summary) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns the time of the last change.
func (s *Summary) UpdatedAt() time.Time { return s.updatedAt }

// State returns the stored form of the summary, for the repositories.
func (s *Summary) State() State {
	st := State{
		Version: s.version, ID: s.id, Transcript: s.transcript, Lesson: s.lesson, PromptVersion: s.promptVersion,
		Text: s.content.text, KeyPoints: s.content.KeyPoints(), CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	}
	if s.content.Sectioned() {
		st.Sections = s.content.Sections()
	}
	return st
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (s *Summary) Version() int64 { return s.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004); use cases and handlers never call it.
func (s *Summary) Persisted(version int64) { s.version = version }

func badControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }

// notInLine also rejects the line and paragraph separators, which break a line in many renderers.
func notInLine(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }
