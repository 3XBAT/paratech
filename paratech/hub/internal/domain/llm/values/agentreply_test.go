package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAgentReplyTurn(t *testing.T) {
	turn, err := AgentReplyTurn(strings.Repeat("я", MaxAgentReplyBytes/2))
	if err != nil || turn.Kind() != TurnAgentReply || turn.Text() != strings.Repeat("я", MaxAgentReplyBytes/2) {
		t.Fatalf("2 KiB reply: %v %v", turn, err)
	}
	for name, text := range map[string]string{
		"empty": "", "blank": " \n\t", "too long": strings.Repeat("a", MaxAgentReplyBytes+1), "NUL": "a\x00", "not UTF-8": "\xff",
	} {
		if _, err := AgentReplyTurn(text); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("agent reply %s: err = %v", name, err)
		}
	}
}

// TestAgentReplyOrder: an agent reply stands only between two student texts (ADR-0023 "History pairs").
func TestAgentReplyOrder(t *testing.T) {
	ok := mustTurn(t)
	h := history(t)
	text, calls, results := h[0], h[1], h[2]
	reply := ok(AgentReplyTurn("reply: done"))
	for name, turns := range map[string][]Turn{
		"first":          {reply, text},
		"last":           {text, reply},
		"before an echo": {text, reply, calls, results, text},
		"after results":  {text, calls, results, reply, text},
		"two in a row":   {text, reply, reply, text},
		"only reply":     {reply},
	} {
		if _, err := testRequest(t, turns); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for name, turns := range map[string][]Turn{
		"one pair":                {text, reply, text},
		"two texts then reply":    {text, text, reply, text},
		"pairs then current loop": {text, reply, text, reply, text, calls, results},
	} {
		if _, err := testRequest(t, turns); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestAgentReplyCountsInLimits: agent replies are turns of the 64-turn limit and bytes of the 256 KiB limit.
func TestAgentReplyCountsInLimits(t *testing.T) {
	ok := mustTurn(t)
	text, reply := ok(StudentTurn("a")), ok(AgentReplyTurn("reply: b"))
	pairs := func(n int) []Turn {
		turns := []Turn{text}
		for range n {
			turns = append(turns, reply, text)
		}
		return turns
	}
	if _, err := testRequest(t, pairs(31)); err != nil {
		t.Errorf("63 turns: %v", err)
	}
	if _, err := testRequest(t, pairs(32)); !errors.Is(err, ErrInvalidAgentRequest) {
		t.Errorf("65 turns: err = %v", err)
	}

	// Results fill the request to exactly 256 KiB without the reply; the reply takes it over the limit.
	head := ok(StudentTurn("b"))
	tail := ok(StudentTurn("c"))
	sizes := make([]int, 16)
	build := func() []Turn {
		turns := []Turn{head, tail}
		for set := range 4 {
			var cs []ToolCall
			var rs []ToolResult
			for i := range 4 {
				id := fmt.Sprintf("toolu_%d_%d", set, i)
				cs = append(cs, mustCall(t, id, "reply", "{}"))
				rs = append(rs, mustResult(t, id, `"`+strings.Repeat("x", sizes[set*4+i])+`"`))
			}
			turns = append(turns, ok(CallsTurn(cs, []byte{1})), ok(ResultsTurn(rs)))
		}
		return turns
	}
	r, err := testRequest(t, build())
	if err != nil {
		t.Fatal(err)
	}
	deficit := int(MaxAgentRequestBytes - r.Bytes())
	for i := range sizes {
		n := min(deficit, MaxResultJSONBytes-2)
		sizes[i], deficit = n, deficit-n
	}
	if deficit != 0 {
		t.Fatalf("cannot fill the request, %d bytes left", deficit)
	}
	full, err := testRequest(t, build())
	if err != nil || full.Bytes() != MaxAgentRequestBytes {
		t.Fatalf("full request: %v bytes, %v", full.Bytes(), err)
	}
	withReply := append([]Turn{head, reply}, build()[1:]...)
	if _, err := testRequest(t, withReply); !errors.Is(err, ErrAgentRequestTooLarge) {
		t.Errorf("with a reply: err = %v, want ErrAgentRequestTooLarge", err)
	}
}

// TestAgentReplyFingerprint: the reply and its kind are part of the fingerprint, so a request with an answer and one
// without it never replay each other.
func TestAgentReplyFingerprint(t *testing.T) {
	ok := mustTurn(t)
	a, b := ok(StudentTurn("what is a goroutine")), ok(StudentTurn("who is Rybakov"))
	fingerprint := func(turns ...Turn) string {
		t.Helper()
		r, err := testRequest(t, turns)
		if err != nil {
			t.Fatal(err)
		}
		return r.Fingerprint()
	}
	pair := fingerprint(a, ok(AgentReplyTurn("reply: x")), b)
	if pair != fingerprint(a, ok(AgentReplyTurn("reply: x")), b) {
		t.Fatal("the same request has two fingerprints")
	}
	for name, other := range map[string]string{
		"without reply":    fingerprint(a, b),
		"other reply":      fingerprint(a, ok(AgentReplyTurn("reply: y")), b),
		"reply as student": fingerprint(a, ok(StudentTurn("reply: x")), b),
	} {
		if other == pair {
			t.Errorf("%s: same fingerprint", name)
		}
	}
}

func TestReplyMarkers(t *testing.T) {
	ok := mustTurn(t)
	h := history(t)
	text, calls, results := h[0], h[1], h[2]
	reply := ok(AgentReplyTurn("reply: done"))
	for name, c := range map[string]struct {
		turns []Turn
		want  []bool
	}{
		"one text":       {[]Turn{text}, []bool{false}},
		"texts in a row": {[]Turn{text, text, text}, []bool{true, true, false}},
		"text then echo": {[]Turn{text, calls, results, text}, []bool{false, false, false, false}},
		"echo between":   {[]Turn{text, text, calls, results}, []bool{true, false, false, false}},
		"with a reply":   {[]Turn{text, text, reply, text}, []bool{false, false, false, false}},
	} {
		if got := ReplyMarkers(c.turns); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: markers = %v, want %v", name, got, c.want)
		}
	}
}

// TestAgentCallCountsMarkers: the reservation covers the markers the wire adds.
func TestAgentCallCountsMarkers(t *testing.T) {
	ok := mustTurn(t)
	text := ok(StudentTurn("a"))
	inputBytes := func(turns ...Turn) int64 {
		t.Helper()
		r, err := testRequest(t, turns)
		if err != nil {
			t.Fatal(err)
		}
		tool, err := NewToolSpec("reply", "Card.", `{"type":"object"}`)
		if err != nil {
			t.Fatal(err)
		}
		c, err := NewAgentCall("model", "system", []ToolSpec{tool}, r, 400)
		if err != nil {
			t.Fatal(err)
		}
		return c.InputBytes()
	}
	if got, want := inputBytes(text, text, text), inputBytes(text)+2*int64(len(text.Text())+len(MissingReplyText)); got != want {
		t.Errorf("input bytes = %d, want %d", got, want)
	}
}

func TestAgentReplyPrintsNoText(t *testing.T) {
	reply := mustTurn(t)(AgentReplyTurn("secret-reply"))
	for _, out := range []string{fmt.Sprint(reply), fmt.Sprintf("%+v", reply), fmt.Sprintf("%#v", reply)} {
		if strings.Contains(out, "secret") {
			t.Errorf("printed value leaks text: %s", out)
		}
	}
}
