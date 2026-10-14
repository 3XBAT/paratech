package tests

// Contract test for the aggregate version protocol of ADR-0004(d).
//
// Two things are enforced without a SQL engine:
//   - shape: every repository method that stores an aggregate takes a type that has Version() int64 and
//     Persisted(int64), and Save returns only an error (the adapter records the new version on the aggregate);
//   - behaviour: a small in-memory compare-and-swap repository that follows the documented rules shows that repeated
//     saves of one instance succeed, a stale instance gets shared.ErrVersionConflict, a purged row is not
//     re-inserted, and a duplicate key is shared.ErrDuplicate. It is the executable form of the doc comments and the
//     reference for the contract tests of the sqlite adapter (TASK-005).

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// versioned is the part of every aggregate that the repository adapters use (ADR-0004(d)).
type versioned interface {
	Version() int64
	Persisted(version int64)
}

func ifaceOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// storingMethods lists the repository methods that write aggregates, with the position of the aggregate argument
// (the context is index 0). ApplyChanges takes slices of aggregates; Enqueue and Delete are listed too, they also
// work with the version.
var storingMethods = []struct {
	iface  reflect.Type
	method string
	arg    int
	slice  bool // the parameter is a slice of aggregates
}{
	{ifaceOf[jobrunner.JobRepository](), "Save", 1, false},
	{ifaceOf[jobrunner.JobRepository](), "Enqueue", 1, false},
	{ifaceOf[recordingsession.RecordingRepository](), "Save", 1, false},
	{ifaceOf[recordingsession.RecordingRepository](), "Delete", 1, false},
	{ifaceOf[transcribe.TranscriptRepository](), "Save", 1, false},
	{ifaceOf[modelcatalog.ModelRepository](), "Save", 1, false},
	{ifaceOf[modelcatalog.SettingsRepository](), "Save", 1, false},
	{ifaceOf[schedulesync.LessonRepository](), "Save", 1, false},
	{ifaceOf[schedulesync.LessonRepository](), "ApplyChanges", 1, true},
	{ifaceOf[homeworkflow.AssignmentRepository](), "Save", 1, false},
	{ifaceOf[homeworkflow.ProposalRepository](), "Save", 1, false},
	{ifaceOf[senddelivery.DeliveryRepository](), "Save", 1, false},
}

func TestStoringPortsUseVersionedAggregates(t *testing.T) {
	versionedType := ifaceOf[versioned]()
	errorType := ifaceOf[error]()
	for _, sm := range storingMethods {
		name := sm.iface.Name() + "." + sm.method
		t.Run(name, func(t *testing.T) {
			m, ok := sm.iface.MethodByName(sm.method)
			if !ok {
				t.Fatalf("%s does not exist", name)
			}
			if m.Type.NumIn() <= sm.arg {
				t.Fatalf("%s has %d parameters, want the aggregate at index %d", name, m.Type.NumIn(), sm.arg)
			}
			agg := m.Type.In(sm.arg)
			if sm.slice {
				agg = agg.Elem()
			}
			if !agg.Implements(versionedType) {
				t.Errorf("%s: parameter type %v has no Version() int64 and Persisted(int64), so a repeated Save of one instance would hit its own earlier write (ADR-0004(d))", name, agg)
			}
			// Save returns only an error: the new version is recorded on the aggregate by the adapter.
			if sm.method == "Save" && (m.Type.NumOut() != 1 || m.Type.Out(0) != errorType) {
				t.Errorf("%s must return only error, the adapter records the new version with Persisted", name)
			}
		})
	}
}

func TestStoringPortsCheckDetectsUnversionedType(t *testing.T) {
	// A type without Persisted must not satisfy the contract (control for the test above).
	type plain struct{}
	if reflect.TypeOf(&plain{}).Implements(ifaceOf[versioned]()) {
		t.Fatal("a type without Version and Persisted implements versioned")
	}
}

// casRepo is the reference behaviour of Save for any versioned aggregate (ADR-0004(d)): rows maps an id to the
// stored version, uniqueKeys maps a unique key to the id that owns it.
type casRepo struct {
	rows       map[string]int64
	uniqueKeys map[string]string
}

func newCASRepo() *casRepo {
	return &casRepo{rows: map[string]int64{}, uniqueKeys: map[string]string{}}
}

// save follows the documented rules; key is the unique key of the aggregate ("" when it has none).
func (r *casRepo) save(_ context.Context, id, key string, a versioned) error {
	if a.Version() == 0 { // never stored: INSERT with version 1
		if _, exists := r.rows[id]; exists {
			return shared.ErrVersionConflict
		}
		if owner, taken := r.uniqueKeys[key]; key != "" && taken && owner != id {
			return shared.ErrDuplicate
		}
		r.rows[id] = 1
		if key != "" {
			r.uniqueKeys[key] = id
		}
		a.Persisted(1)
		return nil
	}
	stored, exists := r.rows[id]
	if !exists || stored != a.Version() { // purged or changed meanwhile: never an insert
		return shared.ErrVersionConflict
	}
	r.rows[id] = stored + 1
	a.Persisted(stored + 1)
	return nil
}

func TestCASRepoContract(t *testing.T) {
	ctx := context.Background()

	t.Run("consecutive saves of one instance succeed", func(t *testing.T) {
		repo, j := newCASRepo(), &job.Job{}
		for i, want := range []int64{1, 2, 3} {
			if err := repo.save(ctx, "j1", "", j); err != nil {
				t.Fatalf("save #%d: %v", i+1, err)
			}
			if j.Version() != want || repo.rows["j1"] != want {
				t.Fatalf("after save #%d: aggregate version %d, stored %d, want %d", i+1, j.Version(), repo.rows["j1"], want)
			}
		}
	})

	t.Run("stale instance gets a version conflict", func(t *testing.T) {
		repo, a := newCASRepo(), &job.Job{}
		if err := repo.save(ctx, "j1", "", a); err != nil {
			t.Fatal(err)
		}
		stale := &job.Job{}
		stale.Persisted(a.Version()) // a second reader loaded the row at version 1
		if err := repo.save(ctx, "j1", "", a); err != nil {
			t.Fatal(err) // the first writer moves the row to version 2
		}
		err := repo.save(ctx, "j1", "", stale)
		if !errors.Is(err, shared.ErrVersionConflict) || !errors.Is(err, shared.ErrConflict) {
			t.Fatalf("stale save error = %v, want ErrVersionConflict (and ErrConflict)", err)
		}
		if errors.Is(err, shared.ErrDuplicate) {
			t.Fatal("a version conflict must not look like a duplicate key")
		}
		if repo.rows["j1"] != 2 || stale.Version() != 1 {
			t.Fatalf("a failed save changed state: stored %d, stale version %d", repo.rows["j1"], stale.Version())
		}
	})

	t.Run("purged row is not re-inserted", func(t *testing.T) {
		repo, j := newCASRepo(), &job.Job{}
		if err := repo.save(ctx, "j1", "", j); err != nil {
			t.Fatal(err)
		}
		delete(repo.rows, "j1")
		if err := repo.save(ctx, "j1", "", j); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("save after purge error = %v, want ErrVersionConflict", err)
		}
		if _, back := repo.rows["j1"]; back {
			t.Fatal("a stored aggregate whose row was purged was inserted again")
		}
	})

	t.Run("new aggregate with an existing id is a version conflict", func(t *testing.T) {
		repo := newCASRepo()
		if err := repo.save(ctx, "s", "", &job.Job{}); err != nil {
			t.Fatal(err)
		}
		if err := repo.save(ctx, "s", "", &job.Job{}); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("second first-save error = %v, want ErrVersionConflict", err)
		}
	})

	t.Run("duplicate key is told apart from a stale version", func(t *testing.T) {
		repo := newCASRepo()
		if err := repo.save(ctx, "d1", "key", &job.Job{}); err != nil {
			t.Fatal(err)
		}
		err := repo.save(ctx, "d2", "key", &job.Job{})
		if !errors.Is(err, shared.ErrDuplicate) || !errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("duplicate key error = %v, want ErrDuplicate (and ErrConflict) only", err)
		}
	})
}

// A handler gets a read-only value, never the aggregate the runner owns (ADR-0004 job ownership).
func TestHandlerReceivesSnapshotNotAggregate(t *testing.T) {
	m, ok := ifaceOf[jobrunner.Handler]().MethodByName("Handle")
	if !ok {
		t.Fatal("Handler.Handle does not exist")
	}
	if m.Type.NumIn() != 3 {
		t.Fatalf("Handle has %d parameters, want ctx, snapshot, report", m.Type.NumIn())
	}
	if got, want := m.Type.In(1), reflect.TypeOf(job.Snapshot{}); got != want {
		t.Fatalf("Handle takes %v, want %v", got, want)
	}
	if reflect.TypeOf(job.Snapshot{}).Implements(ifaceOf[versioned]()) || reflect.TypeOf(&job.Snapshot{}).Implements(ifaceOf[versioned]()) {
		t.Fatal("Snapshot must not expose the persistence methods of the aggregate")
	}
}
