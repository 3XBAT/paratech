// Package strictjson is the strict JSON reader of the quarantined readers (inspect, triage, answer; ADR-0022): exactly one
// object, keys matched byte for byte, no repeated key, nothing after the object. encoding/json alone would match keys
// case-insensitively and keep the last of two equal keys, so a model answer could shadow a field.
package strictjson

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrShape is the cause of every refusal; the caller wraps it with its own sentinel.
var ErrShape = errors.New("not one JSON object of the schema")

// DecodeObject reads data as exactly one JSON object and calls field for each key, in order, with the decoder
// positioned at its value.
func DecodeObject(data string, field func(key string, d *json.Decoder) error) error {
	d := json.NewDecoder(strings.NewReader(data))
	d.UseNumber()
	if err := ReadObject(d, field); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrShape
	}
	return nil
}

// ReadObject reads the next value of d as an object whose keys are distinct; field must consume each value.
func ReadObject(d *json.Decoder, field func(key string, d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return ErrShape
	}
	seen := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return ErrShape
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return ErrShape
		}
		seen[key] = true
		if err := field(key, d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return ErrShape
	}
	return nil
}

// ReadArray reads the next value as an array and calls item for each element.
func ReadArray(d *json.Decoder, item func(d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('[') {
		return ErrShape
	}
	for d.More() {
		if err := item(d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim(']') {
		return ErrShape
	}
	return nil
}

// ReadRaw reads the next value whole, as its raw bytes; null is reported as null. The bytes keep repeated keys, so a
// nested object read later with DecodeObject is checked as strictly.
func ReadRaw(d *json.Decoder) (raw string, null, ok bool) {
	var m json.RawMessage
	if err := d.Decode(&m); err != nil {
		return "", false, false
	}
	return string(m), string(m) == "null", true
}

// ReadString reads the next value as a JSON string.
func ReadString(d *json.Decoder) (string, bool) {
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	s, ok := t.(string)
	return s, ok
}

// ReadNullableString reads the next value as a JSON string or null (present false).
func ReadNullableString(d *json.Decoder) (s string, present, ok bool) {
	t, err := d.Token()
	if err != nil {
		return "", false, false
	}
	if t == nil {
		return "", false, true
	}
	s, ok = t.(string)
	return s, true, ok
}

// ReadBool reads the next value as a JSON boolean.
func ReadBool(d *json.Decoder) (bool, bool) {
	t, err := d.Token()
	if err != nil {
		return false, false
	}
	b, ok := t.(bool)
	return b, ok
}

// ReadInt reads the next value as a JSON integer without fraction or exponent (the decoder must use numbers).
func ReadInt(d *json.Decoder) (int64, bool) {
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

// Unfence trims whitespace and one surrounding Markdown code fence (``` or ```json), which models add despite the
// prompt; what is inside is checked as strictly as without it. ok is false for invalid UTF-8.
func Unfence(output string) (string, bool) {
	if !utf8.ValidString(output) {
		return "", false
	}
	s := strings.TrimSpace(output)
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		rest = strings.TrimPrefix(rest, "json")
		if inner, ok := strings.CutSuffix(rest, "```"); ok && strings.HasPrefix(inner, "\n") {
			s = strings.TrimSpace(inner)
		}
	}
	return s, true
}
