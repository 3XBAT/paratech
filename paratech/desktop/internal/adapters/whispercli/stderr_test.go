package whispercli

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStderrProgress(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []int
	}{
		{
			name:   "cli format",
			chunks: []string{"whisper_print_progress_callback: progress =   5%\nwhisper_print_progress_callback: progress =  10%\n"},
			want:   []int{5, 10},
		},
		{
			name:   "split across writes",
			chunks: []string{"whisper_print_progress_call", "back: progress = 4", "2%\n"},
			want:   []int{42},
		},
		{
			name:   "carriage returns",
			chunks: []string{"f: progress = 1%\rf: progress = 2%\r\n"},
			want:   []int{1, 2},
		},
		{
			name:   "100 is held back for the stored result",
			chunks: []string{"f: progress = 95%\nf: progress = 100%\n"},
			want:   []int{95, 99},
		},
		{
			name:   "not increasing",
			chunks: []string{"f: progress = 30%\nf: progress = 20%\nf: progress = 30%\nf: progress = 31%\n"},
			want:   []int{30, 31},
		},
		{
			name: "noise and echoed paths",
			chunks: []string{
				"main: processing '/data/progress = 50%' (1 samples)\n",
				"  progress = 60%\n",
				"x: progress = 101%\n",
				"x: progress = 1000%\n",
				"x: progress = -5%\n",
				"x: progress = 7% extra\n",
				"no newline at the end: progress = 70%",
			},
			want: nil,
		},
		{
			name:   "overlong line is skipped, the next one counts",
			chunks: []string{strings.Repeat("a", maxLine+10) + " x: progress = 40%\n", "f: progress = 41%\n"},
			want:   []int{41},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []int
			s := newStderrSink(newReporter(func(p int) { got = append(got, p) }, 0, time.Now))
			for _, c := range tt.chunks {
				if n, err := s.Write([]byte(c)); n != len(c) || err != nil {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("progress = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStderrStopAndFinal(t *testing.T) {
	var got []int
	s := newStderrSink(newReporter(func(p int) { got = append(got, p) }, 0, time.Now))
	_, _ = s.Write([]byte("f: progress = 10%\n"))
	s.stop()
	_, _ = s.Write([]byte("f: progress = 20%\n"))
	s.final()
	s.final()

	if want := []int{10, 100}; !slices.Equal(got, want) {
		t.Errorf("progress = %v, want %v", got, want)
	}

	newStderrSink(newReporter(nil, 0, time.Now)).final() // nil callback is allowed
}

func TestReporterRateLimit(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	var got []int
	r := newReporter(func(p int) { got = append(got, p) }, time.Second, clock)

	steps := []struct {
		advance time.Duration
		p       int
	}{
		{0, 0},                       // first value always
		{100 * time.Millisecond, 5},  // too soon
		{900 * time.Millisecond, 10}, // a second after the last report
		{500 * time.Millisecond, 15}, // too soon
		{0, 100},                     // 100 always
		{5 * time.Second, 100},       // not increasing
		{5 * time.Second, -1},        // out of range
		{5 * time.Second, 101},       // out of range
	}
	for _, s := range steps {
		now = now.Add(s.advance)
		r.report(s.p)
	}

	if want := []int{0, 10, 100}; !slices.Equal(got, want) {
		t.Errorf("reports = %v, want %v", got, want)
	}
}

func TestStderrTail(t *testing.T) {
	root := `C:\Users\Ivan Petrov\AppData\Local\Paratech`
	bin := `C:\Program Files\Paratech\bin`
	redact := []redaction{{dir: root, placeholder: placeholderData}, {dir: bin, placeholder: placeholderBin}}

	tests := []struct {
		name     string
		input    string
		contains []string
		absent   []string
	}{
		{
			name:     "paths redacted in any case and slash form",
			input:    "loading '" + root + `\models\m.bin'` + "\nUPPER " + strings.ToUpper(root) + "\nslash " + strings.ReplaceAll(root, `\`, "/") + "/x\nbin " + bin + `\whisper-cli.exe` + "\n",
			contains: []string{`'<data>\models\m.bin'`, "UPPER <data>", "slash <data>/x", `bin <bin>\whisper-cli.exe`},
			absent:   []string{"Ivan", "Program Files"},
		},
		{
			name:     "control characters removed, invalid UTF-8 replaced",
			input:    "a\x1b[31mred\x00\tb\xff\n",
			contains: []string{"a[31mred\tb?"},
			absent:   []string{"\x1b", "\x00"},
		},
		{
			name:     "long output keeps the end",
			input:    strings.Repeat("line of noise\n", 5000) + "error: the last line\n",
			contains: []string{"error: the last line"},
		},
		{
			name:     "a path cut at the start of the kept tail is dropped",
			input:    root + strings.Repeat("z", 3*rawTailMax) + "\nerror: end\n",
			contains: []string{"error: end"},
			absent:   []string{"Petrov", "zzz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStderrSink(newReporter(nil, 0, time.Now))
			for chunk := range slices.Chunk([]byte(tt.input), 4096) {
				_, _ = s.Write(chunk)
			}

			got := s.tailText(redact)
			if len(got) > stderrMax {
				t.Errorf("tail has %d bytes, limit %d", len(got), stderrMax)
			}
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("tail %q does not contain %q", got, c)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("tail %q contains %q", got, a)
				}
			}
		})
	}
}

func TestReplaceFold(t *testing.T) {
	tests := []struct{ s, old, repl, want string }{
		{"abcABCabc", "abc", "x", "xxx"},
		{"", "abc", "x", ""},
		{"ab", "abc", "x", "ab"},
		{"aXbxc", "xb", "-", "a-xc"},
		{"путь ПУТЬ", "путь", "p", "p p"},
		{"no match", "", "x", "no match"},
	}
	for _, tt := range tests {
		if got := replaceFold(tt.s, tt.old, tt.repl); got != tt.want {
			t.Errorf("replaceFold(%q, %q) = %q, want %q", tt.s, tt.old, got, tt.want)
		}
	}
}
