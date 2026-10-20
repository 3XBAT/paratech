package deletion

import (
	"errors"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestClass(t *testing.T) {
	cases := []struct {
		name string
		a    Assignment
		want Class
	}{
		{"plain", Assignment{ID: "a1"}, ClassPlain},
		{"draft", Assignment{ID: "a1", HasDraft: true}, ClassDraft},
		{"emails", Assignment{ID: "a1", Deliveries: 1}, ClassEmails},
		{"draft and emails", Assignment{ID: "a1", HasDraft: true, Deliveries: 2}, ClassEmails},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Class(); got != tc.want {
				t.Fatalf("Class = %q, want %q", got, tc.want)
			}
		})
	}
}

var found = []Assignment{{ID: "plain"}, {ID: "draft", HasDraft: true}, {ID: "sent", Deliveries: 1}}

func TestCheckChoice(t *testing.T) {
	cases := []struct {
		name   string
		chosen []shared.AssignmentID
		want   error
	}{
		{"none", nil, nil},
		{"every found one", []shared.AssignmentID{"plain", "draft", "sent"}, nil},
		{"not found", []shared.AssignmentID{"plain", "other"}, ErrUnknownAssignment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckChoice(found, tc.chosen); !errors.Is(err, tc.want) {
				t.Fatalf("CheckChoice = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDeletable(t *testing.T) {
	cases := []struct {
		name   string
		found  []Assignment
		chosen []shared.AssignmentID
		want   []shared.AssignmentID
	}{
		{"none chosen", found, nil, []shared.AssignmentID{}},
		{"plain and draft, in found order", found, []shared.AssignmentID{"draft", "plain"}, []shared.AssignmentID{"plain", "draft"}},
		{"email history is kept", found, []shared.AssignmentID{"sent"}, []shared.AssignmentID{}},
		{"gone meanwhile", found[:1], []shared.AssignmentID{"plain", "draft"}, []shared.AssignmentID{"plain"}},
		{"emails sent meanwhile", []Assignment{{ID: "plain", Deliveries: 1}}, []shared.AssignmentID{"plain"}, []shared.AssignmentID{}},
		{"duplicate row", []Assignment{{ID: "plain"}, {ID: "plain"}}, []shared.AssignmentID{"plain"}, []shared.AssignmentID{"plain"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Deletable(tc.found, tc.chosen); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Deletable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKept(t *testing.T) {
	cases := []struct {
		name    string
		found   []Assignment
		deleted []shared.AssignmentID
		want    []shared.AssignmentID
	}{
		{"none deleted", found, nil, []shared.AssignmentID{"plain", "draft", "sent"}},
		{"some deleted, in found order", found, []shared.AssignmentID{"draft"}, []shared.AssignmentID{"plain", "sent"}},
		{"all deleted", found, []shared.AssignmentID{"plain", "draft", "sent"}, []shared.AssignmentID{}},
		{"duplicate row", []Assignment{{ID: "plain"}, {ID: "plain"}}, nil, []shared.AssignmentID{"plain"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Kept(tc.found, tc.deleted); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Kept = %v, want %v", got, tc.want)
			}
		})
	}
}
