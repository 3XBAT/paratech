package values

import (
	"errors"
	"testing"
)

func TestTicketJobPayload(t *testing.T) {
	j, err := NewTicketJob("ts1")
	if err != nil || j.Payload() != `{"set_id":"ts1"}` {
		t.Fatalf("payload = %s, %v", j.Payload(), err)
	}
	got, err := ParseTicketJob(j.Payload())
	if err != nil || got != j || got.Set() != "ts1" {
		t.Fatalf("parse = %+v, %v", got, err)
	}
	for _, bad := range []string{`{}`, `{"set_id":""}`, `{"set_id":"ts1","x":1}`, `{"set_id":"ts1"} {}`, `{"set_id":"a/b"}`, `[]`} {
		if _, err := ParseTicketJob(bad); !errors.Is(err, ErrInvalidTicketJob) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if TicketCheckpoint(3, 8) != "3/8" {
		t.Fatal(TicketCheckpoint(3, 8))
	}
}
