package settings

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewProfile(t *testing.T) {
	p, err := NewProfile(ProfileFields{
		FullName: "  Иванов Иван Иванович ", Group: "ИВТ-21", University: "МГУ", Faculty: "ВМК", City: " Москва",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ProfileFields{FullName: "Иванов Иван Иванович", Group: "ИВТ-21", University: "МГУ", Faculty: "ВМК", City: "Москва"}
	if p.Fields() != want || p.IsZero() {
		t.Fatalf("profile = %+v", p.Fields())
	}
	if empty, err := NewProfile(ProfileFields{Group: "   "}); err != nil || !empty.IsZero() {
		t.Fatalf("blank profile = %+v, %v", empty.Fields(), err)
	}

	long := strings.Repeat("я", MaxProfileRunes)
	if _, err := NewProfile(ProfileFields{University: long}); err != nil {
		t.Fatalf("field at the limit: %v", err)
	}
	for name, f := range map[string]ProfileFields{
		"too long":      {University: long + "я"},
		"line break":    {FullName: "Иванов\nИван"},
		"tab":           {Group: "ИВТ\t21"},
		"bidi override": {FullName: "Иванов\u202e"},
		"zero width":    {City: "Мос\u200bква"},
		"line sep":      {Faculty: "ВМК\u2028МГУ"},
		"not UTF-8":     {City: "\xff"},
		"NUL":           {Group: "a\x00"},
	} {
		if _, err := NewProfile(f); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%s: err = %v, want ErrInvalidSettings", name, err)
		}
	}
}

func TestSetProfileAndRestore(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s := New()
	p, err := NewProfile(ProfileFields{FullName: "Иванов И. И.", Group: "ИВТ-21"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProfile(p, now); err != nil || s.Profile() != p || !s.UpdatedAt().Equal(now) {
		t.Fatalf("SetProfile = %v, profile %+v", err, s.Profile().Fields())
	}
	if err := s.SetProfile(p, now.Add(time.Hour)); err != nil || !s.UpdatedAt().Equal(now) {
		t.Fatalf("the same profile again moved UpdatedAt: %v", err)
	}
	if err := s.SetProfile(Profile{}, time.Time{}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("no change time: %v", err)
	}

	r, err := Restore(State{Version: 1, Profile: p.Fields()})
	if err != nil || r.Profile() != p {
		t.Fatalf("Restore = %+v, %v", r.Profile().Fields(), err)
	}
	// A broken stored profile does not block the settings: it is dropped.
	r, err = Restore(State{Version: 1, Profile: ProfileFields{FullName: "x\ny"}})
	if err != nil || !r.Profile().IsZero() {
		t.Fatalf("broken profile: %+v, %v", r.Profile().Fields(), err)
	}
	if New().Profile() != (Profile{}) {
		t.Fatal("a fresh installation has a profile")
	}
}

// The profile is personal data: a support bundle never carries it.
func TestSupportSummaryLeavesTheProfileOut(t *testing.T) {
	s := New()
	p, err := NewProfile(ProfileFields{FullName: "Иванов Иван", Group: "ИВТ-21", University: "МГУ"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProfile(p, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%+v", s.SupportSummary()); strings.Contains(got, "Иванов") || strings.Contains(got, "ИВТ") {
		t.Fatal("the profile reached the support summary")
	}
}
