package lecturetext

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func block(t *testing.T, heading string, start int64, paragraphs ...string) Block {
	t.Helper()
	b, err := NewBlock(heading, start, paragraphs)
	if err != nil {
		t.Fatalf("NewBlock: %v", err)
	}
	return b
}

func TestNewBlock(t *testing.T) {
	cases := []struct {
		name       string
		heading    string
		start      int64
		paragraphs []string
		ok         bool
	}{
		{"valid", " Введение ", 0, []string{" Сегодня о реках. ", "Вторая мысль."}, true},
		{"empty heading", " ", 0, []string{"a"}, false},
		{"multi-line heading", "a\nb", 0, []string{"a"}, false},
		{"heading with line separator", "a\u2028b", 0, []string{"a"}, false},
		{"heading too long", strings.Repeat("я", MaxHeadingRunes+1), 0, []string{"a"}, false},
		{"heading at limit", strings.Repeat("я", MaxHeadingRunes), 0, []string{"a"}, true},
		{"negative start", "a", -1, []string{"a"}, false},
		{"no paragraphs", "a", 0, nil, false},
		{"too many paragraphs", "a", 0, make([]string, MaxParagraphs+1), false},
		{"blank paragraph", "a", 0, []string{"a", " "}, false},
		{"paragraph with control", "a", 0, []string{"a\x00b"}, false},
		{"paragraph with bidi override", "a", 0, []string{"a\u202eb"}, false},
		{"paragraph with line break", "a", 0, []string{"a\nb"}, true},
		{"paragraph too long", "a", 0, []string{strings.Repeat("я", MaxParagraphRunes+1)}, false},
		{"invalid utf8", "a", 0, []string{"\xff"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewBlock(tc.heading, tc.start, tc.paragraphs)
			if tc.ok != (err == nil) {
				t.Fatalf("NewBlock err = %v, want ok %v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidBlock) {
				t.Fatalf("err = %v, want ErrInvalidBlock", err)
			}
			if tc.name == "valid" && (b.Heading() != "Введение" || b.Paragraphs()[0] != "Сегодня о реках.") {
				t.Fatalf("not trimmed: %+v", b.State())
			}
		})
	}
}

func newText(t *testing.T) *LectureText {
	t.Helper()
	l, err := New(Spec{Transcript: "t1", Lesson: "l1"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNew(t *testing.T) {
	l := newText(t)
	if l.ID() != "t1" || l.Ready() || l.Version() != 0 || l.PromptVersion() != 0 || l.Complete() {
		t.Fatalf("new lecture text = %+v", l.State())
	}
	if _, err := New(Spec{Transcript: ""}, t0); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("empty transcript: %v", err)
	}
	if _, err := New(Spec{Transcript: "t1", Lesson: "a/b"}, t0); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("bad lesson: %v", err)
	}
	if _, err := New(Spec{Transcript: "t1"}, time.Time{}); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("zero time: %v", err)
	}
}

// A run: parts in order, then Publish; a text made before stays until then.
func TestRunAndPublish(t *testing.T) {
	l := newText(t)
	a, b, c := block(t, "A", 0, "a"), block(t, "B", 5000, "b"), block(t, "C", 9000, "c")

	if err := l.AddPart("run1", 2, 1, []Block{c}, t0); !errors.Is(err, ErrPartOutOfOrder) {
		t.Fatalf("part 1 first: %v", err)
	}
	if err := l.AddPart("run1", 2, 0, []Block{a, b}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if l.NextPart("run1", 2) != 1 || l.NextPart("run2", 2) != 0 || l.NextPart("run1", 3) != 0 || l.Complete() {
		t.Fatalf("after part 0: next %d, complete %v", l.NextPart("run1", 2), l.Complete())
	}
	if err := l.Publish("run1", []Block{a}, 1, t0); !errors.Is(err, ErrRunIncomplete) {
		t.Fatalf("publish an incomplete run: %v", err)
	}
	if err := l.AddPart("run1", 2, 0, []Block{a}, t0); !errors.Is(err, ErrPartOutOfOrder) {
		t.Fatalf("part 0 again: %v", err)
	}
	if err := l.AddPart("run1", 2, 1, nil, t0); err != nil { // nothing said in part 1
		t.Fatal(err)
	}
	if !l.Complete() || len(l.Parts()) != 2 {
		t.Fatalf("complete %v, parts %d", l.Complete(), len(l.Parts()))
	}
	if err := l.Publish("run2", []Block{a}, 1, t0); !errors.Is(err, ErrRunIncomplete) {
		t.Fatalf("publish another run: %v", err)
	}
	if err := l.Publish("run1", nil, 1, t0); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("publish nothing: %v", err)
	}
	if err := l.Publish("run1", []Block{a, b}, 1, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !l.Ready() || len(l.Blocks()) != 2 || l.PromptVersion() != 1 || l.Complete() || !l.UpdatedAt().Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("after publish: %+v", l.State())
	}
	if run, parts, done := l.Run(); run != "" || parts != 0 || done != 0 {
		t.Fatalf("run after publish = %q %d %d", run, parts, done)
	}

	// A new run keeps the text until it completes.
	if err := l.AddPart("run3", 2, 0, []Block{c}, t0); err != nil {
		t.Fatal(err)
	}
	if !l.Ready() || l.Blocks()[0].Heading() != "A" {
		t.Fatal("the text before was replaced by an unfinished run")
	}
	// Another run starts afresh.
	if err := l.AddPart("run4", 3, 0, []Block{a}, t0); err != nil {
		t.Fatal(err)
	}
	if run, parts, done := l.Run(); run != "run4" || parts != 3 || done != 1 {
		t.Fatalf("run = %q %d %d", run, parts, done)
	}
}

func TestAddPartRejects(t *testing.T) {
	early, late := block(t, "Early", 1000, "a"), block(t, "Late", 9000, "b")
	cases := []struct {
		name   string
		run    string
		parts  int
		index  int
		blocks []Block
		want   error
	}{
		{"empty run", "", 1, 0, nil, ErrInvalidLectureText},
		{"run with space", "a b", 1, 0, nil, ErrInvalidLectureText},
		{"run too long", strings.Repeat("a", MaxRunLen+1), 1, 0, nil, ErrInvalidLectureText},
		{"no parts", "r", 0, 0, nil, ErrInvalidLectureText},
		{"too many parts", "r", MaxParts + 1, 0, nil, ErrInvalidLectureText},
		{"index out of range", "r", 2, 2, nil, ErrInvalidLectureText},
		{"zero block", "r", 1, 0, []Block{{}}, ErrInvalidLectureText},
		{"starts go back", "r", 1, 0, []Block{late, early}, ErrInvalidLectureText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := newText(t)
			if err := l.AddPart(tc.run, tc.parts, tc.index, tc.blocks, t0); !errors.Is(err, tc.want) {
				t.Fatalf("AddPart = %v, want %v", err, tc.want)
			}
			if run, _, _ := l.Run(); run != "" {
				t.Fatal("a refused part changed the run")
			}
		})
	}

	// Across parts too.
	l := newText(t)
	if err := l.AddPart("r", 2, 0, []Block{late}, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.AddPart("r", 2, 1, []Block{early}, t0); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("part going back in time: %v", err)
	}
}

func TestRestore(t *testing.T) {
	l := newText(t)
	a := block(t, "A", 0, "a")
	if err := l.AddPart("r", 1, 0, []Block{a}, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.Publish("r", []Block{a}, 1, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.AddPart("r2", 2, 0, []Block{a}, t0); err != nil {
		t.Fatal(err)
	}
	l.Persisted(3)

	got, err := Restore(l.State())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version() != 3 || !got.Ready() || got.NextPart("r2", 2) != 1 || got.LessonID() != "l1" || got.TranscriptID() != "t1" {
		t.Fatalf("restored = %+v", got.State())
	}

	bad := []struct {
		name   string
		modify func(*State)
	}{
		{"version 0", func(s *State) { s.Version = 0 }},
		{"id differs", func(s *State) { s.ID = "t2" }},
		{"blocks without prompt version", func(s *State) { s.PromptVersion = 0 }},
		{"prompt version without blocks", func(s *State) { s.Blocks = nil }},
		{"bad block", func(s *State) { s.Blocks[0].Heading = "" }},
		{"parts without run", func(s *State) { s.Run = "" }},
		{"more parts than the run", func(s *State) { s.RunParts = 0 }},
		{"run parts over the limit", func(s *State) { s.RunParts = MaxParts + 1 }},
		{"bad run", func(s *State) { s.Run = "a b" }},
		{"starts go back", func(s *State) {
			s.Blocks = []BlockState{{Heading: "B", StartMs: 5, Paragraphs: []string{"b"}}, {Heading: "A", StartMs: 1, Paragraphs: []string{"a"}}}
		}},
		{"missing time", func(s *State) { s.UpdatedAt = time.Time{} }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			st := l.State()
			tc.modify(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidLectureText) {
				t.Fatalf("Restore = %v, want ErrInvalidLectureText", err)
			}
		})
	}

	// The transcript was deleted: the reference is empty, the text stays.
	st := l.State()
	st.Transcript = ""
	if _, err := Restore(st); err != nil {
		t.Fatalf("restore without transcript: %v", err)
	}
}

func TestTextSizeIsBounded(t *testing.T) {
	l := newText(t)
	para := strings.Repeat("я", MaxParagraphRunes) // 8000 bytes
	paragraphs := make([]string, MaxParagraphs)
	for i := range paragraphs {
		paragraphs[i] = para
	}
	big := block(t, "Big", 0, paragraphs...) // 1.6 MB
	if err := l.AddPart("r", 3, 0, []Block{big}, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.AddPart("r", 3, 1, []Block{big}, t0); err != nil {
		t.Fatal(err)
	}
	if err := l.AddPart("r", 3, 2, []Block{big}, t0); !errors.Is(err, ErrInvalidLectureText) {
		t.Fatalf("over MaxTextBytes: %v", err)
	}
}
