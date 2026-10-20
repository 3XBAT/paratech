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

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Line protocol between the app and its recorder child: one JSON object per line. The parent writes exactly one
// request to the child's stdin, a capture or a level check, and then closes stdin to stop it; the child writes events
// to stdout.
const (
	protocolVersion = 1
	maxLineBytes    = 4096
	maxTextBytes    = 512 // error and reason texts in events

	minChunkSeconds = 30
	maxChunkSeconds = 60

	// MaxCheckDuration bounds the capture of one level check ("Check sound").
	MaxCheckDuration = time.Minute
)

// maxLeadSilence bounds the lead silence of a restart. It covers the worst case of the defaults: a lost open chunk,
// the previous lead not yet closed in a chunk, a hang until the kill and the longest backoff (154 s).
const maxLeadSilence = 2*maxChunkSeconds*time.Second + DefaultHangTimeout +
	DefaultRestartBackoff<<(DefaultMaxRestarts-1)

// Event types written by the child.
const (
	evStarted = "started" // the capture runs: mode, format, Windows version, output device
	evAttempt = "attempt" // one activation try
	evSwitch  = "switch"  // a mode change of the running capture
	evChunk   = "chunk"   // a chunk was closed: number, frames, rate
	evStats   = "stats"   // counters; also the heartbeat
	evFailed  = "failed"  // the capture ended by itself or could not start: reason
	evStopped = "stopped" // clean stop after stdin closed: frames written; also the end of a level check

	evLevel = "level" // the peak of the last window of a level check or a running capture (TASK-099), 0..1
)

// kindLevelCheck is the request kind of the "Check sound" level check (TASK-075); a capture request has no kind.
const kindLevelCheck = "levelcheck"

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

// LevelRequest is the request of a level check: capture Target for Duration and report its levels; nothing is
// written.
type LevelRequest struct {
	Target   Target
	Duration time.Duration
}

// Validate checks the target and that the duration is positive, whole milliseconds and at most MaxCheckDuration.
func (r LevelRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return fmt.Errorf("%w: target: %v", ErrProtocol, err)
	}
	if r.Duration <= 0 || r.Duration > MaxCheckDuration || r.Duration%time.Millisecond != 0 {
		return fmt.Errorf("%w: level check duration %v outside 1 ms..%v in whole milliseconds", ErrProtocol, r.Duration,
			MaxCheckDuration)
	}
	return nil
}

type wireTarget struct {
	Mode string `json:"mode"`
	PID  uint32 `json:"pid"`
}

type wireRequest struct {
	V             int        `json:"v"`
	Kind          string     `json:"kind,omitempty"`
	Recording     string     `json:"recording"`
	Chunks        string     `json:"chunks"`
	Target        wireTarget `json:"target"`
	ChunkSeconds  int        `json:"chunk_seconds"`
	LeadSilenceMS int64      `json:"lead_silence_ms"`
	Floor         string     `json:"floor,omitempty"`
	DurationMS    int64      `json:"duration_ms,omitempty"` // level check only
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

// encodeLevelCheck returns the level check request line, newline included.
func encodeLevelCheck(r LevelRequest) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(struct {
		V          int        `json:"v"`
		Kind       string     `json:"kind"`
		Target     wireTarget `json:"target"`
		DurationMS int64      `json:"duration_ms"`
	}{
		V: protocolVersion, Kind: kindLevelCheck, Target: wireTarget{Mode: string(r.Target.Mode), PID: r.Target.PID},
		DurationMS: r.Duration.Milliseconds(),
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// decodeRequest parses and validates a capture request line strictly: unknown fields, trailing data, other versions
// and a level check are rejected.
func decodeRequest(line []byte) (Request, error) {
	r, _, level, err := decodeChildRequest(line)
	if err == nil && level {
		err = fmt.Errorf("%w: a level check is not a capture", ErrProtocol)
	}
	return r, err
}

// decodeChildRequest parses either request strictly; level reports a level check (lc), which takes only a target and
// a duration.
func decodeChildRequest(line []byte) (r Request, lc LevelRequest, level bool, err error) {
	var w wireRequest
	if err := decodeStrict(line, &w); err != nil {
		return Request{}, LevelRequest{}, false, err
	}
	if w.V != protocolVersion {
		return Request{}, LevelRequest{}, false, fmt.Errorf("%w: version %d, want %d", ErrProtocol, w.V, protocolVersion)
	}
	switch w.Kind {
	case "":
	case kindLevelCheck:
		lc, err = w.levelCheck()
		return Request{}, lc, err == nil, err
	default:
		return Request{}, LevelRequest{}, false, fmt.Errorf("%w: unknown request kind %q", ErrProtocol, shortText(w.Kind))
	}
	if w.DurationMS != 0 {
		return Request{}, LevelRequest{}, false, fmt.Errorf("%w: a capture takes no duration", ErrProtocol)
	}
	r, err = w.request()
	return r, LevelRequest{}, false, err
}

// levelCheck turns a level check request into its validated form; the capture fields must be absent.
func (w wireRequest) levelCheck() (LevelRequest, error) {
	if w != (wireRequest{V: w.V, Kind: w.Kind, Target: w.Target, DurationMS: w.DurationMS}) {
		return LevelRequest{}, fmt.Errorf("%w: a level check takes no capture fields", ErrProtocol)
	}
	if w.DurationMS <= 0 || w.DurationMS > MaxCheckDuration.Milliseconds() {
		return LevelRequest{}, fmt.Errorf("%w: level check duration %d ms", ErrProtocol, w.DurationMS)
	}
	r := LevelRequest{Target: Target{Mode: Mode(w.Target.Mode), PID: w.Target.PID},
		Duration: time.Duration(w.DurationMS) * time.Millisecond}
	return r, r.Validate()
}

// request turns a capture request into its validated form.
func (w wireRequest) request() (Request, error) {
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

	Mode    string       `json:"mode,omitempty"`    // started, attempt
	Format  *wireFormat  `json:"format,omitempty"`  // started
	Windows *wireWindows `json:"windows,omitempty"` // started
	Device  string       `json:"device,omitempty"`  // started, attempt: the output device name

	OK      bool   `json:"ok,omitempty"`      // attempt
	HResult string `json:"hresult,omitempty"` // attempt
	Error   string `json:"error,omitempty"`   // attempt

	From   string `json:"from,omitempty"`   // switch
	To     string `json:"to,omitempty"`     // switch
	Cause  string `json:"cause,omitempty"`  // switch: a values.SwitchCause; failed level check: a values.SoundCause
	Reason string `json:"reason,omitempty"` // switch, failed

	N      int    `json:"n,omitempty"`      // chunk
	Frames int64  `json:"frames,omitempty"` // chunk, stats, stopped
	Rate   uint32 `json:"rate,omitempty"`   // chunk

	Packets       uint64 `json:"packets,omitempty"`        // stats
	SilentPackets uint64 `json:"silent_packets,omitempty"` // stats
	// AudiblePackets are the packets whose peak reaches values.HeardLevel (stats).
	AudiblePackets uint64 `json:"audible_packets,omitempty"`
	Reopens        uint64 `json:"reopens,omitempty"` // stats

	// Level is the peak of a window of a level check or a capture, 0..1; required in a level event, absent elsewhere.
	Level *float64 `json:"level,omitempty"`

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

// wireWindows is the Windows version in a started event.
type wireWindows struct {
	Product string `json:"product,omitempty"`
	Display string `json:"display,omitempty"`
	Major   uint32 `json:"major"`
	Minor   uint32 `json:"minor"`
	Build   uint32 `json:"build"`
	UBR     uint32 `json:"ubr,omitempty"`
}

func toWireWindows(v values.WindowsVersion) *wireWindows {
	if v.IsZero() {
		return nil
	}
	return &wireWindows{Product: v.Product, Display: v.DisplayVersion, Major: v.Major, Minor: v.Minor, Build: v.Build, UBR: v.UBR}
}

func (w *wireWindows) version() values.WindowsVersion {
	if w == nil {
		return values.WindowsVersion{}
	}
	return values.WindowsVersion{Product: w.Product, DisplayVersion: w.Display, Major: w.Major, Minor: w.Minor,
		Build: w.Build, UBR: w.UBR}
}

// decodeEvent parses an event line strictly.
func decodeEvent(line []byte) (event, error) {
	var e event
	if err := decodeStrict(line, &e); err != nil {
		return event{}, err
	}
	switch e.Type {
	case evStarted, evAttempt, evSwitch, evChunk, evStats, evFailed, evStopped, evLevel:
		if err := checkCause(e); err != nil {
			return event{}, err
		}
		if err := checkLevel(e); err != nil {
			return event{}, err
		}
		texts := []*string{&e.Mode, &e.Device, &e.HResult, &e.Error, &e.From, &e.To, &e.Reason}
		if e.Windows != nil {
			texts = append(texts, &e.Windows.Product, &e.Windows.Display)
		}
		for _, s := range texts {
			*s = shortText(*s) // the texts reach logs and the diagnostics screen
		}
		return e, nil
	}
	return event{}, fmt.Errorf("%w: unknown event %q", ErrProtocol, shortText(e.Type))
}

// checkCause accepts only the causes of the event's type: a switch cause, the sound cause of a failed level check,
// none elsewhere.
func checkCause(e event) error {
	var ok bool
	switch e.Type {
	case evSwitch:
		ok = e.Cause == "" || values.SwitchCause(e.Cause).Valid()
	case evFailed:
		ok = e.Cause == "" || values.SoundCause(e.Cause).Valid()
	default:
		ok = e.Cause == ""
	}
	if !ok {
		return fmt.Errorf("%w: unexpected cause %q of a %s event", ErrProtocol, shortText(e.Cause), e.Type)
	}
	return nil
}

// checkLevel requires a level in 0..1 in a level event and none elsewhere.
func checkLevel(e event) error {
	if (e.Type == evLevel) != (e.Level != nil) {
		return fmt.Errorf("%w: a level belongs to a level event only", ErrProtocol)
	}
	if e.Level != nil {
		if _, err := values.NewSoundLevel(*e.Level); err != nil {
			return fmt.Errorf("%w: %v", ErrProtocol, err)
		}
	}
	return nil
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
