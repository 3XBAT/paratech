package answer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The reader's answer has no count or length limits of its own (owner, TASK-366): only the output cap of the model
// (MaxOutputTokens, clamped by the Hub) and the size of a Hub response bound it.

// Keys of the answer.
const (
	keyClaims  = "claims"
	keyMissing = "missing"
	keyText    = "text"
	keyCites   = "cites"
	keyChunkID = "chunk_id"
	keyQuote   = "quote"
)

// ReplyCite is a citation as the reader gave it: the chunk ID of a fragment sent and the quote. Not yet checked.
type ReplyCite struct {
	ChunkID string
	Quote   string
}

// ReplyClaim is one claim as the reader gave it.
type ReplyClaim struct {
	Text  string
	Cites []ReplyCite
}

// Reply is the reader's answer after the schema check, before the citation check (New). Dropped counts the claims,
// citations and missing parts the check left out.
type Reply struct {
	Claims  []ReplyClaim
	Missing []string
	Dropped int
}

// Parse checks the reader's output against the schema the Hub returns (canonical JSON). The whole answer is refused
// with ErrInvalidAnswer unless it is exactly one JSON object with the keys claims and missing once each, both arrays
// of syntactically valid JSON. Inside them each part is checked on its own and dropped when it fails, as the Hub does:
// a citation that is not exactly chunk_id (a fragment of r) and quote (plain text), a claim that is not exactly text
// (plain text) and cites or keeps no citation, a missing part that is not plain text. Plain text is valid UTF-8, not
// blank, without control characters but line breaks and tabs. An answer that keeps no claim and no missing part is
// refused. Whether a quote is in its fragment is New's check. Every part is read once, so the cost is linear in the
// output. The error never quotes the output.
func (r Request) Parse(output string) (Reply, error) {
	var (
		rep  Reply
		seen = map[string]bool{}
	)
	err := decodeObject(output, func(key string, d *json.Decoder) error {
		seen[key] = true
		ok := false
		switch key {
		case keyClaims:
			rep.Claims = []ReplyClaim{}
			ok = readArray(d, func(d *json.Decoder) error {
				raw, read := readRaw(d)
				if !read {
					return errShape
				}
				c, dropped, valid := r.checkClaim(raw)
				rep.Dropped += dropped
				if valid {
					rep.Claims = append(rep.Claims, c)
				}
				return nil
			}) == nil
		case keyMissing:
			rep.Missing = []string{}
			ok = readArray(d, func(d *json.Decoder) error {
				raw, read := readRaw(d)
				if !read {
					return errShape
				}
				var s string
				if json.Unmarshal(raw, &s) != nil || !plainText(s) {
					rep.Dropped++
					return nil
				}
				rep.Missing = append(rep.Missing, s)
				return nil
			}) == nil
		}
		if !ok {
			return fmt.Errorf("%w: %s is not a value of the schema", ErrInvalidAnswer, safeKey(key))
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAnswer) {
			return Reply{}, err
		}
		return Reply{}, fmt.Errorf("%w: %w", ErrInvalidAnswer, err)
	}
	if !seen[keyClaims] || !seen[keyMissing] {
		return Reply{}, fmt.Errorf("%w: claims and missing are required", ErrInvalidAnswer)
	}
	if len(rep.Claims) == 0 && len(rep.Missing) == 0 {
		return Reply{}, fmt.Errorf("%w: an answer has a claim or a missing part", ErrInvalidAnswer)
	}
	return rep, nil
}

// checkClaim reads one element of claims: the claim with its valid citations, how many parts were dropped (the
// citations that failed, and the claim itself when it fails or keeps no citation), and whether the claim is kept.
func (r Request) checkClaim(raw json.RawMessage) (ReplyClaim, int, bool) {
	var (
		c       ReplyClaim
		dropped int
		seen    = map[string]bool{}
	)
	err := decodeObject(string(raw), func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyText:
			c.Text, ok = readString(d)
			ok = ok && plainText(c.Text)
		case keyCites:
			c.Cites = []ReplyCite{}
			ok = readArray(d, func(d *json.Decoder) error {
				raw, read := readRaw(d)
				if !read {
					return errShape
				}
				if cite, valid := r.checkCite(raw); valid {
					c.Cites = append(c.Cites, cite)
				} else {
					dropped++
				}
				return nil
			}) == nil
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || !seen[keyText] || !seen[keyCites] || len(c.Cites) == 0 {
		return ReplyClaim{}, dropped + 1, false
	}
	return c, dropped, true
}

// checkCite reads one element of cites: exactly chunk_id, a fragment of r, and quote, plain text.
func (r Request) checkCite(raw json.RawMessage) (ReplyCite, bool) {
	var (
		c    ReplyCite
		seen = map[string]bool{}
	)
	err := decodeObject(string(raw), func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyChunkID:
			c.ChunkID, ok = readString(d)
			// Only a fragment that was sent: the reader cannot point a citation at another lesson.
			_, sent := r.fragment(c.ChunkID)
			ok = ok && sent
		case keyQuote:
			c.Quote, ok = readString(d)
			ok = ok && plainText(c.Quote)
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || !seen[keyChunkID] || !seen[keyQuote] {
		return ReplyCite{}, false
	}
	return c, true
}

// safeKey names a schema key in an error; any other key (model text) is not quoted.
func safeKey(key string) string {
	if key == keyClaims || key == keyMissing {
		return key
	}
	return "an unknown key"
}

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

func readString(d *json.Decoder) (string, bool) {
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	s, ok := t.(string)
	return s, ok
}

// readRaw reads the next value whole, checked as JSON syntax only; false for broken JSON.
func readRaw(d *json.Decoder) (json.RawMessage, bool) {
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		return nil, false
	}
	return raw, true
}

// plainText is valid UTF-8, not blank, without control characters but line breaks and tabs. The Hub's rule.
func plainText(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	blank := true
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	return !blank
}
