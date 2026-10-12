package job

import "testing"

func TestSnapshotCopiesState(t *testing.T) {
	cases := []struct {
		name string
		job  Job
	}{
		{"queued", Job{id: "j1", typ: "stt", status: StatusQueued}},
		{"running", Job{id: "j2", typ: "summarize", status: StatusRunning}},
		{"zero", Job{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := tc.job
			s := j.Snapshot()
			if s.ID() != j.ID() || s.Type() != j.Type() || s.Status() != j.Status() {
				t.Fatalf("Snapshot() = {%q %q %q}, want {%q %q %q}", s.ID(), s.Type(), s.Status(), j.ID(), j.Type(), j.Status())
			}
			// The snapshot is a copy: later changes of the job do not show through.
			j.status = StatusCompleted
			j.id = "other"
			if s.Status() == StatusCompleted || s.ID() == "other" {
				t.Fatal("snapshot follows later changes of the job")
			}
		})
	}
}

func TestVersionAndPersisted(t *testing.T) {
	var j Job
	if j.Version() != 0 {
		t.Fatalf("new job Version() = %d, want 0 (never stored)", j.Version())
	}
	j.Persisted(3)
	if j.Version() != 3 {
		t.Fatalf("Version() after Persisted(3) = %d", j.Version())
	}
}
