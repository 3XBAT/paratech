//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"context"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tonePoll is how often the render buffer of the self-test tone is topped up (the buffer holds 200 ms).
const tonePoll = 10 * time.Millisecond

// PlatformTone returns the tone player of the sound check: a shared-mode render stream in the mix format of the
// default output device, on its own COM thread. It opens nothing until a call.
func PlatformTone() TonePlayer { return &wasapiTone{procs: newComProcs()} }

type wasapiTone struct{ procs *comProcs }

// Output describes the default output device.
func (t *wasapiTone) Output() (OutputDevice, error) {
	var (
		out OutputDevice
		err error
	)
	if cerr := comThread(func() { out, err = t.output() }); cerr != nil {
		return OutputDevice{}, cerr
	}
	return out, err
}

func (t *wasapiTone) output() (OutputDevice, error) {
	enum, err := t.procs.createDeviceEnumerator()
	if err != nil {
		return OutputDevice{}, err
	}
	defer release(enum)
	dev, err := defaultRenderEndpoint(enum) // ERROR_NOT_FOUND without an output device: ErrNoDevice
	if err != nil {
		return OutputDevice{}, err
	}
	defer release(dev)

	p := t.procs.endpointProps(dev)
	return OutputDevice{Name: p.name, HandsFree: handsFreeEndpoint(p.formFactor, p.formFactorOK, p.enumerator)}, nil
}

// Play renders tone on the default output device and returns when it has played out or ctx ended.
func (t *wasapiTone) Play(ctx context.Context, tone Tone) error {
	var err error
	if cerr := comThread(func() { err = t.play(ctx, tone) }); cerr != nil {
		return cerr
	}
	return err
}

func (t *wasapiTone) play(ctx context.Context, tone Tone) error {
	enum, err := t.procs.createDeviceEnumerator()
	if err != nil {
		return err
	}
	defer release(enum)
	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return err
	}
	defer release(dev)
	client, err := activateAudioClient(dev)
	if err != nil {
		return err
	}
	defer release(client)
	mix, f, err := mixFormat(client)
	if err != nil {
		return err
	}
	defer windows.CoTaskMemFree(mix)

	if err := initialize(client, 0, mix); err != nil {
		return err
	}
	size, err := uint32Out("IAudioClient::GetBufferSize(tone)", client, slotClientGetBufferSize)
	if err != nil {
		return err
	}
	render, err := getService(client, iidIAudioRenderClient(), "IAudioRenderClient")
	if err != nil {
		return err
	}
	defer release(render)

	r := &toneRender{client: client, render: render, size: size, f: f, tone: tone, total: framesFor(tone.Duration, f.SampleRate)}
	if err := r.topUp(); err != nil {
		return err
	}
	if err := call("IAudioClient::Start(tone)", client, slotClientStart); err != nil {
		return err
	}
	defer func() { _ = call("IAudioClient::Stop(tone)", client, slotClientStop) }()

	// Played out: every frame written and the buffer drained, bounded by the tone and one second.
	deadline := time.Now().Add(tone.Duration + time.Second)
	for time.Now().Before(deadline) {
		if err := sleepCtx(ctx, tonePoll); err != nil {
			return err
		}
		if err := r.topUp(); err != nil {
			return err
		}
		if r.pos >= r.total {
			padding, err := uint32Out("IAudioClient::GetCurrentPadding(tone)", client, slotClientGetCurrentPadding)
			if err != nil || padding == 0 {
				return err
			}
		}
	}
	return nil
}

// toneRender writes the tone into a render client.
type toneRender struct {
	client, render unsafe.Pointer
	size           uint32 // frames of the render buffer
	f              Format
	tone           Tone
	pos, total     int64 // frames written, frames of the tone
}

// topUp fills the free part of the render buffer with the next frames of the tone.
func (r *toneRender) topUp() error {
	padding, err := uint32Out("IAudioClient::GetCurrentPadding(tone)", r.client, slotClientGetCurrentPadding)
	if err != nil {
		return err
	}
	n := min(int64(r.size)-int64(padding), r.total-r.pos)
	if n <= 0 {
		return nil
	}
	buf, err := renderBuffer(r.render, uint32(n)) //nolint:gosec // n is at most the buffer size, a uint32
	if err != nil {
		return err
	}
	data := sineFrames(r.f, r.tone.Freq, r.tone.Amplitude, r.pos, int(n))
	copy(unsafe.Slice((*byte)(buf), len(data)), data) //nolint:gosec // G103: the n frames GetBuffer gave
	if err := call("IAudioRenderClient::ReleaseBuffer(tone)", r.render, slotRenderReleaseBuffer, uintptr(n), 0); err != nil {
		return err
	}
	r.pos += n
	return nil
}

// renderBuffer returns the buffer of frames frames to write into.
func renderBuffer(render unsafe.Pointer, frames uint32) (unsafe.Pointer, error) {
	var data unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(render, slotRenderGetBuffer), uintptr(render),
		uintptr(frames), uintptr(unsafe.Pointer(&data))) //nolint:gosec // G103: COM out-parameter
	return checkOut("IAudioRenderClient::GetBuffer(tone)", hr(r), data)
}
