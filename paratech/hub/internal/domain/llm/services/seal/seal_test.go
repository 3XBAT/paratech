package seal

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/hub/internal/domain/llm/values"
)

var (
	secretA = bytes.Repeat([]byte{0xa1}, MinSecretBytes)
	secretB = bytes.Repeat([]byte{0xb2}, MinSecretBytes+8)
)

func binding() Binding {
	return Binding{Owner: "00000000000000a1", ThreadID: "th-1", Profile: values.ProfilePanel, CatalogVersion: 1}
}

func calls(t *testing.T, args ...string) []values.ToolCall {
	t.Helper()
	var out []values.ToolCall
	for i, a := range args {
		c, err := values.NewToolCall(fmt.Sprint("toolu_", i), "reply", a)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func sealer(t *testing.T, current Key, older ...Key) *Sealer {
	t.Helper()
	s, err := New(current, older...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSealRoundTrip(t *testing.T) {
	s := sealer(t, Key{Version: 1, Secret: secretA})
	cs := calls(t, `{"text":"a"}`, `{"text":"b"}`)
	seal := s.Seal(binding(), cs)
	if len(seal) != Size || seal[0] != 1 {
		t.Fatalf("seal = %x, want %d bytes with version 1", seal, Size)
	}
	if err := s.Verify(binding(), cs, seal); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.Seal(binding(), cs), seal) {
		t.Fatal("a seal must be deterministic, so a replay returns the same one")
	}
}

// TestForgedSeals: any change of the calls, their order or the binding, a truncated or altered seal, or another
// secret is refused.
func TestForgedSeals(t *testing.T) {
	s := sealer(t, Key{Version: 1, Secret: secretA})
	cs := calls(t, `{"text":"a"}`, `{"text":"b"}`)
	seal := s.Seal(binding(), cs)

	other := func(mutate func(*Binding)) Binding {
		b := binding()
		mutate(&b)
		return b
	}
	flipped := bytes.Clone(seal)
	flipped[Size-1] ^= 1
	foreign := sealer(t, Key{Version: 1, Secret: secretB}).Seal(binding(), cs)
	changed := calls(t, `{"text":"a"}`, `{"text":"c"}`)
	renamed, _ := values.NewToolCall("toolu_0", "show_plan", `{"text":"a"}`)

	for name, c := range map[string]struct {
		b     Binding
		calls []values.ToolCall
		seal  []byte
	}{
		"other key id":      {other(func(b *Binding) { b.Owner = "00000000000000b2" }), cs, seal},
		"other thread":      {other(func(b *Binding) { b.ThreadID = "th-2" }), cs, seal},
		"other profile":     {other(func(b *Binding) { b.Profile = "other" }), cs, seal},
		"other catalog":     {other(func(b *Binding) { b.CatalogVersion = 2 }), cs, seal},
		"changed arguments": {binding(), changed, seal},
		"other tool":        {binding(), []values.ToolCall{renamed, cs[1]}, seal},
		"reordered":         {binding(), []values.ToolCall{cs[1], cs[0]}, seal},
		"dropped call":      {binding(), cs[:1], seal},
		"flipped bit":       {binding(), cs, flipped},
		"truncated":         {binding(), cs, seal[:Size-1]},
		"extended":          {binding(), cs, append(bytes.Clone(seal), 0)},
		"empty":             {binding(), cs, nil},
		"other secret":      {binding(), cs, foreign},
		"unknown version":   {binding(), cs, append([]byte{7}, seal[1:]...)},
	} {
		if err := s.Verify(c.b, c.calls, c.seal); !errors.Is(err, ErrBadSeal) {
			t.Errorf("%s: err = %v, want ErrBadSeal", name, err)
		}
	}
}

// TestFieldBoundaries: the length prefixes keep a byte moved between fields from producing the same seal.
func TestFieldBoundaries(t *testing.T) {
	s := sealer(t, Key{Version: 1, Secret: secretA})
	b1 := Binding{Owner: "00000000000000a1", ThreadID: "ab", Profile: "c", CatalogVersion: 1}
	b2 := Binding{Owner: "00000000000000a1", ThreadID: "a", Profile: "bc", CatalogVersion: 1}
	if bytes.Equal(s.Seal(b1, nil), s.Seal(b2, nil)) {
		t.Fatal("moving a byte between fields kept the seal")
	}
}

// TestRotation: seals of an older configured key still verify; new seals use the current version; a dropped key's
// seals are refused.
func TestRotation(t *testing.T) {
	cs := calls(t, `{}`)
	old := sealer(t, Key{Version: 1, Secret: secretA})
	oldSeal := old.Seal(binding(), cs)

	rotated := sealer(t, Key{Version: 2, Secret: secretB}, Key{Version: 1, Secret: secretA})
	if err := rotated.Verify(binding(), cs, oldSeal); err != nil {
		t.Fatalf("old seal after rotation: %v", err)
	}
	newSeal := rotated.Seal(binding(), cs)
	if newSeal[0] != 2 || bytes.Equal(newSeal[1:], oldSeal[1:]) {
		t.Fatalf("new seal %x does not use the current key", newSeal)
	}
	if err := old.Verify(binding(), cs, newSeal); !errors.Is(err, ErrBadSeal) {
		t.Fatalf("a seal of an unknown version: err = %v", err)
	}
	dropped := sealer(t, Key{Version: 2, Secret: secretB})
	if err := dropped.Verify(binding(), cs, oldSeal); !errors.Is(err, ErrBadSeal) {
		t.Fatalf("a dropped key: err = %v", err)
	}
	// The version byte is bound to its secret: relabelling an old seal as version 2 fails.
	if err := rotated.Verify(binding(), cs, append([]byte{2}, oldSeal[1:]...)); !errors.Is(err, ErrBadSeal) {
		t.Fatalf("relabelled seal: err = %v", err)
	}
}

func TestNewValidatesKeys(t *testing.T) {
	for name, c := range map[string]struct {
		current Key
		older   []Key
	}{
		"version 0":     {Key{Version: 0, Secret: secretA}, nil},
		"short secret":  {Key{Version: 1, Secret: secretA[:MinSecretBytes-1]}, nil},
		"no secret":     {Key{Version: 1}, nil},
		"duplicate":     {Key{Version: 1, Secret: secretA}, []Key{{Version: 1, Secret: secretB}}},
		"short older":   {Key{Version: 1, Secret: secretA}, []Key{{Version: 2, Secret: []byte("short")}}},
		"older version": {Key{Version: 1, Secret: secretA}, []Key{{Version: 0, Secret: secretB}}},
	} {
		if _, err := New(c.current, c.older...); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: err = %v, want ErrInvalidKey", name, err)
		}
	}
}

func TestSecretsAreCopiedAndNeverPrinted(t *testing.T) {
	secret := bytes.Clone(secretA)
	s := sealer(t, Key{Version: 1, Secret: secret})
	cs := calls(t, `{}`)
	seal := s.Seal(binding(), cs)
	secret[0] ^= 0xff
	if err := s.Verify(binding(), cs, seal); err != nil {
		t.Fatalf("the sealer shares the caller's secret: %v", err)
	}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%v", *s)} {
		if strings.Contains(out, "a1a1") || strings.Contains(out, string(secretA)) || strings.Contains(out, "161") {
			t.Errorf("printed sealer leaks the secret: %s", out)
		}
	}
}
