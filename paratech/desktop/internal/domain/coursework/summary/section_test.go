package summary

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func sectioned(t *testing.T, sections []Section, points ...string) Content {
	t.Helper()
	c, err := NewSectionedContent(sections, points)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewSectionedContent(t *testing.T) {
	ok := Section{Heading: "Реки", Text: "Реки текут в море.\n\nДельты образуются в устье.", Points: []string{"Устье", "Дельта"}}
	many := func(n int, s Section) []Section {
		out := make([]Section, n)
		for i := range out {
			out[i] = s
		}
		return out
	}
	points := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "p"
		}
		return out
	}
	// Each section holds a fifth of MaxTextRunes: five fit, a sixth goes over the total.
	fifth := Section{Heading: "H", Text: strings.Repeat("ж", MaxTextRunes/5-1)}
	cases := []struct {
		name     string
		sections []Section
		wantErr  bool
	}{
		{"valid", []Section{ok, {Heading: "Озёра", Text: "Озёра.", Points: nil}}, false},
		{"max sections", many(MaxSections, Section{Heading: "H", Text: "T"}), false},
		{"max points", []Section{{Heading: "H", Text: "T", Points: points(MaxSectionPoints)}}, false},
		{"limits of the fields", []Section{{Heading: strings.Repeat("h", MaxHeadingRunes), Text: strings.Repeat("t", MaxSectionTextRunes),
			Points: []string{strings.Repeat("p", MaxPointRunes)}}}, false},
		{"total at the limit", many(5, fifth), false},
		{"no sections", nil, true},
		{"empty sections", []Section{}, true},
		{"too many sections", many(MaxSections+1, Section{Heading: "H", Text: "T"}), true},
		{"total over the limit", many(6, fifth), true},
		{"no heading", []Section{{Text: "T"}}, true},
		{"blank heading", []Section{{Heading: " \t", Text: "T"}}, true},
		{"heading with a newline", []Section{{Heading: "a\nb", Text: "T"}}, true},
		{"heading with a line separator", []Section{{Heading: "a b", Text: "T"}}, true},
		{"heading too long", []Section{{Heading: strings.Repeat("h", MaxHeadingRunes+1), Text: "T"}}, true},
		{"no text", []Section{{Heading: "H"}}, true},
		{"text too long", []Section{{Heading: "H", Text: strings.Repeat("t", MaxSectionTextRunes+1)}}, true},
		{"text with a control", []Section{{Heading: "H", Text: "a\x1bb"}}, true},
		{"text invalid utf8", []Section{{Heading: "H", Text: "\xff"}}, true},
		{"too many points", []Section{{Heading: "H", Text: "T", Points: points(MaxSectionPoints + 1)}}, true},
		{"empty point", []Section{{Heading: "H", Text: "T", Points: []string{" "}}}, true},
		{"point with a newline", []Section{{Heading: "H", Text: "T", Points: []string{"a\nb"}}}, true},
		{"point too long", []Section{{Heading: "H", Text: "T", Points: []string{strings.Repeat("p", MaxPointRunes+1)}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewSectionedContent(tc.sections, []string{"Главное"})
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidContent) || !c.IsZero() {
					t.Fatalf("NewSectionedContent = %+v, %v; want ErrInvalidContent", c, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !c.Sectioned() || len(c.Sections()) != len(tc.sections) || len(c.KeyPoints()) != 1 {
				t.Fatalf("content = %+v", c)
			}
		})
	}

	if _, err := NewSectionedContent([]Section{ok}, []string{"a\nb"}); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("a bad key point: %v", err)
	}
}

func TestSectionedContentTrimsAndRenders(t *testing.T) {
	c := sectioned(t, []Section{
		{Heading: " Реки: ", Text: " Реки текут.\n\nВ море. ", Points: []string{" Устье ", "Дельта"}},
		{Heading: "Озёра", Text: "Озёра стоят."},
	}, " Вода ")

	want := []Section{
		{Heading: "Реки:", Text: "Реки текут.\n\nВ море.", Points: []string{"Устье", "Дельта"}},
		{Heading: "Озёра", Text: "Озёра стоят."},
	}
	if got := c.Sections(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %#v", got)
	}
	if got := c.KeyPoints(); !reflect.DeepEqual(got, []string{"Вода"}) {
		t.Fatalf("key points = %q", got)
	}
	const text = "Реки:\nРеки текут.\n\nВ море.\n- Устье\n- Дельта\n\nОзёра:\nОзёра стоят."
	if c.Text() != text {
		t.Fatalf("text = %q, want %q", c.Text(), text)
	}
}

func TestPlainContentIsOneSection(t *testing.T) {
	c := content(t, "Про реки.", "Реки текут")
	if c.Sectioned() {
		t.Fatal("a plain text reads as sectioned")
	}
	if got := c.Sections(); !reflect.DeepEqual(got, []Section{{Text: "Про реки."}}) {
		t.Fatalf("sections = %#v", got)
	}
	if (Content{}).Sections() != nil {
		t.Fatal("the zero value has sections")
	}

	// The same text as a plain summary and as the rendering of sections are different contents.
	s := sectioned(t, []Section{{Heading: "Реки", Text: "Про реки."}})
	p := content(t, s.Text())
	if s.Equal(p) || p.Equal(s) {
		t.Fatal("a sectioned and a plain content are equal")
	}
	if !s.Equal(sectioned(t, []Section{{Heading: "Реки", Text: "Про реки."}})) {
		t.Fatal("equal sections are not equal")
	}
	if s.Equal(sectioned(t, []Section{{Heading: "Реки", Text: "Про реки.", Points: []string{"p"}}})) {
		t.Fatal("different points are equal")
	}
}

func TestSectionsAreCopied(t *testing.T) {
	in := []Section{{Heading: "H", Text: "T", Points: []string{"a"}}}
	c := sectioned(t, in)
	in[0].Points[0] = "changed"
	got := c.Sections()
	got[0].Points[0] = "changed too"
	got[0].Heading = "changed"
	if s := c.Sections()[0]; s.Points[0] != "a" || s.Heading != "H" {
		t.Fatal("content shares its sections with the caller")
	}
}

func TestRestoreSectioned(t *testing.T) {
	c := sectioned(t, []Section{{Heading: "Реки", Text: "Реки текут.", Points: []string{"Устье"}}}, "Вода")
	s, err := New(Spec{Transcript: "t1", PromptVersion: 5, Content: c}, t0)
	if err != nil {
		t.Fatal(err)
	}
	s.Persisted(1)
	st := s.State()
	if st.Sections == nil || st.Text != c.Text() {
		t.Fatalf("state = %+v", st)
	}

	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State(), st) || !got.Content().Equal(c) {
		t.Fatalf("Restore(State()) = %+v, want %+v", got.State(), st)
	}

	// The text of a sectioned summary is derived: a stale stored rendering does not change the content.
	stale := st
	stale.Text = "old rendering"
	if r, err := Restore(stale); err != nil || r.Content().Text() != c.Text() {
		t.Fatalf("Restore with a stale text = %v, %v", r, err)
	}

	for name, sections := range map[string][]Section{
		"empty sections": {},
		"no heading":     {{Text: "T"}},
		"no text":        {{Heading: "H"}},
	} {
		t.Run(name, func(t *testing.T) {
			b := st
			b.Sections = sections
			if r, err := Restore(b); !errors.Is(err, ErrInvalidSummary) || r != nil {
				t.Fatalf("Restore = %v, %v; want ErrInvalidSummary", r, err)
			}
		})
	}

	// A plain summary keeps no sections in its state.
	if st := newSummary(t).State(); st.Sections != nil {
		t.Fatalf("plain summary state has sections: %+v", st.Sections)
	}
}

func TestReplacePlainWithSectioned(t *testing.T) {
	s := newSummary(t)
	c := sectioned(t, []Section{{Heading: "Реки", Text: s.Content().Text()}}, s.Content().KeyPoints()...)
	if changed, err := s.Replace(c, s.PromptVersion(), t0); err != nil || !changed || !s.Content().Sectioned() {
		t.Fatalf("Replace = %v, %v; want the sectioned content", changed, err)
	}
}
