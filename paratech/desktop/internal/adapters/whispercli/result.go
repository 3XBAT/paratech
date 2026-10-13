package whispercli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

const (
	// defaultMaxOutput bounds the CLI result file; a 12-hour lecture is a few MiB.
	defaultMaxOutput = 64 << 20

	// maxSegments bounds the number of segments of one result.
	maxSegments = 500_000

	// maxSegmentText bounds the text of one segment in bytes.
	maxSegmentText = 16 << 10

	// maxOffsetMs bounds timestamps: one day is far above the longest recording.
	maxOffsetMs = 24 * 60 * 60 * 1000

	// TranscriptFormatVersion is the version of the transcript document written to the output artifact.
	TranscriptFormatVersion = 1
)

// Transcript is the document the adapter stores as the output artifact (JSON). It is the normalised, validated
// form of the CLI result; the CLI's own JSON never leaves the adapter.
type Transcript struct {
	Version  int       `json:"version"`
	Language string    `json:"language,omitempty"`
	Segments []Segment `json:"segments"`
}

// Segment is one recognised piece of speech with its time span in milliseconds from the start of the audio.
type Segment struct {
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// cliResult is the part of the -oj output the adapter uses (examples/cli/cli.cpp output_json, v1.9.5). Pointers
// make missing required fields detectable; other fields of the CLI are ignored as they vary between versions.
type cliResult struct {
	Result *struct {
		Language *string `json:"language"`
	} `json:"result"`
	Transcription *[]cliSegment `json:"transcription"`
}

type cliSegment struct {
	Offsets *struct {
		From *int64 `json:"from"`
		To   *int64 `json:"to"`
	} `json:"offsets"`
	Text *string `json:"text"`
}

// readResult reads the result file with a size limit. A missing file yields an error wrapping fs.ErrNotExist.
func readResult(path string, limit int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: result is not a regular file", ErrOutput)
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%w: %d bytes", ErrOutputTooLarge, fi.Size())
	}

	f, err := os.Open(path) //nolint:gosec // path is the adapter's own run directory
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %w", ErrOutput, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrOutputTooLarge, limit)
	}

	return data, nil
}

// parseResult validates the CLI JSON and converts it into a Transcript with non-decreasing starts.
func parseResult(data []byte) (Transcript, error) {
	dec := json.NewDecoder(bytes.NewReader(escapeControls(data)))

	var raw cliResult
	if err := dec.Decode(&raw); err != nil {
		return Transcript{}, fmt.Errorf("%w: %w", ErrOutput, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Transcript{}, fmt.Errorf("%w: data after the result object", ErrOutput)
	}

	if raw.Transcription == nil {
		return Transcript{}, fmt.Errorf("%w: no transcription array", ErrOutput)
	}
	segs := *raw.Transcription
	if len(segs) > maxSegments {
		return Transcript{}, fmt.Errorf("%w: %d segments", ErrOutputTooLarge, len(segs))
	}

	out := Transcript{Version: TranscriptFormatVersion, Segments: make([]Segment, 0, len(segs))}
	if raw.Result != nil && raw.Result.Language != nil && languages[*raw.Result.Language] {
		out.Language = *raw.Result.Language
	}

	prev := int64(0)
	for i, s := range segs {
		seg, err := convertSegment(s, prev)
		if err != nil {
			return Transcript{}, fmt.Errorf("%w: segment %d: %w", ErrOutput, i, err)
		}
		prev = seg.StartMs
		out.Segments = append(out.Segments, seg)
	}

	return out, nil
}

// convertSegment checks one segment: required fields and 0 <= from, to <= limit. An odd order is clamped, not
// rejected, so one strange segment does not fail a whole lecture: the start is raised to the previous start and
// the end to the start.
func convertSegment(s cliSegment, prevStart int64) (Segment, error) {
	if s.Offsets == nil || s.Offsets.From == nil || s.Offsets.To == nil || s.Text == nil {
		return Segment{}, errors.New("missing offsets or text")
	}

	from, to := *s.Offsets.From, *s.Offsets.To
	switch {
	case from < 0 || to < 0:
		return Segment{}, errors.New("negative timestamp")
	case from > maxOffsetMs || to > maxOffsetMs:
		return Segment{}, errors.New("timestamp out of range")
	}

	from = max(from, prevStart)
	to = max(to, from)

	if len(*s.Text) > maxSegmentText {
		return Segment{}, errors.New("text too long")
	}

	return Segment{StartMs: from, EndMs: to, Text: normaliseText(*s.Text)}, nil
}

// normaliseText replaces control characters with spaces and trims the leading space the CLI emits.
func normaliseText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// escapeControls turns raw control bytes into spaces. The CLI escapes only quotes and backslashes in strings, so
// a newline or tab in recognised text would make the JSON invalid; outside strings a space is equivalent.
func escapeControls(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		if b < 0x20 {
			b = ' '
		}
		out[i] = b
	}
	return out
}

// encodeTranscript renders the stored document.
func encodeTranscript(t Transcript) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(t); err != nil {
		return nil, fmt.Errorf("%w: encode: %w", ErrOutput, err)
	}
	return buf.Bytes(), nil
}
