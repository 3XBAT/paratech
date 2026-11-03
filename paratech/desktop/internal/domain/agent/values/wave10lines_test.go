package values

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestLineResultValidate: a line result is the zero value or exactly the fields of its kind, within the limits.
func TestLineResultValidate(t *testing.T) {
	long := strings.Repeat("ж", MaxExcerptLineRunes)
	tests := map[string]struct {
		r  LineResult
		ok bool
	}{
		"zero: the generic line":       {LineResult{}, true},
		"marked done":                  {LineResult{Kind: LineMarkedDone, Count: 2}, true},
		"marked undone, the max":       {LineResult{Kind: LineMarkedUndone, Count: MaxLineCount}, true},
		"marked, count 0":              {LineResult{Kind: LineMarkedDone}, false},
		"marked, negative count":       {LineResult{Kind: LineMarkedDone, Count: -1}, false},
		"marked, over the max":         {LineResult{Kind: LineMarkedDone, Count: MaxLineCount + 1}, false},
		"count on another kind":        {LineResult{Kind: LineNoteMove, Count: 1}, false},
		"unknown kind":                 {LineResult{Kind: "sent_email"}, false},
		"count without a kind":         {LineResult{Count: 1}, false},
		"note text without an excerpt": {LineResult{Kind: LineNoteText}, true},
		"note text, 3 lines":           {LineResult{Kind: LineNoteText, Excerpt: "раз\nдва <b>\n" + long}, true},
		"note text, 4 lines":           {LineResult{Kind: LineNoteText, Excerpt: "1\n2\n3\n4"}, false},
		"note text, a long line":       {LineResult{Kind: LineNoteText, Excerpt: long + "ж"}, false},
		"note text, a blank line":      {LineResult{Kind: LineNoteText, Excerpt: "раз\n\nдва"}, false},
		"note text, untrimmed":         {LineResult{Kind: LineNoteText, Excerpt: " раз"}, false},
		"note text, double space":      {LineResult{Kind: LineNoteText, Excerpt: "раз  два"}, false},
		"note text, a tab":             {LineResult{Kind: LineNoteText, Excerpt: "раз\tдва"}, false},
		"note text, a CR":              {LineResult{Kind: LineNoteText, Excerpt: "раз\r"}, false},
		"note text, a bidi override":   {LineResult{Kind: LineNoteText, Excerpt: "раз\u202eдва"}, false},
		"note text, invalid UTF-8":     {LineResult{Kind: LineNoteText, Excerpt: "раз\xffдва"}, false},
		"excerpt on another kind":      {LineResult{Kind: LineNoteUpload, Excerpt: "раз"}, false},
		"note kinds":                   {LineResult{Kind: LineNoteRemove}, true},
		"recording started":            {LineResult{Kind: LineRecordingStarted}, true},
		"recording stopped":            {LineResult{Kind: LineRecordingStopped}, true},
		"setting":                      {LineResult{Kind: LineSetting, Setting: SettingRecordingPrompts, Value: SettingOff}, true},
		"setting, a load":              {LineResult{Kind: LineSetting, Setting: SettingRecognitionLoad, Value: SettingValues(SettingRecognitionLoad)[0]}, true},
		"setting, unknown setting":     {LineResult{Kind: LineSetting, Setting: "profile_name", Value: SettingOff}, false},
		"setting, a value of another":  {LineResult{Kind: LineSetting, Setting: SettingRecognitionLoad, Value: SettingOn}, false},
		"setting, no value":            {LineResult{Kind: LineSetting, Setting: SettingACPowerOnly}, false},
		"setting on another kind":      {LineResult{Kind: LineRecordingStarted, Setting: SettingACPowerOnly, Value: SettingOn}, false},
		"value on another kind":        {LineResult{Kind: LineNoteMove, Value: SettingOn}, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.r.Validate()
			if (err == nil) != tt.ok {
				t.Fatalf("Validate() = %v, want ok %v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidCard) {
				t.Fatalf("Validate() = %v, want ErrInvalidCard", err)
			}
			// The card carrying it is refused alike: the panel drops it.
			if err := (ActionLine{Action: "act1", Result: tt.r}).Validate(); (err == nil) != tt.ok {
				t.Fatalf("ActionLine.Validate() = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

// TestNoteExcerpt: the first three non-blank lines, cleaned, collapsed and cut to the rune limit; always valid.
func TestNoteExcerpt(t *testing.T) {
	long := strings.Repeat("ж", MaxExcerptLineRunes+5)
	exact := strings.Repeat("я", MaxExcerptLineRunes)
	tests := map[string]struct{ in, want string }{
		"empty":                     {"", ""},
		"blank lines only":          {" \n\t\n\r\n", ""},
		"one line, trimmed":         {"  Импульс  тела \t", "Импульс тела"},
		"CRLF":                      {"раз\r\nдва\r\n", "раз\nдва"},
		"blank lines skipped":       {"\n\nраз\n   \nдва\n", "раз\nдва"},
		"at most three lines":       {"1\n2\n3\n4\n5", "1\n2\n3"},
		"cut in runes, not bytes":   {long, strings.Repeat("ж", MaxExcerptLineRunes-1) + "…"},
		"exactly the limit":         {exact, exact},
		"control characters":        {"раз\x00два\x1bтри", "раз два три"},
		"format characters":         {"раз\u200bдва\u202eтри", "раздватри"},
		"invalid UTF-8":             {"раз\xffдва", "раз два"},
		"markup kept as plain text": {"<b>жирный</b> & <script>", "<b>жирный</b> & <script>"},
		"a line of control only":    {"\x07\x08\nдва", "два"},
		"cut on a space":            {strings.Repeat("ж", MaxExcerptLineRunes-2) + " ж ж", strings.Repeat("ж", MaxExcerptLineRunes-2) + "…"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NoteExcerpt(tt.in)
			if got != tt.want {
				t.Fatalf("NoteExcerpt(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for l := range strings.SplitSeq(got, "\n") {
				if n := utf8.RuneCountInString(l); n > MaxExcerptLineRunes {
					t.Fatalf("line of %d runes", n)
				}
			}
			if err := (LineResult{Kind: LineNoteText, Excerpt: got}).Validate(); err != nil {
				t.Fatalf("the excerpt is refused: %v", err)
			}
		})
	}
}
