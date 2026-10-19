package shared

import "strings"

// markdownActive are the characters escaped everywhere: emphasis, code, links and images, raw HTML and autolinks,
// tables, strikethrough, headings (also a closing "#" run), quotes, entities and the backslash itself.
const markdownActive = "\\`*_[]<>!|~&#"

// markdownLineStart are escaped at the start of a line, where they open lists or rules.
const markdownLineStart = "+-="

// EscapeMarkdown makes untrusted text (recognised speech, model output) literal in a Markdown file: it can form
// neither HTML nor links nor other markup. s is one line; the line-start rules apply to its beginning.
//
// The active characters are backslash-escaped (CommonMark allows it for every ASCII punctuation character, and the
// escaped character renders as itself). "&" is written as "&amp;" instead, so an entity in the text stays literal.
// GFM autolinks are broken up: the first slash of "://" and the dot of "www." are escaped, and "@" is escaped as well
// (email autolinks); this is best effort, a renderer with other extensions may still find a link.
func EscapeMarkdown(s string) string {
	var b strings.Builder
	start := true
	for i, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case strings.ContainsRune(markdownActive, r),
			start && strings.ContainsRune(markdownLineStart, r),
			r == '@',
			r == '/' && strings.HasSuffix(s[:i], ":"),
			r == '.' && len(s[:i]) >= 3 && strings.EqualFold(s[i-3:i], "www"):
			b.WriteByte('\\')
			b.WriteRune(r)
		case start && r >= '0' && r <= '9':
			// "1." or "1)" at the start would be an ordered list.
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			b.WriteString(s[i:j])
			if j < len(s) && (s[j] == '.' || s[j] == ')') {
				b.WriteByte('\\')
			}
			b.WriteString(EscapeMarkdown(s[j:]))
			return b.String()
		default:
			b.WriteRune(r)
		}
		start = false
	}
	return b.String()
}
