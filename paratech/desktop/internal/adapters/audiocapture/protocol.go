package audiocapture

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Line protocol between the app and its recorder child: one JSON object per line. The parent writes exactly one
// start request to the child's stdin and then closes stdin to stop it; the child writes events to stdout.
const (
	protocolVersion = 1
	maxLineBytes    = 4096
	maxTextBytes    = 512 // error and reason texts in events

	minChunkSeconds = 30
	maxChunkSeconds = 60
	maxLeadSilence  = 60 * time.Second
)

// Event types written by the child.
const (
	evStarted = "started" // the capture runs: mode, format, Windows build
	evAttempt = "attempt" // one activation try
	evSwitch  = "switch"  // a mode change of the running capture
	evChunk   = "chunk"   // a chunk was closed: number, frames, rate
	evStats   = "stats"   // counters; also the heartbeat
	evFailed  = "failed"  // the capture ended by itself or could not start: reason
	evStopped = "stopped" // clean stop after stdin closed: frames written
)

// ErrProtocol means a protocol line is malformed.
var ErrProtocol = errors.New("audiocapture: recorder protocol error")

// Request is the start request of a recorder child.
type Request struct {
	Recording    shared.RecordingID
	Chunks       shared.ArtifactRef // recordings/<id>
	Target       Target
	ChunkSeconds int
	// LeadSilence is written before the first packet: the time a restarted child missed (ADR-0002 Decision 6).
	LeadSilence time.Duration
	// Floor is the mode the previous child of the recording ended in; ModeSystem keeps a fallen-back recording in
	// system mode (the fallback is one-way). Empty or the target's mode changes nothing.
	Floor Mode
}

// Validate checks every field.
func (r Request) Validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrProtocol, fmt.Sprintf(format, args...))
	}
	if err := r.Recording.Validate(); err != nil {
		return bad("recording: %v", err)
	}
	if r.Chunks.IsZero() || r.Chunks.Kind() != shared.ArtifactRecordings {
		return bad("chunks must be a recordings directory")
	}
	if err := r.Target.Validate(); err != nil {
		return bad("target: %v", err)
	}
	if r.ChunkSeconds < minChunkSeconds || r.ChunkSeconds > maxChunkSeconds {
		return bad("chunk seconds %d outside %d..%d", r.ChunkSeconds, minChunkSeconds, maxChunkSeconds)
	}
	if r.LeadSilence < 0 || r.LeadSilence > maxLeadSilence || r.LeadSilence%time.Millisecond != 0 {
		return bad("lead silence %v outside 0..%v in whole milliseconds", r.LeadSilence, maxLeadSilence)
	}
	if r.Floor != "" && r.Floor != r.Target.Mode && r.Floor != ModeSystem {
		return bad("floor %q does not fit target %q", r.Floor, r.Target.Mode)
	}
	return nil
}

// effectiveTarget is the target to open: system mode once the recording fell back.
func (r Request) effectiveTarget() Target {
	if r.Floor == ModeSystem {
		return Target{Mode: ModeSystem}
	}
	return r.Target
}

type wireTarget struct {
	Mode string `json:"mode"`
	PID  uint32 `json:"pid"`
}

type wireRequest struct {
	V             int        `json:"v"`
	Recording     string     `json:"recording"`
	Chunks        string     `json:"chunks"`
	Target        wireTarget `json:"target"`
	ChunkSeconds  int        `json:"chunk_seconds"`
	LeadSilenceMS int64      `json:"lead_silence_ms"`
	Floor         string     `json:"floor,omitempty"`
}

// encodeRequest returns the request line, newline included.
func encodeRequest(r Request) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(wireRequest{
		V: protocolVersion, Recording: r.Recording.String(), Chunks: r.Chunks.String(),
		Target: wireTarget{Mode: string(r.Target.Mode), PID: r.Target.PID}, ChunkSeconds: r.ChunkSeconds,
		LeadSilenceMS: r.LeadSilence.Milliseconds(), Floor: string(r.Floor),
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// decodeRequest parses and validates a request line strictly: unknown fields, trailing data and other versions are
// rejected.
func decodeRequest(line []byte) (Request, error) {
	var w wireRequest
	if err := decodeStrict(line, &w); err != nil {
		return Request{}, err
	}
	if w.V != protocolVersion {
		return Request{}, fmt.Errorf("%w: version %d, want %d", ErrProtocol, w.V, protocolVersion)
	}
	if w.LeadSilenceMS < 0 || w.LeadSilenceMS > maxLeadSilence.Milliseconds() {
		return Request{}, fmt.Errorf("%w: lead silence %d ms", ErrProtocol, w.LeadSilenceMS)
	}
	chunks, err := shared.ParseArtifactRef(w.Chunks)
	if err != nil {
		return Request{}, fmt.Errorf("%w: chunks: %w", ErrProtocol, err)
	}

	r := Request{
		Recording: shared.RecordingID(w.Recording), Chunks: chunks,
		Target: Target{Mode: Mode(w.Target.Mode), PID: w.Target.PID}, ChunkSeconds: w.ChunkSeconds,
		LeadSilence: time.Duration(w.LeadSilenceMS) * time.Millisecond, Floor: Mode(w.Floor),
	}
	return r, r.Validate()
}

// decodeStrict decodes exactly one JSON object without unknown fields.
func decodeStrict(line []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %s", ErrProtocol, shortText(err.Error()))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data", ErrProtocol)
	}
	return nil
}

type wireFormat struct {
	Rate     uint32 `json:"rate"`
	Channels uint16 `json:"channels"`
	Bits     uint16 `json:"bits"`
	Float    bool   `json:"float,omitempty"`
}

// event is one line written by the child; the fields used depend on Type.
type event struct {
	Type string `json:"type"`

	Mode   string      `json:"mode,omitempty"`   // started, attempt
	Format *wireFormat `json:"format,omitempty"` // started
	Build  string      `json:"build,omitempty"`  // started

	OK      bool   `json:"ok,omitempty"`      // attempt
	HResult string `json:"hresult,omitempty"` // attempt
	Error   string `json:"error,omitempty"`   // attempt

	From   string `json:"from,omitempty"`   // switch
	To     string `json:"to,omitempty"`     // switch
	Reason string `json:"reason,omitempty"` // switch, failed

	N      int    `json:"n,omitempty"`      // chunk
	Frames int64  `json:"frames,omitempty"` // chunk, stats, stopped
	Rate   uint32 `json:"rate,omitempty"`   // chunk

	Packets       uint64 `json:"packets,omitempty"`        // stats
	SilentPackets uint64 `json:"silent_packets,omitempty"` // stats
	Reopens       uint64 `json:"reopens,omitempty"`        // stats

	// Unavailable marks a failure because this build has no capture (recording.ErrCaptureUnavailable).
	Unavailable bool `json:"unavailable,omitempty"`
}

func toWireFormat(f Format) *wireFormat {
	return &wireFormat{Rate: f.SampleRate, Channels: f.Channels, Bits: f.BitsPerSample, Float: f.Float}
}

func (w *wireFormat) format() Format {
	if w == nil {
		return Format{}
	}
	return Format{SampleRate: w.Rate, Channels: w.Channels, BitsPerSample: w.Bits, Float: w.Float}
}

// decodeEvent parses an event line strictly.
func decodeEvent(line []byte) (event, error) {
	var e event
	if err := decodeStrict(line, &e); err != nil {
		return event{}, err
	}
	switch e.Type {
	case evStarted, evAttempt, evSwitch, evChunk, evStats, evFailed, evStopped:
		for _, s := range []*string{&e.Mode, &e.Build, &e.HResult, &e.Error, &e.From, &e.To, &e.Reason} {
			*s = shortText(*s) // the texts reach logs and the diagnostics screen
		}
		return e, nil
	}
	return event{}, fmt.Errorf("%w: unknown event %q", ErrProtocol, shortText(e.Type))
}

// lineReader reads newline-terminated lines of at most max bytes. A longer line is skipped and reported as
// oversized, so a misbehaving peer cannot make the reader buffer without bound.
type lineReader struct {
	r   *bufio.Reader
	max int
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, max+1), max: max}
}

// next returns the next line without its newline. oversized reports a skipped long line (line is nil then). A last
// line without a newline is returned before io.EOF.
func (l *lineReader) next() (line []byte, oversized bool, err error) {
	b, err := l.r.ReadSlice('\n')
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = l.r.ReadSlice('\n')
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, true, err
		}
		return nil, true, nil
	case err != nil && (len(b) == 0 || !errors.Is(err, io.EOF)):
		return nil, false, err
	}
	b = bytes.TrimRight(b, "\r\n")
	if len(b) > l.max {
		return nil, true, nil
	}
	return append([]byte(nil), b...), false, nil
}

// shortText bounds a text for an event or a log line and drops control characters.
func shortText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) <= maxTextBytes {
		return s
	}
	cut := maxTextBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
