package recordingimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// importedIDPrefix starts the id of every imported recording, so its copy recordings/imp-<id>.<format> is told apart
// from the files of a capture: the generated ids (lower-case base32) never contain a hyphen.
const importedIDPrefix = "imp-"

// OrphanAge is how old a copy without a recording must be before SweepOrphans deletes it: older than any copy still
// on its way to its recording.
const OrphanAge = 10 * time.Minute

// Operation names of the sweep.
const (
	opSweep       = "recordingimport: sweep the copies without a recording"
	opSweepFile   = "recordingimport: check a copy without a recording"
	opSweepDelete = "recordingimport: deleted a copy without a recording"
)

// importedID is the id of a new imported recording.
func importedID(generated shared.RecordingID) shared.RecordingID {
	return shared.RecordingID(importedIDPrefix + string(generated))
}

// importedCopyOf returns the recording id of ref when ref is named as an import names its copy:
// recordings/imp-<id>.<format of an import>. Chunk directories and tracks of a capture never match.
func importedCopyOf(ref shared.ArtifactRef) (shared.RecordingID, bool) {
	name := ref.Path()
	dot := strings.LastIndexByte(name, '.')
	if ref.Kind() != shared.ArtifactRecordings || dot < 0 || strings.Contains(name, "/") {
		return "", false
	}
	id := shared.RecordingID(name[:dot])
	if rest, ok := strings.CutPrefix(string(id), importedIDPrefix); !ok || rest == "" {
		return "", false
	}
	want, err := recording.ImportedTrackRef(id, recording.AudioFormat(name[dot+1:]))
	return id, err == nil && want == ref
}

// SweepOrphans deletes, through the Storage Manager, the copies a crash left between the copy and the stored
// recording: files named as an import names its copies, older than OrphanAge and without a recording of their id. It
// runs at start-up. It returns how many it deleted; it keeps going after the failure of one file and returns the first
// error.
func (s *Service) SweepOrphans(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	refs, err := s.store.ListKind(ctx, shared.ArtifactRecordings)
	if err != nil {
		return 0, fmt.Errorf("%s: %w: %w", opSweep, ErrStorage, err)
	}
	before := s.clock.Now().Add(-OrphanAge)

	n := 0
	var first error
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		id, ok := importedCopyOf(ref)
		if !ok {
			continue
		}
		orphan, err := s.orphan(ctx, ref, id, before)
		if err == nil && orphan {
			err = s.store.Delete(ctx, ref)
		}
		switch {
		case err != nil:
			s.log.Warn(opSweepFile, slog.String("file", ref.String()), slog.Any("error", err))
			if first == nil {
				first = fmt.Errorf("%s: %w: %w", opSweep, ErrStorage, err)
			}
		case orphan:
			n++
			s.log.Info(opSweepDelete, slog.String("file", ref.String()))
		}
	}
	return n, first
}

// orphan reports whether the copy at ref was last written before the time and no recording has its id. A file gone
// meanwhile or that is a directory is none.
func (s *Service) orphan(ctx context.Context, ref shared.ArtifactRef, id shared.RecordingID, before time.Time) (bool, error) {
	at, err := s.store.ModTime(ctx, ref)
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, shared.ErrConflict):
		return false, nil
	case err != nil:
		return false, err
	case !at.Before(before):
		return false, nil
	}

	_, err = s.recordings.Get(ctx, id)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, shared.ErrNotFound):
		return true, nil
	}
	return false, err
}
