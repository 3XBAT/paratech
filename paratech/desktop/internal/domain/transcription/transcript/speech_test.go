package transcript

import (
	"reflect"
	"strings"
	"testing"
)

func TestSegmentSpeech(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   string
		speech bool
	}{
		{name: "blank audio", text: " [BLANK_AUDIO]"},
		{name: "two blank audio", text: "[BLANK_AUDIO] [BLANK_AUDIO]"},
		{name: "silence with spaces", text: "[ Silence ]"},
		{name: "music", text: "[MUSIC]"},
		{name: "russian music", text: "[Музыка]"},
		{name: "parenthesised music", text: "(music)"},
		{name: "parenthesised russian", text: " (музыка)"},
		{name: "starred", text: "*music*"},
		{name: "music symbols", text: "♪♪"},
		{name: "music emoji", text: "🎵 🎶"},
		{name: "special token", text: "[_TT_150]"},
		{name: "begin token", text: "[_BEG_]"},
		{name: "marks and punctuation", text: "[MUSIC] ... (applause)!"},
		{name: "only punctuation", text: " ..."},
		{name: "empty", text: ""},
		{name: "spaces", text: "   "},
		{name: "russian speech", text: " Сегодня разберём интегралы.", want: " Сегодня разберём интегралы.", speech: true},
		{name: "english speech", text: " Let us start.", want: " Let us start.", speech: true},
		{name: "math in parentheses", text: "(x + 1)", want: "(x + 1)", speech: true},
		{name: "one letter in parentheses", text: "(x)", want: "(x)", speech: true},
		{name: "number in brackets", text: "[1]", want: "[1]", speech: true},
		{name: "index kept", text: "a[i] равно нулю", want: "a[i] равно нулю", speech: true},
		{name: "marks stripped from speech", text: " [BLANK_AUDIO] Привет [_TT_12] всем", want: "Привет всем", speech: true},
		{name: "parenthesis kept in speech", text: "[MUSIC] (смеётся) хорошо", want: "(смеётся) хорошо", speech: true},
		{name: "music symbol with words", text: "♪ la la ♪", want: "♪ la la ♪", speech: true},
		{name: "number alone", text: "42", want: "42", speech: true},
		{name: "unclosed bracket", text: "[BLANK_AUDIO", want: "[BLANK_AUDIO", speech: true},
		// Marks are short; a longer group is speech (TASK-110 review).
		{name: "short parenthesised phrase", text: "(тихо говорит)"},
		{name: "short starred phrase", text: "*звонок в дверь*"},
		{name: "speech in parentheses", text: "(Ну, хорошо, давайте начнём.)", want: "(Ну, хорошо, давайте начнём.)", speech: true},
		{name: "speech in stars", text: "*очень важно запомнить это правило*", want: "*очень важно запомнить это правило*", speech: true},
		{name: "long word in parentheses", text: "(" + strings.Repeat("а", 31) + ")", want: "(" + strings.Repeat("а", 31) + ")", speech: true},
		{name: "speech in brackets", text: "[Сейчас я покажу вам пример]", want: "[Сейчас я покажу вам пример]", speech: true},
		// Inside speech only whisper-style square-bracket marks are removed.
		{name: "two-letter index kept", text: "a[ij] равно нулю", want: "a[ij] равно нулю", speech: true},
		{name: "lower-case word in brackets kept", text: "см. [пример] выше", want: "см. [пример] выше", speech: true},
		{name: "russian mark stripped", text: "[Музыка] Начнём", want: "Начнём", speech: true},
		{name: "spaced mark stripped", text: "Итак [ Silence ] дальше", want: "Итак дальше", speech: true},
		{name: "upper-case mark stripped", text: "Итак [MUSIC]", want: "Итак", speech: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Segment{StartMs: 5, EndMs: 9, Text: tc.text}.Speech()
			if ok != tc.speech {
				t.Fatalf("Speech(%q) ok = %v, want %v", tc.text, ok, tc.speech)
			}
			if !ok {
				return
			}
			if got != (Segment{StartMs: 5, EndMs: 9, Text: tc.want}) {
				t.Fatalf("Speech(%q) = %+v, want text %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestDocumentSpeech(t *testing.T) {
	blank := make([]Segment, 473)
	for i := range blank {
		blank[i] = Segment{StartMs: int64(i) * 10_000, EndMs: int64(i+1) * 10_000, Text: " [BLANK_AUDIO]"}
	}
	tests := []struct {
		name   string
		segs   []Segment
		want   []Segment
		speech bool
	}{
		{name: "only blank audio", segs: blank, want: []Segment{}},
		{name: "empty", segs: nil, want: []Segment{}},
		{
			name: "mixed",
			segs: []Segment{{0, 1, "[BLANK_AUDIO]"}, {1, 2, " Начнём."}, {2, 3, "(музыка)"}, {3, 4, "[MUSIC] Итак"}},
			want: []Segment{{1, 2, " Начнём."}, {3, 4, "Итак"}}, speech: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDocument("en", tc.segs)
			if err != nil {
				t.Fatal(err)
			}
			if got := d.HasSpeech(); got != tc.speech {
				t.Fatalf("HasSpeech = %v, want %v", got, tc.speech)
			}
			if got := d.Speech(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Speech = %+v, want %+v", got, tc.want)
			}
			if len(d.Segments()) != len(tc.segs) {
				t.Fatalf("Segments changed: %d, want %d", len(d.Segments()), len(tc.segs))
			}
		})
	}
}
