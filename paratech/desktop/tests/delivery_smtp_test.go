package tests

// Scenario tests of the homework email delivery (TASK-044; PRD sections 10 and 14 case 7, AC-14, AC-15, AC-19) on the
// real SQLite adapter, the real job runner, the real filestore and the real smtp adapter. The outside world is an
// in-process SMTP server on 127.0.0.1 (testenv.FakeSMTP), an in-memory credential store and a fake clock.
//
// Test steps of the task:
//   - Step 2 (no send without confirmation): TestSendingWithoutConfirmationIsImpossible
//   - Step 3 (timeout after acceptance -> UNKNOWN, no duplicate): TestTimeoutAfterAcceptanceIsUnknownAndNotResent,
//     TestCrashDuringSendIsReconciledNotResent
//   - Step 4 (the SMTP password never reaches the database): TestSMTPPasswordNeverReachesTheDatabase
//
// Bounded retries (PRD section 7.3): TestTransientFailuresEndFailedWithOneJob, TestUnreadableAttachmentEndsFailed,
// TestCrashBeforeSendingIsRequeued.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite", for setup rows and the secret scan

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// The senddelivery service is the homework mailer of the proposal pipeline: the app passes it as
// homeworkflow.Deps.Mailer without a bridge.
var _ homeworkflow.HomeworkMailer = (*senddelivery.Service)(nil)

var deliveryT0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

const (
	mailUser     = "student@example.org"
	mailPassword = "fake-smtp-secret-7f3a9c1e" // obviously fake; Step 4 looks for it in the database file
	mailContent  = "# Homework\n\nExercise 3: x = 42.\n"
)

// deliveryIDs is the IDGenerator of the scenario.
type deliveryIDs struct {
	mu sync.Mutex
	n  int
}

func (g *deliveryIDs) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s-%d", prefix, g.n)
}

func (g *deliveryIDs) NewDeliveryID() shared.DeliveryID { return shared.DeliveryID(g.next("dlv")) }
func (g *deliveryIDs) NewMessageID() string             { return "<" + g.next("msg") + "@paratech.test>" }
func (g *deliveryIDs) NewJobID() shared.JobID           { return shared.JobID(g.next("job")) }

type deliveryEnv struct {
	t      *testing.T
	path   string
	db     *sqlite.DB
	jobs   *sqlite.JobRepository
	dlv    *sqlite.DeliveryRepository
	files  *filestore.Store
	creds  *testenv.MemCredentials
	srv    *testenv.FakeSMTP
	clock  *testenv.FakeClock
	ids    *deliveryIDs
	svc    *senddelivery.Service
	runner *jobrunner.Service

	filesFail atomic.Bool // makes every Open of an artifact fail with a read error (not a missing file)
	stopped   bool
}

// flakyFiles is the filestore with a switch that makes Open fail.
type flakyFiles struct {
	*filestore.Store
	fail *atomic.Bool
}

var errReadFailed = errors.New("read failed")

func (f flakyFiles) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	if f.fail.Load() {
		return nil, errReadFailed
	}
	return f.Store.Open(ctx, ref)
}

// newDeliveryEnv builds everything on a fresh database with the assignments a1 and a2 and their exported homework.
func newDeliveryEnv(t *testing.T, b testenv.SMTPBehavior, smtpTimeout time.Duration) *deliveryEnv {
	t.Helper()
	dir := t.TempDir()
	e := &deliveryEnv{
		t: t, path: filepath.Join(dir, "app.db"), creds: testenv.NewMemCredentials(),
		clock: testenv.NewFakeClock(deliveryT0), ids: &deliveryIDs{},
	}
	b.User, b.Password = mailUser, mailPassword
	e.srv = testenv.NewFakeSMTP(t, b)

	files, err := filestore.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	e.files = files
	t.Cleanup(func() { _ = files.Close() })
	for _, a := range []shared.AssignmentID{"a1", "a2"} {
		e.writeHomework(a, mailContent)
	}
	if err := e.creds.Set(context.Background(), senddelivery.DefaultCredentialName, mailPassword); err != nil {
		t.Fatal(err)
	}

	e.open(smtpTimeout)
	e.exec(`INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a1', 'hw 1', 0, 0), ('a2', 'hw 2', 0, 0)`)
	return e
}

// open opens the database and builds the use case and a stopped runner, as one start of the app does.
func (e *deliveryEnv) open(smtpTimeout time.Duration) {
	e.t.Helper()
	e.db = openRunnerDB(e.t, e.path)
	e.jobs, e.dlv = sqlite.NewJobRepository(e.db), sqlite.NewDeliveryRepository(e.db)
	e.stopped = false

	mailer, err := smtp.New(smtp.Config{
		Host: e.srv.Host(), Port: e.srv.Port(), Username: mailUser, From: mailUser, RootCAs: e.srv.RootCAs(),
		Timeout: smtpTimeout,
	}, e.creds)
	if err != nil {
		e.t.Fatal(err)
	}
	e.svc, err = senddelivery.New(senddelivery.Deps{
		Deliveries: e.dlv, History: e.dlv, Artifacts: flakyFiles{Store: e.files, fail: &e.filesFail}, Email: mailer, Jobs: e.jobs, Credentials: e.creds, Clock: e.clock,
		IDs: e.ids, Tx: e.db,
	}, senddelivery.Config{})
	if err != nil {
		e.t.Fatal(err)
	}
	e.runner, err = jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, runnerConfig(1))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.runner.Register(jobrunner.Registration{
		Type: senddelivery.JobType, Handler: e.svc, Reconciler: e.svc, Recovery: jobrunner.RecoveryReconcile,
		Uncancellable: true, Timeout: 24 * time.Hour,
	}); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(e.stop)
}

func (e *deliveryEnv) stop() {
	e.t.Helper()
	if e.stopped {
		return
	}
	e.stopped = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.runner.Stop(ctx); err != nil {
		e.t.Errorf("Stop: %v", err)
	}
	if err := e.db.Close(); err != nil {
		e.t.Errorf("Close: %v", err)
	}
}

func (e *deliveryEnv) start() {
	e.t.Helper()
	if err := e.runner.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

// exec runs SQL on a separate connection, for rows of other contexts that the scenario does not create itself.
func (e *deliveryEnv) exec(query string) {
	e.t.Helper()
	raw, err := sql.Open("sqlite", e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(query); err != nil {
		e.t.Fatal(err)
	}
}

func (e *deliveryEnv) writeHomework(a shared.AssignmentID, content string) {
	e.t.Helper()
	ref, err := proposal.ExportRef(a)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, strings.NewReader(content)); err != nil {
		e.t.Fatal(err)
	}
}

// confirmed returns the command of a send_homework_email proposal the user validated and confirmed.
func (e *deliveryEnv) confirmed(id proposal.ID, a shared.AssignmentID) proposal.EmailCommand {
	e.t.Helper()
	p := e.validated(id, a)
	if err := p.Confirm(e.clock.Now()); err != nil {
		e.t.Fatal(err)
	}
	cmd, err := p.EmailCommand()
	if err != nil {
		e.t.Fatal(err)
	}
	return cmd
}

func (e *deliveryEnv) validated(id proposal.ID, a shared.AssignmentID) *proposal.Proposal {
	e.t.Helper()
	p, err := proposal.New(id, values.NewUntrustedOutput(`{"kind":"send_homework_email","assignment_id":"`+string(a)+`"}`), e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	r, err := proposal.NewRecipient("teacher@school.example")
	if err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(mailContent))
	dg, err := proposal.NewDigest(hex.EncodeToString(sum[:]), int64(len(mailContent)))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true, Recipient: r, Attachment: dg}, e.clock.Now()); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *deliveryEnv) delivery(key string) *delivery.Delivery {
	e.t.Helper()
	d, err := e.dlv.GetByIdempotencyKey(context.Background(), key)
	if err != nil {
		e.t.Fatalf("delivery %s: %v", key, err)
	}
	return d
}

func (e *deliveryEnv) jobOf(d *delivery.Delivery, round int) *job.Job {
	e.t.Helper()
	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Key() == fmt.Sprintf("%s:%s:%d", delivery.JobType, d.ID(), round) {
			return j
		}
	}
	e.t.Fatalf("no job of round %d of %s", round, d.ID())
	return nil
}

func (e *deliveryEnv) drive(what string, cond func() bool) {
	e.t.Helper()
	testenv.Eventually(e.t, what, cond, func() { e.clock.Advance(time.Second) })
}

// idle advances the clock far past every backoff, so that anything the runner would retry has run.
func (e *deliveryEnv) idle() {
	for range 300 {
		e.clock.Advance(time.Minute)
		time.Sleep(time.Millisecond)
	}
}

// Step 2: sending without the user's explicit confirmation is impossible (AC-14, AC-18).
func TestSendingWithoutConfirmationIsImpossible(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{}, 5*time.Second)
	ctx := context.Background()
	e.start()

	// A validated but unconfirmed proposal has no command, and the zero command is refused.
	if _, err := e.validated("p1", "a1").EmailCommand(); !errors.Is(err, proposal.ErrNotConfirmed) {
		t.Fatalf("command of an unconfirmed proposal = %v, want ErrNotConfirmed", err)
	}
	if err := e.svc.SendHomework(ctx, proposal.EmailCommand{}); !errors.Is(err, senddelivery.ErrZeroCommand) {
		t.Fatalf("SendHomework(zero) = %v, want ErrZeroCommand", err)
	}

	// The aggregate refuses a delivery without a confirmation time, so no other path can create one either.
	att, err := delivery.NewAttachment(must(proposal.ExportRef("a1")), strings.Repeat("0", 64), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delivery.New(delivery.Spec{
		ID: "d1", AssignmentID: "a1", Recipient: "teacher@school.example", Subject: "Homework", Attachment: att,
		MessageID: "<m@paratech.test>", Key: "k",
	}, e.clock.Now()); !errors.Is(err, delivery.ErrInvalidDelivery) {
		t.Fatalf("delivery without confirmation = %v, want ErrInvalidDelivery", err)
	}

	// The only exported way into the service that creates a delivery takes a proposal command; everything else
	// works on an existing delivery, a job, statuses or the password.
	allowed := map[reflect.Type]bool{
		reflect.TypeOf(proposal.EmailCommand{}): true, reflect.TypeOf(shared.DeliveryID("")): true,
		reflect.TypeOf(job.Snapshot{}): true, reflect.TypeOf([]delivery.Status{}): true, reflect.TypeOf(""): true,
		reflect.TypeOf(func(context.Context, job.Progress) error { return nil }): true,
	}
	svcType := reflect.TypeOf(&senddelivery.Service{})
	for i := range svcType.NumMethod() {
		m := svcType.Method(i)
		for j := 2; j < m.Type.NumIn(); j++ { // 0: receiver, 1: context
			if !allowed[m.Type.In(j)] {
				t.Errorf("Service.%s takes %v: a delivery could be created without a confirmed command", m.Name, m.Type.In(j))
			}
		}
	}

	e.idle()
	if got, _ := e.dlv.ListByStatus(ctx, delivery.Statuses()...); len(got) != 0 {
		t.Fatalf("%d deliveries exist", len(got))
	}
	if e.srv.Sessions() != 0 || len(e.srv.Messages()) != 0 {
		t.Fatal("the SMTP server was contacted without a confirmation")
	}

	// Control: with the confirmation the same homework is sent.
	if err := e.svc.SendHomework(ctx, e.confirmed("p1", "a1")); err != nil {
		t.Fatal(err)
	}
	e.drive("confirmed delivery sent", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusSent })
	if len(e.srv.Messages()) != 1 {
		t.Fatalf("server received %d messages, want 1", len(e.srv.Messages()))
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// Step 3: the server takes the whole message and never answers (timeout after a possible acceptance, PRD section 14
// case 7). The delivery becomes UNKNOWN, the job fails without a retry, and nothing sends the message again: not the
// runner, not a repeated request, not the startup steps, not a healed server. Only the user's "send again" does.
func TestTimeoutAfterAcceptanceIsUnknownAndNotResent(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{AfterData: testenv.AfterDataStall}, 300*time.Millisecond)
	ctx := context.Background()
	cmd := e.confirmed("p1", "a1")
	if err := e.svc.SendHomework(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	e.start()

	e.drive("delivery UNKNOWN", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusUnknown })
	d := e.delivery("proposal:p1")
	e.drive("job finished", func() bool { return e.jobOf(d, 0).Status().IsTerminal() })
	if j := e.jobOf(d, 0); j.Status() != job.StatusFailed || j.Attempts() != 1 {
		t.Fatalf("job %s after %d attempts, want FAILED after 1", j.Status(), j.Attempts())
	}
	if d.Attempts() != 1 || !strings.Contains(d.LastError(), "may have been accepted") {
		t.Fatalf("delivery attempts %d, last error %q", d.Attempts(), d.LastError())
	}
	if n := len(e.srv.Messages()); n != 1 {
		t.Fatalf("server received %d messages, want 1", n)
	}

	// Every automatic path that could retry: the same request again, the startup steps, a healed server, time.
	e.srv.SetBehavior(testenv.SMTPBehavior{User: mailUser, Password: mailPassword})
	if err := e.svc.SendHomework(ctx, cmd); err != nil {
		t.Fatalf("repeated request: %v", err)
	}
	if err := e.svc.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := e.svc.Recover(ctx); err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v", n, err)
	}
	if err := e.svc.Handle(ctx, e.jobOf(d, 0).Snapshot(), func(context.Context, job.Progress) error { return nil }); err != nil {
		t.Fatalf("the job's handler again: %v", err)
	}
	e.idle()
	if n := len(e.srv.Messages()); n != 1 {
		t.Fatalf("server received %d messages after the retries, want still 1 (duplicate sent)", n)
	}
	if got := e.delivery("proposal:p1").Status(); got != delivery.StatusUnknown {
		t.Fatalf("delivery %s, want UNKNOWN until the user resolves it", got)
	}

	// The user decides to send again: a new job of a new round sends it once more.
	if _, err := e.svc.SendAgain(ctx, d.ID()); err != nil {
		t.Fatal(err)
	}
	e.drive("sent again", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusSent })
	if n := len(e.srv.Messages()); n != 2 {
		t.Fatalf("server received %d messages after send again, want 2", n)
	}
}

// Step 3, crash variant: the process dies while a delivery is SENDING. The next start reconciles the job instead of
// running it again: the delivery becomes UNKNOWN, the job fails, and nothing is sent.
func TestCrashDuringSendIsReconciledNotResent(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{}, 5*time.Second)
	ctx := context.Background()
	if err := e.svc.SendHomework(ctx, e.confirmed("p1", "a1")); err != nil {
		t.Fatal(err)
	}

	// First process: the worker claimed the job and saved SENDING, then the process died during the send.
	d := e.delivery("proposal:p1")
	filter, err := job.NewClaimFilter([]job.Type{senddelivery.JobType}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.jobs.Claim(ctx, e.clock.Now(), filter); err != nil || !ok {
		t.Fatalf("Claim: %v %v", ok, err)
	}
	if err := d.StartSending(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.dlv.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	e.stop()

	// Second process: recovery before the workers start, as the app does.
	e.open(5 * time.Second)
	rep, err := e.runner.Recover(ctx)
	if err != nil || len(rep.Failed) != 1 || len(rep.Requeued)+len(rep.Completed)+len(rep.Errors) != 0 {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	if n, err := e.svc.Recover(ctx); err != nil || n != 0 {
		t.Fatalf("delivery Recover = %d, %v (the reconciler already handled it)", n, err)
	}
	e.start()
	e.idle()

	d = e.delivery("proposal:p1")
	if d.Status() != delivery.StatusUnknown || e.jobOf(d, 0).Status() != job.StatusFailed {
		t.Fatalf("delivery %s, job %s; want UNKNOWN and FAILED", d.Status(), e.jobOf(d, 0).Status())
	}
	if e.srv.Sessions() != 0 {
		t.Fatal("the message was sent again after the crash")
	}
}

// Step 4: the SMTP password is used (the server saw it) but is nowhere in the database: not in the file, not in any
// row, not in a last error, also after an authentication failure (hard rule 4, AC-19).
func TestSMTPPasswordNeverReachesTheDatabase(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{}, 5*time.Second)
	ctx := context.Background()
	if err := e.svc.SetMailPassword(ctx, mailPassword); err != nil {
		t.Fatal(err)
	}
	e.start()

	if err := e.svc.SendHomework(ctx, e.confirmed("p1", "a1")); err != nil {
		t.Fatal(err)
	}
	e.drive("first delivery sent", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusSent })

	// The server now refuses the login, so the password takes part in a failure that is stored.
	e.srv.SetBehavior(testenv.SMTPBehavior{User: mailUser, Password: "server-side-other-password"})
	if err := e.svc.SendHomework(ctx, e.confirmed("p2", "a2")); err != nil {
		t.Fatal(err)
	}
	e.drive("second delivery failed", func() bool { return e.delivery("proposal:p2").Status() == delivery.StatusFailed })
	if e.delivery("proposal:p2").LastError() == "" {
		t.Fatal("no last error stored for the failed login")
	}

	logins := e.srv.Logins()
	if len(logins) != 2 || logins[0].Password != mailPassword || !logins[0].TLS {
		t.Fatalf("server logins %+v: the password was not used over TLS", logins)
	}
	if got, err := e.creds.Get(ctx, senddelivery.DefaultCredentialName); err != nil || got != mailPassword {
		t.Fatal("the password is not in the credential store")
	}
	e.stop()

	// Every byte of the database files, including a write-ahead log or journal if one is left.
	files, err := filepath.Glob(e.path + "*")
	if err != nil || len(files) == 0 {
		t.Fatalf("database files: %v %v", files, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range [][]byte{[]byte(mailPassword), utf16le(mailPassword)} {
			if bytes.Contains(data, form) {
				t.Fatalf("%s contains the SMTP password", filepath.Base(f))
			}
		}
	}

	// Every value of every row, read back through SQL.
	raw, err := sql.Open("sqlite", e.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	tables, err := raw.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := tables.Close(); err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, table := range names {
		rows, err := raw.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				if strings.Contains(fmt.Sprint(v), mailPassword) || (v != nil && bytes.Contains(asBytes(v), []byte(mailPassword))) {
					t.Fatalf("%s.%s contains the SMTP password", table, cols[i])
				}
			}
			scanned++
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 4 { // two deliveries, two jobs at least
		t.Fatalf("scanned only %d rows", scanned)
	}
}

// The use case writes the password under the name the adapter reads it from, and the name is valid for wincred.
func TestMailCredentialNameIsShared(t *testing.T) {
	if smtp.DefaultCredentialName != senddelivery.DefaultCredentialName {
		t.Fatalf("smtp reads %q, senddelivery writes %q", smtp.DefaultCredentialName, senddelivery.DefaultCredentialName)
	}
	if err := wincred.CheckName(senddelivery.DefaultCredentialName); err != nil {
		t.Fatalf("credential name: %v", err)
	}
}

func utf16le(s string) []byte {
	out := make([]byte, 0, 2*len(s))
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

func asBytes(v any) []byte {
	if b, ok := v.([]byte); ok {
		return b
	}
	return nil
}

// deliveryJobs returns every job of the delivery, of any round.
func (e *deliveryEnv) deliveryJobs(d *delivery.Delivery) []*job.Job {
	e.t.Helper()
	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []*job.Job
	for _, j := range jobs {
		if strings.HasPrefix(j.Key(), fmt.Sprintf("%s:%s:", delivery.JobType, d.ID())) {
			out = append(out, j)
		}
	}
	return out
}

// The server answers every RCPT with 451 (definitely not accepted, try later). Repeated requests and Resume while the
// job waits for its retry add no job; after the attempts of its one job the delivery is FAILED, nothing was sent, and
// a new delivery of the same homework is allowed (PRD section 7.3: bounded retries).
func TestTransientFailuresEndFailedWithOneJob(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{RcptReply: "451 4.3.0 try again later"}, 5*time.Second)
	ctx := context.Background()
	cmd := e.confirmed("p1", "a1")
	if err := e.svc.SendHomework(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	e.start()

	e.drive("first transient failure", func() bool { return e.delivery("proposal:p1").Attempts() >= 1 })
	for range 3 {
		if err := e.svc.SendHomework(ctx, cmd); err != nil {
			t.Fatalf("repeated request: %v", err)
		}
		if err := e.svc.Resume(ctx); err != nil {
			t.Fatal(err)
		}
	}

	e.drive("delivery FAILED", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusFailed })
	d := e.delivery("proposal:p1")
	e.drive("job finished", func() bool { return e.jobOf(d, 0).Status().IsTerminal() })
	jobs := e.deliveryJobs(d)
	if len(jobs) != 1 || jobs[0].Status() != job.StatusFailed || jobs[0].Attempts() != senddelivery.DefaultMaxAttempts {
		t.Fatalf("%d jobs, first %s after %d attempts; want one FAILED after %d", len(jobs), jobs[0].Status(),
			jobs[0].Attempts(), senddelivery.DefaultMaxAttempts)
	}
	if d.Attempts() != senddelivery.DefaultMaxAttempts || !strings.HasPrefix(d.LastError(), "attempts exhausted") ||
		len(e.srv.Messages()) != 0 {
		t.Fatalf("delivery attempts %d, last error %q, messages %d", d.Attempts(), d.LastError(), len(e.srv.Messages()))
	}

	e.srv.SetBehavior(testenv.SMTPBehavior{User: mailUser, Password: mailPassword})
	if err := e.svc.SendHomework(ctx, e.confirmed("p2", "a1")); err != nil {
		t.Fatalf("new delivery of the same homework: %v", err)
	}
	e.drive("new delivery sent", func() bool { return e.delivery("proposal:p2").Status() == delivery.StatusSent })
}

// The attachment cannot be read on any attempt (a storage error before anything is handed over): the last attempt of
// the job fails the delivery instead of leaving it PENDING with no job, and the server is never contacted.
func TestUnreadableAttachmentEndsFailed(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{}, 5*time.Second)
	ctx := context.Background()
	if err := e.svc.SendHomework(ctx, e.confirmed("p1", "a1")); err != nil {
		t.Fatal(err)
	}
	e.filesFail.Store(true)
	e.start()

	e.drive("delivery FAILED", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusFailed })
	d := e.delivery("proposal:p1")
	e.drive("job finished", func() bool { return e.jobOf(d, 0).Status().IsTerminal() })
	if j := e.jobOf(d, 0); j.Status() != job.StatusFailed || j.Attempts() != senddelivery.DefaultMaxAttempts {
		t.Fatalf("job %s after %d attempts", j.Status(), j.Attempts())
	}
	if d.Attempts() != 0 || !strings.HasPrefix(d.LastError(), "attempts exhausted") || e.srv.Sessions() != 0 {
		t.Fatalf("delivery attempts %d, last error %q, sessions %d", d.Attempts(), d.LastError(), e.srv.Sessions())
	}
	if err := e.svc.Resume(ctx); err != nil || len(e.deliveryJobs(d)) != 1 {
		t.Fatalf("Resume = %v, %d jobs", err, len(e.deliveryJobs(d)))
	}
}

// The process dies after the job was claimed but before SENDING was stored. Recovery finds the job of the current
// round by its key (no new job is added) and requeues it; the message is sent exactly once.
func TestCrashBeforeSendingIsRequeued(t *testing.T) {
	e := newDeliveryEnv(t, testenv.SMTPBehavior{}, 5*time.Second)
	ctx := context.Background()
	if err := e.svc.SendHomework(ctx, e.confirmed("p1", "a1")); err != nil {
		t.Fatal(err)
	}
	filter, err := job.NewClaimFilter([]job.Type{senddelivery.JobType}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.jobs.Claim(ctx, e.clock.Now(), filter); err != nil || !ok {
		t.Fatalf("Claim: %v %v", ok, err)
	}
	e.stop()

	e.open(5 * time.Second)
	rep, err := e.runner.Recover(ctx)
	if err != nil || len(rep.Requeued) != 1 || len(rep.Failed)+len(rep.Completed)+len(rep.Errors) != 0 {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	if err := e.svc.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	e.start()

	e.drive("delivery sent", func() bool { return e.delivery("proposal:p1").Status() == delivery.StatusSent })
	if d := e.delivery("proposal:p1"); len(e.deliveryJobs(d)) != 1 || len(e.srv.Messages()) != 1 {
		t.Fatalf("%d jobs, %d messages; want 1 and 1", len(e.deliveryJobs(d)), len(e.srv.Messages()))
	}
}
