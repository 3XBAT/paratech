package apikey

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// credential builds key material from a seed byte. The domain has no randomness (ARCH-15), and tests must not commit
// realistic-looking secrets, so the bytes are a plain repetition of the seed.
func credential(t *testing.T, seed byte) Credential {
	t.Helper()
	c, err := NewCredential(bytes.Repeat([]byte{seed}, IDBytes), bytes.Repeat([]byte{seed ^ 0x5a}, SecretBytes))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func label(t *testing.T, s string) Label {
	t.Helper()
	l, err := NewLabel(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func newKey(t *testing.T, c Credential) *Key {
	t.Helper()
	k, err := New(c, label(t, "tester-01"), t0)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestNewCredential(t *testing.T) {
	c := credential(t, 0x11)
	key := c.Reveal()
	if len(key) != KeyLen || !strings.HasPrefix(key, Prefix+"_"+string(c.ID())+"_") {
		t.Fatalf("Reveal has the wrong shape (len %d)", len(key))
	}
	if err := c.ID().Validate(); err != nil {
		t.Fatalf("id: %v", err)
	}

	for _, tt := range []struct {
		name       string
		id, secret []byte
	}{
		{"short id", make([]byte, IDBytes-1), bytes.Repeat([]byte{1}, SecretBytes)},
		{"short secret", bytes.Repeat([]byte{1}, IDBytes), bytes.Repeat([]byte{1}, SecretBytes-1)},
		{"zero id", make([]byte, IDBytes), bytes.Repeat([]byte{1}, SecretBytes)},
		{"zero secret", bytes.Repeat([]byte{1}, IDBytes), make([]byte, SecretBytes)},
	} {
		if _, err := NewCredential(tt.id, tt.secret); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("%s: err = %v, want ErrInvalidMaterial", tt.name, err)
		}
	}
}

func TestCredentialIsRedacted(t *testing.T) {
	c := credential(t, 0x22)
	secret := c.Reveal()[len(Prefix)+1+idLen+1:]

	var logged bytes.Buffer
	slog.New(slog.NewTextHandler(&logged, nil)).Info("x", "c", c)
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "c", c)
	for _, out := range []string{fmt.Sprint(c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), fmt.Sprintf("%v", &c), logged.String()} {
		if strings.Contains(out, secret) {
			t.Errorf("output leaks the secret: %s", out)
		}
	}
}

func TestParseCredential(t *testing.T) {
	c := credential(t, 0xab) // hex letters in both parts, for the uppercase cases
	key := c.Reveal()

	got, err := ParseCredential(key)
	if err != nil {
		t.Fatalf("ParseCredential(issued key): %v", err)
	}
	if got != c {
		t.Fatal("parsed credential differs from the issued one")
	}

	idStart, secretStart := len(Prefix)+1, len(Prefix)+1+idLen+1
	bad := map[string]string{
		"empty":            "",
		"too short":        key[:len(key)-1],
		"too long":         key + "0",
		"wrong prefix":     "xthub" + key[len(Prefix):],
		"provider-like":    "sk-" + key[3:],
		"no separator":     key[:len(Prefix)] + "-" + key[len(Prefix)+1:],
		"wrong separator":  key[:secretStart-1] + "-" + key[secretStart:],
		"uppercase id":     key[:idStart] + strings.ToUpper(key[idStart:idStart+idLen]) + key[idStart+idLen:],
		"uppercase secret": key[:secretStart] + strings.ToUpper(key[secretStart:]),
		"non-hex secret":   key[:secretStart] + "g" + key[secretStart+1:],
		"space":            key[:secretStart] + " " + key[secretStart+1:],
		"bearer prefix":    "Bearer " + key[7:],
		"non-ascii":        key[:secretStart] + "é" + key[secretStart+2:],
	}
	for name, raw := range bad {
		_, err := ParseCredential(raw)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
		if err != nil && raw != "" && strings.Contains(err.Error(), raw) {
			t.Errorf("%s: the error quotes the input", name)
		}
	}
}

func TestNewLabel(t *testing.T) {
	for _, ok := range []string{"a", "tester-07", "t_1", "0", strings.Repeat("a", MaxLabelLen)} {
		if _, err := NewLabel(ok); err != nil {
			t.Errorf("NewLabel(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", "_a", "Tester", "a b", "anna@example.com", "a.b", "имя", strings.Repeat("a", MaxLabelLen+1)} {
		if _, err := NewLabel(bad); !errors.Is(err, ErrInvalidLabel) {
			t.Errorf("NewLabel(%q) = %v, want ErrInvalidLabel", bad, err)
		}
	}
}

func TestNewInvariants(t *testing.T) {
	c, l := credential(t, 0x44), label(t, "t1")

	k, err := New(c, l, t0)
	if err != nil {
		t.Fatal(err)
	}
	if k.ID() != c.ID() || k.Label() != l || k.Status() != StatusActive || !k.CreatedAt().Equal(t0) || !k.RevokedAt().IsZero() {
		t.Fatalf("unexpected key state: %v %v %v %v %v", k.ID(), k.Label(), k.Status(), k.CreatedAt(), k.RevokedAt())
	}
	want := sha256.Sum256([]byte(c.secret))
	if !bytes.Equal(k.SecretHash(), want[:]) {
		t.Fatal("stored hash is not the SHA-256 of the secret")
	}
	if bytes.Contains(k.SecretHash(), []byte(c.secret)) {
		t.Fatal("the aggregate keeps the secret")
	}

	for name, tt := range map[string]struct {
		c   Credential
		l   Label
		now time.Time
	}{
		"zero credential": {Credential{}, l, t0},
		"id only":         {Credential{id: c.id}, l, t0},
		"zero label":      {c, Label{}, t0},
		"zero time":       {c, l, time.Time{}},
	} {
		if _, err := New(tt.c, tt.l, tt.now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestSecretHashIsACopy(t *testing.T) {
	k := newKey(t, credential(t, 0x45))
	h := k.SecretHash()
	h[0] ^= 0xff
	if bytes.Equal(h, k.SecretHash()) {
		t.Fatal("SecretHash exposes the internal array")
	}
}

func TestRestore(t *testing.T) {
	c := credential(t, 0x55)
	orig := newKey(t, c)
	l := orig.Label()

	active, err := Restore(orig.ID(), l, orig.SecretHash(), t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status() != StatusActive || active.Verify(c) != nil {
		t.Fatal("restored active key does not verify")
	}

	revoked, err := Restore(orig.ID(), l, orig.SecretHash(), t0, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Status() != StatusRevoked || !revoked.RevokedAt().Equal(t0.Add(time.Hour)) {
		t.Fatalf("restored revoked key: %v %v", revoked.Status(), revoked.RevokedAt())
	}

	for name, tt := range map[string]struct {
		id   shared.APIKeyID
		l    Label
		hash []byte
		at   time.Time
	}{
		"bad id":     {"XYZ", l, orig.SecretHash(), t0},
		"zero label": {orig.ID(), Label{}, orig.SecretHash(), t0},
		"short hash": {orig.ID(), l, orig.SecretHash()[:31], t0},
		"no created": {orig.ID(), l, orig.SecretHash(), time.Time{}},
	} {
		if _, err := Restore(tt.id, tt.l, tt.hash, tt.at, time.Time{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestVerify(t *testing.T) {
	c := credential(t, 0x66)
	k := newKey(t, c)

	if err := k.Verify(c); err != nil {
		t.Fatalf("valid key: %v", err)
	}

	// Same id, different secret.
	other := credential(t, 0x67)
	wrong := Credential{id: c.id, secret: other.secret}
	if err := k.Verify(wrong); !errors.Is(err, ErrSecretMismatch) {
		t.Fatalf("wrong secret: %v, want ErrSecretMismatch", err)
	}

	// Right secret presented under another id.
	moved := Credential{id: other.id, secret: c.secret}
	if err := k.Verify(moved); !errors.Is(err, ErrSecretMismatch) {
		t.Fatalf("foreign id: %v, want ErrSecretMismatch", err)
	}

	if err := k.Verify(Credential{}); !errors.Is(err, ErrSecretMismatch) {
		t.Fatalf("zero credential: %v, want ErrSecretMismatch", err)
	}
}

func TestRevoke(t *testing.T) {
	c := credential(t, 0x77)
	k := newKey(t, c)

	at := t0.Add(time.Minute)
	if err := k.Revoke(at); err != nil {
		t.Fatal(err)
	}
	if k.Status() != StatusRevoked || !k.RevokedAt().Equal(at) {
		t.Fatalf("after Revoke: %v %v", k.Status(), k.RevokedAt())
	}
	if err := k.Verify(c); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked key: %v, want ErrRevoked", err)
	}

	// A wrong secret on a revoked key is still a mismatch: the revocation is not revealed without the secret.
	wrong := Credential{id: c.id, secret: credential(t, 0x78).secret}
	if err := k.Verify(wrong); !errors.Is(err, ErrSecretMismatch) {
		t.Fatalf("wrong secret on revoked key: %v, want ErrSecretMismatch", err)
	}

	// Revoke twice is an invalid transition and keeps the first time.
	if err := k.Revoke(at.Add(time.Hour)); !errors.Is(err, ErrAlreadyRevoked) {
		t.Fatalf("second Revoke: %v, want ErrAlreadyRevoked", err)
	}
	if !k.RevokedAt().Equal(at) {
		t.Fatal("second Revoke changed the revocation time")
	}
}

func TestRevokeClockSkewAndZeroTime(t *testing.T) {
	k := newKey(t, credential(t, 0x79))
	if err := k.Revoke(time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero time: %v, want ErrInvalid", err)
	}
	if k.Status() != StatusActive {
		t.Fatal("failed Revoke changed the state")
	}

	if err := k.Revoke(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !k.RevokedAt().Equal(t0) {
		t.Fatalf("revoked at %v, want the creation time %v", k.RevokedAt(), t0)
	}
}

func TestTransitionTable(t *testing.T) {
	for _, tt := range []struct {
		from, to Status
		want     bool
	}{
		{StatusActive, StatusRevoked, true},
		{StatusActive, StatusActive, false},
		{StatusRevoked, StatusActive, false},
		{StatusRevoked, StatusRevoked, false},
	} {
		if got := canMove(tt.from, tt.to); got != tt.want {
			t.Errorf("canMove(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestVerifyMissing(t *testing.T) {
	if err := VerifyMissing(credential(t, 0x7a)); !errors.Is(err, ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
	if err := VerifyMissing(Credential{}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("zero credential: err = %v, want ErrUnknown", err)
	}
}
