package senddelivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	t0      = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	errDisk = errors.New("disk error")
)

const homework = "# Homework\n\nExercise 3: done.\n"

// store is the in-memory database behind the repository fakes. Deliveries are kept as State and rebuilt with Restore,
// as the sqlite adapter does, and Save is the version compare-and-swap of ADR-0004(d).
type store struct {
	t *testing.T

	mu         sync.Mutex
	deliveries map[shared.DeliveryID]delivery.State
	saveErr    error // returned by the next saves when set
	saveFails  int   // when above 0, the next saves fail with errDisk, one less each
	saves      int

	jobs   map[string]*job.Job
	nextID int
}

type txKey struct{}

func newStore(t *testing.T) *store {
	return &store{t: t, deliveries: map[shared.DeliveryID]delivery.State{}, jobs: map[string]*job.Job{}}
}

func (s *store) Get(ctx context.Context, id shared.DeliveryID) (*delivery.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.deliveries[id]
	if !ok {
		return nil, fmt.Errorf("delivery %s: %w", id, shared.ErrNotFound)
	}
	return delivery.Restore(st)
}

func (s *store) GetByIdempotencyKey(ctx context.Context, key string) (*delivery.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.deliveries {
		if st.Key == key {
			return delivery.Restore(st)
		}
	}
	return nil, fmt.Errorf("delivery by key: %w", shared.ErrNotFound)
}

func (s *store) Save(ctx context.Context, d *delivery.Delivery) error {
	if ctx.Value(txKey{}) == nil {
		s.t.Errorf("delivery saved outside Tx.Do")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.saveFails > 0 {
		s.saveFails--
		return errDisk
	}
	stored, exists := s.deliveries[d.ID()]
	if (d.Version() == 0) == exists || (exists && stored.Version != d.Version()) {
		return shared.ErrVersionConflict
	}
	for _, other := range s.deliveries {
		if d.Version() == 0 && other.Key == d.IdempotencyKey() {
			return shared.ErrDuplicate
		}
	}
	st := d.State()
	st.Version = d.Version() + 1
	s.deliveries[d.ID()] = st
	s.saves++
	d.Persisted(st.Version)
	return nil
}

func (s *store) ListByStatus(ctx context.Context, statuses ...delivery.Status) ([]*delivery.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*delivery.Delivery
	for _, st := range s.deliveries {
		for _, want := range statuses {
			if st.Status == want {
				d, err := delivery.Restore(st)
				if err != nil {
					return nil, err
				}
				out = append(out, d)
			}
		}
	}
	return out, nil
}

func (s *store) status(id shared.DeliveryID) delivery.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveries[id].Status
}

func (s *store) only() delivery.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deliveries) != 1 {
		s.t.Fatalf("%d deliveries, want 1", len(s.deliveries))
	}
	for _, st := range s.deliveries {
		return st
	}
	return delivery.State{}
}

// Tx runs fn with a marked context; the fake has no rollback, which the tests do not rely on.
func (s *store) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(context.WithValue(ctx, txKey{}, true))
}

// JobEnqueuer: one job per key.
func (s *store) Enqueue(ctx context.Context, j *job.Job) (shared.JobID, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.jobs[j.Key()]; ok {
		return old.ID(), false, nil
	}
	s.jobs[j.Key()] = j
	j.Persisted(1)
	return j.ID(), true, nil
}

func (s *store) jobKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.jobs {
		out = append(out, k)
	}
	return out
}

// claim runs the job runner's part of one attempt of the job with key: a job that ran before is retried and
// requeued first, then the job is claimed, which counts the attempt. It returns the snapshot a handler gets.
func (s *store) claim(t *testing.T, key string) job.Snapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[key]
	if !ok {
		t.Fatalf("no job %s", key)
	}
	if j.Status() == job.StatusRunning {
		if err := j.Retry(t0, t0, "retry"); err != nil {
			t.Fatal(err)
		}
		if err := j.Requeue(t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Claim(t0); err != nil {
		t.Fatalf("claim %s: %v", key, err)
	}
	return j.Snapshot()
}

func (s *store) snapshot(t *testing.T, key string) job.Snapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[key]
	if !ok {
		t.Fatalf("no job %s", key)
	}
	return j.Snapshot()
}

// IDGenerator.
func (s *store) NewDeliveryID() shared.DeliveryID { return shared.DeliveryID(s.next("d")) }
func (s *store) NewMessageID() string             { return "<" + s.next("m") + "@paratech.test>" }
func (s *store) NewJobID() shared.JobID           { return shared.JobID(s.next("j")) }

func (s *store) next(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("%s%d", prefix, s.nextID)
}

// email is a scripted EmailAdapter. result decides each Send; nil means accepted.
type email struct {
	mu        sync.Mutex
	sent      []string // attachment contents handed over, in call order
	result    func(ctx context.Context, d *delivery.Delivery) error
	reconcile func(d *delivery.Delivery) (bool, error)
	onSend    func(d *delivery.Delivery)
}

func (e *email) Send(ctx context.Context, d *delivery.Delivery, attachment io.Reader) error {
	if e.onSend != nil {
		e.onSend(d)
	}
	b, err := io.ReadAll(attachment)
	if err != nil {
		return fmt.Errorf("read attachment: %w", delivery.ErrTransient)
	}
	e.mu.Lock()
	e.sent = append(e.sent, string(b))
	result := e.result
	e.mu.Unlock()
	if result == nil {
		return nil
	}
	return result(ctx, d)
}

func (e *email) Reconcile(ctx context.Context, d *delivery.Delivery) (bool, error) {
	if e.reconcile == nil {
		return false, delivery.ErrOutcomeUnknown
	}
	return e.reconcile(d)
}

func (e *email) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.sent)
}

// artifacts is the artifact store of the tests; openErr makes Open fail (a read error, not a missing file).
type artifacts struct {
	*testenv.MemArtifacts

	mu      sync.Mutex
	openErr error
}

func (a *artifacts) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	a.mu.Lock()
	err := a.openErr
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return a.MemArtifacts.Open(ctx, ref)
}

func (a *artifacts) failOpen(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openErr = err
}

type env struct {
	st    *store
	email *email
	files *artifacts
	creds *testenv.MemCredentials
	clock *testenv.FakeClock
	svc   *Service
}

func newEnv(t *testing.T) *env { t.Helper(); return newEnvWith(t, Config{}) }

func newEnvWith(t *testing.T, cfg Config) *env {
	t.Helper()
	e := &env{
		st: newStore(t), email: &email{}, files: &artifacts{MemArtifacts: testenv.NewMemArtifacts()},
		creds: testenv.NewMemCredentials(), clock: testenv.NewFakeClock(t0),
	}
	svc, err := New(Deps{
		Deliveries: e.st, Artifacts: e.files, Email: e.email, Jobs: e.st, Credentials: e.creds, Clock: e.clock,
		IDs: e.st, Tx: e.st,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	e.writeHomework(t, "a1", homework)
	return e
}

func (e *env) writeHomework(t *testing.T, a shared.AssignmentID, content string) {
	t.Helper()
	ref, err := proposal.ExportRef(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, bytes.NewReader([]byte(content))); err != nil {
		t.Fatal(err)
	}
}

func digestOf(t *testing.T, content string) proposal.Digest {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	d, err := proposal.NewDigest(hex.EncodeToString(sum[:]), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// command returns the EmailCommand of a confirmed send_homework_email proposal for assignment a, pinned to content.
func command(t *testing.T, id proposal.ID, a shared.AssignmentID, content string) proposal.EmailCommand {
	t.Helper()
	p := validated(t, id, a, content)
	if err := p.Confirm(t0.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	cmd, err := p.EmailCommand()
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

func validated(t *testing.T, id proposal.ID, a shared.AssignmentID, content string) *proposal.Proposal {
	t.Helper()
	p, err := proposal.New(id, values.NewUntrustedOutput(`{"kind":"send_homework_email","assignment_id":"`+string(a)+`"}`),
		t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r, err := proposal.NewRecipient("teacher@school.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true, Recipient: r, Attachment: digestOf(t, content)},
		t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	return p
}

// created runs SendHomework for proposal p1 of a1 and returns the delivery id and the job snapshot.
func (e *env) created(t *testing.T) (shared.DeliveryID, job.Snapshot) {
	t.Helper()
	if err := e.svc.SendHomework(context.Background(), command(t, "p1", "a1", homework)); err != nil {
		t.Fatalf("SendHomework: %v", err)
	}
	st := e.st.only()
	return st.ID, e.st.snapshot(t, jobKey(st.ID, 0))
}

func jobKey(id shared.DeliveryID, round int) string {
	return fmt.Sprintf("send_email:%s:%d", id, round)
}

func noReport(context.Context, job.Progress) error { return nil }
