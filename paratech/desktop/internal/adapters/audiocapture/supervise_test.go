package audiocapture

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func TestRestartLead(t *testing.T) {
	tests := []struct {
		name   string
		missed time.Duration
		lead   time.Duration
		capped bool
	}{
		{"nothing missed", 0, 0, false},
		{"more closed than elapsed", -time.Second, 0, false},
		{"whole milliseconds", 1500*time.Millisecond + 400*time.Microsecond, 1500 * time.Millisecond, false},
		// A lost open chunk of 60 s, a hang of 30 s and the backoff fit.
		{"a hung child with a full open chunk", 60*time.Second + DefaultHangTimeout + 4*time.Second, 94 * time.Second, false},
		{"at the cap", maxLeadSilence, maxLeadSilence, false},
		{"beyond the cap", maxLeadSilence + time.Second, maxLeadSilence, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lead, capped := restartLead(tt.missed)
			if lead != tt.lead || capped != tt.capped {
				t.Fatalf("restartLead(%v) = %v, %v; want %v, %v", tt.missed, lead, capped, tt.lead, tt.capped)
			}
			req := Request{Recording: "rec1", Chunks: mustRef(t, "rec1"), Target: Target{Mode: ModeSystem},
				ChunkSeconds: 60, LeadSilence: lead}
			if err := req.Validate(); err != nil {
				t.Fatalf("the child refuses the lead %v: %v", lead, err)
			}
		})
	}
}

// A recording captured again stays listed once, so its latest capture is kept as long as any other.
func TestRetireListsARecordingOnce(t *testing.T) {
	r := &Recorder{captures: map[shared.RecordingID]*capture{}}
	end := func(id shared.RecordingID) {
		c := newCapture(id, shared.ArtifactRef{}, Target{Mode: ModeSystem}, nil)
		c.end(nil, nil)
		r.captures[id] = c
		r.retire(c)
	}

	end("rec1")
	end("rec1")
	for i := 2; i <= keptCaptures; i++ {
		end(shared.RecordingID(fmt.Sprintf("rec%d", i)))
	}
	if len(r.ended) != keptCaptures || slices.Index(r.ended, "rec1") != 0 {
		t.Fatalf("ended = %v", r.ended)
	}
	if r.captures["rec1"] == nil {
		t.Fatal("the latest capture of rec1 was dropped before the oldest kept one")
	}

	end("rec9")
	if r.captures["rec1"] != nil || len(r.ended) != keptCaptures {
		t.Fatalf("the oldest capture stays: ended = %v", r.ended)
	}
}
