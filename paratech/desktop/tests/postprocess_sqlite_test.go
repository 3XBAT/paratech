package tests

// Scenario tests of the post-processing of a transcript (TASK-042, PRD sections 7, 9 and 10, AC-11, AC-13, AC-18) on
// the real stack: SQLite, the file store, the job runner, the transcribe and homeworkflow use cases. Only the outside
// world is fake (ARCH-61): the clock, the STT engine, and the LLM (testenv.FakeLLM, or the real Hub client against
// testenv.FakeHub).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/transcript"
	tvalues "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// The adapters satisfy the ports of the post-processing, and the PostProcessor is both the STT completion consumer
// and the handler of its jobs.
var (
	_ homeworkflow.LLMClient             = (*hubclient.Client)(nil)
	_ homeworkflow.TranscriptReader      = (*sqlite.TranscriptRepository)(nil)
	_ homeworkflow.RecordingReader       = (*sqlite.RecordingRepository)(nil)
	_ homeworkflow.LessonReader          = (*sqlite.LessonRepository)(nil)
	_ homeworkflow.SummaryRepository     = (*sqlite.SummaryRepository)(nil)
	_ homeworkflow.LectureTextRepository = (*sqlite.LectureTextRepository)(nil)
	_ homeworkflow.AssignmentRepository  = (*sqlite.AssignmentRepository)(nil)
	_ homeworkflow.JobEnqueuer           = (*sqlite.JobRepository)(nil)
	_ homeworkflow.ArtifactStore         = (*filestore.Store)(nil)
	_ homeworkflow.Tx                    = (*sqlite.DB)(nil)
	_ hubclient.Credentials              = (*testenv.MemCredentials)(nil)
	_ transcribe.EventPublisher          = (*homeworkflow.PostProcessor)(nil)
	_ jobrunner.Handler                  = (*homeworkflow.PostProcessor)(nil)
)

// The student's zone; the lesson is on Wednesday 14 October 2026, 10:00 MSK.
var (
	ppZone        = time.FixedZone("MSK", 3*60*60)
	ppLessonStart = time.Date(2026, 10, 14, 10, 0, 0, 0, ppZone)
	ppNow         = time.Date(2026, 10, 14, 12, 0, 0, 0, ppZone)
)

const (
	ppSummary = `{"sections":[{"heading":"Rivers","text":"Rivers and their deltas.","points":[]}],` +
		`"key_points":["Rivers flow to the sea"]}`
	// ppSummaryText is ppSummary as plain text (summary.Content.Text).
	ppSummaryText = "Rivers:\nRivers and their deltas."
	ppExtract     = `{"assignments":[` +
		`{"title":"Read chapter 4","description":"Pages 40-52","due":"2026-10-16T23:59:00+03:00"},` +
		`{"title":"Draw a map of rivers","description":"","due":"2026-10-21T10:00:00+03:00"}]}`
)

// postIDs generates job and assignment ids for the scenario.
type postIDs struct {
	mu sync.Mutex
	n  int
}

func (g *postIDs) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s-%d", prefix, g.n)
}

func (g *postIDs) NewAssignmentID() shared.AssignmentID { return shared.AssignmentID(g.next("asg")) }
func (g *postIDs) NewJobID() shared.JobID               { return shared.JobID(g.next("job")) }
func (g *postIDs) NewProposalID() proposal.ID           { return proposal.ID(g.next("prop")) }

type ppEnv struct {
	t           *testing.T
	db          *sqlite.DB
	store       *filestore.Store
	clock       *testenv.FakeClock
	jobs        *sqlite.JobRepository
	assignments *sqlite.AssignmentRepository
	summaries   *sqlite.SummaryRepository
	post        *homeworkflow.PostProcessor
	runner      *jobrunner.Service
}

// newPPEnv wires the stack with llm as the LLM Client and stores lesson l1, its completed recording r1 and model m1.
func newPPEnv(t *testing.T, llm homeworkflow.LLMClient) *ppEnv {
	t.Helper()
	return newPPEnvWith(t, llm, []transcript.Segment{
		{StartMs: 0, EndMs: 4000, Text: "Today: rivers and their deltas."},
		{StartMs: 4000, EndMs: 9000, Text: "Homework: read chapter 4 by Friday and draw a map of rivers by next Wednesday."},
	})
}

// newPPEnvWith is newPPEnv whose STT engine recognises segs.
func newPPEnvWith(t *testing.T, llm homeworkflow.LLMClient, segs []transcript.Segment) *ppEnv {
	t.Helper()
	dir := t.TempDir()
	e := &ppEnv{t: t, clock: testenv.NewFakeClock(ppNow)}

	store, err := filestore.New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	e.store = store
	e.db = openRunnerDB(t, filepath.Join(dir, "app.db"))
	e.jobs = sqlite.NewJobRepository(e.db)
	e.assignments = sqlite.NewAssignmentRepository(e.db)
	e.summaries = sqlite.NewSummaryRepository(e.db)
	recordings, lessons, transcripts := sqlite.NewRecordingRepository(e.db), sqlite.NewLessonRepository(e.db), sqlite.NewTranscriptRepository(e.db)

	e.post, err = homeworkflow.NewPostProcessor(homeworkflow.PostProcessDeps{
		LLM: llm, Transcripts: transcripts, Recordings: recordings, Lessons: lessons, Artifacts: store,
		Summaries: e.summaries, Assignments: e.assignments, LectureTexts: sqlite.NewLectureTextRepository(e.db), Jobs: e.jobs,
		Clock: e.clock, IDs: &postIDs{},
		ModelAlias: "default", MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	doc, err := transcript.NewDocument("ru", segs)
	if err != nil {
		t.Fatal(err)
	}
	stt, err := transcribe.New(transcribe.Deps{
		Engine: testenv.NewFakeSTT(store, doc), Transcripts: transcripts, Recordings: recordings,
		Models: sqlite.NewModelRepository(e.db), Artifacts: store, Events: e.post, Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.runner, err = jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, runnerConfig(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, reg := range []jobrunner.Registration{
		{Type: transcribe.JobType, Handler: stt, Heavy: true, Recovery: jobrunner.RecoveryRequeue},
		{Type: homeworkflow.SummarizeJobType, Handler: e.post, Recovery: jobrunner.RecoveryRequeue},
		{Type: homeworkflow.ExtractHomeworkJobType, Handler: e.post, Recovery: jobrunner.RecoveryRequeue},
	} {
		if err := e.runner.Register(reg); err != nil {
			t.Fatal(err)
		}
	}

	e.seed(lessons, recordings)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := e.runner.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := e.db.Close(); err != nil {
			t.Errorf("Close database: %v", err)
		}
		if err := e.store.Close(); err != nil {
			t.Errorf("Close store: %v", err)
		}
	})
	return e
}

// seed stores lesson l1, the completed recording r1 of it with its track, model m1 and the STT job of r1.
func (e *ppEnv) seed(lessons *sqlite.LessonRepository, recordings *sqlite.RecordingRepository) {
	e.t.Helper()
	ctx := context.Background()
	l, err := lesson.New(lesson.Spec{ID: "l1", Content: lesson.Content{
		Title: "Geography", Start: ppLessonStart, End: ppLessonStart.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
	}}, ppNow.Add(-48*time.Hour))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := lessons.Save(ctx, l); err != nil {
		e.t.Fatal(err)
	}

	start := ppLessonStart.Add(time.Minute)
	r, err := recording.New(recording.Spec{ID: "r1", LessonID: "l1", MaxDuration: 3 * time.Hour}, start)
	if err != nil {
		e.t.Fatal(err)
	}
	end := start.Add(85 * time.Minute)
	track, err := r.TrackRef("wav")
	if err != nil {
		e.t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return r.Begin(start) }, func() error { return r.Stop(end) }, func() error { return r.Finalize(end) },
		func() error { return r.Complete(end, track, 85*time.Minute) },
	} {
		if err := step(); err != nil {
			e.t.Fatal(err)
		}
	}
	if err := e.store.Write(ctx, track, strings.NewReader("RIFF fake audio")); err != nil {
		e.t.Fatal(err)
	}
	if err := recordings.Save(ctx, r); err != nil {
		e.t.Fatal(err)
	}

	file, err := shared.NewArtifactRef(shared.ArtifactModels, "ggml-base.bin")
	if err != nil {
		e.t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: "m1", Name: "base", ModelVersion: "1", File: file, SizeBytes: 1, SHA256: tvalues.SHA256(strings.Repeat("ab", 32)),
	}, ppNow.Add(-72*time.Hour))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := sqlite.NewModelRepository(e.db).Save(ctx, m); err != nil {
		e.t.Fatal(err)
	}

	req, err := tvalues.NewSTTRequest("r1")
	if err != nil {
		e.t.Fatal(err)
	}
	j, err := job.New(job.Spec{
		ID: "stt-1", Type: transcribe.JobType, Key: "stt:r1:m1", Payload: req.Payload(), PinnedModelID: "m1", MaxAttempts: 2,
	}, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, j); err != nil {
		e.t.Fatal(err)
	}
}

func (e *ppEnv) start() {
	e.t.Helper()
	if err := e.runner.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

// postJob returns the post-processing job of typ for the transcript of the STT job (its id is the STT job id).
func (e *ppEnv) postJob(typ job.Type) (*job.Job, bool) {
	e.t.Helper()
	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Type() == typ {
			return j, true
		}
	}
	return nil, false
}

// waitPost drives the clock until the post-processing job of typ is in status s.
func (e *ppEnv) waitPost(typ job.Type, s job.Status) *job.Job {
	e.t.Helper()
	var found *job.Job
	testenv.Eventually(e.t, fmt.Sprintf("%s job %s", typ, s), func() bool {
		j, ok := e.postJob(typ)
		found = j
		return ok && j.Status() == s
	}, func() { e.clock.Advance(time.Second) })
	return found
}

// The transcript id is the STT job id (TASK-029).
const ppTranscript shared.TranscriptID = "stt-1"

func (e *ppEnv) storedAssignments() []*assignment.Assignment {
	e.t.Helper()
	list, err := e.assignments.ListByTranscript(context.Background(), ppTranscript)
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

// checkStructures verifies the summary and the two assignments with deadlines of ppSummary and ppExtract.
func (e *ppEnv) checkStructures() {
	e.t.Helper()
	s, err := e.summaries.Get(context.Background(), summary.IDOf(ppTranscript))
	if err != nil {
		e.t.Fatalf("summary: %v", err)
	}
	if s.Content().Text() != ppSummaryText || s.LessonID() != "l1" || s.PromptVersion() != prompts.Version {
		e.t.Fatalf("summary = %+v", s.State())
	}

	list := e.storedAssignments()
	if len(list) != 2 {
		e.t.Fatalf("assignments = %d, want 2", len(list))
	}
	wantDue := []time.Time{
		time.Date(2026, 10, 16, 20, 59, 0, 0, time.UTC),
		time.Date(2026, 10, 21, 7, 0, 0, 0, time.UTC),
	}
	for i, a := range list {
		if !a.Details().Due().Equal(wantDue[i]) || a.LessonID() != "l1" || a.UserEdited() || a.SourceIndex() != i {
			e.t.Fatalf("assignment %d = %+v, want due %v", i, a.State(), wantDue[i])
		}
	}
}

// Test step 2 and AC-1, AC-2 of TASK-042: STT completes, both jobs are enqueued and run, and the summary and two
// deadlines are in the database.
func TestTranscriptToSummaryAndDeadlines(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e := newPPEnv(t, llm)
	e.start()

	sum := e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	ext := e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	e.checkStructures()

	for _, j := range []*job.Job{sum, ext} {
		if j.Payload() != `{"transcript_id":"stt-1"}` || j.Attempts() != 1 {
			t.Fatalf("job %s: payload %s attempts %d", j.Type(), j.Payload(), j.Attempts())
		}
	}

	// Only the transcript text and the lesson context went to the LLM: no audio, no file references (AC-06, section 9.1).
	reqs := llm.Requests()
	if len(reqs) != 2 {
		t.Fatalf("LLM requests = %d, want 2", len(reqs))
	}
	for _, r := range reqs {
		data := r.UntrustedData()
		if !strings.Contains(data, "read chapter 4 by Friday") || strings.Contains(data, "recordings/") ||
			strings.Contains(data, "RIFF") || strings.Contains(data, "transcripts/") {
			t.Fatalf("%s untrusted data = %s", r.Operation(), data)
		}
	}
}

// AC-4 of TASK-042: end to end through the real Hub client and a fake Hub over TLS.
func TestTranscriptToStructuresThroughFakeHub(t *testing.T) {
	hub := testenv.NewFakeHub()
	hub.Answer(values.OperationSummary, ppSummary)
	hub.Answer(values.OperationExtractHomework, ppExtract)
	url, httpClient := testenv.StartFakeHub(t, hub)

	creds := testenv.NewMemCredentials()
	if err := creds.Set(context.Background(), hubclient.DefaultCredentialName, "la_test_key_0123456789"); err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(hubclient.Config{BaseURL: url, HTTPClient: httpClient, RequestTimeout: 10 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}

	e := newPPEnv(t, client)
	e.start()
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	e.checkStructures()

	reqs := hub.Requests()
	if len(reqs) != 2 || hub.Charged() != 2 {
		t.Fatalf("hub requests = %d, charged = %d; want 2 and 2", len(reqs), hub.Charged())
	}
	for i, r := range reqs {
		spec := r.GetSpec()
		if spec.GetModelAlias() != "default" || !strings.HasPrefix(spec.GetIdempotencyKey(), "pp:") ||
			!strings.HasPrefix(spec.GetUntrustedData(), prompts.DataOpen) || strings.Contains(spec.GetInstructions(), "chapter 4") {
			t.Fatalf("request %d = %v", i, spec)
		}
		if hub.Authorizations()[i] != "Bearer la_test_key_0123456789" {
			t.Fatalf("request %d not authenticated with the tester key", i)
		}
	}
}

// fakeHubClient returns the real Hub client of a started fake Hub, authenticated with a tester key.
func fakeHubClient(t *testing.T, hub *testenv.FakeHub) *hubclient.Client {
	t.Helper()
	url, httpClient := testenv.StartFakeHub(t, hub)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(context.Background(), hubclient.DefaultCredentialName, "la_test_key_0123456789"); err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(hubclient.Config{BaseURL: url, HTTPClient: httpClient, RequestTimeout: 10 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// The fake Hub refuses a key reused with another request, as the real one does (ADR-0005(d)).
func TestFakeHubRefusesAKeyReusedWithAnotherRequest(t *testing.T) {
	hub := testenv.NewFakeHub()
	hub.Answer(values.OperationSummary, ppSummary)
	client := fakeHubClient(t, hub)

	send := func(data string) error {
		req, err := values.NewLLMRequest(values.OperationSummary, "default", "Summarise.", data, "key-1")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Complete(context.Background(), req)
		return err
	}
	if err := send("lesson A"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := send("lesson A"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := send("lesson B"); !errors.Is(err, hubclient.ErrIdempotencyConflict) {
		t.Fatalf("same key, other request = %v, want ErrIdempotencyConflict", err)
	}
}

// ADR-0005(d), AC-11: the lesson is renamed while a job waits for its retry. The unchanged retry reuses the key; the
// retry after the rename sends a new request under a new key, so the Hub does not refuse it and the job completes.
func TestRenamedLessonBetweenAttemptsStillCompletes(t *testing.T) {
	hub := testenv.NewFakeHub() // no summary answer yet: the Hub is unavailable for it
	hub.Answer(values.OperationExtractHomework, ppExtract)
	e := newPPEnv(t, fakeHubClient(t, hub))
	e.start()

	waitAttempts := func(n int) {
		t.Helper()
		testenv.Eventually(t, fmt.Sprintf("summary attempt %d failed", n), func() bool {
			j, ok := e.postJob(homeworkflow.SummarizeJobType)
			return ok && j.Status() == job.StatusRetryWait && j.Attempts() == n
		}, func() { e.clock.Advance(time.Second) })
	}
	waitAttempts(1)
	waitAttempts(2)

	ctx := context.Background()
	lessons := sqlite.NewLessonRepository(e.db)
	l, err := lessons.Get(ctx, "l1")
	if err != nil {
		t.Fatal(err)
	}
	c := l.Content()
	c.Title = "Physical geography"
	if _, err := l.ChangeContent(c, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lessons.Save(ctx, l); err != nil {
		t.Fatal(err)
	}

	hub.Answer(values.OperationSummary, ppSummary)
	j := e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	if j.Attempts() != 3 {
		t.Fatalf("attempts = %d, want 3", j.Attempts())
	}

	var keys []string
	for _, r := range hub.Requests() {
		if r.GetSpec().GetOperationType() == hubv1.OperationType_OPERATION_TYPE_SUMMARY {
			keys = append(keys, r.GetSpec().GetIdempotencyKey())
		}
	}
	if len(keys) != 3 || keys[0] != keys[1] || keys[1] == keys[2] {
		t.Fatalf("summary keys = %v, want the same key twice, then a new one", keys)
	}
	s, err := e.summaries.Get(ctx, summary.IDOf(ppTranscript))
	if err != nil || s.Content().Text() != ppSummaryText {
		t.Fatalf("summary: %v", err)
	}
}

// Test step 3 of TASK-042: an invalid answer creates no records and fails the job, without retries of the job. Since
// TASK-086 one attempt asks again twice with a format reminder before it fails.
func TestInvalidLLMAnswerFailsTheJobWithoutRecords(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, `Sure! Here is the summary: rivers.`)
	llm.Answer(values.OperationExtractHomework,
		`{"assignments":[{"title":"Read chapter 4","description":"","due":null,"send_to":"teacher@example.com"}]}`)
	e := newPPEnv(t, llm)
	e.start()

	var failed []shared.JobID
	for _, typ := range []job.Type{homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType} {
		j := e.waitPost(typ, job.StatusFailed)
		failed = append(failed, j.ID())
		if j.Attempts() != 1 || !strings.HasSuffix(j.LastError(), homeworkflow.ErrInvalidAnswer.Error()) ||
			strings.Contains(j.LastError(), "teacher@example.com") || strings.Contains(j.LastError(), "rivers") {
			t.Fatalf("%s: attempts %d, last error %q", typ, j.Attempts(), j.LastError())
		}
	}
	// A failed job never runs again (AC-10). Two hours pass; a sentinel job (failing before any LLM call) shows that
	// the runner polled after that, and Stop waits for every handler still running, so the count is final.
	for range 120 {
		e.clock.Advance(time.Minute)
	}
	ctx := context.Background()
	sentinel, err := job.New(job.Spec{
		ID: "sentinel", Type: homeworkflow.SummarizeJobType, Key: "sentinel", Payload: `{"recording_id":"r1"}`, MaxAttempts: 1,
	}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, sentinel); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the sentinel job fails", func() bool {
		j, err := e.jobs.Get(ctx, "sentinel")
		return err == nil && j.Status() == job.StatusFailed
	}, func() { e.clock.Advance(time.Second) })
	stopCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := e.runner.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if n := len(llm.Requests()); n != 2*3 {
		t.Fatalf("LLM requests = %d, want 3 per job (no retry of a permanent failure)", n)
	}
	for _, id := range failed {
		if j, err := e.jobs.Get(ctx, id); err != nil || j.Status() != job.StatusFailed || j.Attempts() != 1 {
			t.Fatalf("job %s after two hours: %v, %v", id, j, err)
		}
	}

	if list := e.storedAssignments(); len(list) != 0 {
		t.Fatalf("assignments = %d, want 0", len(list))
	}
	if _, err := e.summaries.Get(ctx, summary.IDOf(ppTranscript)); err == nil {
		t.Fatal("a summary was stored from an invalid answer")
	}
}

// TASK-086 on SQLite through the real Hub client: the gateway wraps the extraction in a fence (accepted) and cuts the
// summary off (three paid calls, then a permanent failure). A rerun of the failed job is replayed by the Hub for free;
// «Повторить» makes a new job whose new keys reach the model, and the summary is stored.
func TestRetryAfterACutOffAnswerThroughFakeHub(t *testing.T) {
	hub := testenv.NewFakeHub()
	hub.Answer(values.OperationSummary, `{"summary":"Rivers and their del`)
	hub.Answer(values.OperationExtractHomework, "Here is the JSON:\n```json\n"+ppExtract+"\n```")
	e := newPPEnv(t, fakeHubClient(t, hub))
	e.start()

	failed := e.waitPost(homeworkflow.SummarizeJobType, job.StatusFailed)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	if !strings.HasSuffix(failed.LastError(), homeworkflow.ErrTruncatedAnswer.Error()) || failed.Attempts() != 1 {
		t.Fatalf("summary job: attempts %d, last error %q", failed.Attempts(), failed.LastError())
	}
	if hub.Charged() != 3+1 {
		t.Fatalf("charged = %d, want three summary calls and one extraction", hub.Charged())
	}

	ctx := context.Background()
	rr, err := homeworkflow.NewRerunner(homeworkflow.RerunDeps{
		Transcripts: sqlite.NewTranscriptRepository(e.db), Jobs: e.jobs, Summaries: e.summaries, Clock: e.clock,
		IDs: &postIDs{n: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rr.RedoSummary(ctx, "r1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("RedoSummary without a summary = %v", err)
	}
	if _, err := rr.Retry(ctx, "r1", homeworkflow.WorkHomework); !errors.Is(err, job.ErrNotFailed) {
		t.Fatalf("Retry of the completed extraction = %v", err)
	}

	hub.Answer(values.OperationSummary, ppSummary)
	id, err := rr.Retry(ctx, "r1", homeworkflow.WorkSummary)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	testenv.Eventually(t, "the retried summary job completes", func() bool {
		j, err := e.jobs.Get(ctx, id)
		return err == nil && j.Status() == job.StatusCompleted
	}, func() { e.clock.Advance(time.Second) })
	e.checkStructures()
	if hub.Charged() != 3+1+1 {
		t.Fatalf("charged = %d, want one more call for the retry", hub.Charged())
	}
	if old, err := e.jobs.Get(ctx, failed.ID()); err != nil || old.Status() != job.StatusFailed {
		t.Fatalf("the failed job changed: %v %v", old, err)
	}
	if _, err := sqlite.NewTranscriptRepository(e.db).Get(ctx, ppTranscript); err != nil {
		t.Fatalf("transcript: %v", err)
	}

	// «Сделать конспект заново»: the new summary replaces the old one only once it passed.
	hub.Answer(values.OperationSummary, "no JSON at all")
	redo, err := rr.RedoSummary(ctx, "r1")
	if err != nil {
		t.Fatalf("RedoSummary: %v", err)
	}
	testenv.Eventually(t, "the redo fails", func() bool {
		j, err := e.jobs.Get(ctx, redo)
		return err == nil && j.Status() == job.StatusFailed
	}, func() { e.clock.Advance(time.Second) })
	e.checkStructures()
	kept, err := e.summaries.Get(ctx, summary.IDOf(ppTranscript))
	if err != nil || kept.Content().Text() != ppSummaryText {
		t.Fatalf("the failed redo replaced the summary: %v %v", kept, err)
	}
}

// AC-3 of TASK-042 on SQLite: the user's edit of an assignment and its deadline survives reprocessing.
func TestUserEditSurvivesReprocessing(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e := newPPEnv(t, llm)
	e.start()
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)

	// The user edits the first assignment (the use case does exactly this in one transaction).
	ctx := context.Background()
	first := e.storedAssignments()[0]
	mine, err := assignment.NewDetails("Chapter 4 with notes", "My plan", time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Edit(mine, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, first); err != nil {
		t.Fatal(err)
	}

	// The transcript is processed again (a new job, as after a prompt update) and the model now says something else.
	llm.Answer(values.OperationExtractHomework, `{"assignments":[`+
		`{"title":"Read chapter 5","description":"","due":"2026-10-15T23:59:00+03:00"},`+
		`{"title":"Draw a map of lakes","description":"Colours","due":"2026-10-22T23:59:00+03:00"}]}`)
	req, err := values.NewPostProcessRequest(ppTranscript)
	if err != nil {
		t.Fatal(err)
	}
	again, err := job.New(job.Spec{
		ID: "reprocess-1", Type: homeworkflow.ExtractHomeworkJobType, Key: "extract_homework:stt-1:reprocess-1",
		Payload: req.Payload(), MaxAttempts: 3,
	}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, again); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the reprocessing job completes", func() bool {
		j, err := e.jobs.Get(ctx, "reprocess-1")
		return err == nil && j.Status() == job.StatusCompleted
	}, func() { e.clock.Advance(time.Second) })

	list := e.storedAssignments()
	if len(list) != 2 {
		t.Fatalf("assignments = %d, want 2 (reprocessing adds no duplicates)", len(list))
	}
	if d := list[0].Details(); !list[0].UserEdited() || !d.Equal(mine) || list[0].ID() != first.ID() {
		t.Fatalf("the user's edit was overwritten: %+v", list[0].State())
	}
	if d := list[1].Details(); d.Title() != "Draw a map of lakes" || !d.Due().Equal(time.Date(2026, 10, 22, 20, 59, 0, 0, time.UTC)) {
		t.Fatalf("the unedited assignment was not updated: %+v", list[1].State())
	}
}

// A shorter answer on reprocessing withdraws the extracted assignment it no longer has; one that a delivery references
// (ON DELETE RESTRICT) is kept and the job still completes.
func TestShorterReprocessingAnswerWithdrawsStaleAssignments(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, `{"assignments":[`+
		`{"title":"Read chapter 4","description":"","due":"2026-10-16T23:59:00+03:00"},`+
		`{"title":"Draw a map of rivers","description":"","due":"2026-10-21T10:00:00+03:00"},`+
		`{"title":"Essay on deltas","description":"","due":"2026-10-23T10:00:00+03:00"}]}`)
	e := newPPEnv(t, llm)
	e.start()
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)

	ctx := context.Background()
	first := e.storedAssignments()
	if len(first) != 3 {
		t.Fatalf("assignments = %d, want 3", len(first))
	}
	if _, err := e.db.DBTX(ctx).ExecContext(ctx, `INSERT INTO deliveries (id, assignment_id, recipient, subject,
		artifact_ref, message_id, idempotency_key, status, confirmed_at, sent_at, created_at, updated_at)
		VALUES ('d1', ?, 'teacher@example.com', 'Essay', 'exports/essay.pdf', 'msg-1', 'dk-1', 'SENT', 1, 1, 1, 1)`,
		string(first[2].ID())); err != nil {
		t.Fatal(err)
	}

	llm.Answer(values.OperationExtractHomework, `{"assignments":[`+
		`{"title":"Read chapter 4","description":"","due":"2026-10-16T23:59:00+03:00"}]}`)
	req, err := values.NewPostProcessRequest(ppTranscript)
	if err != nil {
		t.Fatal(err)
	}
	again, err := job.New(job.Spec{
		ID: "reprocess-1", Type: homeworkflow.ExtractHomeworkJobType, Key: "extract_homework:stt-1:reprocess-1",
		Payload: req.Payload(), MaxAttempts: 3,
	}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, again); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the reprocessing job completes", func() bool {
		j, err := e.jobs.Get(ctx, "reprocess-1")
		return err == nil && j.Status() == job.StatusCompleted
	}, func() { e.clock.Advance(time.Second) })

	list := e.storedAssignments()
	if len(list) != 2 || list[0].ID() != first[0].ID() || list[1].ID() != first[2].ID() {
		t.Fatalf("assignments = %d, want item 0 and the delivered item 2", len(list))
	}
	if _, err := e.assignments.Get(ctx, first[1].ID()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("stale assignment: %v, want it withdrawn", err)
	}
}

// PRD sections 7.2 and 9.2: no quota parks the job without using attempts; it runs again after the recheck.
func TestQuotaExceededWaitsAndResumes(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationExtractHomework, ppExtract)
	llm.Reply(values.OperationSummary, testenv.LLMReply{Err: fmt.Errorf("hub: %w", shared.ErrQuotaExceeded)})
	e := newPPEnv(t, llm)
	e.start()

	j := e.waitPost(homeworkflow.SummarizeJobType, job.StatusWaitingForQuota)
	if j.Attempts() != 0 {
		t.Fatalf("attempts while waiting for quota = %d, want 0", j.Attempts())
	}

	llm.Answer(values.OperationSummary, ppSummary) // the quota was topped up
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	if _, err := e.summaries.Get(context.Background(), summary.IDOf(ppTranscript)); err != nil {
		t.Fatalf("summary after the quota came back: %v", err)
	}
	keys := map[string]bool{}
	for _, r := range llm.RequestsFor(values.OperationSummary) {
		keys[r.IdempotencyKey()] = true
	}
	if len(keys) != 1 {
		t.Fatalf("summary requests used %d idempotency keys, want 1", len(keys))
	}
}
