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

// Limits of the reader's answer, those the Hub checks.
const (
	// MaxClaims bounds the claims of one answer.
	MaxClaims = 8
	// MaxClaimRunes bounds the text of one claim.
	MaxClaimRunes = 400
	// MaxCites bounds the citations of one claim; the reader gives at least one.
	MaxCites = 3
	// MaxQuoteRunes bounds one quote.
	MaxQuoteRunes = 200
	// MaxMissing bounds the unanswered parts; MaxMissingRunes bounds each.
	MaxMissing, MaxMissingRunes = 5, 300
)

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

// Reply is the reader's answer after the schema check, before the citation check (New).
type Reply struct {
	Claims  []ReplyClaim
	Missing []string
}

// Parse checks the reader's output against the schema the Hub returns (canonical JSON): one object with exactly the
// keys claims and missing once each; at most MaxClaims claims of exactly text (1..MaxClaimRunes) and cites
// (1..MaxCites of exactly chunk_id, a fragment of r, and quote, 1..MaxQuoteRunes); at most MaxMissing parts of
// 1..MaxMissingRunes; every text not blank, without control characters but line breaks and tabs; at least one claim or
// one part. Anything else refuses the whole answer with ErrInvalidAnswer; the error never quotes the output.
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
				c, err := r.readClaim(d)
				if err != nil || len(rep.Claims) == MaxClaims {
					return errShape
				}
				rep.Claims = append(rep.Claims, c)
				return nil
			}) == nil
		case keyMissing:
			rep.Missing = []string{}
			ok = readArray(d, func(d *json.Decoder) error {
				s, read := readString(d)
				if !read || len(rep.Missing) == MaxMissing || !boundedText(s, MaxMissingRunes) {
					return errShape
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

func (r Request) readClaim(d *json.Decoder) (ReplyClaim, error) {
	var c ReplyClaim
	seen := map[string]bool{}
	err := readObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyText:
			c.Text, ok = readString(d)
			ok = ok && boundedText(c.Text, MaxClaimRunes)
		case keyCites:
			c.Cites = []ReplyCite{}
			ok = readArray(d, func(d *json.Decoder) error {
				cite, err := r.readCite(d)
				if err != nil || len(c.Cites) == MaxCites {
					return errShape
				}
				c.Cites = append(c.Cites, cite)
				return nil
			}) == nil
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || len(seen) != 2 || !seen[keyText] || !seen[keyCites] || len(c.Cites) == 0 {
		return ReplyClaim{}, errShape
	}
	return c, nil
}

func (r Request) readCite(d *json.Decoder) (ReplyCite, error) {
	var c ReplyCite
	seen := map[string]bool{}
	err := readObject(d, func(key string, d *json.Decoder) error {
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
			ok = ok && boundedText(c.Quote, MaxQuoteRunes)
		}
		if !ok {
			return errShape
		}
		return nil
	})
	if err != nil || len(seen) != 2 || !seen[keyChunkID] || !seen[keyQuote] {
		return ReplyCite{}, errShape
	}
	return c, nil
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

// boundedText is valid UTF-8 of 1..maxRunes characters, not blank, without control characters but line breaks and
// tabs. The Hub's rule.
func boundedText(s string, maxRunes int) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
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
