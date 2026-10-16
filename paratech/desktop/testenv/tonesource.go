package testenv

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/values"
)

// ErrToneDeviceLost is the error of a ToneSource stream that fails by itself (FailAfter).
var ErrToneDeviceLost = errors.New("fake source: device lost")

// ToneSource is a fake audiocapture.Source: a sine of Freq Hz in Format, PacketFrames per packet. Its fields are
// the script of every stream it opens; set them before the first Open. No real audio device is touched.
type ToneSource struct {
	Format       audiocapture.Format
	Freq         float64               // default 1000 Hz
	Amplitude    float64               // default 0.5
	PacketFrames int                   // default 10 ms
	Interval     time.Duration         // real time between packets; 0 = as fast as they are read
	Windows      values.WindowsVersion // reported Windows; default ToneWindows()
	Device       string                // reported output device; default ToneDevice

	// FallbackTo, when set and the target is another mode, reports a failed attempt of the target and runs in this
	// mode (process loopback failing on an old build).
	FallbackTo audiocapture.Mode
	OpenErr    error // Open fails with it
	OpenBlock  bool  // Open waits until its context ends (an activation that never completes)

	SwitchAfter  int                 // after this many packets the stream switches to SwitchFormat and SwitchMode
	SwitchFormat audiocapture.Format // format after the switch
	SwitchMode   audiocapture.Mode   // mode after the switch (reported through the observer)
	SwitchCause  values.SwitchCause  // cause of the switch; default healthy but mute

	DiscontinuityAt int       // the packet with this number (from 1) is flagged as a discontinuity
	SilentAt        int       // the packet with this number is flagged silent
	FailAfter       int       // after this many packets Read fails with ErrToneDeviceLost
	HangAfter       int       // after this many packets Read blocks until its context ends, even after Stop
	Limit           int       // after this many packets Read waits for Stop (a quiet stream)
	Drain           int       // packets still delivered after Stop, before io.EOF
	OnPacket        func(int) // called with the number of each packet before Read returns it
	// Gate, when set, lets the tone through only while it reports true; the other packets carry zeros (a FakeTone's
	// Playing makes the source hear the self-test tone only while it plays).
	Gate func() bool

	mu     sync.Mutex
	opened []audiocapture.Target
	closes int
}

// Opened returns the targets of every Open, in order.
func (s *ToneSource) Opened() []audiocapture.Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]audiocapture.Target(nil), s.opened...)
}

// Closes returns how many streams were closed.
func (s *ToneSource) Closes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// Open starts a stream of the script.
func (s *ToneSource) Open(ctx context.Context, target audiocapture.Target, obs audiocapture.Observer) (audiocapture.Stream, error) {
	s.mu.Lock()
	s.opened = append(s.opened, target)
	s.mu.Unlock()

	if s.OpenBlock {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.OpenErr != nil {
		obs.Attempt(audiocapture.Attempt{Mode: target.Mode, Error: s.OpenErr.Error()})
		return nil, s.OpenErr
	}

	mode := target.Mode
	if s.FallbackTo != "" && s.FallbackTo != target.Mode {
		obs.Attempt(audiocapture.Attempt{Mode: target.Mode, HResult: "0x80070005", Error: "activation refused"})
		obs.Switch(audiocapture.Switch{From: target.Mode, To: s.FallbackTo, Cause: values.SwitchActivationFailed,
			Reason: "activation failed: activation refused"})
		mode = s.FallbackTo
	}
	obs.Attempt(audiocapture.Attempt{Mode: mode, OK: true, Device: s.device()})

	st := &toneStream{src: s, obs: obs, mode: mode, format: s.Format, stopCh: make(chan struct{})}
	st.frames = s.PacketFrames
	if st.frames <= 0 {
		st.frames = int(s.Format.SampleRate / 100)
	}
	return st, nil
}

type toneStream struct {
	src *ToneSource
	obs audiocapture.Observer

	mu     sync.Mutex
	mode   audiocapture.Mode
	format audiocapture.Format
	frames int
	n      int   // packets delivered
	pos    int64 // next sample index
	drain  int

	stopOnce sync.Once
	stopCh   chan struct{}
}

func (st *toneStream) Info() audiocapture.StreamInfo {
	st.mu.Lock()
	defer st.mu.Unlock()
	win := st.src.Windows
	if win.IsZero() {
		win = ToneWindows()
	}
	return audiocapture.StreamInfo{Mode: st.mode, Format: st.format, Windows: win, Device: st.src.device()}
}

func (st *toneStream) Reopens() uint64 { return 0 }

func (st *toneStream) Stop() {
	st.stopOnce.Do(func() { close(st.stopCh) })
}

func (st *toneStream) Close() error {
	st.Stop()
	st.src.mu.Lock()
	st.src.closes++
	st.src.mu.Unlock()
	return nil
}

func (st *toneStream) stopped() bool {
	select {
	case <-st.stopCh:
		return true
	default:
		return false
	}
}

func (st *toneStream) Read(ctx context.Context) (audiocapture.Packet, error) {
	s := st.src
	n := st.count()
	switch {
	case s.HangAfter > 0 && n >= s.HangAfter:
		<-ctx.Done()
		return audiocapture.Packet{}, ctx.Err()
	case st.stopped():
		if st.drained() {
			return audiocapture.Packet{}, io.EOF
		}
	case s.FailAfter > 0 && n >= s.FailAfter:
		return audiocapture.Packet{}, ErrToneDeviceLost
	case s.Limit > 0 && n >= s.Limit:
		select {
		case <-st.stopCh:
			return st.Read(ctx)
		case <-ctx.Done():
			return audiocapture.Packet{}, ctx.Err()
		}
	case s.Interval > 0:
		t := time.NewTimer(s.Interval)
		defer t.Stop()
		select {
		case <-t.C:
		case <-st.stopCh:
			return st.Read(ctx)
		case <-ctx.Done():
			return audiocapture.Packet{}, ctx.Err()
		}
	}
	return st.next(), nil
}

func (st *toneStream) count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.n
}

// drained reports whether the packets owed after a stop were delivered, and counts one more otherwise.
func (st *toneStream) drained() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.drain >= st.src.Drain {
		return true
	}
	st.drain++
	return false
}

func (st *toneStream) next() audiocapture.Packet {
	s := st.src
	st.mu.Lock()
	if s.SwitchAfter > 0 && st.n == s.SwitchAfter {
		from := st.mode
		st.format, st.pos = s.SwitchFormat, 0
		if s.SwitchMode != "" {
			st.mode = s.SwitchMode
		}
		if s.SwitchFormat.SampleRate != 0 {
			st.frames = int(s.SwitchFormat.SampleRate / 100)
		}
		cause := s.SwitchCause
		if cause == "" {
			cause = values.SwitchHealthyButMute
		}
		st.mu.Unlock()
		st.obs.Switch(audiocapture.Switch{From: from, To: st.mode, Cause: cause, Reason: "healthy but mute: only zeros for 15s"})
		st.mu.Lock()
	}
	st.n++
	n, f, frames, pos := st.n, st.format, st.frames, st.pos
	st.pos += int64(frames)
	st.mu.Unlock()

	if s.OnPacket != nil {
		s.OnPacket(n)
	}
	p := audiocapture.Packet{Format: f, Frames: frames, Discontinuity: n == 1 || n == s.DiscontinuityAt}
	if n == s.SilentAt {
		p.Silent = true
		return p
	}
	p.Data = ToneFrames(f, s.freq(), s.amplitude(), pos, frames)
	if s.Gate != nil && !s.Gate() {
		p.Data = make([]byte, len(p.Data))
	}
	return p
}

// FakeTone is a fake audiocapture.TonePlayer: Play marks the tone as playing for its duration and plays nothing.
// Set its fields before the first call.
type FakeTone struct {
	Device    string // reported output device; default ToneDevice
	HandsFree bool   // the output device is a Bluetooth hands-free endpoint
	NoDevice  bool   // there is no output device
	PlayErr   error  // Play fails with it

	mu      sync.Mutex
	asked   bool
	playing bool
	plays   int
}

// Output describes the fake output device.
func (f *FakeTone) Output() (audiocapture.OutputDevice, error) {
	f.mu.Lock()
	f.asked = true
	f.mu.Unlock()
	if f.NoDevice {
		return audiocapture.OutputDevice{}, audiocapture.ErrNoDevice
	}
	name := f.Device
	if name == "" {
		name = ToneDevice
	}
	return audiocapture.OutputDevice{Name: name, HandsFree: f.HandsFree}, nil
}

// Play is "playing" for t.Duration, or until ctx ends.
func (f *FakeTone) Play(ctx context.Context, t audiocapture.Tone) error {
	if f.PlayErr != nil {
		return f.PlayErr
	}
	f.setPlaying(true)
	defer f.setPlaying(false)
	timer := time.NewTimer(t.Duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *FakeTone) setPlaying(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.playing = on
	if on {
		f.plays++
	}
}

// Playing reports whether the tone plays now.
func (f *FakeTone) Playing() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.playing
}

// Audible is the Gate of a ToneSource that serves both a capture and a self-test: always true until a self-test
// asked for the output device, then true only while the tone plays.
func (f *FakeTone) Audible() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.asked || f.playing
}

// Plays returns how often Play started.
func (f *FakeTone) Plays() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plays
}

// ToneDevice is the output device a ToneSource reports by default.
const ToneDevice = "Speakers (Fake Audio)"

// ToneWindows is the Windows a ToneSource reports by default: the owner's Windows 10 22H2.
func ToneWindows() values.WindowsVersion {
	return values.WindowsVersion{Product: "Windows 10 Pro", DisplayVersion: "22H2", Major: 10, Build: 19045, UBR: 6456}
}

func (s *ToneSource) device() string {
	if s.Device == "" {
		return ToneDevice
	}
	return s.Device
}

func (s *ToneSource) freq() float64 {
	if s.Freq == 0 {
		return 1000
	}
	return s.Freq
}

func (s *ToneSource) amplitude() float64 {
	if s.Amplitude == 0 {
		return 0.5
	}
	return s.Amplitude
}

// ToneFrames returns frames interleaved frames of a sine in f (the same on every channel), from sample index start.
func ToneFrames(f audiocapture.Format, freq, amp float64, start int64, frames int) []byte {
	le := binary.LittleEndian
	out := make([]byte, 0, frames*f.BlockAlign())
	for i := start; i < start+int64(frames); i++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(f.SampleRate))
		for range f.Channels {
			switch {
			case f.Float:
				out = le.AppendUint32(out, math.Float32bits(float32(v)))
			case f.BitsPerSample == 16:
				out = le.AppendUint16(out, uint16(int16(math.Round(v*math.MaxInt16)))) //nolint:gosec // two's complement bits of a sample in range
			case f.BitsPerSample == 24:
				x := int32(math.Round(v * (1<<23 - 1)))
				out = append(out, byte(x), byte(x>>8), byte(x>>16)) //nolint:gosec // the three low bytes of a 24-bit sample
			default:
				out = le.AppendUint32(out, uint32(int32(math.Round(v*math.MaxInt32)))) //nolint:gosec // two's complement bits of a sample in range
			}
		}
	}
	return out
}
