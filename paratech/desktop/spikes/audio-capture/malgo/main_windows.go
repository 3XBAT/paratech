//go:build windows && cgo

package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	sampleRate = 48000
	channels   = 2
	blockAlign = channels * 4 // float32
	headerLen  = 58           // RIFF + fmt(18) + fact + data, as for any non-PCM WAV

	dirPerm  = 0o700
	filePerm = 0o600
)

func main() {
	out := flag.String("out", "", "output directory (required; inherits the parent folder's ACL, use one under your profile such as %TEMP%)")
	duration := flag.Duration("duration", 10*time.Second, "recording length, at most 3h")
	chunk := flag.Duration("chunk", 30*time.Second, "chunk length, 1s..60s")
	flag.Parse()
	if *out == "" || *duration <= 0 || *duration > 3*time.Hour || *chunk < time.Second || *chunk > time.Minute {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*out, *duration, *chunk); err != nil {
		fmt.Fprintln(os.Stderr, "malgospike:", err)
		os.Exit(1)
	}
}

func run(out string, duration, chunk time.Duration) error {
	if err := os.MkdirAll(out, dirPerm); err != nil {
		return err
	}
	w, err := newWriter(out, int64(chunk/time.Millisecond)*sampleRate/1000)
	if err != nil {
		return err
	}

	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi}, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("init context: %w", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	cfg := malgo.DefaultDeviceConfig(malgo.Loopback)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = channels
	cfg.SampleRate = sampleRate

	// The data callback runs on a miniaudio thread: it only copies, the main goroutine writes.
	frames := make(chan []byte, 256)
	var dropped, callbacks, captured atomic.Uint64
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, n uint32) {
			callbacks.Add(1)
			captured.Add(uint64(n))
			select {
			case frames <- append([]byte(nil), in...):
			default:
				dropped.Add(1)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("init loopback device: %w", err)
	}
	defer dev.Uninit()

	if err := dev.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Printf("capturing: loopback %d Hz, %d ch, float32 (device internal rate %d)\n", sampleRate, channels, dev.CaptureInternalSampleRate())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	timer := time.NewTimer(duration)
	defer timer.Stop()

	var werr error
loop:
	for {
		select {
		case p := <-frames:
			if werr == nil {
				werr = w.write(p)
			}
		case <-timer.C:
			break loop
		case <-sig:
			break loop
		}
	}

	_ = dev.Stop()
	for len(frames) > 0 {
		if p := <-frames; werr == nil {
			werr = w.write(p)
		}
	}
	// Classic loopback delivers no callbacks while nothing plays: malgo opens no keep-alive render stream.
	fmt.Printf("stopped: callbacks=%d frames=%d dropped=%d\n", callbacks.Load(), captured.Load(), dropped.Load())
	return errors.Join(werr, w.close())
}

// writer is a minimal copy of the desktop wavchunk writer (this module cannot import it): every chunk is written
// as .partial with a placeholder header, then patched, fsynced and renamed.
type writer struct {
	root     *os.Root
	perChunk int64
	next     int

	f      *os.File
	buf    *bufio.Writer
	name   string
	frames int64
}

func newWriter(dir string, perChunk int64) (*writer, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &writer{root: root, perChunk: perChunk, next: 1}, nil
}

func (w *writer) write(p []byte) error {
	for len(p) >= blockAlign {
		if w.f == nil {
			w.name = fmt.Sprintf("chunk-%06d.wav", w.next)
			w.next++
			f, err := w.root.OpenFile(w.name+".partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
			if err != nil {
				return err
			}
			w.f, w.buf, w.frames = f, bufio.NewWriterSize(f, 64<<10), 0
			if _, err := w.buf.Write(header(0, false)); err != nil {
				return err
			}
		}

		n := min(int64(len(p)/blockAlign), w.perChunk-w.frames)
		if _, err := w.buf.Write(p[:n*blockAlign]); err != nil {
			return err
		}
		w.frames += n
		p = p[n*blockAlign:]
		if w.frames == w.perChunk {
			if err := w.finish(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *writer) finish() error {
	if w.f == nil {
		return nil
	}
	f := w.f
	w.f = nil
	err := w.buf.Flush()
	if err == nil {
		_, err = f.WriteAt(header(uint32(w.frames), true), 0) //nolint:gosec // frames <= perChunk (at most 60 s)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := w.root.Rename(w.name+".partial", w.name); err != nil {
		return err
	}
	fmt.Printf("chunk closed: %s frames=%d\n", w.name, w.frames)
	return nil
}

func (w *writer) close() error { return errors.Join(w.finish(), w.root.Close()) }

// header is the WAV header of a float32 stereo chunk; the placeholder (final == false) has RIFF size 0.
func header(frames uint32, final bool) []byte {
	le := binary.LittleEndian
	data := frames * blockAlign
	riff := uint32(headerLen-8) + data
	if !final {
		riff, data, frames = 0, 0, 0
	}
	b := append([]byte("RIFF"), le.AppendUint32(nil, riff)...)
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 18)
	b = le.AppendUint16(b, 3) // WAVE_FORMAT_IEEE_FLOAT
	b = le.AppendUint16(b, channels)
	b = le.AppendUint32(b, sampleRate)
	b = le.AppendUint32(b, sampleRate*blockAlign)
	b = le.AppendUint16(b, blockAlign)
	b = le.AppendUint16(b, 32)
	b = le.AppendUint16(b, 0)
	b = append(b, "fact"...)
	b = le.AppendUint32(b, 4)
	b = le.AppendUint32(b, frames)
	b = append(b, "data"...)
	return le.AppendUint32(b, data)
}
