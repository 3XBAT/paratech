package toolcatalog

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/values"
)

// ellipsis marks a reply text the Hub cut (TASK-223).
const ellipsis = "…"

// Shrinking of a cut text that is still over values.MaxArgumentsBytes once encoded (multi-byte runes, escaped line
// breaks): the rune limit drops by a tenth each round, down to minFitRunes, below which the call is left to fail.
const minFitRunes = 500

// longReply reports whether reply.text is a reply_text (catalog replyTextFrom on).
func (c *Catalog) longReply() bool {
	t, ok := c.tools[toolReply]
	if !ok || t.Class != classUI {
		return false
	}
	for _, a := range t.Arguments {
		if a.Name == argReplyText {
			return a.Type == typeReplyText
		}
	}
	return false
}

// FitReturned cuts the text of a returned reply call over MaxReplyTextRunes, or whose arguments are over
// values.MaxArgumentsBytes, at a sentence and marks it with "…", in a catalog whose reply.text is a reply_text; cut
// reports whether any call changed. Every other call, and every call of
// an older catalog, is returned as it is: CheckReturned still decides about all of them, so a reply whose arguments do
// not decode or whose text is not a string stays the provider's failure.
func (c *Catalog) FitReturned(calls []values.ToolCall) (out []values.ToolCall, cut bool) {
	out = append([]values.ToolCall(nil), calls...)
	if !c.longReply() {
		return out, false
	}
	for i, call := range out {
		if call.Tool() != toolReply {
			continue
		}
		args, err := decodeObject(call.Arguments())
		if err != nil {
			continue
		}
		text, ok := args[argReplyText].(string)
		if !ok || !utf8.ValidString(text) ||
			(utf8.RuneCountInString(text) <= MaxReplyTextRunes && len(call.Arguments()) <= values.MaxArgumentsBytes) {
			continue
		}
		encoded, changed, err := fitReplyText(args, text)
		if err != nil || !changed {
			continue
		}
		fitted, err := values.NewToolCall(call.CallID(), call.Tool(), encoded)
		if err != nil {
			continue
		}
		out[i], cut = fitted, true
	}
	return out, cut
}

// fitReplyText puts text into args as reply.text, cut so that it has at most MaxReplyTextRunes characters and the
// encoded arguments fit values.MaxArgumentsBytes when possible; changed reports a cut. Returns the encoded arguments.
func fitReplyText(args map[string]any, text string) (encoded string, changed bool, err error) {
	limit := MaxReplyTextRunes
	for {
		fitted := cutAtSentence(text, limit)
		args[argReplyText] = fitted
		encoded, err = encodeObject(args)
		if err != nil {
			return "", false, err
		}
		if len(encoded) <= values.MaxArgumentsBytes || limit <= minFitRunes {
			return encoded, fitted != text, nil
		}
		limit = max(limit-limit/10, minFitRunes)
	}
}

// cutAtSentence returns s when it has at most maxRunes characters. Otherwise it keeps the longest start of s that ends
// a sentence (".", "!", "?" or "…" before a space or a line break, or the end of a line) and adds " …"; without such an
// end in the second half of the room it cuts at a space, and without a space there at a character, adding "…". The
// result has at most maxRunes characters and is cut only between characters, never inside a UTF-8 sequence.
func cutAtSentence(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	room := maxRunes - 2 // " …"
	floor := room / 2

	sentence, space := -1, -1
	for i := room; i >= floor && i > 0; i-- {
		// A cut at i keeps runes[:i].
		if sentence < 0 && (runes[i] == '\n' || (unicode.IsSpace(runes[i]) && strings.ContainsRune(".!?…", runes[i-1]))) {
			sentence = i
		}
		if space < 0 && unicode.IsSpace(runes[i]) {
			space = i
		}
		if sentence >= 0 {
			break
		}
	}

	switch {
	case sentence >= 0:
		kept := strings.TrimRightFunc(string(runes[:sentence]), unicode.IsSpace)
		if strings.HasSuffix(kept, ellipsis) {
			return kept
		}
		if strings.ContainsRune(".!?", lastRune(kept)) {
			return kept + " " + ellipsis
		}
		return kept + ellipsis
	case space >= 0:
		return strings.TrimRightFunc(string(runes[:space]), unicode.IsSpace) + ellipsis
	default:
		return string(runes[:room]) + ellipsis
	}
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// encodeObject is the canonical form of returned arguments (the anthropic adapter): sorted keys, exact numbers kept as
// json.Number, no HTML escaping, so "<" and ">" stay as they are.
func encodeObject(v map[string]any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
