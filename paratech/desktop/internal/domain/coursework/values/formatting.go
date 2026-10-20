package values

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Formatting is the formatting profile of a document: font, size, line spacing, page margins and the first-line
// indent of a paragraph. A zero field means the GOST 7.32 default, so the zero Formatting is GOST. Body text is
// always justified. Renderers use Normalized; Validate is for a profile the student edits.
type Formatting struct {
	FontName       string // a font installed on the student's machine, e.g. "Times New Roman"
	FontSizePt     int    // body text, points
	LineSpacingPct int    // line spacing, percent of a single line: 150 is one and a half
	MarginTopMM    int
	MarginRightMM  int
	MarginBottomMM int
	MarginLeftMM   int
	IndentTenthMM  int // tenths of a millimetre: 125 is 1.25 cm; 0 is the default, so no indent cannot be chosen yet
}

// GOST 7.32 defaults.
const (
	DefaultFontName       = "Times New Roman"
	DefaultFontSizePt     = 14
	DefaultLineSpacingPct = 150
	DefaultMarginTopMM    = 20
	DefaultMarginRightMM  = 15
	DefaultMarginBottomMM = 20
	DefaultMarginLeftMM   = 30
	DefaultIndentTenthMM  = 125
)

// Ranges of a formatting profile; a value out of range falls back to its default in Normalized.
const (
	MaxFontNameRunes  = 64
	MinFontSizePt     = 8
	MaxFontSizePt     = 28
	MinLineSpacingPct = 100
	MaxLineSpacingPct = 300
	MinMarginMM       = 5
	MaxMarginMM       = 60
	MaxIndentTenthMM  = 300 // 3 cm
)

// GOSTFormatting returns the GOST 7.32 profile with every field set.
func GOSTFormatting() Formatting {
	return Formatting{
		FontName: DefaultFontName, FontSizePt: DefaultFontSizePt, LineSpacingPct: DefaultLineSpacingPct,
		MarginTopMM: DefaultMarginTopMM, MarginRightMM: DefaultMarginRightMM, MarginBottomMM: DefaultMarginBottomMM,
		MarginLeftMM: DefaultMarginLeftMM, IndentTenthMM: DefaultIndentTenthMM,
	}
}

// Normalized returns the profile with every field set: a zero or invalid field becomes its default.
func (f Formatting) Normalized() Formatting {
	d := GOSTFormatting()
	if ValidFontName(f.FontName) {
		d.FontName = f.FontName
	}
	pick := func(v, lo, hi int, dst *int) {
		if v >= lo && v <= hi {
			*dst = v
		}
	}
	pick(f.FontSizePt, MinFontSizePt, MaxFontSizePt, &d.FontSizePt)
	pick(f.LineSpacingPct, MinLineSpacingPct, MaxLineSpacingPct, &d.LineSpacingPct)
	pick(f.MarginTopMM, MinMarginMM, MaxMarginMM, &d.MarginTopMM)
	pick(f.MarginRightMM, MinMarginMM, MaxMarginMM, &d.MarginRightMM)
	pick(f.MarginBottomMM, MinMarginMM, MaxMarginMM, &d.MarginBottomMM)
	pick(f.MarginLeftMM, MinMarginMM, MaxMarginMM, &d.MarginLeftMM)
	pick(f.IndentTenthMM, 1, MaxIndentTenthMM, &d.IndentTenthMM)
	return d
}

// Validate reports the first field that is set but out of range; zero fields are fine (defaults). The error wraps
// ErrInvalidFormatting.
func (f Formatting) Validate() error {
	if f.FontName != "" && !ValidFontName(f.FontName) {
		return fmt.Errorf("%w: font name", ErrInvalidFormatting)
	}
	checks := []struct {
		name      string
		v, lo, hi int
	}{
		{"font size", f.FontSizePt, MinFontSizePt, MaxFontSizePt},
		{"line spacing", f.LineSpacingPct, MinLineSpacingPct, MaxLineSpacingPct},
		{"top margin", f.MarginTopMM, MinMarginMM, MaxMarginMM},
		{"right margin", f.MarginRightMM, MinMarginMM, MaxMarginMM},
		{"bottom margin", f.MarginBottomMM, MinMarginMM, MaxMarginMM},
		{"left margin", f.MarginLeftMM, MinMarginMM, MaxMarginMM},
		{"first-line indent", f.IndentTenthMM, 1, MaxIndentTenthMM},
	}
	for _, c := range checks {
		if c.v != 0 && (c.v < c.lo || c.v > c.hi) {
			return fmt.Errorf("%w: %s %d, from %d to %d", ErrInvalidFormatting, c.name, c.v, c.lo, c.hi)
		}
	}
	return nil
}

// ValidFontName reports whether s can name a font: 1 to MaxFontNameRunes letters, digits, spaces, '-', '_' or '.',
// no leading or trailing space. It keeps markup and control characters out of a font attribute.
func ValidFontName(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > MaxFontNameRunes || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}
