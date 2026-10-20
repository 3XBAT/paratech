package values

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrInvalidPresentation is returned by Presentation.Validate for a presentation that must not be rendered (ARCH-18).
var ErrInvalidPresentation = errors.New("invalid presentation")

// Presentation is a slide deck as the student saves it (TASK-106): a template and the slides in order. Like Document
// it is format-neutral and holds plain text only; an adapter renders it (PowerPoint .pptx). Its content will come
// from the model, so Validate bounds everything before a renderer sees it.
type Presentation struct {
	Template PresentationTemplate // empty: PresentationPaper
	Slides   []Slide
}

// PresentationTemplate is the look of a presentation, in the palette of the application.
type PresentationTemplate string

// Presentation templates.
const (
	PresentationPaper    PresentationTemplate = "paper"    // warm paper background, graphite text
	PresentationGraphite PresentationTemplate = "graphite" // dark graphite background, light text
	PresentationWhite    PresentationTemplate = "white"    // white background, graphite text
)

// PresentationTemplates lists the templates in the order a picker shows them; the first is the default.
func PresentationTemplates() []PresentationTemplate {
	return []PresentationTemplate{PresentationPaper, PresentationGraphite, PresentationWhite}
}

// OrDefault returns t, or PresentationPaper when t is empty.
func (t PresentationTemplate) OrDefault() PresentationTemplate {
	if t == "" {
		return PresentationPaper
	}
	return t
}

// SlideKind is what a slide shows.
type SlideKind string

// Slide kinds.
const (
	SlideTitle      SlideKind = "title"       // Title and an optional Subtitle (author, course, date: one per line, up to MaxSlideSubtitleLines)
	SlideBullets    SlideKind = "bullets"     // Title and 1 to MaxSlideBullets Bullets
	SlideTwoColumns SlideKind = "two_columns" // Title, Left and Right
	SlideConclusion SlideKind = "conclusion"  // Title and up to MaxSlideBullets short Bullets
)

// Limits of a presentation. Texts are counted in runes.
const (
	MaxPresentationSlides = 30
	MaxSlideBullets       = 6 // per slide and per column
	MaxSlideTitleRunes    = 120
	MaxSlideTextRunes     = 250 // a bullet or a column heading
	MaxSlideSubtitleRunes = 300
	MaxSlideSubtitleLines = 4 // blank lines are not counted
	MaxSlideNotesRunes    = 4000
)

// Slide is one slide. A field the kind does not show must be empty, so no content is lost silently. Notes are the
// speaker notes of any slide; they may have several lines.
type Slide struct {
	Kind     SlideKind
	Title    string
	Subtitle string
	Bullets  []string
	Left     SlideColumn
	Right    SlideColumn
	Notes    string
}

// SlideColumn is one column of a two-column slide: an optional heading and 1 to MaxSlideBullets bullets.
type SlideColumn struct {
	Heading string
	Bullets []string
}

// IsZero reports whether the column has no heading and no bullet.
func (c SlideColumn) IsZero() bool { return c.Heading == "" && len(c.Bullets) == 0 }

// Validate checks the template, the number of slides and every slide.
func (p Presentation) Validate() error {
	switch p.Template {
	case "", PresentationPaper, PresentationGraphite, PresentationWhite:
	default:
		return fmt.Errorf("%w: unknown template %q", ErrInvalidPresentation, p.Template)
	}
	if len(p.Slides) == 0 || len(p.Slides) > MaxPresentationSlides {
		return fmt.Errorf("%w: %d slides, want 1 to %d", ErrInvalidPresentation, len(p.Slides), MaxPresentationSlides)
	}
	for i, s := range p.Slides {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("slide %d: %w", i+1, err)
		}
	}
	return nil
}

// Validate checks that the slide has what its kind needs, nothing it does not show, and texts within the limits.
func (s Slide) Validate() error {
	if err := presentationText("title", s.Title, MaxSlideTitleRunes, true); err != nil {
		return err
	}
	if err := presentationText("notes", s.Notes, MaxSlideNotesRunes, false); err != nil {
		return err
	}

	switch s.Kind {
	case SlideTitle:
		if len(s.Bullets) > 0 || !s.Left.IsZero() || !s.Right.IsZero() {
			return fmt.Errorf("%w: a title slide has only a title and a subtitle", ErrInvalidPresentation)
		}
		if n := presentationLineCount(s.Subtitle); n > MaxSlideSubtitleLines {
			return fmt.Errorf("%w: subtitle has %d lines, want at most %d", ErrInvalidPresentation, n, MaxSlideSubtitleLines)
		}
		return presentationText("subtitle", s.Subtitle, MaxSlideSubtitleRunes, false)
	case SlideBullets, SlideConclusion:
		if s.Subtitle != "" || !s.Left.IsZero() || !s.Right.IsZero() {
			return fmt.Errorf("%w: a %s slide has only a title and bullets", ErrInvalidPresentation, s.Kind)
		}
		return presentationBullets("bullets", s.Bullets, s.Kind == SlideBullets)
	case SlideTwoColumns:
		if s.Subtitle != "" || len(s.Bullets) > 0 {
			return fmt.Errorf("%w: a two-column slide has only a title and two columns", ErrInvalidPresentation)
		}
		if err := s.Left.validate("left"); err != nil {
			return err
		}
		return s.Right.validate("right")
	default:
		return fmt.Errorf("%w: unknown slide kind %q", ErrInvalidPresentation, s.Kind)
	}
}

func (c SlideColumn) validate(name string) error {
	if err := presentationText(name+" heading", c.Heading, MaxSlideTextRunes, false); err != nil {
		return err
	}
	return presentationBullets(name+" bullets", c.Bullets, true)
}

func presentationBullets(name string, list []string, required bool) error {
	if required && len(list) == 0 {
		return fmt.Errorf("%w: no %s", ErrInvalidPresentation, name)
	}
	if len(list) > MaxSlideBullets {
		return fmt.Errorf("%w: %d %s, want at most %d", ErrInvalidPresentation, len(list), name, MaxSlideBullets)
	}
	for _, b := range list {
		if err := presentationText(name, b, MaxSlideTextRunes, true); err != nil {
			return err
		}
	}
	return nil
}

// Lengths are counted on the raw text, so the input stays bounded; emptiness on the clean text, so a required text
// is never rendered blank.
func presentationText(name, s string, maxRunes int, required bool) error {
	if required && strings.TrimSpace(CleanPresentationText(s)) == "" {
		return fmt.Errorf("%w: empty %s", ErrInvalidPresentation, name)
	}
	if n := utf8.RuneCountInString(s); n > maxRunes {
		return fmt.Errorf("%w: %s has %d characters, want at most %d", ErrInvalidPresentation, name, n, maxRunes)
	}
	return nil
}

// CleanPresentationText is the text a renderer shows: invalid UTF-8, the characters XML 1.0 cannot hold (C0 controls
// other than tab and line breaks, U+FFFE, U+FFFF) and the bidi controls (StripBidi) are dropped.
func CleanPresentationText(s string) string {
	return StripBidi(strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, strings.ToValidUTF8(s, "")))
}

// presentationLineCount counts the lines of s that are not blank once clean, as a renderer shows them.
func presentationLineCount(s string) int {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(CleanPresentationText(s))
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.Trim(l, " \t") != "" {
			n++
		}
	}
	return n
}
