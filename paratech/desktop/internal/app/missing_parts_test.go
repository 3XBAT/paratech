package app

// The alpha build (TASK-061, AC 3): no whisper-cli next to the executable, no Hub or mail settings (TASK-057: the real
// adapters with empty settings and credential stores) and no recorder (a build without capture).
// The core still opens and starts, the pages keep working, and every job ends in a state the jobs page can explain.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// alphaEnv is productionEnv with whisper-cli missing and the recorder of a build without capture ("no recorder"), so
// the test never starts a recorder child. Only what a test cannot take from the machine is replaced besides: the
// power source (on a laptop on battery the STT job would wait), the credential stores (empty, as on a fresh install:
// tests never touch the real Credential Manager) and the runner's poll interval and stop timeout.
func alphaEnv(t *testing.T) env {
	t.Helper()
	e := productionEnv()
	e.speech.bundledDir = t.TempDir() // nothing next to the executable, nothing installed
	e.recorder = func(string, *filestore.Store, *slog.Logger) (recordingsession.Recorder, error) {
		return audiocapture.Unavailable{}, nil
	}
	e.power = testenv.NewFakePower(true)
	e.secrets = func(wincred.Profile, string) (secretStore, error) { return testenv.NewMemCredentials(), nil }
	e.runner.PollInterval = 10 * time.Millisecond
	e.stopTimeout = 5 * time.Second
	return e
}

func TestAlphaCoreWithoutMissingPartsStaysUsable(t *testing.T) {
	ctx := context.Background()
	var logs syncBuffer
	cfg := config.Default()
	cfg.Headless = true
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.env = alphaEnv(t)
	t.Cleanup(func() { _ = a.Close() }) // before TempDir removal: Windows cannot delete open files

	core := startCore(t, a)
	out := logs.String()
	for _, line := range []string{
		"instance lock acquired",
		"speech recognition is not available: whisper-cli and its libraries are missing or do not verify",
		"transcription jobs wait in the queue until the speech component is installed",
		"background work started",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("start log lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out, "level=ERROR") {
		t.Errorf("start log has errors:\n%s", out)
	}

	settings := wails.NewSettingsService(core.Settings, slog.New(slog.DiscardHandler))
	if _, err := settings.GetSettings(ctx); err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got, err := settings.SetProcessOnlyOnACPower(ctx, false); err != nil || got.ProcessOnlyOnACPower {
		t.Fatalf("SetProcessOnlyOnACPower(false) = %+v, %v", got, err)
	}
	page := pageBindings(core)
	if recs, err := page.ListRecordings(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("ListRecordings = %+v, %v; want an empty list", recs, err)
	}
	if jobs, err := page.ListJobs(ctx); err != nil || len(jobs) != 0 {
		t.Fatalf("ListJobs = %+v, %v; want an empty list", jobs, err)
	}

	checkRecorderUnavailable(t, a, page)

	// One job per missing part, seeded as the producers of this build would store them.
	model := installModel(t, a, "m1", testenv.WhisperOK)
	recSTT := completedRecording(t, a, "rec-stt")
	enqueueSTT(t, a, "job-stt", recSTT, model)
	recLLM := completedRecording(t, a, "rec-llm")
	tr := storeTranscript(t, a, "tr-1", recLLM, model)
	enqueueSummary(t, a, "job-summary", tr)
	sendHomework(t, a, tr)
	if ok, err := a.core.deliveries.MailPasswordConfigured(ctx); err != nil || ok {
		t.Errorf("MailPasswordConfigured = %v, %v; want no password on a fresh install", ok, err)
	}

	// The jobs are read from the store: since TASK-076 the jobs page lists neither stt nor summarize jobs, the rows of
	// their recordings show them (checked below).
	tests := []struct {
		name     string
		typ      job.Type
		status   job.Status
		attempts int
		lastErr  string // the exact text of the error column
	}{
		// Parked without using an attempt: it runs once a build with a Hub starts.
		{"no hub", homeworkflow.SummarizeJobType, job.StatusWaitingForNetwork, 0, ""},
		// A definite refusal, never an unknown outcome: nothing was handed over.
		{
			"no mail settings", senddelivery.JobType, job.StatusFailed, 1,
			"permanent failure: senddelivery: send: smtp: mail is not configured: delivery rejected",
		},
		// TASK-060: the stt type is not registered without the speech component, so the job waits QUEUED with its key
		// unused (installing the component activates it without a restart: TestSpeechComponentInstallsWithoutRestart)
		// instead of failing. Last: by now the runner has settled the other jobs, so it had every chance to claim this one.
		{"no whisper-cli", transcribe.JobType, job.StatusQueued, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *job.Job
			testenv.Eventually(t, string(tt.typ)+" job settles", func() bool {
				got = jobOfType(t, a, tt.typ)
				return got != nil && got.Status() == tt.status
			}, nil)
			if got.Attempts() != tt.attempts || got.Status().OffersRunNow() {
				t.Errorf("job %s %s: want %d attempts, no Run now", got.ID(), got.Status(), tt.attempts)
			}
			if got.LastError() != tt.lastErr {
				t.Errorf("error column %q, want %q", got.LastError(), tt.lastErr)
			}
		})
	}

	// TASK-076: each recording row shows the step its chain waits at, with the reason. TASK-083: the parked summary
	// says the Hub is not set up, not "no network", and offers the connections page.
	rows, err := page.ListRecordings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]string{}
	for _, r := range rows {
		stages[r.ID] = r.Stage + "/" + r.Step + "/" + r.QueueReason + "/" + r.Action
	}
	if stages[string(recSTT)] != "queued/transcription/inLine/" ||
		stages[string(recLLM)] != "queued/summary/serviceNotConfigured/openConnections" {
		t.Errorf("chain stages %v: want the transcription in line and the summary waiting for the Hub setup", stages)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// checkRecorderUnavailable starts a recording on the recorder of this build: the start fails, and the recordings page
// shows a failed recording with the reason.
func checkRecorderUnavailable(t *testing.T, a *App, page *wails.ActivityService) {
	t.Helper()
	ctx := context.Background()
	var startErr error
	testenv.Eventually(t, "the recording start is refused", func() bool {
		_, startErr = a.core.recordings.Start(ctx, recordingsession.StartRequest{})
		return !errors.Is(startErr, recordingsession.ErrRecovering)
	}, nil)
	if !errors.Is(startErr, recordingsession.ErrStartFailed) || !errors.Is(startErr, audiocapture.ErrUnavailable) {
		t.Fatalf("Start = %v, want a failed start of the unavailable recorder", startErr)
	}

	recs, err := page.ListRecordings(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("ListRecordings = %+v, %v; want the failed recording", recs, err)
	}
	r := recs[0]
	if r.Status != string(recording.StatusFailed) || r.Stoppable || !strings.Contains(r.Problem, "not available in this build") {
		t.Fatalf("recording %+v: want FAILED, not stoppable, with the reason", r)
	}
}

// jobOfType returns the stored job of typ, or nil.
func jobOfType(t *testing.T, a *App, typ job.Type) *job.Job {
	t.Helper()
	jobs, _, err := sqlite.NewJobRepository(a.core.db).ListActivity(context.Background(), 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Type() == typ {
			return j
		}
	}
	return nil
}

// storeTranscript stores the transcript of a recording with a readable document, as a finished STT job leaves it.
func storeTranscript(t *testing.T, a *App, id shared.TranscriptID, rec shared.RecordingID, model shared.SttModelID) shared.TranscriptID {
	t.Helper()
	ctx := context.Background()
	doc, err := transcript.NewDocument("ru", []transcript.Segment{
		{StartMs: 0, EndMs: 5000, Text: "Homework: read chapter 4 by Friday."},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := transcript.DocumentRef(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.store.Write(ctx, ref, strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
	tr, err := transcript.New(transcript.Spec{
		ID: id, RecordingID: rec, ModelID: model, ModelName: "base", ModelVersion: "1", Language: "ru", Text: ref,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewTranscriptRepository(a.core.db).Save(ctx, tr); err != nil {
		t.Fatal(err)
	}
	return id
}

// enqueueSummary stores the summary job of a transcript, as the post-processor enqueues it after STT.
func enqueueSummary(t *testing.T, a *App, id shared.JobID, tr shared.TranscriptID) {
	t.Helper()
	req, err := cvalues.NewPostProcessRequest(tr)
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{
		ID: id, Type: homeworkflow.SummarizeJobType, Key: "summarize:" + string(tr) + ":v1", Payload: req.Payload(),
		MaxAttempts: 3,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := sqlite.NewJobRepository(a.core.db).Enqueue(context.Background(), j); err != nil || !created {
		t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
}

// sendHomework confirms sending the exported homework of an assignment of tr, which enqueues the email job.
func sendHomework(t *testing.T, a *App, tr shared.TranscriptID) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	details, err := assignment.NewDetails("Read chapter 4", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	asg, err := assignment.NewExtracted(assignment.Spec{
		ID: "a1", Origin: assignment.Origin{Transcript: tr}, PromptVersion: 1, Details: details,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewAssignmentRepository(a.core.db).Save(ctx, asg); err != nil {
		t.Fatal(err)
	}

	const content = "Homework: chapter 4"
	ref, err := proposal.ExportRef("a1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.store.Write(ctx, ref, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	digest, err := proposal.NewDigest(hex.EncodeToString(sum[:]), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := proposal.NewRecipient("teacher@school.example")
	if err != nil {
		t.Fatal(err)
	}
	p, err := proposal.New("p1", cvalues.NewUntrustedOutput(`{"kind":"send_homework_email","assignment_id":"a1"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true, Recipient: recipient, Attachment: digest}, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm(now); err != nil {
		t.Fatal(err)
	}
	cmd, err := p.EmailCommand()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.deliveries.SendHomework(ctx, cmd); err != nil {
		t.Fatalf("SendHomework: %v", err)
	}
}
