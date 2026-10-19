package audiocapture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// readFrames is how many input frames the assembly decodes at a time.
const readFrames = 4096

// TrackFiles is the part of the Storage Manager that Finalize reads chunks and writes the track with.
// *filestore.Store satisfies it.
type TrackFiles interface {
	// List returns the direct children of dir sorted by name; a missing dir yields shared.ErrNotFound.
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)

	// Open returns a reader of the file.
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// Size returns the size of the file in bytes.
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)

	// Write stores the content atomically, replacing an existing file.
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}

// trackChunk is a readable chunk of the assembly.
type trackChunk struct {
	ref  shared.ArtifactRef
	size int64
	info chunkInfo
}

// segment is a run of consecutive chunks of one format, resampled as one stream so that no edge appears between them.
type segment struct {
	format Format
	chunks []trackChunk
	out    int64 // output frames
}

// assemble writes the readable final chunks of dir, in name order, as one 16 kHz mono 16-bit WAV track and returns
// its duration (frames / rate) and how many of its seconds are audible (TASK-082: a silent recording). Temporary and
// broken chunks are skipped and left on disk; without a readable chunk the error wraps recording.ErrNoAudio. Other
// errors are transient. It reads the chunks twice (check and count, then convert) and streams the track, so memory
// does not grow with the recording.
func assemble(ctx context.Context, files TrackFiles, dir, output shared.ArtifactRef, log *slog.Logger) (values.AssembledTrack, error) {
	segs, total, err := scanChunks(ctx, files, dir, log)
	if err != nil {
		return values.AssembledTrack{}, err
	}
	if total == 0 {
		return values.AssembledTrack{}, fmt.Errorf("audiocapture: %s: %w in %s", opAssemble, recording.ErrNoAudio, dir)
	}
	if total*int64(trackFormat().BlockAlign()) > maxDataBytes(trackFormat()) {
		return values.AssembledTrack{}, fmt.Errorf("audiocapture: %s: %d frames exceed the WAV size limit", opAssemble, total)
	}

	tr := &trackReader{ctx: ctx, files: files, segs: segs, total: total}
	defer tr.close()
	if err := files.Write(ctx, output, tr); err != nil {
		return values.AssembledTrack{}, fmt.Errorf("audiocapture: %s: %w", opAssemble, err)
	}
	if tr.written != total {
		return values.AssembledTrack{}, fmt.Errorf("audiocapture: %s: wrote %d of %d frames", opAssemble, tr.written, total)
	}
	tr.loud.close()
	return values.AssembledTrack{
		Duration: durationOf(total, TrackRate), Seconds: tr.loud.seconds, AudibleSeconds: tr.loud.audible,
	}, nil
}

// loudness counts the one-second windows of the track and those whose peak reaches values.HeardLevel.
type loudness struct {
	pos     int // samples of the open window
	peak    float64
	seconds int
	audible int
}

func (l *loudness) add(samples []float32) {
	for _, x := range samples {
		l.peak = max(l.peak, math.Abs(float64(x)))
		if l.pos++; l.pos == TrackRate {
			l.close()
		}
	}
}

// close ends the open window, if any.
func (l *loudness) close() {
	if l.pos == 0 {
		return
	}
	l.seconds++
	if values.ClampSoundLevel(l.peak).Heard() {
		l.audible++
	}
	l.pos, l.peak = 0, 0
}

// scanChunks validates the final chunks of dir and groups them into segments.
func scanChunks(ctx context.Context, files TrackFiles, dir shared.ArtifactRef, log *slog.Logger) ([]segment, int64, error) {
	refs, err := files.List(ctx, dir)
	if errors.Is(err, shared.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("audiocapture: %s: %w", opAssemble, err)
	}

	var segs []segment
	for _, ref := range refs {
		name := baseName(ref)
		if _, partial, ok := chunkNumber(name); !ok || partial {
			if ok {
				log.Warn("skipping an unfinished chunk", slog.String("chunk", name))
			}
			continue
		}

		c, err := checkChunk(ctx, files, ref)
		if errors.Is(err, errNotAudio) {
			log.Warn("skipping a broken chunk", slog.String("chunk", name), slog.String("reason", err.Error()))
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("audiocapture: %s: %s: %w", opAssemble, name, err)
		}
		if c.info.Frames == 0 {
			continue
		}

		if n := len(segs); n > 0 && segs[n-1].format == c.info.Format {
			segs[n-1].chunks = append(segs[n-1].chunks, c)
		} else {
			segs = append(segs, segment{format: c.info.Format, chunks: []trackChunk{c}})
		}
	}

	var total int64
	for i := range segs {
		var in int64
		for _, c := range segs[i].chunks {
			in += c.info.Frames
		}
		segs[i].out = outputFrames(in, segs[i].format.SampleRate, TrackRate)
		total += segs[i].out
	}
	return segs, total, nil
}

// checkChunk reads the header of one chunk.
func checkChunk(ctx context.Context, files TrackFiles, ref shared.ArtifactRef) (trackChunk, error) {
	size, err := files.Size(ctx, ref)
	if err != nil {
		return trackChunk{}, err
	}
	rc, err := files.Open(ctx, ref)
	if err != nil {
		return trackChunk{}, err
	}
	defer func() { _ = rc.Close() }()

	info, err := readHeader(bufio.NewReader(rc), size)
	if err != nil {
		return trackChunk{}, err
	}
	return trackChunk{ref: ref, size: size, info: info}, nil
}

// trackReader produces the WAV track: the header, then the converted samples segment by segment.
type trackReader struct {
	ctx     context.Context
	files   TrackFiles
	segs    []segment
	total   int64
	written int64
	loud    loudness

	pending []byte
	pos     int
	header  bool
	done    bool
	err     error

	seg      int
	chunk    int
	produced int64 // output frames of the current segment
	res      *resampler
	cur      io.ReadCloser
	in       *bufio.Reader
	left     int64 // input frames left in the current chunk

	raw  []byte
	mono []float32
	out  []float32
}

func (t *trackReader) Read(p []byte) (int, error) {
	for t.pos >= len(t.pending) {
		if t.err != nil {
			return 0, t.err
		}
		if t.done {
			return 0, io.EOF
		}
		t.pending, t.pos = t.pending[:0], 0
		if err := t.fill(); err != nil {
			t.err = err
		}
	}
	n := copy(p, t.pending[t.pos:])
	t.pos += n
	return n, nil
}

// fill produces the next piece of the track into pending.
func (t *trackReader) fill() error {
	if err := t.ctx.Err(); err != nil {
		return err
	}
	if !t.header {
		t.header = true
		t.pending = append(t.pending, encodeHeader(trackFormat(), t.total, true)...)
		return nil
	}
	if t.seg >= len(t.segs) {
		t.done = true
		return nil
	}

	s := &t.segs[t.seg]
	if t.res == nil {
		t.res, t.produced = newResampler(int(s.format.SampleRate), TrackRate), 0
	}

	switch {
	case t.cur != nil:
		return t.convert(s)
	case t.chunk < len(s.chunks):
		return t.open(s.chunks[t.chunk])
	}

	// The segment's input is consumed: the filter tail completes its frame count.
	t.emit(s, t.res.flush(s.out-t.produced, t.out[:0]))
	t.seg, t.chunk, t.res = t.seg+1, 0, nil
	return nil
}

// open reopens a chunk checked in the first pass; a chunk that changed since is an error, not a skip.
func (t *trackReader) open(c trackChunk) error {
	rc, err := t.files.Open(t.ctx, c.ref)
	if err != nil {
		return err
	}
	in := bufio.NewReader(rc)
	info, err := readHeader(in, c.size)
	if err == nil && info != c.info {
		err = errors.New("the chunk changed during the assembly")
	}
	if err != nil {
		_ = rc.Close()
		return fmt.Errorf("%s: %w", baseName(c.ref), err)
	}
	t.cur, t.in, t.left = rc, in, info.Frames
	return nil
}

// convert decodes the next block of the open chunk.
func (t *trackReader) convert(s *segment) error {
	block := s.format.BlockAlign()
	n := min(t.left, readFrames)
	if need := int(n) * block; cap(t.raw) < need {
		t.raw = make([]byte, need)
	}
	raw := t.raw[:int(n)*block]
	if _, err := io.ReadFull(t.in, raw); err != nil {
		return err
	}
	t.left -= n

	t.mono = toMono(s.format, raw, t.mono[:0])
	t.emit(s, t.res.push(t.mono, t.out[:0]))

	if t.left == 0 {
		err := t.cur.Close()
		t.cur, t.in = nil, nil
		t.chunk++
		return err
	}
	return nil
}

// emit encodes outputs, never more than the segment's frame count.
func (t *trackReader) emit(s *segment, out []float32) {
	t.out = out
	if room := s.out - t.produced; int64(len(out)) > room {
		out = out[:room]
	}
	t.produced += int64(len(out))
	t.written += int64(len(out))
	t.loud.add(out)
	t.pending = appendPCM16(t.pending, out)
}

func (t *trackReader) close() {
	if t.cur != nil {
		_ = t.cur.Close()
		t.cur = nil
	}
}
