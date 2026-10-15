package notifypolicy

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/jobs/job"
)

const (
	typeStt  job.Type = "stt"
	typeMail job.Type = "send_email"
)

func TestNewValidates(t *testing.T) {
	tests := []struct {
		name  string
		types []job.Type
		ok    bool
	}{
		{"one type", []job.Type{typeStt}, true},
		{"duplicates are harmless", []job.Type{typeStt, typeStt}, true},
		{"no types", nil, false},
		{"blank type", []job.Type{typeStt, ""}, false},
		{"type with a space", []job.Type{"s t"}, false},
		{"type with a control character", []job.Type{"stt\n"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.types)
			if tt.ok != (err == nil) {
				t.Fatalf("New(%q) err = %v, want ok %v", tt.types, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("err = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestNotifies(t *testing.T) {
	p, err := New([]job.Type{typeStt})
	if err != nil {
		t.Fatal(err)
	}
	notifying := map[job.Status]bool{job.StatusCompleted: true, job.StatusFailed: true}
	for _, s := range job.Statuses() {
		for _, typ := range []job.Type{typeStt, typeMail} {
			for _, enabled := range []bool{true, false} {
				want := enabled && typ == typeStt && notifying[s]
				if got := p.Notifies(enabled, typ, s); got != want {
					t.Errorf("Notifies(enabled=%v, %s, %s) = %v, want %v", enabled, typ, s, got, want)
				}
			}
			if got, want := p.Concerns(typ, s), typ == typeStt && notifying[s]; got != want {
				t.Errorf("Concerns(%s, %s) = %v, want %v", typ, s, got, want)
			}
		}
	}
	if (Policy{}).Notifies(true, typeStt, job.StatusCompleted) || (Policy{}).Concerns(typeStt, job.StatusCompleted) {
		t.Error("the zero policy notifies")
	}
}
