package inspect

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// errJSON is the cause of every refusal of the strict reader; the caller wraps it with its own sentinel.
var errJSON = errors.New("not one JSON object of the schema")

// decodeObject reads data as exactly one JSON object and calls field for each key, in order, with the decoder
// positioned at its value. Keys match byte for byte (encoding/json would also match other cases); a repeated key or
// anything after the object is refused, so no key can shadow another.
func decodeObject(data string, field func(key string, d *json.Decoder) error) error {
	d := json.NewDecoder(strings.NewReader(data))
	d.UseNumber()
	if err := readObject(d, field); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errJSON
	}
	return nil
}

// readObject reads the next value of d as an object whose keys are distinct; field must consume each value.
func readObject(d *json.Decoder, field func(key string, d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return errJSON
	}
	seen := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return errJSON
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return errJSON
		}
		seen[key] = true
		if err := field(key, d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return errJSON
	}
	return nil
}

// readString reads the next value as a JSON string.
func readString(d *json.Decoder) (string, bool) {
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	s, ok := t.(string)
	return s, ok
}

// readNullableString reads the next value as a JSON string or null (present false).
func readNullableString(d *json.Decoder) (s string, present, ok bool) {
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

// readBool reads the next value as a JSON boolean.
func readBool(d *json.Decoder) (bool, bool) {
	t, err := d.Token()
	if err != nil {
		return false, false
	}
	b, ok := t.(bool)
	return b, ok
}

// readArray reads the next value as an array and calls item for each element.
func readArray(d *json.Decoder, item func(d *json.Decoder) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('[') {
		return errJSON
	}
	for d.More() {
		if err := item(d); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim(']') {
		return errJSON
	}
	return nil
}
