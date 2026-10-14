package clock

import (
	"testing"
	"time"
)

func TestSystemNow(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	after := time.Now()

	if got.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", got.Location())
	}
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("Now = %v, want between %v and %v", got, before, after)
	}
}
