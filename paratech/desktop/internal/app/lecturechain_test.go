package app

// TASK-097 on the wired core: the lecture text is made one part per job step, and the job goes to the end of the queue
// after each part. Real SQLite, job runner (one worker, as testEnv), post-processor, drafts, deletion and Hub client;
// fake are the Hub (testenv.FakeHub over TLS) and a gate in front of it that lets the test hold a call.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// Answers of the fake Hub in these tests. Every part holds one short segment, so no part is checked for condensing.
const (
	chainLecture = `{"blocks":[{"heading":"Rivers","from":0,"paragraphs":["Rivers reach the sea."]}]}`
	chainDraft   = `{"title":"Rivers","body":"Deltas form where rivers reach the sea."}`
	chainParts   = 3
)

// hubGate holds the Hub's Complete calls until the test lets them through, one by one or all; a held call ends with
// its context.
type hubGate struct {
	base    http.RoundTripper
	tokens  chan struct{}
	opened  chan struct{}
	waiting atomic.Int32
}

func newHubGate(base http.RoundTripper) *hubGate {
	return &hubGate{base: base, tokens: make(chan struct{}, 100), opened: make(chan struct{})}
}

func (g *hubGate) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/Complete") {
		g.waiting.Add(1)
		select {
		case <-g.tokens:
		case <-g.opened:
		case <-r.Context().Done():
			g.waiting.Add(-1)
			return nil, r.Context().Err()
		}
		g.waiting.Add(-1)
	}
	return g.base.RoundTrip(r)
}

func (g *hubGate) allow()     { g.tokens <- struct{}{} }
func (g *hubGate) openAll()   { close(g.opened) }
func (g *hubGate) held() bool { return g.waiting.Load() > 0 }

// chainEnv is a UI core with the Hub configured behind a gate, a completed recording rec-1 and its transcript tr-1 of
// chainParts parts; the core is not started.
type chainEnv struct {
	dir  string
	e    env
	u    *uiCore
	hub  *testenv.FakeHub
	url  string
	gate *hubGate
}

func newChainEnv(t *testing.T) *chainEnv {
	t.Helper()
	e, _, hub, url := pipelineEnv(t)
	gate := newHubGate(e.hubHTTP.Transport)
	client := *e.hubHTTP
	client.Transport = gate
	e.hubHTTP = &client
	e.stopTimeout = 500 * time.Millisecond // a held Hub call ends at the shutdown
	hub.Answer(cvalues.OperationLectureText, chainLecture)
	hub.Answer(cvalues.OperationHomeworkDraft, chainDraft)

	c := &chainEnv{dir: t.TempDir(), e: e, hub: hub, url: url, gate: gate}
	c.u = openChainCore(t, c.dir, e)
	configureHub(t, c.u.c, time.Now(), url)
	completedRecording(t, c.u.app, "rec-1")
	storeChainTranscript(t, c.u.app, "tr-1", "rec-1", installModel(t, c.u.app, "m1", testenv.WhisperOK))
	return c
}

// openChainCore is openUICore whose shutdown may interrupt a held Hub call: the runner then reports its stop deadline.
func openChainCore(t *testing.T, dataDir string, e env) *uiCore {
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
		if err := u.life.shutdown(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Error(err)
		}
	})
	return u
}

// storeChainTranscript stores a transcript of chainParts segments 7 minutes apart: each one is a part of its own
// (lectureparts.MaxPartMs).
func storeChainTranscript(t *testing.T, a *App, id shared.TranscriptID, rec shared.RecordingID, model shared.SttModelID) {
	t.Helper()
	ctx := context.Background()
	var segs []transcript.Segment
	for i := range chainParts {
		start := int64(i) * 7 * 60 * 1000
		segs = append(segs, transcript.Segment{StartMs: start, EndMs: start + 5000, Text: fmt.Sprintf("Segment %d about rivers.", i)})
	}
	doc, err := transcript.NewDocument("ru", segs)
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
}

// startLectureText presses «Сделать текст лекции» (or «Повторить» after a failure) of rec-1.
func (c *chainEnv) startLectureText(t *testing.T) {
	t.Helper()
	if err := pageBindings(c.u.c.ui).RetryPostProcessing(context.Background(), "rec-1", string(homeworkflow.WorkLectureText)); err != nil {
		t.Fatalf("RetryPostProcessing: %v", err)
	}
}

// lectureJob returns the newest lecture_text job.
func (c *chainEnv) lectureJob(t *testing.T) *job.Job {
	t.Helper()
	j := jobOfType(t, c.u.app, homeworkflow.LectureTextJobType)
	if j == nil {
		t.Fatal("no lecture_text job")
	}
	return j
}

// waitLecture waits until the newest lecture_text job is in status.
func (c *chainEnv) waitLecture(t *testing.T, status job.Status) *job.Job {
	t.Helper()
	var j *job.Job
	testenv.Eventually(t, "the lecture text job is "+status.String(), func() bool {
		j = c.lectureJob(t)
		return j.Status() == status
	}, nil)
	return j
}

// heldAfterPart lets one Hub call through and waits until its part is stored ("1/3") and the call of the next part
// is held at the gate.
func (c *chainEnv) heldAfterPart(t *testing.T) {
	t.Helper()
	c.gate.allow()
	testenv.Eventually(t, "part 1 is stored and part 2 is held", func() bool {
		j := c.lectureJob(t)
		return j.Status() == job.StatusRunning && j.Progress().Checkpoint() == cvalues.LectureTextCheckpoint(1, chainParts) &&
			c.gate.held()
	}, nil)
}

var partField = regexp.MustCompile(`"part":(\d+)`)

// lectureParts returns the part numbers of the lecture text calls the Hub received, in order.
func lectureParts(t *testing.T, hub *testenv.FakeHub) []int {
	t.Helper()
	var out []int
	for _, r := range hub.Requests() {
		if r.GetSpec().GetOperationType() != hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT {
			continue
		}
		m := partField.FindStringSubmatch(r.GetSpec().GetUntrustedData())
		if m == nil {
			t.Fatal("a lecture text call without its part number")
		}
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

func checkLectureReady(t *testing.T, c *core) {
	t.Helper()
	lt, err := sqlite.NewLectureTextRepository(c.db).Get(context.Background(), lecturetext.IDOf("tr-1"))
	if err != nil || !lt.Ready() {
		t.Fatalf("lecture text: %v, want it ready", err)
	}
}

// A draft generation queued while the lecture text waits runs after the first part, not after the whole lecture.
func TestLectureTextLetsAQueuedDraftRunBetweenParts(t *testing.T) {
	ctx := context.Background()
	c := newChainEnv(t)
	c.gate.openAll()
	insertAssignment(t, c.dir, "a1", "Rivers")

	c.startLectureText(t)
	time.Sleep(5 * time.Millisecond) // the draft is queued later than the lecture text, also by its millisecond
	draft, err := c.u.c.drafts.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	c.u.start(t)

	lecture := c.waitLecture(t, job.StatusCompleted)
	jobs := sqlite.NewJobRepository(c.u.c.db)
	testenv.Eventually(t, "the draft completes", func() bool {
		j, err := jobs.Get(ctx, draft.JobID)
		return err == nil && j.Status() == job.StatusCompleted
	}, nil)

	var ops []string
	for _, r := range c.hub.Requests() {
		ops = append(ops, strings.TrimPrefix(r.GetSpec().GetOperationType().String(), "OPERATION_TYPE_"))
	}
	if want := []string{"LECTURE_TEXT", "HOMEWORK_DRAFT", "LECTURE_TEXT", "LECTURE_TEXT"}; !slices.Equal(ops, want) {
		t.Fatalf("Hub calls %v, want %v: the draft runs between the parts", ops, want)
	}
	if got := lectureParts(t, c.hub); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("lecture parts asked %v, want each once in order", got)
	}
	if lecture.Attempts() != 1 || lecture.Progress().Checkpoint() != cvalues.LectureTextCheckpoint(chainParts, chainParts) {
		t.Fatalf("lecture job: %d attempts, checkpoint %q", lecture.Attempts(), lecture.Progress().Checkpoint())
	}
	checkLectureReady(t, c.u.c)
}

// A restart in the middle of the chain continues with the first part not stored; the stored one is not asked again.
func TestLectureTextContinuesAfterARestart(t *testing.T) {
	c := newChainEnv(t)
	c.startLectureText(t)
	c.u.start(t)
	c.heldAfterPart(t)
	// The app closes while part 2 is being asked: the call is interrupted at the stop deadline.
	if err := c.u.life.shutdown(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown = %v, want the interrupted part 2", err)
	}
	c.gate.openAll()
	again := openChainCore(t, c.dir, c.e)
	stopped := jobOfType(t, again.app, homeworkflow.LectureTextJobType)
	if stopped.Status() != job.StatusQueued || stopped.Progress().Checkpoint() != cvalues.LectureTextCheckpoint(1, chainParts) {
		t.Fatalf("after the shutdown: %s at %q, want QUEUED after part 1", stopped.Status(), stopped.Progress().Checkpoint())
	}
	again.start(t)
	testenv.Eventually(t, "the lecture text completes after the restart", func() bool {
		j := jobOfType(t, again.app, homeworkflow.LectureTextJobType)
		return j != nil && j.Status() == job.StatusCompleted
	}, nil)
	if got := lectureParts(t, c.hub); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("lecture parts asked %v, want part 1 once and then the rest", got)
	}
	checkLectureReady(t, again.c)
}

// «Повторить» after a part failed for good keeps the stored parts: a new job asks only the parts left.
func TestLectureTextRetryAfterAFailedPart(t *testing.T) {
	c := newChainEnv(t)
	c.startLectureText(t)
	c.u.start(t)
	c.heldAfterPart(t)
	c.hub.Answer(cvalues.OperationLectureText, "no JSON") // part 2 fails the checks with every reminder
	c.gate.openAll()
	failed := c.waitLecture(t, job.StatusFailed)
	if failed.Progress().Checkpoint() != cvalues.LectureTextCheckpoint(1, chainParts) {
		t.Fatalf("failed job checkpoint %q, want 1 of %d", failed.Progress().Checkpoint(), chainParts)
	}

	c.hub.Answer(cvalues.OperationLectureText, chainLecture)
	c.startLectureText(t)
	testenv.Eventually(t, "the retried lecture text completes", func() bool {
		j := c.lectureJob(t)
		return j.ID() != failed.ID() && j.Status() == job.StatusCompleted
	}, nil)
	want := []int{1, 2, 2, 2, 2, 2, 3} // part 2 with its reminders, then the retry from part 2
	if got := lectureParts(t, c.hub); !slices.Equal(got, want) {
		t.Fatalf("lecture parts asked %v, want %v", got, want)
	}
	checkLectureReady(t, c.u.c)
}

// Deleting the recording in the middle of the chain cancels it: no part is asked after the deletion and nothing of
// the lecture text is left.
func TestLectureTextChainEndsWithTheRecordingDeletion(t *testing.T) {
	ctx := context.Background()
	c := newChainEnv(t)
	c.startLectureText(t)
	c.u.start(t)
	c.heldAfterPart(t)
	id := c.lectureJob(t).ID()

	svc := wails.NewRecordingDeleteService(c.u.c.ui.Deletion, slog.New(slog.DiscardHandler))
	res, err := svc.Delete(ctx, []wails.DeleteRequest{{RecordingID: "rec-1"}})
	if err != nil || res.Deleted != 1 {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	c.gate.openAll()
	if _, err := sqlite.NewJobRepository(c.u.c.db).Get(ctx, id); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("lecture job after the deletion: %v, want it purged", err)
	}
	if _, err := sqlite.NewLectureTextRepository(c.u.c.db).Get(ctx, lecturetext.IDOf("tr-1")); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("lecture text after the deletion: %v, want it purged", err)
	}
	time.Sleep(100 * time.Millisecond) // ten polls of the runner: a part left queued would be asked by now
	if got := lectureParts(t, c.hub); !slices.Equal(got, []int{1}) {
		t.Fatalf("lecture parts asked %v, want only the part before the deletion", got)
	}
}
