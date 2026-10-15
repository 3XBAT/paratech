package audiocapture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

const (
	partialSuffix = ".partial"
	maxChunkIndex = 999999

	// writeBuffer bounds what a kill can lose on top of the unfinished chunk (ADR-0002 Decision 5).
	writeBuffer = 64 << 10
)

// Operation names used in errors.
const (
	opScanChunks   = "scan chunks"
	opCreateChunk  = "create chunk"
	opWriteChunk   = "write chunk"
	opFinishChunk  = "finish chunk"
	opRenameChunk  = "rename chunk"
	opAssemble     = "assemble track"
	opStartCapture = "start capture"
	opStopCapture  = "stop capture"
	opWaitCapture  = "wait for capture"
)

// ErrPartialFrame means audio bytes are not a whole number of frames.
var ErrPartialFrame = errors.New("audiocapture: partial frame")

// ChunkFiles is the part of the Storage Manager the recorder child writes chunks with. *filestore.Store satisfies it.
type ChunkFiles interface {
	// List returns the direct children of dir sorted by name; a missing dir yields shared.ErrNotFound.
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)

	// Create makes a new file exclusively (an existing one is shared.ErrConflict) and returns it open for writing.
	Create(ctx context.Context, ref shared.ArtifactRef) (*os.File, error)

	// Rename moves a file to a reference that does not exist yet.
	Rename(ctx context.Context, from, to shared.ArtifactRef) error
}

// chunkWriter splits a stream of frames into closed chunk files chunk-NNNNNN.wav (ADR-0002 Decision 5). A chunk grows
// as chunk-NNNNNN.wav.partial with a placeholder header; when it is full, the format changes or the writer closes,
// the final header is patched in, the file is synced and renamed. It is not safe for concurrent use.
type chunkWriter struct {
	ctx     context.Context // never cancelled: the last chunk is closed after a stop too
	files   ChunkFiles
	dir     shared.ArtifactRef
	seconds int
	onChunk func(n int, frames int64, f Format)

	format   Format
	perChunk int64 // rotation threshold in frames of format
	next     int   // number of the next chunk

	cur       *os.File
	buf       *bufio.Writer
	curN      int
	curFrames int64

	err    error // sticky: after a failed write the writer refuses more data
	closed bool
}

// newChunkWriter scans dir: numbering continues after the highest chunk present (final or temporary), so a restarted
// recorder never overwrites. No file is created before the first frames.
func newChunkWriter(ctx context.Context, files ChunkFiles, dir shared.ArtifactRef, seconds int,
	onChunk func(n int, frames int64, f Format)) (*chunkWriter, error) {
	refs, err := files.List(ctx, dir)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return nil, fmt.Errorf("audiocapture: %s: %w", opScanChunks, err)
	}

	last := 0
	for _, ref := range refs {
		if n, _, ok := chunkNumber(baseName(ref)); ok && n > last {
			last = n
		}
	}
	return &chunkWriter{
		ctx: context.WithoutCancel(ctx), files: files, dir: dir, seconds: seconds, onChunk: onChunk, next: last + 1,
	}, nil
}

// chunkNumber returns the number of a final (chunk-NNNNNN.wav) or temporary (.partial) chunk name.
func chunkNumber(name string) (n int, partial bool, ok bool) {
	rest, found := strings.CutPrefix(name, "chunk-")
	if !found || len(rest) < 6 {
		return 0, false, false
	}
	digits, ext := rest[:6], rest[6:]
	switch ext {
	case ".wav":
	case ".wav" + partialSuffix:
		partial = true
	default:
		return 0, false, false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false, false
		}
	}
	n, err := strconv.Atoi(digits)
	return n, partial, err == nil
}

// Format returns the current format (zero before the first SetFormat).
func (w *chunkWriter) Format() Format { return w.format }

// SetFormat switches the format; a different one closes the current chunk first, so every chunk has one format.
func (w *chunkWriter) SetFormat(f Format) error {
	if err := w.usable(); err != nil {
		return err
	}
	if f == w.format {
		return nil
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if err := w.finish(); err != nil {
		return err
	}

	frames := int64(w.seconds) * int64(f.SampleRate)
	if frames*int64(f.BlockAlign()) > maxDataBytes(f) {
		return fmt.Errorf("%w: %d s chunks of %s exceed the WAV size limit", ErrInvalidFormat, w.seconds, f)
	}
	w.format, w.perChunk = f, frames
	return nil
}

// Write appends whole frames in the current format, rotating chunks every perChunk frames.
func (w *chunkWriter) Write(p []byte) error {
	if err := w.usable(); err != nil {
		return err
	}
	block := w.format.BlockAlign()
	if block == 0 {
		return fmt.Errorf("%w: no format set", ErrInvalidFormat)
	}
	if len(p)%block != 0 {
		return fmt.Errorf("%w: %d bytes, a frame is %d bytes", ErrPartialFrame, len(p), block)
	}

	for len(p) > 0 {
		if w.cur == nil {
			if err := w.start(); err != nil {
				return w.fail(err)
			}
		}

		n := min(int64(len(p)/block), w.perChunk-w.curFrames)
		if _, err := w.buf.Write(p[:n*int64(block)]); err != nil {
			return w.fail(fmt.Errorf("audiocapture: %s: %w", opWriteChunk, err))
		}
		w.curFrames += n
		p = p[n*int64(block):]

		if w.curFrames == w.perChunk {
			if err := w.finish(); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteSilence appends frames of digital silence (all-zero samples are silence for PCM and float alike).
func (w *chunkWriter) WriteSilence(frames int64) error {
	if frames <= 0 {
		return nil
	}
	block := int64(w.format.BlockAlign())
	if block == 0 {
		return fmt.Errorf("%w: no format set", ErrInvalidFormat)
	}
	zeros := make([]byte, min(frames*block, 16<<10/block*block))
	for frames > 0 {
		n := min(frames, int64(len(zeros))/block)
		if err := w.Write(zeros[:n*block]); err != nil {
			return err
		}
		frames -= n
	}
	return nil
}

// Close makes the current chunk final (a chunk without frames is never created). After a failed write the
// temporary file stays on disk as it is: validation reports it, nothing is silently dropped.
func (w *chunkWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if w.err == nil {
		return w.finish()
	}
	if w.cur != nil {
		_ = w.cur.Close()
		w.cur = nil
	}
	return w.err
}

func (w *chunkWriter) usable() error {
	switch {
	case w.closed:
		return errors.New("audiocapture: chunk writer closed")
	case w.err != nil:
		return w.err
	}
	return nil
}

func (w *chunkWriter) fail(err error) error {
	w.err = err
	return err
}

func (w *chunkWriter) chunkRefs(n int) (partial, final shared.ArtifactRef, err error) {
	name := fmt.Sprintf("chunk-%06d.wav", n)
	if final, err = w.dir.Child(name); err != nil {
		return
	}
	partial, err = w.dir.Child(name + partialSuffix)
	return
}

// start creates the next chunk under its temporary name with a placeholder header.
func (w *chunkWriter) start() error {
	if w.next > maxChunkIndex {
		return fmt.Errorf("audiocapture: %s: more than %d chunks", opCreateChunk, maxChunkIndex)
	}
	n := w.next
	w.next++

	partial, _, err := w.chunkRefs(n)
	if err != nil {
		return fmt.Errorf("audiocapture: %s: %w", opCreateChunk, err)
	}
	f, err := w.files.Create(w.ctx, partial)
	if err != nil {
		return fmt.Errorf("audiocapture: %s: %w", opCreateChunk, err)
	}
	w.cur, w.curN, w.curFrames = f, n, 0
	w.buf = bufio.NewWriterSize(f, writeBuffer)

	if _, err := w.buf.Write(encodeHeader(w.format, 0, false)); err != nil {
		return fmt.Errorf("audiocapture: %s: %w", opWriteChunk, err)
	}
	return nil
}

// finish patches the final header in, syncs, closes and renames the current chunk. Without a current chunk it does
// nothing.
func (w *chunkWriter) finish() error {
	if w.cur == nil {
		return nil
	}
	f, buf, n, frames := w.cur, w.buf, w.curN, w.curFrames
	w.cur, w.buf = nil, nil

	var err error
	if data := frames * int64(w.format.BlockAlign()); data&1 == 1 {
		_, err = buf.Write([]byte{0}) // pad byte of an odd data chunk
	}
	if err == nil {
		err = buf.Flush()
	}
	if err == nil {
		_, err = f.WriteAt(encodeHeader(w.format, frames, true), 0)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return w.fail(fmt.Errorf("audiocapture: %s %d: %w", opFinishChunk, n, err))
	}

	partial, final, err := w.chunkRefs(n)
	if err == nil {
		err = w.files.Rename(w.ctx, partial, final)
	}
	if err != nil {
		return w.fail(fmt.Errorf("audiocapture: %s %d: %w", opRenameChunk, n, err))
	}

	if w.onChunk != nil {
		w.onChunk(n, frames, w.format)
	}
	return nil
}

// baseName is the last path segment of a reference.
func baseName(ref shared.ArtifactRef) string {
	p := ref.Path()
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
