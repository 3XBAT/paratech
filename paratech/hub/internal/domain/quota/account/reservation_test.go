package account

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var allPhases = []phase{phaseReserved, phaseSettled, phaseReleasedExplicit, phaseReleasedFailed, phaseReleasedExpired, phaseInvalid}

// TestTransitionTable covers every allowed and every forbidden transition (ARCH-62).
func TestTransitionTable(t *testing.T) {
	allowed := map[[2]phase]bool{
		{phaseReserved, phaseSettled}:          true,
		{phaseReserved, phaseReleasedExplicit}: true,
		{phaseReserved, phaseReleasedFailed}:   true,
		{phaseReserved, phaseReleasedExpired}:  true,
		{phaseReleasedExpired, phaseSettled}:   true,
	}
	for _, from := range allPhases {
		for _, to := range allPhases {
			want := allowed[[2]phase{from, to}]
			if got := canMove(from, to); got != want {
				t.Errorf("canMove(%q, %q) = %v, want %v", from, to, got, want)
			}

			if from == phaseInvalid {
				continue
			}
			state, reason := from.parts()
			r := &reservation{rec: ReservationRecord{State: state, Reason: reason}}
			err := r.moveTo(to, t0)
			switch {
			case want && err != nil:
				t.Errorf("moveTo %q -> %q: %v", from, to, err)
			case want && r.phase() != to:
				t.Errorf("moveTo %q -> %q left phase %q", from, to, r.phase())
			case !want && !errors.Is(err, ErrInvalidTransition):
				t.Errorf("moveTo %q -> %q = %v, want ErrInvalidTransition", from, to, err)
			case !want && r.phase() != from:
				t.Errorf("a refused moveTo %q -> %q changed the phase to %q", from, to, r.phase())
			}
		}
	}
}

func TestPhaseOf(t *testing.T) {
	for _, p := range allPhases[:5] {
		if s, r := p.parts(); phaseOf(s, r) != p {
			t.Errorf("phaseOf(parts(%q)) = %q", p, phaseOf(s, r))
		}
	}
	for _, bad := range [][2]string{{"reserved", "explicit"}, {"settled", "expired"}, {"released", ""}, {"released", "other"}, {"lost", ""}} {
		if got := phaseOf(State(bad[0]), ReleaseReason(bad[1])); got != phaseInvalid {
			t.Errorf("phaseOf(%v) = %q, want invalid", bad, got)
		}
	}
	if s, r := phaseInvalid.parts(); s != "" || r != ReasonNone {
		t.Errorf("parts(invalid) = %q %q", s, r)
	}
}

func TestNewReservationID(t *testing.T) {
	b := make([]byte, ReservationIDBytes)
	b[3] = 0xab
	id, err := NewReservationID(b)
	if err != nil || id.Validate() != nil || id != "000000ab000000000000000000000000" {
		t.Fatalf("NewReservationID = %q, %v", id, err)
	}
	for name, in := range map[string][]byte{"short": b[:15], "long": append(b, 1), "zero": make([]byte, ReservationIDBytes), "nil": nil} {
		if _, err := NewReservationID(in); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
}

func validRecord() ReservationRecord {
	return ReservationRecord{
		ID: rid(1), Key: "k-1", Attempt: 1, Amount: 100, State: StateReserved,
		CreatedAt: t0, ExpiresAt: t0.Add(ttl),
	}
}

func TestRestoreReservation(t *testing.T) {
	settled := validRecord()
	settled.State, settled.Charged, settled.Actual, settled.Partial, settled.ClosedAt = StateSettled, 80, 90, true, t0.Add(time.Minute)
	released := validRecord()
	released.State, released.Reason, released.ClosedAt = StateReleased, ReasonExpired, t0.Add(ttl)
	for name, rec := range map[string]ReservationRecord{"reserved": validRecord(), "settled": settled, "released": released} {
		if _, err := restoreReservation(rec); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	bad := map[string]func(*ReservationRecord){
		"invalid id":             func(r *ReservationRecord) { r.ID = "x" },
		"invalid key":            func(r *ReservationRecord) { r.Key = "has space" },
		"zero attempt":           func(r *ReservationRecord) { r.Attempt = 0 },
		"zero amount":            func(r *ReservationRecord) { r.Amount = 0 },
		"negative charged":       func(r *ReservationRecord) { r.Charged = -1 },
		"charged above actual":   func(r *ReservationRecord) { r.State, r.Charged, r.Actual, r.ClosedAt = StateSettled, 5, 4, t0 },
		"no creation":            func(r *ReservationRecord) { r.CreatedAt = time.Time{} },
		"expiry before creation": func(r *ReservationRecord) { r.ExpiresAt = r.CreatedAt },
		"reserved with charge":   func(r *ReservationRecord) { r.Charged, r.Actual = 1, 1 },
		"reserved partial":       func(r *ReservationRecord) { r.Partial = true },
		"reserved closed":        func(r *ReservationRecord) { r.ClosedAt = t0 },
		"settled not closed":     func(r *ReservationRecord) { r.State = StateSettled },
		"released with charge": func(r *ReservationRecord) {
			r.State, r.Reason, r.Actual, r.Charged, r.ClosedAt = StateReleased, ReasonExplicit, 1, 1, t0
		},
		"released not closed":     func(r *ReservationRecord) { r.State, r.Reason = StateReleased, ReasonFailed },
		"released without reason": func(r *ReservationRecord) { r.State, r.ClosedAt = StateReleased, t0 },
		"unknown state":           func(r *ReservationRecord) { r.State = "lost" },
	}
	for name, mutate := range bad {
		rec := validRecord()
		mutate(&rec)
		if _, err := restoreReservation(rec); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("%s: err = %v, want ErrInvalidRecord", name, err)
		}
	}
}

func TestParseValues(t *testing.T) {
	okKeys := []string{"a", "job-1:summary", strings.Repeat("~", maxIdempotencyKeyLen), "!#$%&'()*+,-./"}
	for _, k := range okKeys {
		if _, err := ParseIdempotencyKey(k); err != nil {
			t.Errorf("key %q: %v", k, err)
		}
	}
	for _, k := range []string{"", "a b", "tab\t", "café", strings.Repeat("a", maxIdempotencyKeyLen+1), "nul\x00", "del\x7f"} {
		if _, err := ParseIdempotencyKey(k); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("key %q: err = %v, want ErrInvalidArgument", k, err)
		}
	}

	for _, a := range []string{"a", "0", "claude-sonnet_4", strings.Repeat("z", maxModelAliasLen)} {
		if _, err := ParseModelAlias(a); err != nil {
			t.Errorf("alias %q: %v", a, err)
		}
	}
	for _, a := range []string{"", "-a", "_a", "A", "a.b", "a/b", strings.Repeat("z", maxModelAliasLen+1)} {
		if _, err := ParseModelAlias(a); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("alias %q: err = %v, want ErrInvalidArgument", a, err)
		}
	}

	for _, op := range []Operation{OperationSummary, OperationExtractHomework, OperationHomeworkDraft, OperationLectureText} {
		if got, err := ParseOperation(string(op)); err != nil || got != op {
			t.Errorf("operation %q: %q, %v", op, got, err)
		}
	}
	for _, op := range []string{"", "unspecified", "SUMMARY", "translate"} {
		if _, err := ParseOperation(op); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("operation %q: err = %v, want ErrInvalidArgument", op, err)
		}
	}

	if u, err := ParseUnit("tokens"); err != nil || u != UnitTokens {
		t.Errorf("ParseUnit(tokens) = %q, %v", u, err)
	}
	for _, u := range []string{"", "TOKENS", "credits", "seconds"} {
		if _, err := ParseUnit(u); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("unit %q: err = %v, want ErrInvalidArgument", u, err)
		}
	}
}

func TestNewReserveRequest(t *testing.T) {
	r, err := NewReserveRequest("k", "summary", "fast", "tokens", 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Key() != "k" || r.Fingerprint() != (Fingerprint{Operation: OperationSummary, Alias: "fast", Amount: 10}) {
		t.Fatalf("request = %+v", r)
	}
	for name, args := range map[string]struct {
		key, op, alias, unit string
		amount               int64
	}{
		"key":         {"", "summary", "fast", "tokens", 1},
		"operation":   {"k", "", "fast", "tokens", 1},
		"alias":       {"k", "summary", "Fast", "tokens", 1},
		"unit":        {"k", "summary", "fast", "credits", 1},
		"zero amount": {"k", "summary", "fast", "tokens", 0},
		"negative":    {"k", "summary", "fast", "tokens", -5},
	} {
		if _, err := NewReserveRequest(args.key, args.op, args.alias, args.unit, args.amount); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
}

func TestNewUsage(t *testing.T) {
	u, err := NewUsage("tokens", 0, true)
	if err != nil || u.Actual() != 0 || !u.Partial() {
		t.Fatalf("NewUsage = %+v, %v", u, err)
	}
	if _, err := NewUsage("tokens", -1, false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative: err = %v", err)
	}
	if _, err := NewUsage("", 1, false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("no unit: err = %v", err)
	}
}

func TestNewPolicy(t *testing.T) {
	p, err := NewPolicy("tokens", 1000, ttl, retention)
	if err != nil || p.Unit() != UnitTokens || p.Limit() != 1000 || p.ReservationTTL() != ttl || p.KeyRetention() != retention {
		t.Fatalf("NewPolicy = %+v, %v", p, err)
	}
	if _, err := NewPolicy("tokens", 1, time.Hour, time.Hour); err != nil {
		t.Errorf("retention equal to the TTL: %v", err)
	}
	for name, args := range map[string]struct {
		unit           string
		limit          int64
		ttl, retention time.Duration
	}{
		"unit":                 {"credits", 1, ttl, retention},
		"zero limit":           {"tokens", 0, ttl, retention},
		"negative limit":       {"tokens", -1, ttl, retention},
		"zero ttl":             {"tokens", 1, 0, retention},
		"retention below ttl":  {"tokens", 1, time.Hour, time.Hour - time.Second},
		"retention at purge":   {"tokens", 1, time.Minute, ResultPurgeInterval},
		"zero retention":       {"tokens", 1, ttl, 0},
		"negative durations":   {"tokens", 1, -ttl, -retention},
		"retention unset only": {"tokens", 1, ttl, -1},
	} {
		if _, err := NewPolicy(args.unit, args.limit, args.ttl, args.retention); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("%s: err = %v, want ErrInvalidPolicy", name, err)
		}
	}
}
