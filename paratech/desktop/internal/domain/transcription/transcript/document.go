package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// Limits and version of the transcript document.
const (
	// DocumentFormatVersion is the version of the stored document format.
	DocumentFormatVersion = 1

	// MaxDocumentBytes bounds a stored document; a 12-hour lecture is a few MiB.
	MaxDocumentBytes = 64 << 20

	// MaxSegments bounds the number of segments of one document.
	MaxSegments = 500_000

	// MaxSegmentTextBytes bounds the text of one segment.
	MaxSegmentTextBytes = 16 << 10

	// MaxOffsetMs bounds timestamps: one day is far above the longest recording.
	MaxOffsetMs = 24 * 60 * 60 * 1000
)

// Segment is one recognised piece of speech with its time span in milliseconds from the start of the audio.
// NewDocument validates it.
type Segment struct {
	StartMs int64
	EndMs   int64
	Text    string
}

// Document is the recognised text of one recording: an optional language and segments ordered by start time.
// It is a validated value; the zero value is an empty document without language.
type Document struct {
	language Language
	segments []Segment
}

// NewDocument validates the segments: 0 <= start <= end <= MaxOffsetMs, starts never decrease, text is valid UTF-8
// without control characters and at most MaxSegmentTextBytes long.
func NewDocument(lang Language, segments []Segment) (Document, error) {
	if err := lang.Validate(); err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	if len(segments) > MaxSegments {
		return Document{}, fmt.Errorf("%w: %d segments", ErrInvalidDocument, len(segments))
	}

	prev := int64(0)
	for i, s := range segments {
		if err := checkSegment(s, prev); err != nil {
			return Document{}, fmt.Errorf("%w: segment %d: %w", ErrInvalidDocument, i, err)
		}
		prev = s.StartMs
	}

	return Document{language: lang, segments: append([]Segment(nil), segments...)}, nil
}

func checkSegment(s Segment, prevStart int64) error {
	switch {
	case s.StartMs < 0 || s.EndMs < 0:
		return errors.New("negative timestamp")
	case s.StartMs > MaxOffsetMs || s.EndMs > MaxOffsetMs:
		return errors.New("timestamp out of range")
	case s.EndMs < s.StartMs:
		return errors.New("ends before it starts")
	case s.StartMs < prevStart:
		return errors.New("starts before the previous segment")
	case len(s.Text) > MaxSegmentTextBytes:
		return errors.New("text too long")
	case !utf8.ValidString(s.Text):
		return errors.New("text is not valid UTF-8")
	case containsControl(s.Text):
		return errors.New("text contains a control character")
	}
	return nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// Language returns the detected language, empty if unknown.
func (d Document) Language() Language { return d.language }

// Segments returns a copy of the segments.
func (d Document) Segments() []Segment { return append([]Segment(nil), d.segments...) }

// wireDocument is the stored JSON form. Pointers make missing required fields detectable on parse.
type wireDocument struct {
	Version  *int           `json:"version"`
	Language string         `json:"language,omitempty"`
	Segments *[]wireSegment `json:"segments"`
}

type wireSegment struct {
	StartMs *int64  `json:"start_ms"`
	EndMs   *int64  `json:"end_ms"`
	Text    *string `json:"text"`
}

// Encode renders the stored JSON form (indented, one trailing newline).
func (d Document) Encode() ([]byte, error) {
	version := DocumentFormatVersion
	segs := make([]wireSegment, len(d.segments))
	for i := range d.segments {
		s := &d.segments[i]
		segs[i] = wireSegment{StartMs: &s.StartMs, EndMs: &s.EndMs, Text: &s.Text}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(wireDocument{Version: &version, Language: string(d.language), Segments: &segs}); err != nil {
		return nil, fmt.Errorf("%w: encode: %w", ErrInvalidDocument, err)
	}
	return buf.Bytes(), nil
}

// ParseDocument reads the stored JSON form strictly: at most MaxDocumentBytes, format version DocumentFormatVersion,
// every field present, no unknown fields, no trailing data, and the rules of NewDocument.
func ParseDocument(data []byte) (Document, error) {
	if len(data) > MaxDocumentBytes {
		return Document{}, fmt.Errorf("%w: more than %d bytes", ErrInvalidDocument, MaxDocumentBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var w wireDocument
	if err := dec.Decode(&w); err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Document{}, fmt.Errorf("%w: data after the document", ErrInvalidDocument)
	}

	switch {
	case w.Version == nil:
		return Document{}, fmt.Errorf("%w: no format version", ErrInvalidDocument)
	case *w.Version != DocumentFormatVersion:
		return Document{}, fmt.Errorf("%w: format version %d", ErrInvalidDocument, *w.Version)
	case w.Segments == nil:
		return Document{}, fmt.Errorf("%w: no segments", ErrInvalidDocument)
	}

	segs := make([]Segment, len(*w.Segments))
	for i, s := range *w.Segments {
		if s.StartMs == nil || s.EndMs == nil || s.Text == nil {
			return Document{}, fmt.Errorf("%w: segment %d: missing field", ErrInvalidDocument, i)
		}
		segs[i] = Segment{StartMs: *s.StartMs, EndMs: *s.EndMs, Text: *s.Text}
	}

	return NewDocument(Language(w.Language), segs)
}
