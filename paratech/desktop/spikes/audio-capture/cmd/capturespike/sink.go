package main

import (
	"log/slog"

	"paratech/desktop/spikes/audio-capture/wasapi"
	"paratech/desktop/spikes/audio-capture/wavchunk"
)

// chunkSink connects a capture stream to a chunk writer. The writer is created on the first SetFormat, when the
// stream format is known. It is used from the capture goroutine only, and closed after the stream stopped.
type chunkSink struct {
	cfg wavchunk.Config
	w   *wavchunk.Writer
}

func toChunkFormat(f wasapi.Format) wavchunk.Format {
	return wavchunk.Format{SampleRate: f.SampleRate, Channels: f.Channels, BitsPerSample: f.BitsPerSample, Float: f.Float}
}

func (s *chunkSink) SetFormat(f wasapi.Format) error {
	if s.w == nil {
		w, err := wavchunk.New(s.cfg, toChunkFormat(f))
		if err != nil {
			return err
		}
		s.w = w
		return nil
	}
	return s.w.SetFormat(toChunkFormat(f))
}

func (s *chunkSink) Write(p []byte) error { return s.w.Write(p) }

func (s *chunkSink) Close() error {
	if s.w == nil {
		return nil
	}
	return s.w.Close()
}

// report validates every chunk file in dir and prints one line per file.
func report(dir string, log *slog.Logger) (valid, invalid int) {
	res, err := wavchunk.ValidateDir(dir)
	if err != nil {
		log.Error("validate chunks", "error", err.Error())
		return 0, 0
	}
	for _, r := range res {
		if r.Err != nil {
			invalid++
			log.Info("chunk check", "name", r.Name, "valid", false, "error", r.Err.Error())
			continue
		}
		valid++
		log.Info("chunk check", "name", r.Name, "valid", true, "duration", r.Info.Duration().String(),
			"frames", r.Info.Frames, "format", r.Info.Format.String())
	}
	log.Info("chunk summary", "valid", valid, "invalid", invalid)
	return valid, invalid
}
