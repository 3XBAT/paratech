package values

import (
	"errors"
	"strings"
	"testing"
)

func TestZeroFormattingIsGOST(t *testing.T) {
	want := Formatting{
		FontName: "Times New Roman", FontSizePt: 14, LineSpacingPct: 150,
		MarginTopMM: 20, MarginRightMM: 15, MarginBottomMM: 20, MarginLeftMM: 30, IndentTenthMM: 125,
	}
	if got := (Formatting{}).Normalized(); got != want {
		t.Fatalf("normalized = %+v, want %+v", got, want)
	}
	if GOSTFormatting() != want {
		t.Fatal("GOSTFormatting differs from the GOST 7.32 defaults")
	}
	if err := (Formatting{}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFormattingNormalizedAndValidate(t *testing.T) {
	gost := GOSTFormatting()
	cases := []struct {
		name  string
		in    Formatting
		want  func(f *Formatting) // the change from GOST that Normalized keeps
		valid bool
	}{
		{"custom profile kept", Formatting{
			FontName: "PT Astra Serif", FontSizePt: 12, LineSpacingPct: 100,
			MarginTopMM: 25, MarginRightMM: 10, MarginBottomMM: 25, MarginLeftMM: 35, IndentTenthMM: 100,
		}, func(f *Formatting) {
			*f = Formatting{"PT Astra Serif", 12, 100, 25, 10, 25, 35, 100}
		}, true},
		{"bounds kept", Formatting{FontSizePt: MaxFontSizePt, LineSpacingPct: MaxLineSpacingPct, MarginTopMM: MinMarginMM, IndentTenthMM: MaxIndentTenthMM},
			func(f *Formatting) {
				f.FontSizePt, f.LineSpacingPct, f.MarginTopMM, f.IndentTenthMM = MaxFontSizePt, MaxLineSpacingPct, MinMarginMM, MaxIndentTenthMM
			}, true},
		{"font too small", Formatting{FontSizePt: MinFontSizePt - 1}, nil, false},
		{"font too big", Formatting{FontSizePt: MaxFontSizePt + 1}, nil, false},
		{"negative size", Formatting{FontSizePt: -14}, nil, false},
		{"spacing too small", Formatting{LineSpacingPct: 99}, nil, false},
		{"spacing too big", Formatting{LineSpacingPct: 301}, nil, false},
		{"margin too small", Formatting{MarginLeftMM: MinMarginMM - 1}, nil, false},
		{"margin too big", Formatting{MarginRightMM: MaxMarginMM + 1}, nil, false},
		{"bottom margin", Formatting{MarginBottomMM: 1000}, nil, false},
		{"top margin", Formatting{MarginTopMM: -1}, nil, false},
		{"indent too big", Formatting{IndentTenthMM: MaxIndentTenthMM + 1}, nil, false},
		{"negative indent", Formatting{IndentTenthMM: -5}, nil, false},
		{"markup font", Formatting{FontName: `Times"/><w:b/>`}, nil, false},
		{"font with angle brackets", Formatting{FontName: "<Arial>"}, nil, false},
		{"font with control", Formatting{FontName: "Arial\x00"}, nil, false},
		{"font with bidi", Formatting{FontName: "Arial" + string(rune(0x202E))}, nil, false},
		{"font with newline", Formatting{FontName: "Arial\nBold"}, nil, false},
		{"font with spaces around", Formatting{FontName: " Arial"}, nil, false},
		{"font too long", Formatting{FontName: strings.Repeat("a", MaxFontNameRunes+1)}, nil, false},
		{"font at the limit", Formatting{FontName: strings.Repeat("я", MaxFontNameRunes)}, func(f *Formatting) {
			f.FontName = strings.Repeat("я", MaxFontNameRunes)
		}, true},
		{"invalid utf-8 font", Formatting{FontName: "Ari\xffal"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := gost
			if tc.want != nil {
				tc.want(&want)
			}
			if got := tc.in.Normalized(); got != want {
				t.Fatalf("normalized = %+v, want %+v", got, want)
			}
			err := tc.in.Validate()
			if tc.valid && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidFormatting) {
				t.Fatalf("err = %v, want ErrInvalidFormatting", err)
			}
		})
	}
}

func TestValidFontName(t *testing.T) {
	for _, s := range []string{"Times New Roman", "PT Astra Serif", "Liberation_Serif", "Arial-Narrow", "Font 2.0"} {
		if !ValidFontName(s) {
			t.Errorf("%q is a valid font name", s)
		}
	}
	for _, s := range []string{"", " ", "a&b", "a'b", `a"b`, "a;b", "a\tb"} {
		if ValidFontName(s) {
			t.Errorf("%q is not a valid font name", s)
		}
	}
}
