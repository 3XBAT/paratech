package job

import (
	"errors"
	"testing"
)

func TestNewClaimFilterValidation(t *testing.T) {
	cases := []struct {
		name    string
		types   []Type
		heavy   []Type
		invalid bool
	}{
		{"light only", []Type{"summarize"}, nil, false},
		{"with heavy", []Type{"stt", "summarize"}, []Type{"stt"}, false},
		{"no types", nil, nil, true},
		{"empty list", []Type{}, nil, true},
		{"blank type", []Type{""}, nil, true},
		{"space in type", []Type{"a b"}, nil, true},
		{"duplicate type", []Type{"stt", "stt"}, nil, true},
		{"heavy not listed", []Type{"summarize"}, []Type{"stt"}, true},
		{"duplicate heavy", []Type{"stt"}, []Type{"stt", "stt"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewClaimFilter(tc.types, tc.heavy, true)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidClaimFilter) {
					t.Fatalf("error = %v, want ErrInvalidClaimFilter", err)
				}
				if !f.IsZero() {
					t.Fatalf("invalid input produced %+v", f)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.IsZero() {
				t.Fatal("valid filter reports IsZero")
			}
		})
	}
}

func TestClaimFilterAdmits(t *testing.T) {
	types := []Type{"stt", "summarize"}
	heavy := []Type{"stt"}
	cases := []struct {
		name         string
		heavyAllowed bool
		typ          Type
		runNow       bool
		want         bool
	}{
		{"light job on battery", false, "summarize", false, true},
		{"light job on ac", true, "summarize", false, true},
		{"heavy job on ac", true, "stt", false, true},
		{"heavy job on battery", false, "stt", false, false},
		{"heavy run_now job on battery", false, "stt", true, true},
		{"heavy run_now job on ac", true, "stt", true, true},
		{"unregistered type", true, "email", false, false},
		{"unregistered type with run_now", true, "email", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewClaimFilter(types, heavy, tc.heavyAllowed)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Admits(tc.typ, tc.runNow); got != tc.want {
				t.Fatalf("Admits(%q, runNow=%v) = %v, want %v", tc.typ, tc.runNow, got, tc.want)
			}
		})
	}
}

func TestZeroClaimFilterAdmitsNothing(t *testing.T) {
	var f ClaimFilter
	if !f.IsZero() || f.Admits("stt", true) {
		t.Fatal("zero filter must admit nothing")
	}
}

func TestClaimFilterDoesNotShareInput(t *testing.T) {
	types := []Type{"stt"}
	f, err := NewClaimFilter(types, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	types[0] = "other"
	f.Types()[0] = "mutated"
	if !f.Admits("stt", false) || f.Types()[0] != "stt" {
		t.Fatal("ClaimFilter shares its slice with the caller")
	}
}
