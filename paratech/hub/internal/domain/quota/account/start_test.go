package account

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/shared"
)

var (
	hashA = strings.Repeat("a", 64)
	hashB = strings.Repeat("b", 64)
)

func gatewayRequest(t *testing.T, key string, amount int64, hash string) ReserveRequest {
	t.Helper()
	r, err := request(t, key, amount).WithRequestHash(hash)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func result(t *testing.T, output string, in, out int64) Result {
	t.Helper()
	r, err := NewResult(output, in, out)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func start(t *testing.T, a *Account, key string, amount int64, hash string, id shared.ReservationID, now time.Time) (ReservationRecord, Result, error) {
	t.Helper()
	return a.Start(gatewayRequest(t, key, amount, hash), id, now)
}

func TestWithRequestHash(t *testing.T) {
	r := gatewayRequest(t, "k", 10, hashA)
	if r.Fingerprint().RequestHash != RequestHash(hashA) {
		t.Fatalf("fingerprint = %+v", r.Fingerprint())
	}
	for _, bad := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 65)} {
		if _, err := request(t, "k", 10).WithRequestHash(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("WithRequestHash(%q) error = %v, want ErrInvalidArgument", bad, err)
		}
	}
}

func TestNewResult(t *testing.T) {
	r := result(t, "untrusted-output", 10, 5)
	if !r.Stored || r.Total() != 15 {
		t.Fatalf("result = %v", r)
	}
	for _, c := range [][2]int64{{-1, 0}, {0, -1}, {math.MaxInt64, 1}} {
		if _, err := NewResult("", c[0], c[1]); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("NewResult(%d, %d) error = %v", c[0], c[1], err)
		}
	}
	if s := r.String() + r.GoString(); strings.Contains(s, "untrusted-output") {
		t.Fatalf("printed result leaks the output: %s", s)
	}
}

// TestStartLifeCycle walks the gateway key life cycle: new attempt, running, completed with replay, conflict.
func TestStartLifeCycle(t *testing.T) {
	a := newAccount(t, 1000)
	rec, res, err := start(t, a, "k", 300, hashA, rid(1), t0)
	if err != nil || res.Stored || rec.ID != rid(1) || rec.State != StateReserved {
		t.Fatalf("first Start = %+v, %v, %v", rec, res, err)
	}

	a = reload(t, a, t0, "k", "")
	if _, _, err := start(t, a, "k", 300, hashA, rid(2), t0.Add(time.Minute)); !errors.Is(err, ErrAttemptRunning) {
		t.Fatalf("Start while running: err = %v, want ErrAttemptRunning", err)
	}
	if _, _, err := start(t, a, "k", 300, hashB, rid(2), t0.Add(time.Minute)); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("Start with another request while running: err = %v, want ErrIdempotencyConflict", err)
	}

	a = reload(t, a, t0, "k", rid(1))
	settled, err := a.Complete(rid(1), usage(t, 120, false), result(t, "answer", 100, 20), t0.Add(2*time.Minute))
	if err != nil || settled.State != StateSettled || settled.Charged != 120 {
		t.Fatalf("Complete = %+v, %v", settled, err)
	}
	if a.Used() != 120 || a.Held() != 0 {
		t.Fatalf("used %d held %d", a.Used(), a.Held())
	}
	if k := a.Changes().Keys; len(k) != 1 || !k[0].Result.Stored || k[0].Result.Output != "answer" {
		t.Fatalf("key writes = %+v", k)
	}

	a = reload(t, a, t0, "k", "")
	rec, res, err = start(t, a, "k", 300, hashA, rid(3), t0.Add(3*time.Minute))
	if err != nil || !res.Stored || res.Output != "answer" || res.Total() != 120 || rec.ID != rid(1) {
		t.Fatalf("replay = %+v, %v, %v", rec, res, err)
	}
	if ch := a.Changes(); len(ch.Created) != 0 || len(ch.Movements) != 0 || len(ch.Keys) != 0 {
		t.Fatalf("a replay changed something: %+v", ch)
	}
	// The conflict wins over the stored result.
	if _, res, err := start(t, a, "k", 300, hashB, rid(3), t0.Add(3*time.Minute)); !errors.Is(err, ErrIdempotencyConflict) || res.Stored {
		t.Fatalf("Start with another request after completion: %v, %v", res, err)
	}
	if _, _, err := start(t, a, "k", 301, hashA, rid(3), t0.Add(3*time.Minute)); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("Start with another amount: err = %v", err)
	}
}

func TestStartNeedsARequestHash(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := a.Start(request(t, "k", 10), rid(1), t0); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
	// A key opened by a plain Reserve has no hash: the gateway request conflicts with it.
	reserve(t, a, "plain", 10, rid(1), t0)
	if _, _, err := start(t, a, "plain", 10, hashA, rid(2), t0); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestStartAfterAFailedAttemptOpensANewOne(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Release(rid(1), ReasonFailed, t0); err != nil {
		t.Fatal(err)
	}
	rec, res, err := start(t, a, "k", 300, hashA, rid(2), t0.Add(time.Second))
	if err != nil || res.Stored || rec.ID != rid(2) || rec.Attempt != 2 {
		t.Fatalf("retry = %+v, %v, %v", rec, res, err)
	}
	assertTotals(t, a, 0, 300)
}

func TestStartQuotaExceeded(t *testing.T) {
	a := newAccount(t, 100)
	if _, _, err := start(t, a, "k", 101, hashA, rid(1), t0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	if ch := a.Changes(); len(ch.Created) != 0 || len(ch.Keys) != 0 {
		t.Fatalf("QUOTA_EXCEEDED recorded something: %+v", ch)
	}
}

func TestStartReplaysNothingAfterRetention(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(rid(1), usage(t, 50, false), result(t, "answer", 40, 10), t0); err != nil {
		t.Fatal(err)
	}
	late := t0.Add(retention)
	a = reload(t, a, late, "k", "")
	rec, res, err := start(t, a, "k", 300, hashB, rid(2), late) // a new key: another request is no conflict
	if err != nil || res.Stored || rec.ID != rid(2) {
		t.Fatalf("Start after retention = %+v, %v, %v", rec, res, err)
	}
	if k := a.Changes().Keys; len(k) != 1 || k[0].Result.Stored || !k[0].CreatedAt.Equal(late) {
		t.Fatalf("renewed key = %+v", k)
	}
	if !a.policy.ResultCutoff(late).Equal(t0) {
		t.Fatalf("ResultCutoff = %s", a.policy.ResultCutoff(late))
	}
}

func TestStartWithoutStoredResult(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Settle(rid(1), usage(t, 50, false), t0); err != nil { // settled outside the gateway
		t.Fatal(err)
	}
	if _, _, err := start(t, a, "k", 300, hashA, rid(2), t0); !errors.Is(err, ErrResultMissing) {
		t.Fatalf("err = %v, want ErrResultMissing", err)
	}
}

func TestCompleteRejects(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		u   Usage
		res Result
	}{
		"partial":        {u: usage(t, 15, true), res: result(t, "x", 10, 5)},
		"no result":      {u: usage(t, 0, false), res: Result{}},
		"usage mismatch": {u: usage(t, 16, false), res: result(t, "x", 10, 5)},
		"bad result":     {u: usage(t, 0, false), res: Result{Stored: true, InputTokens: -1, OutputTokens: 1}},
	} {
		if _, err := a.Complete(rid(1), c.u, c.res, t0); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := a.Complete(rid(9), usage(t, 15, false), result(t, "x", 10, 5), t0); !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("unknown reservation: err = %v", err)
	}

	// Without the key record loaded the gateway could store a result for the wrong attempt.
	b := reload(t, a, t0, "", rid(1))
	if _, err := b.Complete(rid(1), usage(t, 15, false), result(t, "x", 10, 5), t0); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("without the key record: err = %v, want ErrInvalidArgument", err)
	}

	if _, err := a.Release(rid(1), ReasonFailed, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(rid(1), usage(t, 15, false), result(t, "x", 10, 5), t0); !errors.Is(err, ErrReleased) {
		t.Fatalf("after release: err = %v, want ErrReleased", err)
	}
}

func TestCompleteIsIdempotent(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(rid(1), usage(t, 15, false), result(t, "first", 10, 5), t0); err != nil {
		t.Fatal(err)
	}
	a = reload(t, a, t0, "k", rid(1))
	rec, err := a.Complete(rid(1), usage(t, 15, false), result(t, "second", 10, 5), t0)
	if err != nil || rec.State != StateSettled {
		t.Fatalf("repeat = %+v, %v", rec, err)
	}
	if ch := a.Changes(); len(ch.Keys) != 0 || len(ch.Movements) != 0 {
		t.Fatalf("a repeat changed something: %+v", ch)
	}
	if _, err := a.Complete(rid(1), usage(t, 16, false), result(t, "x", 10, 6), t0); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("repeat with other usage: err = %v", err)
	}
}

// TestCompleteOfALateAttemptDiscardsTheResult: attempt 1 expires, attempt 2 owns the key, then attempt 1 finishes.
func TestCompleteOfALateAttemptDiscardsTheResult(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	afterTTL := t0.Add(ttl)
	if rec, _, err := start(t, a, "k", 300, hashA, rid(2), afterTTL); err != nil || rec.ID != rid(2) {
		t.Fatalf("second attempt = %+v, %v", rec, err)
	}
	rec, err := a.Complete(rid(1), usage(t, 40, false), result(t, "late", 30, 10), afterTTL.Add(time.Second))
	if err != nil || rec.State != StateSettled || rec.Charged != 40 {
		t.Fatalf("late Complete = %+v, %v", rec, err)
	}
	if k := a.keys["k"]; k.Result.Stored || k.Latest != rid(2) {
		t.Fatalf("the late result was stored: %+v", k)
	}
	assertTotals(t, a, 40, 300)
}

// TestCompleteAfterExpiryOfTheLatestAttemptStoresTheResult: no newer attempt, so the late answer is the key's result.
func TestCompleteAfterExpiryOfTheLatestAttemptStoresTheResult(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(rid(1), usage(t, 40, false), result(t, "slow", 30, 10), t0.Add(ttl+time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, res, err := start(t, a, "k", 300, hashA, rid(2), t0.Add(ttl+2*time.Second)); err != nil || res.Output != "slow" {
		t.Fatalf("replay = %v, %v", res, err)
	}
}

func TestCompleteAfterRetentionStoresNoResult(t *testing.T) {
	a := newAccount(t, 1000)
	if _, _, err := start(t, a, "k", 300, hashA, rid(1), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(rid(1), usage(t, 40, false), result(t, "x", 30, 10), t0.Add(retention)); err != nil {
		t.Fatal(err)
	}
	if a.keys["k"].Result.Stored {
		t.Fatal("a result was stored past the retention")
	}
}

func TestRestoreRejectsBadResults(t *testing.T) {
	p := policy(t, 1000)
	settled := validRecord()
	settled.State, settled.Charged, settled.Actual, settled.ClosedAt = StateSettled, 15, 15, t0
	key := KeyRecord{
		Key: "k-1", Fingerprint: Fingerprint{Operation: OperationSummary, Alias: "fast", Amount: 100, RequestHash: RequestHash(hashA)},
		CreatedAt: t0, Attempts: 1, Latest: rid(1), Result: Result{Stored: true, Output: "x", InputTokens: 10, OutputTokens: 5},
	}
	if _, err := Restore(owner, p, 15, 0, []ReservationRecord{settled}, []KeyRecord{key}); err != nil {
		t.Fatalf("valid account: %v", err)
	}

	partial := settled
	partial.Partial = true
	reserved := validRecord()
	cases := map[string]struct {
		rec  ReservationRecord
		held int64
		key  func(*KeyRecord)
	}{
		"bad hash":          {rec: settled, key: func(k *KeyRecord) { k.Fingerprint.RequestHash = "zz" }},
		"result no hash":    {rec: settled, key: func(k *KeyRecord) { k.Fingerprint.RequestHash = "" }},
		"usage mismatch":    {rec: settled, key: func(k *KeyRecord) { k.Result.OutputTokens = 6 }},
		"negative tokens":   {rec: settled, key: func(k *KeyRecord) { k.Result.InputTokens, k.Result.OutputTokens = -1, 16 }},
		"content not saved": {rec: settled, key: func(k *KeyRecord) { k.Result = Result{Output: "x"} }},
		"partial attempt":   {rec: partial, key: func(*KeyRecord) {}},
		"running attempt":   {rec: reserved, held: 100, key: func(*KeyRecord) {}},
	}
	for name, c := range cases {
		k := key
		c.key(&k)
		if _, err := Restore(owner, p, 15, c.held, []ReservationRecord{c.rec}, []KeyRecord{k}); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("%s: err = %v, want ErrInvalidRecord", name, err)
		}
	}
}
