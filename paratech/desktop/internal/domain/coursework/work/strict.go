package work

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The strict JSON reader of the reader answer, as the Hub's (hub domain llm/services/strictjson): exactly one object,
// keys matched byte for byte, no repeated key, nothing after the object. encoding/json alone would match keys
// case-insensitively and keep the last of two equal keys, so an answer could shadow a field.

// errShape is the cause of every refusal; the caller wraps it with ErrInvalidAnswer.
var errShape = errors.New("not one JSON object of the schema")

func decodeObject(data string, field func(key string, d *json.Decoder) error) error {
	d := json.NewDecoder(strings.NewReader(data))
	d.UseNumber()
	if err := readObject(d, field); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errShape
	}
	return nil
}

func readObject(d *json.Decoder, field func(key string, d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return errShape
	}
	seen := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return errShape
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return errShape
		}
		seen[key] = true
		if err := field(key, d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return errShape
	}
	return nil
}

func readArray(d *json.Decoder, item func(d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('[') {
		return errShape
	}
	for d.More() {
		if err := item(d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim(']') {
		return errShape
	}
	return nil
}

// readRaw reads the next value whole; the bytes keep repeated keys, so a nested object read later with decodeObject
// is checked as strictly.
func readRaw(d *json.Decoder) (raw string, null, ok bool) {
	var m json.RawMessage
	if err := d.Decode(&m); err != nil {
		return "", false, false
	}
	return string(m), string(m) == "null", true
}

func readString(d *json.Decoder) (string, bool) {
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	s, ok := t.(string)
	return s, ok
}

// readInt reads an integer without fraction or exponent (the decoder uses numbers).
func readInt(d *json.Decoder) (int64, bool) {
	t, err := d.Token()
	if err != nil {
		return 0, false
	}
	n, ok := t.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return i, err == nil
}

// readTexts reads an array of minItems..maxItems non-blank texts of at most maxRunes each.
func readTexts(d *json.Decoder, minItems, maxItems, maxRunes int, multiline bool) ([]string, bool) {
	out := []string{}
	err := readArray(d, func(d *json.Decoder) error {
		s, ok := readString(d)
		if !ok || len(out) == maxItems || !boundedText(s, maxRunes, multiline, false) {
			return errShape
		}
		out = append(out, s)
		return nil
	})
	return out, err == nil && len(out) >= minItems
}

// boundedText is valid UTF-8 of at most maxRunes characters without control characters (a multiline text may hold
// line breaks and tabs), non-blank unless empty is allowed; an allowed empty value is exactly "". The Hub's rule.
func boundedText(s string, maxRunes int, multiline, empty bool) bool {
	if s == "" {
		return empty
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	blank := true
	for _, r := range s {
		lineBreak := multiline && (r == '\n' || r == '\t')
		if unicode.IsControl(r) && !lineBreak {
			return false
		}
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	return !blank
}
