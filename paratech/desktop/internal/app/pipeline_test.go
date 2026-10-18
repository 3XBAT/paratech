package app

// TASK-060, AC 4: the whole pipeline on the wired UI-mode core. Real SQLite, filestore, job runner, scheduler, recording
// prompt, recording session, transcription queue, whispercli and Hub client; fake are only the popup window, the
// recorder, the power source, whisper-cli (the test binary, TestMain) and the Hub (testenv.FakeHub over TLS) (ARCH-61).

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
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
	c, err := buildCore(context.Background(), dataDir, testProfile, e, log)
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

// pipelineAudio is what the fake recorder captures; the fake recorder's track is exactly these bytes. Binary (NUL,
// 0xFF, non-UTF-8), as real audio is, so a text-only path could not carry it unchanged.
const pipelineAudio = "RIFF\x00\x01\xfe\xffWAVE\x00\xffaudio of the lesson\x80\x00\x7f\xff"

// pipelineEnv is the test environment with the fake Hub configured: its URL in the settings (stored by
// configureHub) and the tester key in the in-memory credential store.
func pipelineEnv(t *testing.T) (env, *fakes, *testenv.FakeHub, string) {
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
	return e, f, hub, hubURL
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
// TASK-055 (end-to-end test, criterion 1) adds: every status a job or the recording was stored with, the track in the
// Storage Manager (AC-03), the transcript pinned to the model (AC-08), no audio in the Hub requests (AC-06), the
// homework export of an extracted assignment and no second Hub charge after the restart (AC-11).
func TestLessonToAssignmentsThroughTheWiredCore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, f, hub, hubURL := pipelineEnv(t)

	// The lesson starts within the scheduler's lead; the clock is the real one because the core runs on goroutines.
	start := time.Now().Add(2 * time.Minute)
	u := openUICore(t, dir, e)
	logStatuses(t, dir) // before anything is stored
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
	if _, err := f.recorder.Feed(ctx, info.ID, []byte(pipelineAudio)); err != nil {
		t.Fatal(err)
	}

	// Stop returns once the recording is COMPLETED and the completion was handed over: the STT job exists.
	if err := pageBindings(u.c.ui).StopRecording(ctx, string(info.ID)); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	checkRecordingTrack(t, u.c, info.ID) // TASK-055: the stored recording and its track (AC-03)
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

	// TASK-055: what the sampled chains above can miss, from the statuses SQLite committed.
	exact := []string{string(job.StatusQueued), string(job.StatusRunning), string(job.StatusCompleted)}
	statuses := loggedStatuses(t, dir)
	for _, j := range before.jobs {
		if got := statuses["job:"+string(j.id)]; !slices.Equal(got, exact) {
			t.Fatalf("%s job was stored as %v, want exactly %v", j.typ, got, exact)
		}
	}
	// The recording went the whole user-stop path of PRD section 6.2.
	wantRec := []string{
		string(recording.StatusStarting), string(recording.StatusRecording), string(recording.StatusStopping),
		string(recording.StatusFinalizing), string(recording.StatusCompleted),
	}
	if got := statuses["recording:"+string(info.ID)]; !slices.Equal(got, wantRec) {
		t.Fatalf("the recording was stored as %v, want exactly %v", got, wantRec)
	}
	checkTranscriptPin(t, u.c, tr, info.ID, model)
	checkHubSawNoAudio(t, hub)
	exportExtractedHomework(t, u, tr)

	// Restart on the same data directory: the recoveries and the sweep run, then the completion arrives again.
	if err := u.life.shutdown(); err != nil {
		t.Fatal(err)
	}
	again := openUICore(t, dir, e)
	again.start(t)
	// The recording recovery runs in the background; once it ended it can store no status after the comparison below.
	recovered := make(chan struct{})
	go func() {
		again.life.bg.Wait()
		close(recovered)
	}()
	select {
	case <-recovered:
	case <-time.After(10 * time.Second):
		t.Fatal("the background recording recovery did not finish within 10s after the restart")
	}
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
	// TASK-055: no status was stored again and the Hub was neither asked nor charged again (AC-11).
	if got := loggedStatuses(t, dir); !maps.EqualFunc(got, statuses, slices.Equal) {
		t.Fatalf("statuses after the restart %v, want unchanged %v", got, statuses)
	}
	if n, charged := len(hub.Requests()), hub.Charged(); n != 2 || charged != 2 {
		t.Fatalf("the Hub got %d requests and charged %d keys, want 2 and 2", n, charged)
	}
}

// checkRecordingTrack checks that the recording id is stored COMPLETED with one final track in the Storage Manager,
// and that the track holds the captured audio.
func checkRecordingTrack(t *testing.T, c *core, id shared.RecordingID) {
	t.Helper()
	ctx := context.Background()
	rec, err := sqlite.NewRecordingRepository(c.db).Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	track, ok := rec.TranscribableAudio()
	if rec.Status() != recording.StatusCompleted || !ok || rec.Problem() != "" {
		t.Fatalf("recording %s %s track %q problem %q, want COMPLETED with a track", id, rec.Status(), track, rec.Problem())
	}
	r, err := c.store.Open(ctx, track)
	if err != nil {
		t.Fatalf("open the track %q: %v", track, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(pipelineAudio)) {
		t.Fatalf("the track holds %q, want the captured audio %q", data, pipelineAudio)
	}
}

// checkTranscriptPin checks that the transcript tr of the recording rec names the exact model it was made with
// (AC-08): the model id and the name "base" and version "1" the test imported it with (importModel).
func checkTranscriptPin(t *testing.T, c *core, tr shared.TranscriptID, rec shared.RecordingID, model shared.SttModelID) {
	t.Helper()
	got, err := sqlite.NewTranscriptRepository(c.db).Get(context.Background(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordingID() != rec || got.ModelID() != model || got.ModelName() != "base" || got.ModelVersion() != "1" {
		t.Fatalf("transcript of %s by %s %s %s, want of %s by %s base 1",
			got.RecordingID(), got.ModelID(), got.ModelName(), got.ModelVersion(), rec, model)
	}
}

// checkHubSawNoAudio checks AC-06 on the wire: each of the two post-processing requests carried the transcript text
// and the tester key, and no marshalled request contained the captured audio raw, as hex or as base64 (audioForms).
func checkHubSawNoAudio(t *testing.T, hub *testenv.FakeHub) {
	t.Helper()
	reqs, auth := hub.Requests(), hub.Authorizations()
	if len(reqs) != 2 || len(auth) != 2 {
		t.Fatalf("the Hub got %d requests (%d authorized), want 2", len(reqs), len(auth))
	}
	forms := audioForms([]byte(pipelineAudio))
	for i, r := range reqs {
		raw, err := proto.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range forms {
			if bytes.Contains(raw, form) {
				t.Fatalf("request %d (%s) carries the recorded audio as %q", i, r.GetSpec().GetOperationType(), form)
			}
		}
		if !strings.Contains(r.GetSpec().GetUntrustedData(), testenv.WhisperOKSegments[1]) {
			t.Fatalf("request %d (%s) does not carry the transcript text", i, r.GetSpec().GetOperationType())
		}
		if auth[i] != "Bearer "+testHubKey {
			t.Fatalf("request %d is not authorized with the tester key", i)
		}
	}
}

// audioForms returns the audio as it is, as lower- and upper-case hex, and as base64 in the standard and URL
// alphabets. Inside a longer encoded stream the audio can start at any of three byte offsets of a base64 group, so
// for each offset k only the whole groups of audio[k:] are taken: they are encoded the same whatever surrounds them,
// with or without padding (the Raw encodings differ only in the padding).
func audioForms(audio []byte) [][]byte {
	h := hex.EncodeToString(audio)
	forms := [][]byte{audio, []byte(h), []byte(strings.ToUpper(h))}
	for k := range 3 {
		whole := audio[k:]
		whole = whole[:len(whole)/3*3]
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
			forms = append(forms, []byte(enc.EncodeToString(whole)))
		}
	}
	return forms
}

// audioForms finds the audio wherever an encoder would put it: hex or base64 of a longer message, at any offset.
func TestAudioFormsFindEncodedAudio(t *testing.T) {
	audio := []byte(pipelineAudio)
	found := func(wire []byte) bool {
		return slices.ContainsFunc(audioForms(audio), func(f []byte) bool { return bytes.Contains(wire, f) })
	}
	for pad := range 4 {
		msg := append(append(bytes.Repeat([]byte{'p'}, pad), audio...), "tail"...)
		for name, wire := range map[string]string{
			"raw":           string(msg),
			"hex":           hex.EncodeToString(msg),
			"HEX":           strings.ToUpper(hex.EncodeToString(msg)),
			"base64":        base64.StdEncoding.EncodeToString(msg),
			"raw base64":    base64.RawStdEncoding.EncodeToString(msg),
			"url base64":    base64.URLEncoding.EncodeToString(msg),
			"raw url64":     base64.RawURLEncoding.EncodeToString(msg),
			"base64 suffix": base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{'p'}, pad), audio...)),
		} {
			if !found([]byte(wire)) {
				t.Errorf("offset %d, %s: the audio is not found in %q", pad, name, wire)
			}
		}
	}
	// A short form would match unrelated bytes, an empty one anything.
	for _, f := range audioForms(audio) {
		if len(f) < 16 {
			t.Errorf("audio form %q is too short to mean the audio", f)
		}
	}
}

// exportExtractedHomework continues on the running core from an assignment the pipeline extracted: a confirmed
// export proposal writes its homework file under that assignment's title (the proposal paths themselves are tested in
// proposals_test.go).
func exportExtractedHomework(t *testing.T, u *uiCore, tr shared.TranscriptID) {
	t.Helper()
	asg, err := sqlite.NewAssignmentRepository(u.c.db).ListByTranscript(context.Background(), tr)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(asg, func(a *assignment.Assignment) bool { return a.Details().Title() == "Exercise 5" })
	if i < 0 {
		t.Fatal("the pipeline extracted no assignment \"Exercise 5\"")
	}
	id := asg[i].ID()
	answer := fmt.Sprintf(`{"kind":"export_homework","assignment_id":%q,"body":"Page 12, all exercises."}`, id)
	v := propose(t, u.app, answer)
	if v.Status != proposal.StatusValidated {
		t.Fatalf("proposal %s (%s), want VALIDATED", v.Status, v.Reason)
	}
	if v = confirmAndExecute(t, u.app, v.ID); v.Status != proposal.StatusExecuted {
		t.Fatalf("after Execute %s, want EXECUTED", v.Status)
	}
	file := filepath.Join(u.dir, artifactsDir, "exports", string(id), "homework.md")
	want := cvalues.HomeworkMarkdown("Exercise 5", "Page 12, all exercises.")
	if data, err := os.ReadFile(file); err != nil || string(data) != want {
		t.Fatalf("export file = %q, %v; want %q", data, err, want)
	}
}

// statusLogTable is the test's own table in the database of a wired core: one row per stored status of a job or a
// recording, in commit order.
const statusLogTable = "test_status_log"

// logStatuses adds triggers to the database of dataDir (already migrated) that record every status a job or a
// recording is stored with. A trigger writes in the transaction of the change, so a rolled-back change leaves no row
// and an unchanged status (progress, heartbeat) leaves none either. Only this test adds them; the core never reads
// the table.
func logStatuses(t *testing.T, dataDir string) {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dataDir, dbFile)+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	stmts := []string{`CREATE TABLE ` + statusLogTable +
		` (seq INTEGER PRIMARY KEY AUTOINCREMENT, subject TEXT NOT NULL, status TEXT NOT NULL)`}
	for _, tbl := range []struct{ name, subject string }{{"jobs", "job:"}, {"recordings", "recording:"}} {
		insert := `INSERT INTO ` + statusLogTable + ` (subject, status) VALUES ('` + tbl.subject + `' || NEW.id, NEW.status);`
		stmts = append(stmts,
			`CREATE TRIGGER test_`+tbl.name+`_insert AFTER INSERT ON `+tbl.name+` BEGIN `+insert+` END`,
			`CREATE TRIGGER test_`+tbl.name+`_update AFTER UPDATE OF status ON `+tbl.name+
				` WHEN OLD.status IS NOT NEW.status BEGIN `+insert+` END`)
	}
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// loggedStatuses reads the statuses logStatuses recorded, by subject ("job:<id>", "recording:<id>"), in order.
func loggedStatuses(t *testing.T, dataDir string) map[string][]string {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dataDir, dbFile))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	rows, err := raw.Query(`SELECT subject, status FROM ` + statusLogTable + ` ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string][]string{}
	for rows.Next() {
		var subject, status string
		if err := rows.Scan(&subject, &status); err != nil {
			t.Fatal(err)
		}
		got[subject] = append(got[subject], status)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
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
