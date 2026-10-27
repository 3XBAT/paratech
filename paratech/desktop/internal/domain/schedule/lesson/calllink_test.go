package lesson

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/values"
)

const (
	meetLink = "https://meet.google.com/abc-defg-hij"
	zoomLink = "https://us02web.zoom.us/j/81234567890?pwd=AbCdEf"
)

func TestContentValidateCallLink(t *testing.T) {
	tests := []struct {
		name  string
		link  string
		valid bool
	}{
		{"none", "", true},
		{"meet", meetLink, true},
		{"zoom with password", zoomLink, true},
		{"not canonical", "https://Meet.Google.com/abc-defg-hij", false},
		{"surrounding blanks", " " + meetLink, false},
		{"javascript", "javascript:alert(1)", false},
		{"port", "https://meet.google.com:444/abc", false},
		{"too long", "https://example.com/" + strings.Repeat("a", values.MaxCallLinkBytes), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := content("Math", t0, time.Hour)
			c.CallLink = tt.link
			err := c.Validate()
			if tt.valid && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidLesson) {
				t.Fatalf("Validate() = %v, want ErrInvalidLesson", err)
			}
		})
	}
}

func TestContentEqualCallLink(t *testing.T) {
	a := content("Math", t0, time.Hour)
	a.CallLink = meetLink
	same := a
	if !a.Equal(same) {
		t.Fatal("same call link is not Equal")
	}
	for name, link := range map[string]string{"other link": zoomLink, "no link": ""} {
		other := a
		other.CallLink = link
		if a.Equal(other) || other.Equal(a) {
			t.Errorf("%s is Equal", name)
		}
	}
}

func TestChangeContentCallLink(t *testing.T) {
	t1 := t0.Add(time.Hour)
	steps := []struct {
		name     string
		link     string
		changed  bool
		revision int64
	}{
		{"link appears", meetLink, true, 2},
		{"same link", meetLink, false, 2},
		{"link changes", zoomLink, true, 3},
		{"link dropped", "", true, 4},
	}
	l := newLesson(t)
	for _, s := range steps {
		c := l.Content()
		c.CallLink = s.link
		changed, err := l.ChangeContent(c, t1)
		if err != nil || changed != s.changed || l.ContentRevision() != s.revision || l.Content().CallLink != s.link {
			t.Fatalf("%s: changed=%v err=%v revision=%d link=%q", s.name, changed, err, l.ContentRevision(), l.Content().CallLink)
		}
	}

	c := l.Content()
	c.CallLink = "file:///C:/x"
	if _, err := l.ChangeContent(c, t1); !errors.Is(err, ErrInvalidLesson) || l.ContentRevision() != 4 {
		t.Fatalf("bad link: err=%v revision=%d", err, l.ContentRevision())
	}
}

func TestRestoreKeepsCallLink(t *testing.T) {
	s := validState()
	s.Content.CallLink = zoomLink
	l, err := Restore(s)
	if err != nil || l.Content().CallLink != zoomLink {
		t.Fatalf("Restore: %v, link %q", err, l.Content().CallLink)
	}

	s.Content.CallLink = "https://user@zoom.us/j/1"
	if _, err := Restore(s); !errors.Is(err, ErrInvalidLesson) {
		t.Fatalf("Restore of a bad stored link: %v, want ErrInvalidLesson", err)
	}
}
