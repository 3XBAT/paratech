package app

// TASK-060, AC 4: the whole pipeline on the wired UI-mode core. Real SQLite, filestore, job runner, scheduler, recording
// prompt, recording session, transcription queue, whispercli and Hub client; fake are only the popup window, the
// recorder, the power source, whisper-cli (the test binary, TestMain) and the Hub (testenv.FakeHub over TLS) (ARCH-61).

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/transcribe"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// The Hub's answers: a summary and two assignments without a deadline (a deadline must carry the UTC offset of the
// lesson in the machine's zone).
const (
	pipelineSummary = `{"summary":"Greetings and exercise 5.","key_points":["Exercise 5 is homework"]}`
	pipelineExtract = `{"assignments":[` +
		`{"title":"Exercise 5","description":"Page 12","due":null},` +
		`{"title":"Read the next chapter","description":"","due":null}]}`
)

// uiCore is a UI-mode core on dataDir with a fake popup, as the Started hook starts it.
type uiCore struct {
	dir    string
	c      *core
	life   *lifecycle
	popup  *testenv.FakePopup
	prompt *wails.PromptService
	app    *App // the core and env for the helpers of this package
}

// openUICore builds the UI-mode core on dataDir; start runs the lifecycle. The test cleanup shuts it down.
func openUICore(t *testing.T, dataDir string, e env) *uiCore {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	c, err := buildCore(context.Background(), dataDir, e, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	popup := &testenv.FakePopup{}
	if err := c.wirePrompts(e, popup, log); err != nil {
		_ = c.close()
		t.Fatal(err)
	}
	u := &uiCore{
		dir: dataDir, c: c, life: c.newLifecycle(log, e.stopTimeout, closer{name: "storage", close: c.close}), popup: popup,
		prompt: wails.NewPromptService(c.ui.Prompt, log), app: &App{core: c, env: e},
	}
	t.Cleanup(func() { // before TempDir removal: Windows cannot delete open files
		if err := u.life.shutdown(); err != nil {
			t.Error(err)
		}
	})
	return u
}

func (u *uiCore) start(t *testing.T) {
	t.Helper()
	if err := u.life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pipelineEnv is the test environment with the fake Hub configured: its URL in the settings (stored by
// configureHub) and the tester key in the in-memory credential store.
func pipelineEnv(t *testing.T) (env, *fakes, string) {
	t.Helper()
	hub := testenv.NewFakeHub()
	hub.Answer(cvalues.OperationSummary, pipelineSummary)
	hub.Answer(cvalues.OperationExtractHomework, pipelineExtract)
	hubURL, hubHTTP := testenv.StartFakeHub(t, hub)

	e, f := testEnv(t)
	e.hubHTTP = hubHTTP
	if err := f.secrets[hubSecretsService].Set(context.Background(), hubclient.DefaultCredentialName, testHubKey); err != nil {
		t.Fatal(err)
	}
	return e, f, hubURL
}

// configureHub stores the Hub URL as the settings page will.
func configureHub(t *testing.T, c *core, now time.Time, url string) {
	t.Helper()
	repo := sqlite.NewSettingsRepository(c.db)
	s, err := repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHubURL(url, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

// AC 4, test step 3: lesson -> scheduler -> popup -> recording -> stop -> COMPLETED -> STT job (fake whisper-cli) ->
// summarize and extract_homework (fake Hub) -> one summary and the assignments in SQLite. After a restart (Recover
// and the startup sweep run) and a repeated completion event nothing is duplicated and no job changes.
func TestLessonToAssignmentsThroughTheWiredCore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, f, hubURL := pipelineEnv(t)

	// The lesson starts within the scheduler's lead; the clock is the real one because the core runs on goroutines.
	start := time.Now().Add(2 * time.Minute)
	u := openUICore(t, dir, e)
	l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
		Title: "English", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
	}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewLessonRepository(u.c.db).Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	configureHub(t, u.c, time.Now(), hubURL)
	// A model imported and made the default through the wired model manager, as the models screen will do.
	model := importModel(t, u.c, "base", testenv.WhisperOK)
	if err := u.c.models.SetDefault(ctx, model); err != nil {
		t.Fatal(err)
	}
	u.start(t)

	// The scheduler announces the lesson, the prompt shows the popup, "Start recording" starts it.
	testenv.Eventually(t, "the scheduler shows the popup", u.popup.Visible, nil)
	cur := u.prompt.Current()
	if cur == nil || cur.Kind != "lesson" || cur.Title != "English" {
		t.Fatalf("current prompt %+v", cur)
	}
	if v := startFromPopup(t, u.prompt, cur.ID); v.State != string(prompt.StateStarted) {
		t.Fatalf("Start = %+v, want STARTED", v)
	}
	info, ok := u.c.recordings.Active()
	if !ok || info.LessonID != "lesson-1" {
		t.Fatalf("active recording %+v, %v; want one for the lesson", info, ok)
	}
	if _, err := f.recorder.Feed(ctx, info.ID, []byte("audio of the lesson")); err != nil {
		t.Fatal(err)
	}

	// Stop returns once the recording is COMPLETED and the completion was handed over: the STT job exists.
	if err := pageBindings(u.c.ui).StopRecording(ctx, string(info.ID)); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	jobs := sqlite.NewJobRepository(u.c.db)
	stt := onlyJob(t, jobs, transcribe.JobType)
	if want := fmt.Sprintf("stt:%s:%s", info.ID, model); stt.Key() != want || stt.PinnedModelID() != model {
		t.Fatalf("STT job key %q pin %q, want %q pinned to %s", stt.Key(), stt.PinnedModelID(), want, model)
	}
	if chain := statusChain(t, jobs, stt.ID(), job.StatusCompleted); !orderedChain(chain) {
		t.Fatalf("STT job went %v, want QUEUED -> RUNNING -> COMPLETED (sampled)", chain)
	}

	// The transcript (id = the STT job id) feeds both Hub jobs.
	tr := shared.TranscriptID(stt.ID())
	for _, typ := range []job.Type{homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType} {
		pp := onlyJob(t, jobs, typ)
		if chain := statusChain(t, jobs, pp.ID(), job.StatusCompleted); !orderedChain(chain) {
			t.Fatalf("%s job went %v", typ, chain)
		}
	}
	before := pipelineState(t, u, tr)
	if before.transcripts != 1 || before.summaries != 1 || len(before.assignments) != 2 {
		t.Fatalf("after the pipeline: %+v; want one transcript, one summary, two assignments", before)
	}
	for _, j := range before.jobs {
		if j.status != job.StatusCompleted || j.attempts != 1 {
			t.Fatalf("job %+v, want COMPLETED after one attempt", j)
		}
	}

	// Restart on the same data directory: the recoveries and the sweep run, then the completion arrives again.
	if err := u.life.shutdown(); err != nil {
		t.Fatal(err)
	}
	again := openUICore(t, dir, e)
	again.start(t)
	if err := again.c.transcriptions.RecordingCompleted(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := again.c.transcriptions.EnqueuePending(ctx); err != nil || n != 0 {
		t.Fatalf("a second sweep = %d, %v; want nothing new", n, err)
	}
	after := pipelineState(t, again, tr)
	if !after.equal(before) {
		t.Fatalf("after the restart and a repeated event:\n%+v\nwant unchanged:\n%+v", after, before)
	}
}

// Test step 3 on the real SQLite: a repeated completion event, also concurrent ones and the startup sweep, makes one
// STT job.
func TestRepeatedCompletionMakesOneSTTJob(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	model := installModel(t, a, "m1", testenv.WhisperOK)
	rec := completedRecording(t, a, "rec-1")

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() { errs <- a.core.transcriptions.RecordingCompleted(ctx, rec) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := a.core.transcriptions.EnqueuePending(ctx); err != nil || n != 0 {
		t.Fatalf("EnqueuePending = %d, %v; want nothing new", n, err)
	}

	jobs := sqlite.NewJobRepository(a.core.db)
	j := onlyJob(t, jobs, transcribe.JobType)
	if j.Key() != "stt:rec-1:"+string(model) || j.PinnedModelID() != model || j.Status() != job.StatusQueued {
		t.Fatalf("job %s key %q pin %q status %s", j.ID(), j.Key(), j.PinnedModelID(), j.Status())
	}
}

// Without whisper-cli the STT job waits QUEUED (no attempt, no error) while the runner works on other jobs; after
// whisper-cli is installed and the app restarts, the same job (same id and key) runs to COMPLETED.
func TestSTTJobWaitsForWhisperCLI(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	a, _ := newTestAppWith(t, dir)
	a.env.whisperBinary = filepath.Join(t.TempDir(), "whisper-cli.exe") // not installed
	startCore(t, a)
	model := installModel(t, a, "m1", testenv.WhisperOK)
	rec := completedRecording(t, a, "rec-1")
	if err := a.core.transcriptions.RecordingCompleted(ctx, rec); err != nil {
		t.Fatal(err)
	}
	jobs := sqlite.NewJobRepository(a.core.db)
	waiting := onlyJob(t, jobs, transcribe.JobType)

	// A light job enqueued after it is taken (it then waits for the Hub): the runner polled past the STT job.
	enqueueSummary(t, a, "job-canary", storeTranscript(t, a, "tr-canary", completedRecording(t, a, "rec-2"), model))
	testenv.Eventually(t, "the runner takes the light job", func() bool {
		j, err := jobs.Get(ctx, "job-canary")
		return err == nil && j.Status() == job.StatusWaitingForNetwork
	}, nil)
	if j, err := jobs.Get(ctx, waiting.ID()); err != nil || j.Status() != job.StatusQueued || j.Attempts() != 0 || j.LastError() != "" {
		t.Fatalf("STT job %v, %v; want QUEUED without attempts or error", j, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, _ := newTestAppWith(t, dir) // the test binary as whisper-cli: installed now
	startCore(t, b)
	jobs = sqlite.NewJobRepository(b.core.db)
	testenv.Eventually(t, "the waiting STT job runs", func() bool {
		j, err := jobs.Get(ctx, waiting.ID())
		return err == nil && j.Status() == job.StatusCompleted
	}, nil)
	if j := onlyJob(t, jobs, transcribe.JobType); j.ID() != waiting.ID() || j.Key() != waiting.Key() || j.Attempts() != 1 {
		t.Fatalf("STT job %s key %q after %d attempts, want the waiting one after one", j.ID(), j.Key(), j.Attempts())
	}
}

// onlyJob returns the single job of typ.
func onlyJob(t *testing.T, jobs *sqlite.JobRepository, typ job.Type) *job.Job {
	t.Helper()
	var found []*job.Job
	testenv.Eventually(t, "a job of type "+string(typ), func() bool {
		all, _, err := jobs.ListActivity(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}
		found = slices.DeleteFunc(all, func(j *job.Job) bool { return j.Type() != typ })
		return len(found) > 0
	}, nil)
	if len(found) != 1 {
		t.Fatalf("%d jobs of type %s, want 1", len(found), typ)
	}
	return found[0]
}

// statusChain samples the status of the job until it reaches final and returns the statuses seen, without repeats.
// Sampling can miss a short status; orderedChain checks what was seen.
func statusChain(t *testing.T, jobs *sqlite.JobRepository, id shared.JobID, final job.Status) []job.Status {
	t.Helper()
	var chain []job.Status
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j, err := jobs.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(chain) == 0 || chain[len(chain)-1] != j.Status() {
			chain = append(chain, j.Status())
		}
		if j.Status() == final || j.Status().IsTerminal() {
			return chain
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s: %v", id, final, chain)
	return nil
}

// orderedChain reports whether the statuses follow QUEUED -> RUNNING -> COMPLETED (any may be unseen) and end
// COMPLETED: no retry, wait or failure on the way.
func orderedChain(chain []job.Status) bool {
	order := []job.Status{job.StatusQueued, job.StatusRunning, job.StatusCompleted}
	last := -1
	for _, s := range chain {
		i := slices.Index(order, s)
		if i <= last {
			return false
		}
		last = i
	}
	return last == len(order)-1
}

// storedJob is what the restart must not change about a job.
type storedJob struct {
	id       shared.JobID
	typ      job.Type
	status   job.Status
	attempts int
	version  int64
}

// stored is the outcome of the pipeline in the database.
type stored struct {
	jobs        []storedJob
	transcripts int
	summaries   int
	assignments []string // id:title:version
}

func (s stored) equal(o stored) bool {
	return slices.Equal(s.jobs, o.jobs) && s.transcripts == o.transcripts && s.summaries == o.summaries &&
		slices.Equal(s.assignments, o.assignments)
}

// pipelineState reads every job, the transcripts, the summaries and the assignments of the transcript tr.
func pipelineState(t *testing.T, u *uiCore, tr shared.TranscriptID) stored {
	t.Helper()
	ctx := context.Background()
	all, unreadable, err := sqlite.NewJobRepository(u.c.db).ListActivity(ctx, 100)
	if err != nil || len(unreadable) != 0 {
		t.Fatalf("ListActivity: %v, unreadable %v", err, unreadable)
	}
	var s stored
	for _, j := range all {
		s.jobs = append(s.jobs, storedJob{id: j.ID(), typ: j.Type(), status: j.Status(), attempts: j.Attempts(), version: j.Version()})
	}
	slices.SortFunc(s.jobs, func(a, b storedJob) int { return cmp.Compare(a.id, b.id) })

	asg, err := sqlite.NewAssignmentRepository(u.c.db).ListByTranscript(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range asg {
		s.assignments = append(s.assignments, fmt.Sprintf("%s:%s:%d", a.ID(), a.Details().Title(), a.Version()))
	}
	slices.Sort(s.assignments)
	s.transcripts = countRows(t, u.dir, "transcripts")
	s.summaries = countRows(t, u.dir, "summaries")
	return s
}

// countRows counts the rows of a table of the database in dataDir through a read-only connection of its own.
func countRows(t *testing.T, dataDir, table string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dataDir, dbFile))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM "` + table + `"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The completion of a recording that cannot be transcribed is ignored by the queue (no error, no job).
func TestFailedRecordingGetsNoSTTJob(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	installModel(t, a, "m1", testenv.WhisperOK)
	r, err := recording.New(recording.Spec{ID: "rec-failed", MaxDuration: time.Hour}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(time.Now(), "no device"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewRecordingRepository(a.core.db).Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := a.core.transcriptions.RecordingCompleted(ctx, r.ID()); err != nil {
		t.Fatal(err)
	}
	if n, err := a.core.transcriptions.EnqueuePending(ctx); err != nil || n != 0 {
		t.Fatalf("EnqueuePending = %d, %v; want nothing", n, err)
	}
	if all, _, err := sqlite.NewJobRepository(a.core.db).ListActivity(ctx, 10); err != nil || len(all) != 0 {
		t.Fatalf("jobs %v, %v; want none", all, err)
	}
}
