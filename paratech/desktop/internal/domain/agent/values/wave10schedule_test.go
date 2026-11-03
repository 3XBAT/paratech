package values

import (
	"errors"
	"testing"
)

func TestScheduleSubjectsResult(t *testing.T) {
	tests := []struct {
		name   string
		groups int
		want   string
		ok     bool
	}{
		{"some groups", 3, `{"groups":3,"proposed":true,"waits_for":"student"}`, true},
		{"the limit", MaxScheduleGroups, `{"groups":200,"proposed":true,"waits_for":"student"}`, true},
		{"count not known", 0, `{"proposed":true,"waits_for":"student"}`, true},
		{"negative", -1, "", false},
		{"over the limit", MaxScheduleGroups + 1, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := ScheduleSubjectsResult("c1", tt.groups)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidResult) {
					t.Fatalf("err = %v, want ErrInvalidResult", err)
				}
				return
			}
			if err != nil || r.ResultJSON != tt.want || r.IsError || r.CallID != "c1" {
				t.Fatalf("got %+v, %v; want %s", r, err, tt.want)
			}
		})
	}
}

func TestNothingToCreateResult(t *testing.T) {
	r := NothingToCreateResult("c1")
	if err := r.Validate(); err != nil || r.IsError || r.ResultJSON != `{"nothing_to_create":true}` {
		t.Fatalf("got %+v, %v", r, err)
	}
}
