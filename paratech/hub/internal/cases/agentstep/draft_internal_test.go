package agentstep

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/toolcatalog"
)

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }

// TestDraftCoalesces: the first chunk goes at once; later ones are joined until 64 bytes are pending or 50 ms have
// passed since the last flush; flush sends the rest; nothing is lost or reordered.
func TestDraftCoalesces(t *testing.T) {
	clock := &stepClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	var out []string
	d := newDraft(clock, func(c string) { out = append(out, c) })

	d.push("При")
	d.push("")
	d.push("вет")
	d.push(",")
	if len(out) != 1 || out[0] != "При" {
		t.Fatalf("after the first chunks: %q", out)
	}
	clock.now = clock.now.Add(draftFlushEvery)
	d.push(" мир")
	if len(out) != 2 || out[1] != "вет, мир" {
		t.Fatalf("after 50 ms: %q", out)
	}
	d.push(strings.Repeat("a", draftFlushBytes))
	if len(out) != 3 {
		t.Fatalf("after 64 bytes: %q", out)
	}
	d.push("хвост")
	d.flush()
	d.flush()
	if strings.Join(out, "") != "Привет, мир"+strings.Repeat("a", draftFlushBytes)+"хвост" || len(out) != 4 || d.chunks != 4 {
		t.Fatalf("out %q, chunks %d", out, d.chunks)
	}
}

// TestDraftIsBounded: the draft stops at MaxReplyTextRunes characters, cut between characters, and flushes at the bound.
func TestDraftIsBounded(t *testing.T) {
	clock := &stepClock{}
	var out strings.Builder
	d := newDraft(clock, func(c string) { out.WriteString(c) })
	for range toolcatalog.MaxReplyTextRunes {
		d.push("ё😀")
	}
	if got := out.String(); utf8.RuneCountInString(got) != toolcatalog.MaxReplyTextRunes || !utf8.ValidString(got) {
		t.Fatalf("%d characters", utf8.RuneCountInString(got))
	}
	d.push("more")
	d.flush()
	if utf8.RuneCountInString(out.String()) != toolcatalog.MaxReplyTextRunes {
		t.Fatal("pushed past the bound")
	}
}
