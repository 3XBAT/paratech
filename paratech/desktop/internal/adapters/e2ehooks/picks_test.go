package e2ehooks

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Every path of a single-file pick: only regular files below fixtures, named without tricks, are picked.
func TestPickFilePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // rel below fixtures; "" cancelled
	}{
		{"forward slashes", "fixtures/clip.wav", "clip.wav"},
		{"back slashes", `fixtures\clip.wav`, "clip.wav"},
		{"nested", "fixtures/sub/notes.pdf", "sub/notes.pdf"},
		{"mixed slashes", `fixtures/sub\photo.jpg`, "sub/photo.jpg"},

		{"empty", "", ""},
		{"rooted slash", "/fixtures/clip.wav", ""},
		{"rooted back slash", `\fixtures\clip.wav`, ""},
		{"unc", `\\server\share\clip.wav`, ""},
		{"unc forward", "//server/share/clip.wav", ""},
		{"extended prefix", `\\?\C:\clip.wav`, ""},
		{"device prefix", `\\.\C:\clip.wav`, ""},
		{"drive absolute", `C:\fixtures\clip.wav`, ""},
		{"drive relative", "C:clip.wav", ""},
		{"drive relative fixtures", "C:fixtures/clip.wav", ""},
		{"dot dot out", "fixtures/../picks.jsonl", ""},
		{"dot dot twice", "fixtures/sub/../../outside/secret.wav", ""},
		{"dot dot back slash", `fixtures\..\outside\secret.wav`, ""},
		{"dot dot staying inside", "fixtures/sub/../clip.wav", ""},
		{"dot dot first", "../fixtures/clip.wav", ""},
		{"dot first", "./fixtures/clip.wav", ""},
		{"dot inside", "fixtures/./clip.wav", ""},
		{"doubled separator", "fixtures//clip.wav", ""},
		{"trailing separator", "fixtures/clip.wav/", ""},
		{"outside fixtures", "outside/secret.wav", ""},
		{"control file", "picks.jsonl", ""},
		{"fixtures itself", "fixtures", ""},
		{"a folder", "fixtures/sub", ""},
		{"fixtures in capitals", "FIXTURES/clip.wav", ""},
		{"missing", "fixtures/none.wav", ""},
		{"nul", "fixtures/NUL", ""},
		{"con with extension", "fixtures/con.wav", ""},
		{"com port", "fixtures/COM1", ""},
		{"com superscript", "fixtures/COM¹.wav", ""},
		{"lpt with extension", "fixtures/lpt9.txt", ""},
		{"aux with a space", "fixtures/aux .wav", ""},
		{"conin", "fixtures/CONIN$", ""},
		{"stream", "fixtures/clip.wav:stream", ""},
		{"data stream", "fixtures/clip.wav::$DATA", ""},
		{"short name", "fixtures/CLIP~1.WAV", ""},
		{"trailing dot", "fixtures/clip.wav.", ""},
		{"trailing space", "fixtures/clip.wav ", ""},
		{"trailing dot folder", "fixtures/sub./notes.pdf", ""},
		{"nul byte", "fixtures/clip\x00.wav", ""},
		{"newline", "fixtures/clip\n.wav", ""},
		{"wildcard", "fixtures/*.wav", ""},
		{"question mark", "fixtures/clip?.wav", ""},
		{"quote", `fixtures/"clip.wav"`, ""},
		{"pipe", "fixtures/clip.wav|x", ""},
		{"too long", "fixtures/" + strings.Repeat("a", maxPathLen), ""},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, struct{ name, path, want string }{"file in capitals", "fixtures/CLIP.WAV", "clip.wav"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRun(t)
			r.queue(t, pickJSON(t, DialogRecording, tt.path))
			got, err := r.h.PickFile(context.Background(), DialogRecording)
			if err != nil {
				t.Fatalf("PickFile error %v: a hook never fails", err)
			}
			c := r.consumed(t)
			if len(c) != 1 || c[0].Line != 1 || c[0].Dialog != DialogRecording {
				t.Fatalf("consumed %+v, want line 1 of %s used", c, DialogRecording)
			}
			if tt.want == "" {
				if got != "" || c[0].Result != resultCancelled || c[0].Reason == "" {
					t.Fatalf("got %q, consumed %+v; want cancelled with a reason", got, c[0])
				}
				if !strings.Contains(r.logs.String(), "level=ERROR") {
					t.Fatalf("no error line in the log:\n%s", r.logs.String())
				}
				return
			}
			want := r.fixture(t, tt.want)
			if !strings.EqualFold(got, want) || c[0].Result != resultPicked {
				t.Fatalf("got %q (%s), want %q", got, c[0].Result, want)
			}
			if runtime.GOOS != "windows" && got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// Lines that are not a pick for the dialog are used and cancelled.
func TestPickLines(t *testing.T) {
	clip := `"fixtures/clip.wav"`
	tests := []struct {
		name     string
		line     string
		multiple bool
		want     []string // rel below fixtures; nil cancelled
	}{
		{"one path", `{"dialog":"notes","paths":[` + clip + `]}`, false, []string{"clip.wav"}},
		{"several when multiple", `{"dialog":"notes","paths":[` + clip + `,"fixtures/sub/notes.pdf"]}`, true,
			[]string{"clip.wav", "sub/notes.pdf"}},
		{"crlf ending", `{"dialog":"notes","paths":[` + clip + "]}\r", false, []string{"clip.wav"}},

		{"several for a single-file dialog", `{"dialog":"notes","paths":[` + clip + `,"fixtures/sub/notes.pdf"]}`, false, nil},
		{"one bad of several", `{"dialog":"notes","paths":[` + clip + `,"../x"]}`, true, nil},
		{"another dialog", `{"dialog":"recording","paths":[` + clip + `]}`, false, nil},
		{"dialog in capitals", `{"dialog":"NOTES","paths":[` + clip + `]}`, false, nil},
		{"no dialog", `{"paths":[` + clip + `]}`, false, nil},
		{"empty paths", `{"dialog":"notes","paths":[]}`, true, nil},
		{"null paths", `{"dialog":"notes","paths":null}`, true, nil},
		{"no paths", `{"dialog":"notes"}`, true, nil},
		{"a path as a string", `{"dialog":"notes","paths":` + clip + `}`, false, nil},
		{"unknown field", `{"dialog":"notes","paths":[` + clip + `],"x":1}`, false, nil},
		{"two values", `{"dialog":"notes","paths":[` + clip + `]}{}`, false, nil},
		{"bad json", `{"dialog":"notes",`, false, nil},
		{"not an object", `["notes"]`, false, nil},
		{"empty line", ``, false, nil},
		{"huge line", `{"dialog":"notes","paths":[` + clip + `],"pad":"` + strings.Repeat("x", maxLineBytes) + `"}`, false, nil},
		{"too many paths", `{"dialog":"notes","paths":[` + strings.Repeat(clip+",", maxPaths) + clip + `]}`, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRun(t)
			r.queue(t, tt.line)
			got, err := r.h.PickFiles(context.Background(), DialogNotes, tt.multiple)
			if err != nil {
				t.Fatalf("PickFiles error %v", err)
			}
			c := r.consumed(t)
			if len(c) != 1 {
				t.Fatalf("consumed %+v, want the line used", c)
			}
			if tt.want == nil {
				if got != nil || c[0].Result != resultCancelled {
					t.Fatalf("got %q (%+v), want cancelled", got, c[0])
				}
				return
			}
			var want []string
			for _, w := range tt.want {
				want = append(want, r.fixture(t, w))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// The queue is read in order, one line per dialog, and the record carries the index over a restart of the app.
func TestPickQueue(t *testing.T) {
	ctx := context.Background()
	r := newRun(t)

	// No queue at all: cancelled, nothing used.
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != "" || r.consumed(t) != nil {
		t.Fatalf("no queue: %q, consumed %+v", got, r.consumed(t))
	}

	r.queue(t,
		pickJSON(t, DialogSchedule, "fixtures/clip.wav"),
		pickJSON(t, DialogModel, "fixtures/sub/notes.pdf"),
		pickJSON(t, DialogMaterial, "fixtures/sub/photo.jpg"),
	)
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != r.fixture(t, "clip.wav") {
		t.Fatalf("first: %q", got)
	}
	// The second line is for the model dialog: the material dialog uses it up and is cancelled.
	if got, _ := r.h.PickFile(ctx, DialogMaterial); got != "" {
		t.Fatalf("second, wrong dialog: %q", got)
	}

	// A new instance (the app restarted) goes on at the third line.
	h2 := New(r.dir, nil)
	if got, _ := h2.PickFile(ctx, DialogMaterial); got != r.fixture(t, "sub/photo.jpg") {
		t.Fatalf("third after a restart: %q", got)
	}
	// The queue is empty now: cancelled and nothing more recorded.
	if got, _ := h2.PickFile(ctx, DialogMaterial); got != "" {
		t.Fatalf("past the end: %q", got)
	}
	c := r.consumed(t)
	want := []consumedLine{
		{Line: 1, Dialog: DialogSchedule, Result: resultPicked},
		{Line: 2, Dialog: DialogMaterial, Result: resultCancelled},
		{Line: 3, Dialog: DialogMaterial, Result: resultPicked},
	}
	if len(c) != len(want) {
		t.Fatalf("consumed %+v, want %+v", c, want)
	}
	for i := range want {
		if c[i].Line != want[i].Line || c[i].Dialog != want[i].Dialog || c[i].Result != want[i].Result {
			t.Errorf("consumed %d = %+v, want %+v", i, c[i], want[i])
		}
	}
	if !strings.Contains(c[1].Reason, `dialog "model"`) {
		t.Errorf("reason %q does not name the line's dialog", c[1].Reason)
	}

	// The runner appends a line: the next dialog takes it.
	r.queue(t,
		pickJSON(t, DialogSchedule, "fixtures/clip.wav"),
		pickJSON(t, DialogModel, "fixtures/sub/notes.pdf"),
		pickJSON(t, DialogMaterial, "fixtures/sub/photo.jpg"),
		pickJSON(t, DialogRecording, "fixtures/clip.wav"),
	)
	if got, _ := r.h.PickFile(ctx, DialogRecording); got != r.fixture(t, "clip.wav") {
		t.Fatalf("appended line: %q", got)
	}
}

// A last line without '\n' may still be being written: it is not used until its newline is there.
func TestPickQueuePartialLine(t *testing.T) {
	ctx := context.Background()
	r := newRun(t)
	first := pickJSON(t, DialogSchedule, "fixtures/clip.wav")
	second := pickJSON(t, DialogModel, "fixtures/sub/notes.pdf")
	path := filepath.Join(r.dir, picksFile)

	writeFile(t, path, first+"\n"+second[:len(second)/2])
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != r.fixture(t, "clip.wav") {
		t.Fatalf("complete first line: %q", got)
	}
	if got, _ := r.h.PickFile(ctx, DialogModel); got != "" {
		t.Fatalf("half-written line picked: %q", got)
	}
	writeFile(t, path, first+"\n"+second)
	if got, _ := r.h.PickFile(ctx, DialogModel); got != "" {
		t.Fatalf("line without a newline picked: %q", got)
	}
	if c := r.consumed(t); len(c) != 1 {
		t.Fatalf("consumed %+v, want only line 1 used", c)
	}

	writeFile(t, path, first+"\n"+second+"\n")
	if got, _ := r.h.PickFile(ctx, DialogModel); got != r.fixture(t, "sub/notes.pdf") {
		t.Fatalf("line with its newline: %q", got)
	}
	c := r.consumed(t)
	if len(c) != 2 || c[1].Line != 2 || c[1].Result != resultPicked {
		t.Fatalf("consumed %+v, want line 2 picked", c)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"partial", nil},
		{"\n", []string{""}},
		{"a\n", []string{"a"}},
		{"a\r\n", []string{"a"}},
		{"a\nb", []string{"a"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\nb\r\npart\r", []string{"a", "b"}},
		{"a\n\nb\n", []string{"a", "", "b"}},
	}
	for _, tt := range tests {
		if got := splitLines([]byte(tt.in)); !slices.Equal(got, tt.want) {
			t.Errorf("splitLines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A queue or record that is not a plain file, or too large, cancels without using a line.
func TestPickQueueFiles(t *testing.T) {
	ctx := context.Background()

	r := newRun(t)
	writeFile(t, filepath.Join(r.dir, picksFile, "x"), "") // picks.jsonl is a folder
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != "" || r.consumed(t) != nil {
		t.Fatalf("queue is a folder: %q, %+v", got, r.consumed(t))
	}

	r = newRun(t)
	writeFile(t, filepath.Join(r.dir, picksFile), strings.Repeat("x", maxQueueBytes+1))
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != "" || r.consumed(t) != nil {
		t.Fatalf("queue too large: %q, %+v", got, r.consumed(t))
	}

	r = newRun(t)
	r.queue(t, pickJSON(t, DialogSchedule, "fixtures/clip.wav"))
	writeFile(t, filepath.Join(r.dir, consumedFile, "x"), "") // consumed.jsonl is a folder
	if got, _ := r.h.PickFile(ctx, DialogSchedule); got != "" {
		t.Fatalf("record is a folder: %q", got)
	}

	// A relative control dir (config refuses it) fails closed.
	h := New("relative", nil)
	if got, _ := h.PickFile(ctx, DialogSchedule); got != "" {
		t.Fatalf("relative dir: %q", got)
	}
	if got, _ := h.SaveFile(ctx, "bundle", "b.zip"); got != "" {
		t.Fatalf("relative dir save: %q", got)
	}
	if err := h.SetText(ctx, "x"); err == nil {
		t.Fatal("relative dir record: no error")
	}
}

func TestReservedName(t *testing.T) {
	for _, n := range []string{"CON", "con", "Con.txt", "PRN", "AUX", "NUL", "nul.tar.gz", "COM0", "COM9", "com1.wav",
		"LPT1", "lpt9.x", "COM¹", "LPT³", "CONIN$", "conout$.txt", "NUL ", "AUX  .wav"} {
		if !reservedName(n) {
			t.Errorf("%q is a device name", n)
		}
	}
	for _, n := range []string{"CONSOLE", "nul_", "COM10", "LPT", "COM", "clip.wav", "x.con", "a.NUL", "COMa", "_CON"} {
		if reservedName(n) {
			t.Errorf("%q is not a device name", n)
		}
	}
}

func TestBelow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fixtures")
	tests := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "a.wav"), true},
		{filepath.Join(root, "sub", "a.wav"), true},
		{root, false},
		{root + string(filepath.Separator), false},
		{root + "2", false},
		{root + "-x" + string(filepath.Separator) + "a.wav", false},
		{filepath.Dir(root), false},
	}
	for _, tt := range tests {
		if got := below(root, tt.path); got != tt.want {
			t.Errorf("below(%q, %q) = %v, want %v", root, tt.path, got, tt.want)
		}
	}
}
