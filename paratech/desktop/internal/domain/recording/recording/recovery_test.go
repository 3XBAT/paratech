package recording

import (
	"errors"
	"testing"
	"time"
)

// AC-05: the startup decision for every status (PRD section 6.2, section 14 case 1).
func TestMarkInterrupted(t *testing.T) {
	cases := []struct {
		from     Status
		want     Status
		ok       bool
		assemble bool
	}{
		{StatusStarting, StatusFailed, true, false},
		{StatusRecording, StatusRecoveryRequired, true, true},
		{StatusStopping, StatusStopping, true, true},
		{StatusFinalizing, StatusFinalizing, true, true},
		{StatusRecoveryRequired, StatusRecoveryRequired, true, true},
		{StatusCompleted, StatusCompleted, false, false},
		{StatusFailed, StatusFailed, false, false},
	}
	if len(cases) != len(Statuses()) {
		t.Fatal("a status is not covered")
	}
	now := t0.Add(5 * time.Hour)
	for _, tc := range cases {
		t.Run(string(tc.from), func(t *testing.T) {
			r := inStatus(t, tc.from)
			before := r.Problem()
			err := r.MarkInterrupted(now, "the app stopped")
			if !tc.ok {
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("error = %v, want ErrInvalidTransition", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if r.Status() != tc.want || r.AwaitsAssembly() != tc.assemble {
				t.Fatalf("status %s, awaits assembly %v", r.Status(), r.AwaitsAssembly())
			}
			changed := tc.ok && tc.from != tc.want
			if changed && r.Problem() != "the app stopped" || !changed && r.Problem() != before {
				t.Fatalf("problem = %q", r.Problem())
			}
		})
	}
}

// The capture of an interrupted RECORDING counts as ended at its last heartbeat.
func TestMarkInterruptedStopsAtTheLastHeartbeat(t *testing.T) {
	r := inStatus(t, StatusRecording)
	beat := t0.Add(time.Hour)
	if err := r.Heartbeat(beat); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkInterrupted(t0.Add(5*time.Hour), "the app stopped"); err != nil {
		t.Fatal(err)
	}
	if !r.StoppedAt().Equal(beat) || !r.HeartbeatAt().Equal(beat) {
		t.Fatalf("stopped %v, heartbeat %v", r.StoppedAt(), r.HeartbeatAt())
	}
	if _, err := restoreStored(r); err != nil {
		t.Fatalf("a marked recording does not restore: %v", err)
	}
	if err := r.MarkInterrupted(t0.Add(6*time.Hour), "again"); err != nil || r.Problem() != "the app stopped" {
		t.Fatalf("a repeated mark = %v, problem %q", err, r.Problem())
	}
}

func TestRecoveryOutcomesFromEveryStatus(t *testing.T) {
	later := t0.Add(5 * time.Hour)
	awaiting := []Status{StatusStopping, StatusFinalizing, StatusRecoveryRequired}
	outcomes := []struct {
		name    string
		call    func(t *testing.T, r *Recording) error
		reached Status // "" means unchanged
	}{
		{"CompleteRecovery", func(t *testing.T, r *Recording) error {
			return r.CompleteRecovery(later, mustRef(t, "recordings/r1.wav"), time.Minute)
		}, StatusCompleted},
		{"FailRecovery", func(_ *testing.T, r *Recording) error { return r.FailRecovery(later, "no audio") }, StatusFailed},
		{"DeferRecovery", func(_ *testing.T, r *Recording) error { return r.DeferRecovery(later, "disk busy") }, ""},
	}
	for _, o := range outcomes {
		for _, s := range Statuses() {
			t.Run(o.name+"/"+string(s), func(t *testing.T) {
				r := inStatus(t, s)
				err := o.call(t, r)
				allowed := false
				for _, a := range awaiting {
					allowed = allowed || a == s
				}
				if !allowed {
					if !errors.Is(err, ErrInvalidTransition) || r.Status() != s {
						t.Fatalf("error = %v, status %s", err, r.Status())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := o.reached
				if want == "" {
					want = s
				}
				if r.Status() != want || !r.UpdatedAt().Equal(later) {
					t.Fatalf("status %s, updated %v", r.Status(), r.UpdatedAt())
				}
				if _, err := restoreStored(r); err != nil {
					t.Fatalf("the outcome does not restore: %v", err)
				}
			})
		}
	}
}

func TestRecoveryOutcomesValidate(t *testing.T) {
	later := t0.Add(5 * time.Hour)
	cases := []struct {
		name string
		call func(r *Recording) error
	}{
		{"foreign track", func(r *Recording) error {
			return r.CompleteRecovery(later, mustRef(t, "recordings/other.wav"), time.Minute)
		}},
		{"zero duration", func(r *Recording) error {
			return r.CompleteRecovery(later, mustRef(t, "recordings/r1.wav"), 0)
		}},
		{"empty failure", func(r *Recording) error { return r.FailRecovery(later, " ") }},
		{"empty deferral", func(r *Recording) error { return r.DeferRecovery(later, "") }},
	}
	for _, tc := range cases {
		for _, s := range []Status{StatusStopping, StatusRecoveryRequired} {
			t.Run(tc.name+"/"+string(s), func(t *testing.T) {
				r := inStatus(t, s)
				before := stateOf(r)
				if err := tc.call(r); !errors.Is(err, ErrInvalidRecording) {
					t.Fatalf("error = %v, want ErrInvalidRecording", err)
				}
				if stateOf(r) != before {
					t.Fatal("a refused outcome changed the recording")
				}
			})
		}
	}
}

// restoreStored restores r as a repository would after storing it.
func restoreStored(r *Recording) (*Recording, error) {
	st := stateOf(r)
	st.Version = 1
	return Restore(st)
}
