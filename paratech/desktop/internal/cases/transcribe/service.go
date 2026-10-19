package transcribe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
)

// Steps of an attempt, for the log.
const (
	stepPayload    = "payload"
	stepTranscript = "load transcript"
	stepRecording  = "load recording"
	stepModel      = "load model"
	stepDocument   = "read document"
	stepEngine     = "transcribe"
	stepSave       = "save transcript"
	stepPublish    = "publish"
	stepCheckpoint = "checkpoint"
	stepLiveSave   = "store live parts"
	stepLiveDelete = "delete live parts"
	stepLoad       = "read the load mode"
	stepSpeed      = "store the measured speed"
)

// Service is the handler of STT jobs (PRD sections 4.3, 7 and 8): it transcribes the final audio of a COMPLETED
// recording with the model the job pins and stores the transcript. Raw audio never leaves the machine (AC-06): the
// engine is a local process and no port of this package talks to the network.
//
// One STT job produces one transcript whose id is the job id, with its document at transcript.DocumentRef, so every
// attempt of the job works on the same row and file:
//
//   - a stored transcript means an earlier attempt got that far: only the completion event is published again;
//   - a valid stored document means the engine already succeeded: the engine is not run again;
//   - otherwise the engine runs; it writes the document atomically and only on success, so a crash or a kill never
//     leaves a torn file, and a rerun overwrites what a crashed run left.
//
// The completion event is published at least once (EventPublisher). Errors are fixed texts (errors.go); missing or
// unusable inputs fail the job at once (backoff.Permanent), everything else is retried by the runner.
type Service struct {
	engine      STTEngine
	transcripts TranscriptRepository
	recordings  RecordingReader
	models      ModelReader
	artifacts   ArtifactStore
	events      EventPublisher
	clock       Clock
	live        LiveParts      // nil: no live transcript
	settings    SettingsReader // nil: the default load mode
	speeds      SpeedStore     // nil: no speed measurement
	log         *slog.Logger
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Engine == nil || d.Transcripts == nil || d.Recordings == nil || d.Models == nil || d.Artifacts == nil ||
		d.Events == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: engine, transcripts, recordings, models, artifacts, events and clock are required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		engine: d.Engine, transcripts: d.Transcripts, recordings: d.Recordings, models: d.Models, artifacts: d.Artifacts,
		events: d.Events, clock: d.Clock, live: d.Live, settings: d.Settings, speeds: d.Speeds, log: log,
	}, nil
}

// Handle runs one attempt of an STT job; it satisfies the Handler port of the job runner structurally (ARCH-20).
//
// Progress of the engine is reported as 0..90 percent, the stored document as 95 with CheckpointTranscribed. When a
// report fails (AC power lost, the job cancelled or changed elsewhere) the engine process is killed and Handle returns
// an error wrapping the runner's error, so the runner moves the job on (PRD section 7.4, section 14 case 12).
func (s *Service) Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, p job.Progress) error) error {
	if report == nil {
		report = func(context.Context, job.Progress) error { return nil }
	}

	req, err := values.ParseSTTPayload(snap.Payload())
	if err != nil {
		return s.permanent(snap, stepPayload, ErrInvalidPayload, err)
	}
	modelID := snap.PinnedModelID()
	if modelID == "" {
		return s.permanent(snap, stepPayload, ErrNoPinnedModel, nil)
	}
	id := shared.TranscriptID(snap.ID())
	docRef, err := transcript.DocumentRef(id)
	if err != nil {
		return s.permanent(snap, stepPayload, ErrInvalidPayload, err)
	}

	// An earlier attempt stored the transcript and then crashed or failed to publish.
	switch t, err := s.transcripts.Get(ctx, id); {
	case err == nil:
		return s.finish(ctx, snap, req, t)
	case !errors.Is(err, shared.ErrNotFound):
		return s.retry(snap, stepTranscript, ErrStorage, err)
	}

	rec, audio, model, err := s.inputs(ctx, snap, req, modelID)
	if err != nil {
		return err
	}

	doc, err := s.prepareDocument(ctx, snap, rec, audio, model, docRef, report)
	if err != nil {
		return err
	}

	// When an earlier attempt's document is resumed, Created is the time of this attempt, not of the engine run.
	t, err := transcript.New(transcript.Spec{
		ID: id, RecordingID: rec.ID(), ModelID: model.ID(), ModelName: model.Name(), ModelVersion: model.ModelVersion(),
		Language: doc.Language(), Text: docRef,
	}, s.clock.Now())
	if err != nil {
		return s.permanent(snap, stepSave, ErrInvalidTranscript, err)
	}

	if err := s.transcripts.Save(ctx, t); err != nil {
		if !errors.Is(err, shared.ErrConflict) {
			return s.retry(snap, stepSave, ErrStorage, err)
		}
		// Another attempt of this job stored it meanwhile; a missing row means the conflict was a foreign key
		// (the recording was deleted), which the next attempt reports as such.
		stored, gerr := s.transcripts.Get(ctx, id)
		if gerr != nil {
			return s.retry(snap, stepSave, ErrStorage, errors.Join(err, gerr))
		}
		t = stored
	}

	return s.finish(ctx, snap, req, t)
}

// inputs loads the recording with its final track and the pinned model.
func (s *Service) inputs(ctx context.Context, snap job.Snapshot, req values.STTRequest, modelID shared.SttModelID) (
	*recording.Recording, shared.ArtifactRef, *sttmodel.Model, error) {
	none := shared.ArtifactRef{}
	rec, err := s.recordings.Get(ctx, req.Recording())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, none, nil, s.permanent(snap, stepRecording, ErrRecordingNotFound, err)
	case err != nil:
		return nil, none, nil, s.retry(snap, stepRecording, ErrStorage, err)
	}
	audio, ok := rec.TranscribableAudio()
	if !ok {
		return nil, none, nil, s.permanent(snap, stepRecording, ErrRecordingNotReady, nil)
	}

	model, err := s.models.Get(ctx, modelID)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, none, nil, s.permanent(snap, stepModel, ErrModelNotInstalled, err)
	case err != nil:
		return nil, none, nil, s.retry(snap, stepModel, ErrStorage, err)
	}

	return rec, audio, model, nil
}

// prepareDocument returns the document of this job, running the engine unless an earlier attempt stored a valid one,
// and reports the checkpoint.
func (s *Service) prepareDocument(ctx context.Context, snap job.Snapshot, rec *recording.Recording,
	audio shared.ArtifactRef, model *sttmodel.Model, docRef shared.ArtifactRef,
	report func(context.Context, job.Progress) error) (transcript.Document, error) {
	doc, err := s.readDocument(ctx, docRef)
	switch {
	case err == nil:
		s.log.Info("stt job resumes after the engine run", slog.String("job", snap.ID().String()))
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, transcript.ErrInvalidDocument):
		if err := s.runEngine(ctx, snap, rec, audio, model, docRef, report); err != nil {
			return transcript.Document{}, err
		}
		if doc, err = s.readDocument(ctx, docRef); err != nil {
			if errors.Is(err, shared.ErrNotFound) || errors.Is(err, transcript.ErrInvalidDocument) {
				return transcript.Document{}, s.permanent(snap, stepDocument, ErrInvalidDocument, err)
			}
			return transcript.Document{}, s.retry(snap, stepDocument, ErrStorage, err)
		}
	default:
		return transcript.Document{}, s.retry(snap, stepDocument, ErrStorage, err)
	}

	p, err := job.NewProgress(documentStored, CheckpointTranscribed)
	if err != nil {
		return transcript.Document{}, s.permanent(snap, stepCheckpoint, ErrStorage, err)
	}
	if err := report(ctx, p); err != nil {
		return transcript.Document{}, s.stopped(snap, err)
	}
	return doc, nil
}

// runEngine runs the STT engine in the load mode read now and forwards its progress to the runner. A failed report
// kills the engine. The segments recognised meanwhile are stored in batches (live). A successful run is measured.
func (s *Service) runEngine(ctx context.Context, snap job.Snapshot, rec *recording.Recording, audio shared.ArtifactRef,
	model *sttmodel.Model, out shared.ArtifactRef, report func(context.Context, job.Progress) error) error {
	engineCtx, kill := context.WithCancel(ctx)
	defer kill()

	live := s.newLiveWriter(snap, rec.ID())
	// Parts recognised before a stop or a failure stay for the viewer; the store does not depend on the job's context.
	defer live.flush(context.WithoutCancel(ctx), true)

	var mu sync.Mutex
	var stop error
	last := -1
	progress := func(percent int) {
		scaled := min(max(percent, 0), 100) * engineShare / 100

		// Calls never overlap; the lock orders them with the read below for the race detector.
		mu.Lock()
		defer mu.Unlock()
		if stop != nil || scaled <= last {
			return
		}
		last = scaled
		p, err := job.NewProgress(scaled, "")
		if err != nil {
			return
		}
		if err := report(ctx, p); err != nil {
			stop = err
			kill()
		}
		live.flush(ctx, false)
	}

	load := s.loadMode(ctx, snap)
	started := s.clock.Now()
	err := s.engine.Transcribe(engineCtx, audio, model.File(), out, load, progress, live.segment(ctx))

	mu.Lock()
	stopErr := stop
	mu.Unlock()
	switch {
	case stopErr != nil:
		// Even if the engine finished in the meantime, the runner asked to stop; the next attempt finds the document.
		return s.stopped(snap, stopErr)
	case err == nil:
		s.measure(ctx, snap, values.SpeedKey{Model: model.ID(), Load: load}, s.clock.Now().Sub(started), rec)
		return nil
	case ctx.Err() != nil:
		return s.retry(snap, stepEngine, ErrTranscriptionFailed, err)
	case errors.Is(err, shared.ErrNotFound):
		return s.permanent(snap, stepEngine, ErrInputMissing, err)
	}
	// backoff.Classify still fails at once on a permanent cause (an invalid reference, permission denied).
	return s.retry(snap, stepEngine, ErrTranscriptionFailed, err)
}

// readDocument reads and validates the stored document; a missing one yields shared.ErrNotFound.
func (s *Service) readDocument(ctx context.Context, ref shared.ArtifactRef) (transcript.Document, error) {
	rc, err := s.artifacts.Open(ctx, ref)
	if err != nil {
		return transcript.Document{}, err
	}
	// Closed before the engine runs: on Windows an open reader blocks the atomic replace of the file.
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, transcript.MaxDocumentBytes+1))
	if err != nil {
		return transcript.Document{}, err
	}
	return transcript.ParseDocument(data)
}

// finish checks that the stored transcript is this job's and publishes the completion event.
func (s *Service) finish(ctx context.Context, snap job.Snapshot, req values.STTRequest, t *transcript.Transcript) error {
	if t.RecordingID() != req.Recording() {
		return s.permanent(snap, stepTranscript, ErrTranscriptConflict, nil)
	}
	s.deleteLive(ctx, snap, t.RecordingID())
	if err := s.events.TranscriptCreated(ctx, t.Created()); err != nil {
		return s.retry(snap, stepPublish, ErrPublish, err)
	}
	return nil
}

// permanent fails the job at once; the cause goes to the log only.
func (s *Service) permanent(snap job.Snapshot, step string, msg, cause error) error {
	s.warn(snap, step, msg, cause)
	return backoff.Permanent(fail(msg, cause))
}

// retry leaves the decision to the runner: transient unless the cause is classified permanent.
func (s *Service) retry(snap job.Snapshot, step string, msg, cause error) error {
	s.warn(snap, step, msg, cause)
	return fail(msg, cause)
}

// stopped wraps the runner's report error, which the runner recognises (powerpolicy.ErrPowerLost, a conflict).
func (s *Service) stopped(snap job.Snapshot, cause error) error {
	s.log.Info("stt job stopped at a checkpoint", slog.String("job", snap.ID().String()), slog.Any("reason", cause))
	return fail(ErrStopped, cause)
}

func (s *Service) warn(snap job.Snapshot, step string, msg, cause error) {
	attrs := []any{slog.String("job", snap.ID().String()), slog.String("step", step), slog.String("error", msg.Error())}
	if cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	s.log.Warn("stt job step failed", attrs...)
}
