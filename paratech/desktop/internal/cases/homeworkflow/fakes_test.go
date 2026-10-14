package homeworkflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

// store is the in-memory local database behind the repository fakes. Proposals are kept as State and rebuilt with
// Restore, as the sqlite adapter will do, and Save is the version compare-and-swap of ADR-0004(d).
type store struct {
	t *testing.T

	mu          sync.Mutex
	proposals   map[proposal.ID]proposal.State
	assignments map[shared.AssignmentID]bool
	recipient   string
	recipErr    error // returned by HomeworkRecipient when set
	nextID      int
	saveErr     error // returned by the next proposal saves

	txMu sync.Mutex
}

type txKey struct{}

func newStore(t *testing.T) *store {
	return &store{t: t, proposals: map[proposal.ID]proposal.State{}, assignments: map[shared.AssignmentID]bool{}}
}

// ProposalRepository.

type proposalRepo struct{ s *store }

func (r proposalRepo) Get(ctx context.Context, id proposal.ID) (*proposal.Proposal, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.proposals[id]
	if !ok {
		return nil, fmt.Errorf("proposal %s: %w", id, shared.ErrNotFound)
	}
	return proposal.Restore(st)
}

func (r proposalRepo) Save(ctx context.Context, p *proposal.Proposal) error {
	if ctx.Value(txKey{}) == nil {
		r.s.t.Errorf("proposal saved outside Tx.Do")
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.saveErr != nil {
		return r.s.saveErr
	}
	stored, exists := r.s.proposals[p.ID()]
	if (p.Version() == 0) == exists || (exists && stored.Version != p.Version()) {
		return shared.ErrVersionConflict
	}
	st := p.State()
	st.Version = p.Version() + 1
	r.s.proposals[p.ID()] = st
	p.Persisted(st.Version)
	return nil
}

// AssignmentRepository. Only existence matters to the proposal pipeline; the aggregate is a skeleton until TASK-042.

type assignmentRepo struct{ s *store }

func (r assignmentRepo) Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if !r.s.assignments[id] {
		return nil, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	return new(assignment.Assignment), nil
}

func (r assignmentRepo) GetBySource(context.Context, shared.TranscriptID, int) (*assignment.Assignment, error) {
	r.s.t.Error("the proposal pipeline must not look assignments up by their extraction source")
	return nil, shared.ErrNotFound
}

func (r assignmentRepo) Save(context.Context, *assignment.Assignment) error {
	r.s.t.Error("the proposal pipeline must not write assignments")
	return nil
}

// RecipientSource.

type recipients struct{ s *store }

func (r recipients) HomeworkRecipient(context.Context) (proposal.Recipient, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.recipErr != nil {
		return proposal.Recipient{}, r.s.recipErr
	}
	if r.s.recipient == "" {
		return proposal.Recipient{}, nil
	}
	return proposal.NewRecipient(r.s.recipient)
}

// IDGenerator.

type ids struct{ s *store }

func (g ids) next(prefix string) string {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()
	g.s.nextID++
	return fmt.Sprintf("%s%d", prefix, g.s.nextID)
}

func (g ids) NewAssignmentID() shared.AssignmentID { return shared.AssignmentID(g.next("a")) }
func (g ids) NewJobID() shared.JobID               { return shared.JobID(g.next("j")) }
func (g ids) NewProposalID() proposal.ID           { return proposal.ID(g.next("p")) }

// Tx serializes transactions; there is no rollback because every fn here writes once, last.

type tx struct{ s *store }

func (x tx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(txKey{}) != nil {
		return fn(ctx)
	}
	x.s.txMu.Lock()
	defer x.s.txMu.Unlock()
	return fn(context.WithValue(ctx, txKey{}, true))
}

// env is a Service over the fakes, with the side-effect fakes of testenv.
type env struct {
	*store
	svc      *Service
	clock    *testenv.FakeClock
	files    *testenv.MemArtifacts
	mailer   *testenv.FakeMailer
	exporter *testenv.FakeExporter
	calendar *testenv.FakeCalendar
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		store: newStore(t), clock: testenv.NewFakeClock(t0), files: testenv.NewMemArtifacts(),
		mailer: &testenv.FakeMailer{}, exporter: &testenv.FakeExporter{}, calendar: &testenv.FakeCalendar{},
	}
	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: assignmentRepo{e.store}, Artifacts: e.files,
		Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Clock: e.clock, IDs: ids{e.store}, Tx: tx{e.store},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// withHomework sets up the trusted local state of a ready-to-send homework: assignment a1, its export and the recipient.
func (e *env) withHomework(t *testing.T) *env {
	t.Helper()
	e.assignments["a1"] = true
	e.recipient = "teacher@school.example"
	ref, err := proposal.ExportRef("a1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, strings.NewReader("my homework")); err != nil {
		t.Fatal(err)
	}
	return e
}

// sideEffects returns the number of calls of every side-effect port.
func (e *env) sideEffects() int {
	return len(e.mailer.Calls()) + len(e.exporter.Calls()) + len(e.calendar.Calls())
}

// Compile-time checks: the store satisfies the ports it stands for.
var (
	_ ProposalRepository   = proposalRepo{}
	_ AssignmentRepository = assignmentRepo{}
	_ RecipientSource      = recipients{}
	_ IDGenerator          = ids{}
	_ Tx                   = tx{}
	_ HomeworkMailer       = (*testenv.FakeMailer)(nil)
	_ HomeworkExporter     = (*testenv.FakeExporter)(nil)
	_ DeadlineCalendar     = (*testenv.FakeCalendar)(nil)
	_ ArtifactStore        = (*testenv.MemArtifacts)(nil)
)
