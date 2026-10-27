package answer

import "strconv"

// OwnNoteName names the student's own note in a citation (ADR-0027). It lives here only: every label of a note
// source is OwnNoteLabel.
const OwnNoteName = "ваш конспект"

// Wording of the place of a note source.
const (
	labelSeparator = " · "
	labelPage      = "стр. "
	labelSlide     = "слайд "
	labelSection   = "раздел "
	labelFragment  = "фрагмент "
)

// OwnNoteLabel is how a citation of the student's note names its place: «ваш конспект · стр. 3», «ваш конспект ·
// слайд 2», «ваш конспект · раздел „Ряды“» (the heading kept, else «раздел 2»), «ваш конспект · фрагмент 4». "" for a
// source of a recording. The heading is the student's text: show the label as plain text only.
func (s Source) OwnNoteLabel() string {
	if s.Section != SectionOwnNote {
		return ""
	}
	n := strconv.Itoa(s.Locator.N)
	switch s.Locator.Kind {
	case LocatorPage:
		return OwnNoteName + labelSeparator + labelPage + n
	case LocatorSlide:
		return OwnNoteName + labelSeparator + labelSlide + n
	case LocatorSection:
		if s.Heading != "" {
			return OwnNoteName + labelSeparator + labelSection + "„" + s.Heading + "“"
		}
		return OwnNoteName + labelSeparator + labelSection + n
	case LocatorFragment:
		return OwnNoteName + labelSeparator + labelFragment + n
	}
	return OwnNoteName
}
