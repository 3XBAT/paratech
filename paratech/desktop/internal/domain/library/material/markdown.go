package material

import (
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
)

// Limits of the Markdown reader.
const (
	// MaxHintRunes bounds one hint of a front matter.
	MaxHintRunes = 200

	// MaxHintTags bounds the tags of a front matter.
	MaxHintTags = 20

	// maxFrontMatterLines bounds the lines a front matter may have; a longer block is text.
	maxFrontMatterLines = 200

	maxYAMLKeyBytes = 64
)

// Hints are what the front matter of a note says about it. They are untrusted strings, at most MaxHintRunes each, for
// matching the note on the desktop only (ADR-0027 «Bulk import»); they are not part of the text.
type Hints struct {
	Date    string
	Subject string
	Tags    []string
}

// Markdown is a note read by ReadMarkdown: its fragments for NewText and the hints of its front matter.
type Markdown struct {
	Fragments []Fragment
	Hints     Hints
}

// ReadMarkdown reads a text note (TXT or Markdown, Obsidian or Notion export) as plain text, never as HTML: a front
// matter (a block between «---» lines at the top whose lines all look like YAML with ASCII keys, one of them a known
// note key) is dropped and its date, subject and
// tags become Hints; «[[x|y]]» reads y and «[[x]]» x; embeds «![[…]]» and «![…](…)», HTML tags and comments are
// dropped; heading markers are dropped. With at least two headings of levels 1–3 the note is cut into sections
// (SectionsOf), else into fragments (FragmentsOf). Fenced code is kept as it is. Text without Markdown reads as
// before; the output is never longer than s.
func ReadMarkdown(s string) Markdown {
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s), "\n")
	var md Markdown
	if end, ok := frontMatter(lines); ok {
		md.Hints = hintsOf(lines[1:end])
		lines = lines[end+1:]
	}

	out := make([]string, 0, len(lines))
	var (
		heads []int
		fence string
	)
	for _, l := range lines {
		lead := strings.TrimLeft(l, " \t")
		if fence != "" {
			if closesFence(lead, fence) {
				fence = ""
			}
			out = append(out, l)
			continue
		}
		if fence = opensFence(lead); fence != "" {
			out = append(out, l)
			continue
		}
		if level, text, ok := atxHeading(l); ok {
			if level <= 3 {
				heads = append(heads, len(out))
			}
			out = append(out, inline(text))
			continue
		}
		out = append(out, inline(l))
	}

	if fs, ok := SectionsOf(out, heads); ok {
		md.Fragments = fs
	} else {
		md.Fragments = FragmentsOf(out)
	}
	return md
}

// --- Front matter -------------------------------------------------------------------------------------------

// frontMatter returns the index of the line that closes a front matter opened by the first line. A block counts only
// when every key is ASCII and one of them is a known note key, so a plain text between two rules («Задание: …») stays
// text.
func frontMatter(lines []string) (int, bool) {
	if len(lines) < 2 || strings.TrimRight(lines[0], " \t") != "---" {
		return 0, false
	}
	known := false
	for i := 1; i < len(lines) && i <= maxFrontMatterLines+1; i++ {
		l := strings.TrimRight(lines[i], " \t")
		if l == "---" || l == "..." {
			return i, known
		}
		key, ok := yamlLine(lines[i])
		if !ok {
			return 0, false // a text between two rules, not a front matter
		}
		known = known || knownKey(key)
	}
	return 0, false
}

// yamlLine reports whether l looks like a line of a simple YAML map: blank, a comment, an indented or list line, or
// «key: value» with an ASCII key, which it returns.
func yamlLine(l string) (string, bool) {
	t := strings.TrimSpace(l)
	switch {
	case t == "", t[0] == '#', l[0] == ' ', l[0] == '\t', strings.HasPrefix(t, "- "), t == "-":
		return "", true
	}
	key, _, ok := strings.Cut(l, ":")
	key = strings.TrimSpace(key)
	return key, ok && yamlKey(key)
}

func yamlKey(k string) bool {
	if k == "" || len(k) > maxYAMLKeyBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		if c := k[i]; !isASCIILetter(c) && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// knownKey reports whether k is a key of a note front matter (Obsidian, Notion export).
func knownKey(k string) bool {
	switch strings.ToLower(k) {
	case "title", "date", "tags", "subject", "aliases", "created":
		return true
	}
	return false
}

// hintsOf reads date, subject and tags of the front matter lines; anything else is ignored.
func hintsOf(lines []string) Hints {
	var (
		h   Hints
		key string
	)
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || t[0] == '#' {
			continue
		}
		if item, ok := strings.CutPrefix(t, "-"); ok && (l[0] == ' ' || l[0] == '\t' || l[0] == '-') {
			if key == "tags" {
				h.Tags = addTag(h.Tags, item)
			}
			continue
		}
		if l[0] == ' ' || l[0] == '\t' {
			continue // a continuation of a value: not read
		}
		k, v, _ := strings.Cut(l, ":")
		key = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch key {
		case "date":
			h.Date = hint(v)
		case "subject":
			h.Subject = hint(v)
		case "tags":
			v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
			for _, tag := range strings.Split(v, ",") {
				h.Tags = addTag(h.Tags, tag)
			}
		}
	}
	return h
}

func addTag(tags []string, s string) []string {
	if len(tags) == MaxHintTags {
		return tags
	}
	if t := hint(strings.TrimPrefix(strings.TrimSpace(s), "#")); t != "" {
		tags = append(tags, t)
	}
	return tags
}

// hint is a front matter value as one plain line without its quotes, at most MaxHintRunes.
func hint(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	v = values.CleanText(v)
	if utf8.RuneCountInString(v) > MaxHintRunes {
		v = strings.TrimSpace(string([]rune(v)[:MaxHintRunes]))
	}
	return v
}

// --- Blocks -------------------------------------------------------------------------------------------------

// opensFence returns the marker of a code fence the line opens («```» or «~~~», three or more), or "".
func opensFence(lead string) string {
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(lead) && lead[n] == c {
			n++
		}
		if n >= 3 {
			return lead[:n]
		}
	}
	return ""
}

// closesFence reports whether the line closes the fence: the same character at least as many times, nothing after.
func closesFence(lead, fence string) bool {
	n := 0
	for n < len(lead) && lead[n] == fence[0] {
		n++
	}
	return n >= len(fence) && strings.TrimSpace(lead[n:]) == ""
}

// atxHeading reads «# text» (up to three spaces before, one to six «#», then a space or the end): the level and the
// text without the markers, the closing ones too.
func atxHeading(l string) (int, string, bool) {
	i := 0
	for i < 3 && i < len(l) && l[i] == ' ' {
		i++
	}
	n := 0
	for i+n < len(l) && l[i+n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	rest := l[i+n:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	rest = strings.TrimSpace(rest)
	if trimmed := strings.TrimRight(rest, "#"); trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t") {
		rest = strings.TrimSpace(trimmed)
	}
	return n, rest, true
}

// --- Inline -------------------------------------------------------------------------------------------------

// maxTagBytes bounds an HTML tag; a longer one stays text.
const maxTagBytes = 1024

// finder finds the next sub in s at or after a position; searches from rising positions reuse the last answer, so a
// line full of unclosed «[[» is read in linear time.
type finder struct {
	s, sub   string
	from, at int // the last search: from where, and its answer (-1: none after from)
	searched bool
}

func (f *finder) next(from int) int {
	if f.searched && from >= f.from && (f.at < 0 || f.at >= from) {
		return f.at
	}
	f.searched, f.from, f.at = true, from, -1
	if from <= len(f.s) {
		if i := strings.Index(f.s[from:], f.sub); i >= 0 {
			f.at = from + i
		}
	}
	return f.at
}

// inline drops embeds and HTML of a line and turns wiki links into their text.
func inline(l string) string {
	if !strings.ContainsAny(l, "[<!") {
		return l
	}
	var (
		b       strings.Builder
		wikiEnd = finder{s: l, sub: "]]"}
		wikiNew = finder{s: l, sub: "[["}
		altEnd  = finder{s: l, sub: "]"}
		urlEnd  = finder{s: l, sub: ")"}
		comment = finder{s: l, sub: "-->"}
	)
	for i := 0; i < len(l); {
		rest := l[i:]
		switch {
		case strings.HasPrefix(rest, "![["):
			if end := wikiEnd.next(i + 3); end >= 0 {
				i = end + 2
				continue
			}
		case strings.HasPrefix(rest, "[["):
			if end := wikiEnd.next(i + 2); end >= 0 {
				if inner := wikiNew.next(i + 2); inner < 0 || inner+2 > end {
					target, label, ok := strings.Cut(l[i+2:end], "|")
					if ok {
						target = label
					}
					b.WriteString(target)
					i = end + 2
					continue
				}
			}
		case strings.HasPrefix(rest, "!["):
			// «![alt](target)»
			if alt := altEnd.next(i + 2); alt >= 0 && strings.HasPrefix(l[alt+1:], "(") {
				if end := urlEnd.next(alt + 2); end >= 0 {
					i = end + 1
					continue
				}
			}
		case strings.HasPrefix(rest, "<!--"):
			if end := comment.next(i + 4); end >= 0 {
				i = end + 3
				continue
			}
		case rest[0] == '<':
			if n := tagLen(rest[:min(len(rest), maxTagBytes)]); n > 0 {
				i += n
				continue
			}
		}
		b.WriteByte(l[i])
		i++
	}
	return b.String()
}

// tagLen returns the length of a tag of a known element with quoted or plain attribute values only at the start of
// s, or 0. The grammar is strict so that «a<b and c>d» stays text.
func tagLen(s string) int {
	i := 1
	closing := i < len(s) && s[i] == '/'
	if closing {
		i++
	}
	start := i
	for i < len(s) && (isASCIILetter(s[i]) || (i > start && s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == start || !htmlElement(strings.ToLower(s[start:i])) {
		return 0
	}
	for {
		spaces := i
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		switch {
		case i >= len(s):
			return 0
		case s[i] == '>':
			return i + 1
		case !closing && strings.HasPrefix(s[i:], "/>"):
			return i + 2
		case closing || i == spaces:
			return 0 // a closing tag has no attributes; an attribute follows a space
		}
		if i = attrEnd(s, i); i < 0 {
			return 0
		}
	}
}

// attrEnd reads «name=value» at s[i:] and returns the index after it, or -1.
func attrEnd(s string, i int) int {
	start := i
	for i < len(s) && (isASCIILetter(s[i]) || (i > start && (s[i] == '-' || s[i] == '_' || s[i] == ':' || (s[i] >= '0' && s[i] <= '9')))) {
		i++
	}
	if i == start || i >= len(s) || s[i] != '=' {
		return -1
	}
	i++
	if i >= len(s) {
		return -1
	}
	if q := s[i]; q == '"' || q == '\'' {
		end := strings.IndexByte(s[i+1:], q)
		if end < 0 {
			return -1
		}
		return i + 1 + end + 1
	}
	start = i
	for i < len(s) && !strings.ContainsRune(" \t\"'=<>`", rune(s[i])) {
		i++
	}
	if i == start {
		return -1
	}
	return i
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// htmlElement reports whether name is an HTML element a note export may hold.
func htmlElement(name string) bool {
	switch name {
	case "a", "abbr", "aside", "audio", "b", "big", "blockquote", "body", "br", "button", "caption", "center", "cite",
		"code", "col", "colgroup", "dd", "del", "details", "div", "dl", "dt", "em", "embed", "figcaption", "figure",
		"font", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html", "i", "iframe",
		"img", "input", "ins", "kbd", "label", "li", "link", "mark", "meta", "nav", "object", "ol", "p", "picture",
		"pre", "q", "s", "script", "section", "small", "source", "span", "strike", "strong", "style", "sub", "summary",
		"sup", "svg", "table", "tbody", "td", "tfoot", "th", "thead", "title", "tr", "u", "ul", "video", "wbr":
		return true
	}
	return false
}
