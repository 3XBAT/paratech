package values

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func validPresentation() Presentation {
	return Presentation{Slides: []Slide{
		{Kind: SlideTitle, Title: "Пределы", Subtitle: "Иванов И. И.\nАнализ, 2026", Notes: "Добрый день."},
		{Kind: SlideBullets, Title: "Определение", Bullets: []string{"Первое", "Второе"}},
		{Kind: SlideTwoColumns, Title: "Сравнение",
			Left:  SlideColumn{Heading: "За", Bullets: []string{"Быстро"}},
			Right: SlideColumn{Bullets: []string{"Дорого"}}},
		{Kind: SlideConclusion, Title: "Спасибо за внимание"},
	}}
}

func TestPresentationValidate(t *testing.T) {
	if err := validPresentation().Validate(); err != nil {
		t.Fatalf("valid presentation: %v", err)
	}

	long := func(n int) string { return strings.Repeat("я", n) }
	many := func(n int) []string { return slices.Repeat([]string{"тезис"}, n) }
	cases := []struct {
		name   string
		edit   func(p *Presentation)
		wantOK bool
	}{
		{"every template", func(p *Presentation) { p.Template = PresentationGraphite }, true},
		{"unknown template", func(p *Presentation) { p.Template = "neon" }, false},
		{"no slides", func(p *Presentation) { p.Slides = nil }, false},
		{"most slides", func(p *Presentation) { p.Slides = slices.Repeat(p.Slides[1:2], MaxPresentationSlides) }, true},
		{"too many slides", func(p *Presentation) { p.Slides = slices.Repeat(p.Slides[1:2], MaxPresentationSlides+1) }, false},
		{"unknown kind", func(p *Presentation) { p.Slides[1].Kind = "chart" }, false},
		{"empty kind", func(p *Presentation) { p.Slides[1].Kind = "" }, false},
		{"blank title", func(p *Presentation) { p.Slides[0].Title = " \n " }, false},
		{"longest title", func(p *Presentation) { p.Slides[0].Title = long(MaxSlideTitleRunes) }, true},
		{"title too long", func(p *Presentation) { p.Slides[0].Title = long(MaxSlideTitleRunes + 1) }, false},
		{"subtitle too long", func(p *Presentation) { p.Slides[0].Subtitle = long(MaxSlideSubtitleRunes + 1) }, false},
		{"most subtitle lines", func(p *Presentation) { p.Slides[0].Subtitle = "a\nb\r\n\n \nc\rd" }, true},
		{"too many subtitle lines", func(p *Presentation) { p.Slides[0].Subtitle = "a\nb\r\nc\rd\ne" }, false},
		{"control-only line is not a subtitle line", func(p *Presentation) { p.Slides[0].Subtitle = "a\nb\nc\nd\n\x00\u202E" }, true},
		{"NUL title", func(p *Presentation) { p.Slides[0].Title = "\x00" }, false},
		{"bidi title", func(p *Presentation) { p.Slides[0].Title = "\u202E" }, false},
		{"control-only title", func(p *Presentation) { p.Slides[1].Title = "\x01\uFFFE\u2066 \xff" }, false},
		{"NUL bullet", func(p *Presentation) { p.Slides[1].Bullets[0] = "\x00" }, false},
		{"bidi bullet", func(p *Presentation) { p.Slides[1].Bullets[0] = "\u202E" }, false},
		{"bidi column bullet", func(p *Presentation) { p.Slides[2].Right.Bullets[0] = "\u202E\x00" }, false},
		{"text around controls", func(p *Presentation) { p.Slides[1].Bullets[0] = "\u202Ea\x00" }, true},
		{"title slide with bullets", func(p *Presentation) { p.Slides[0].Bullets = many(1) }, false},
		{"title slide with a column", func(p *Presentation) { p.Slides[0].Right.Heading = "x" }, false},
		{"notes too long", func(p *Presentation) { p.Slides[2].Notes = long(MaxSlideNotesRunes + 1) }, false},
		{"longest notes", func(p *Presentation) { p.Slides[2].Notes = long(MaxSlideNotesRunes) }, true},
		{"bullets slide without bullets", func(p *Presentation) { p.Slides[1].Bullets = nil }, false},
		{"most bullets", func(p *Presentation) { p.Slides[1].Bullets = many(MaxSlideBullets) }, true},
		{"too many bullets", func(p *Presentation) { p.Slides[1].Bullets = many(MaxSlideBullets + 1) }, false},
		{"blank bullet", func(p *Presentation) { p.Slides[1].Bullets[1] = "  " }, false},
		{"bullet too long", func(p *Presentation) { p.Slides[1].Bullets[0] = long(MaxSlideTextRunes + 1) }, false},
		{"bullets slide with a subtitle", func(p *Presentation) { p.Slides[1].Subtitle = "x" }, false},
		{"bullets slide with a column", func(p *Presentation) { p.Slides[1].Left.Bullets = many(1) }, false},
		{"conclusion with lines", func(p *Presentation) { p.Slides[3].Bullets = many(MaxSlideBullets) }, true},
		{"conclusion with too many lines", func(p *Presentation) { p.Slides[3].Bullets = many(MaxSlideBullets + 1) }, false},
		{"two columns without left bullets", func(p *Presentation) { p.Slides[2].Left.Bullets = nil }, false},
		{"two columns with too many right bullets", func(p *Presentation) { p.Slides[2].Right.Bullets = many(MaxSlideBullets + 1) }, false},
		{"column heading too long", func(p *Presentation) { p.Slides[2].Left.Heading = long(MaxSlideTextRunes + 1) }, false},
		{"two columns with bullets", func(p *Presentation) { p.Slides[2].Bullets = many(1) }, false},
		{"two columns with a subtitle", func(p *Presentation) { p.Slides[2].Subtitle = "x" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPresentation()
			tc.edit(&p)
			err := p.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrInvalidPresentation) {
				t.Fatalf("Validate() = %v, want ErrInvalidPresentation", err)
			}
		})
	}
}

func TestCleanPresentationText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain текст", "plain текст"},
		{"tab\tline\nbreak\r", "tab\tline\nbreak\r"},
		{"a\x00b\x1fc\x7fd", "abc\x7fd"},
		{"x\uFFFEy\uFFFFz\uFFFD", "xyz\uFFFD"},
		{"\u202Eright\u2066\u200F", "right"},
		{"\xffbad", "bad"},
		{"😀", "😀"},
	}
	for _, tc := range cases {
		if got := CleanPresentationText(tc.in); got != tc.want {
			t.Errorf("CleanPresentationText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPresentationTemplates(t *testing.T) {
	if got := PresentationTemplate("").OrDefault(); got != PresentationPaper {
		t.Fatalf("default template = %q", got)
	}
	if got := PresentationGraphite.OrDefault(); got != PresentationGraphite {
		t.Fatalf("OrDefault(graphite) = %q", got)
	}
	all := PresentationTemplates()
	if len(all) < 2 || len(all) > 3 || all[0] != PresentationPaper {
		t.Fatalf("templates = %v", all)
	}
	for _, tpl := range all {
		p := validPresentation()
		p.Template = tpl
		if err := p.Validate(); err != nil {
			t.Fatalf("template %q: %v", tpl, err)
		}
	}
}
