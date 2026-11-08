package retranscribe

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

func at(id string, minutes int, s State) Job {
	return Job{ID: shared.JobID(id), Created: t0.Add(time.Duration(minutes) * time.Minute), State: s}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		jobs       []Job
		transcript bool
		key        string // "" = not allowed
	}{
		{"no job: the automatic enqueue makes the first", nil, false, ""},
		{"cancelled once", []Job{at("a", 0, StateCancelled)}, false, "stt:rec-1:m-1:r2"},
		{"cancelled again after a retry", []Job{at("a", 0, StateCancelled), at("b", 5, StateCancelled)}, false, "stt:rec-1:m-1:r3"},
		{"the order of the list does not matter", []Job{at("b", 5, StateCancelled), at("a", 0, StateFinished)}, false, "stt:rec-1:m-1:r3"},
		{"a transcript exists", []Job{at("a", 0, StateCancelled)}, true, ""},
		{"the newest is unfinished", []Job{at("a", 0, StateCancelled), at("b", 5, StateUnfinished)}, false, ""},
		{"an older one is unfinished", []Job{at("a", 0, StateUnfinished), at("b", 5, StateCancelled)}, false, ""},
		{"the newest failed", []Job{at("a", 0, StateCancelled), at("b", 5, StateFinished)}, false, ""},
		{"the only one completed", []Job{at("a", 0, StateFinished)}, false, ""},
		{"the only one runs", []Job{at("a", 0, StateUnfinished)}, false, ""},
		{"a tie goes to the greater id", []Job{at("b", 0, StateCancelled), at("a", 0, StateFinished)}, false, "stt:rec-1:m-1:r3"},
		{"a tie the other way", []Job{at("a", 0, StateCancelled), at("b", 0, StateFinished)}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := New().Decide("rec-1", tt.jobs, tt.transcript)
			if tt.key == "" {
				if !errors.Is(err, ErrNotAllowed) {
					t.Fatalf("Decide = %v, want ErrNotAllowed", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got := d.Key("m-1"); got != tt.key {
				t.Fatalf("Key = %q, want %q", got, tt.key)
			}
		})
	}
}

// The same jobs give the same key, so a double click makes one job.
func TestDecideIsDeterministic(t *testing.T) {
	jobs := []Job{at("a", 0, StateCancelled)}
	first, err := New().Decide("rec-1", jobs, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := New().Decide("rec-1", jobs, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key("m-1") != again.Key("m-1") {
		t.Fatalf("keys differ: %q, %q", first.Key("m-1"), again.Key("m-1"))
	}
}

func TestUnfinished(t *testing.T) {
	tests := []struct {
		name string
		jobs []Job
		want shared.JobID // "" = none
	}{
		{"none", nil, ""},
		{"only finished", []Job{at("a", 0, StateFinished), at("b", 1, StateCancelled)}, ""},
		{"the unfinished one", []Job{at("a", 0, StateUnfinished), at("b", 1, StateCancelled)}, "a"},
		{"the newest unfinished", []Job{at("a", 0, StateUnfinished), at("b", 1, StateUnfinished)}, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, ok := Unfinished(tt.jobs)
			if ok != (tt.want != "") || j.ID != tt.want {
				t.Fatalf("Unfinished = %q, %v; want %q", j.ID, ok, tt.want)
			}
		})
	}
}
