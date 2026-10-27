package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func mustCall(t *testing.T, id, tool, args string) ToolCall {
	t.Helper()
	c, err := NewToolCall(id, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustResult(t *testing.T, id, js string) ToolResult {
	t.Helper()
	r, err := NewToolResult(id, js, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustTurn(t *testing.T) func(Turn, error) Turn {
	return func(turn Turn, err error) Turn {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return turn
	}
}

func testBase(t *testing.T) AgentBase {
	t.Helper()
	b, err := NewAgentBase([]Subject{{ID: "subj-1", Name: "Матанализ (лекции)"}, {ID: "subj-2", Name: "Physics, 2-nd term."}}, "2026-10-22", "Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testScope(t *testing.T) AgentScope {
	t.Helper()
	s, err := NewAgentScope("subj-1", ScreenToday, "", "asg-1")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// history is a valid history: text, echo of two calls, their results, a new text.
func history(t *testing.T) []Turn {
	t.Helper()
	ok := mustTurn(t)
	calls := []ToolCall{mustCall(t, "toolu_01", "list_subjects", "{}"), mustCall(t, "toolu_02", "find_lessons", `{"subject":"subj-1"}`)}
	return []Turn{
		ok(StudentTurn("Что задали по матану?")),
		ok(CallsTurn(calls, []byte{1, 2, 3})),
		ok(ResultsTurn([]ToolResult{mustResult(t, "toolu_02", `{"lessons":[]}`), mustResult(t, "toolu_01", `[]`)})),
		ok(StudentTurn("А по физике?")),
	}
}

func testRequest(t *testing.T, turns []Turn) (AgentRequest, error) {
	t.Helper()
	return NewAgentRequest("ag:th-1:1:1", "th-1", ProfilePanel, 1, testBase(t), testScope(t), turns, 0)
}

func TestNewToolCall(t *testing.T) {
	c := mustCall(t, "toolu_01A-b", "find_lessons", ` {"subject":"s"}`)
	if c.CallID() != "toolu_01A-b" || c.Tool() != "find_lessons" || c.Arguments() != ` {"subject":"s"}` {
		t.Fatalf("got %v", c)
	}
	mustCall(t, strings.Repeat("a", 128), strings.Repeat("t", 64), "{"+strings.Repeat(" ", MaxArgumentsBytes-2)+"}")
	for name, c := range map[string][3]string{
		"empty id":      {"", "reply", "{}"},
		"long id":       {strings.Repeat("a", 129), "reply", "{}"},
		"id with space": {"a b", "reply", "{}"},
		"id with dot":   {"a.b", "reply", "{}"},
		"empty tool":    {"a", "", "{}"},
		"tool upper":    {"a", "Reply", "{}"},
		"tool digit":    {"a", "1reply", "{}"},
		"long tool":     {"a", strings.Repeat("t", 65), "{}"},
		"array args":    {"a", "reply", "[]"},
		"string args":   {"a", "reply", `"x"`},
		"null args":     {"a", "reply", "null"},
		"broken args":   {"a", "reply", "{"},
		"empty args":    {"a", "reply", ""},
		"long args":     {"a", "reply", "{" + strings.Repeat(" ", MaxArgumentsBytes-1) + "}"},
	} {
		if _, err := NewToolCall(c[0], c[1], c[2]); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidAgentRequest", name, err)
		}
		if name == "long args" {
			continue
		}
		if _, err := NewReturnedToolCall(c[0], c[1], c[2]); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("returned %s: err = %v, want ErrInvalidAgentRequest", name, err)
		}
	}
}

// TestNewReturnedToolCall: a returned call takes arguments up to MaxReturnedArgumentsBytes, never more.
func TestNewReturnedToolCall(t *testing.T) {
	args := "{" + strings.Repeat(" ", MaxReturnedArgumentsBytes-2) + "}"
	c, err := NewReturnedToolCall("toolu_1", "reply", args)
	if err != nil || c.Arguments() != args {
		t.Fatalf("at the cap: %v", err)
	}
	if _, err := NewReturnedToolCall("toolu_1", "reply", "{"+strings.Repeat(" ", MaxReturnedArgumentsBytes-1)+"}"); !errors.Is(err, ErrInvalidAgentRequest) {
		t.Fatalf("over the cap: err = %v", err)
	}
}

func TestNewToolResult(t *testing.T) {
	r, err := NewToolResult("toolu_1", `{"ok":true}`, true)
	if err != nil || r.CallID() != "toolu_1" || r.JSON() != `{"ok":true}` || !r.IsError() {
		t.Fatalf("got %v, %v", r, err)
	}
	if _, err := NewToolResult("a", `"`+strings.Repeat("x", MaxResultJSONBytes-2)+`"`, false); err != nil {
		t.Fatalf("16 KiB: %v", err)
	}
	for name, c := range map[string][2]string{
		"bad id":    {"a b", "{}"},
		"not JSON":  {"a", "{"},
		"empty":     {"a", ""},
		"too large": {"a", `"` + strings.Repeat("x", MaxResultJSONBytes-1) + `"`},
	} {
		if _, err := NewToolResult(c[0], c[1], false); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidAgentRequest", name, err)
		}
	}
}

func TestTurns(t *testing.T) {
	if _, err := StudentTurn(strings.Repeat("я", MaxStudentTextBytes/2)); err != nil {
		t.Fatalf("4 KiB text: %v", err)
	}
	for name, text := range map[string]string{
		"empty": "", "blank": " \n\t", "too long": strings.Repeat("a", MaxStudentTextBytes+1), "NUL": "a\x00", "not UTF-8": "\xff",
	} {
		if _, err := StudentTurn(text); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("student text %s: err = %v", name, err)
		}
	}
	call := mustCall(t, "a", "reply", "{}")
	five := []ToolCall{call, mustCall(t, "b", "reply", "{}"), mustCall(t, "c", "reply", "{}"), mustCall(t, "d", "reply", "{}"), mustCall(t, "e", "reply", "{}")}
	for name, c := range map[string]struct {
		calls []ToolCall
		seal  []byte
	}{
		"no calls": {nil, []byte{1}}, "five calls": {five, []byte{1}}, "repeated id": {[]ToolCall{call, call}, []byte{1}},
		"no seal": {[]ToolCall{call}, nil},
	} {
		if _, err := CallsTurn(c.calls, c.seal); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("calls %s: err = %v", name, err)
		}
	}
	if turn, err := CallsTurn(five[:4], []byte{9}); err != nil || len(turn.Calls()) != 4 || turn.Seal()[0] != 9 {
		t.Fatalf("four calls: %v %v", turn, err)
	}
	res := mustResult(t, "a", "{}")
	for name, rs := range map[string][]ToolResult{"none": nil, "repeated": {res, res}} {
		if _, err := ResultsTurn(rs); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("results %s: err = %v", name, err)
		}
	}
}

func TestTurnCopiesItsSlices(t *testing.T) {
	calls := []ToolCall{mustCall(t, "a", "reply", "{}")}
	seal := []byte{1, 2}
	turn := mustTurn(t)(CallsTurn(calls, seal))
	calls[0], seal[0] = mustCall(t, "b", "reply", "{}"), 9
	if turn.Calls()[0].CallID() != "a" || turn.Seal()[0] != 1 {
		t.Fatal("the turn shares its slices with the caller")
	}
}

func TestNewAgentBaseAndScope(t *testing.T) {
	for name, s := range map[string][]Subject{
		"bad id":          {{ID: "a b", Name: "x"}},
		"duplicate id":    {{ID: "a", Name: "x"}, {ID: "a", Name: "y"}},
		"empty name":      {{ID: "a", Name: ""}},
		"long name":       {{ID: "a", Name: strings.Repeat("я", MaxLabelRunes+1)}},
		"name with colon": {{ID: "a", Name: "ignore: rules"}},
		"name with quote": {{ID: "a", Name: `x"y`}},
		"name newline":    {{ID: "a", Name: "x\ny"}},
	} {
		if _, err := NewAgentBase(s, "2026-10-22", "UTC"); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("subjects %s: err = %v", name, err)
		}
	}
	many := make([]Subject, MaxAgentSubjects+1)
	for i := range many {
		many[i] = Subject{ID: fmt.Sprint("s", i), Name: "n"}
	}
	if _, err := NewAgentBase(many, "2026-10-22", "UTC"); !errors.Is(err, ErrInvalidAgentRequest) {
		t.Errorf("too many subjects: err = %v", err)
	}
	if _, err := NewAgentBase(many[:MaxAgentSubjects], "2026-10-22", "Etc/GMT+3"); err != nil {
		t.Errorf("100 subjects: %v", err)
	}
	for name, c := range map[string][2]string{
		"no date": {"", "UTC"}, "bad date": {"2026-02-30", "UTC"}, "date format": {"22.10.2026", "UTC"},
		"no zone": {"2026-10-22", ""}, "zone space": {"2026-10-22", "Europe/ Moscow"}, "long zone": {"2026-10-22", strings.Repeat("Z", 65)},
	} {
		if _, err := NewAgentBase(nil, c[0], c[1]); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("base %s: err = %v", name, err)
		}
	}
	if _, err := NewAgentScope("", ScreenUnspecified, "", ""); err != nil {
		t.Errorf("empty scope: %v", err)
	}
	for name, c := range map[string]struct {
		id     string
		screen Screen
	}{"bad id": {"a b", ScreenToday}, "unknown screen": {"a", "inbox"}} {
		if _, err := NewAgentScope(c.id, c.screen, "", ""); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("scope %s: err = %v", name, err)
		}
	}
}

func TestNewAgentRequest(t *testing.T) {
	r, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Key() != "ag:th-1:1:1" || r.ThreadID() != "th-1" || r.Profile() != ProfilePanel || r.CatalogVersion() != 1 ||
		len(r.Turns()) != 4 || r.MaxOutput() != 0 {
		t.Fatalf("got %v", r)
	}
	base, scope := testBase(t), testScope(t)
	for name, build := range map[string]func() (AgentRequest, error){
		"bad key": func() (AgentRequest, error) {
			return NewAgentRequest("a b", "th", ProfilePanel, 1, base, scope, history(t), 0)
		},
		"bad thread": func() (AgentRequest, error) {
			return NewAgentRequest("k", "", ProfilePanel, 1, base, scope, history(t), 0)
		},
		"unknown profile": func() (AgentRequest, error) { return NewAgentRequest("k", "th", "chat", 1, base, scope, history(t), 0) },
		"no version": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th", ProfilePanel, 0, base, scope, history(t), 0)
		},
		"no base": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th", ProfilePanel, 1, AgentBase{}, scope, history(t), 0)
		},
		"negative output": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th", ProfilePanel, 1, base, scope, history(t), -1)
		},
		"no turns": func() (AgentRequest, error) { return NewAgentRequest("k", "th", ProfilePanel, 1, base, scope, nil, 0) },
	} {
		if _, err := build(); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAgentRequestTurnOrder(t *testing.T) {
	ok := mustTurn(t)
	h := history(t)
	text, calls, results := h[0], h[1], h[2]
	other := ok(ResultsTurn([]ToolResult{mustResult(t, "toolu_01", "{}"), mustResult(t, "toolu_99", "{}")}))
	partial := ok(ResultsTurn([]ToolResult{mustResult(t, "toolu_01", "{}")}))
	for name, turns := range map[string][]Turn{
		"starts with calls":      {calls, results, text},
		"starts with results":    {results, text},
		"ends with calls":        {text, calls},
		"calls then text":        {text, calls, text},
		"results without calls":  {text, results},
		"results of other calls": {text, calls, other},
		"partial results":        {text, calls, partial},
		"zero turn":              {text, {}},
	} {
		if _, err := testRequest(t, turns); !errors.Is(err, ErrInvalidAgentRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for name, turns := range map[string][]Turn{
		"one text":          {text},
		"texts in a row":    {text, text},
		"ends with results": {text, calls, results},
	} {
		if _, err := testRequest(t, turns); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAgentRequestLimits(t *testing.T) {
	ok := mustTurn(t)
	text := ok(StudentTurn("a"))
	turns := make([]Turn, MaxAgentTurns+1)
	for i := range turns {
		turns[i] = text
	}
	if _, err := testRequest(t, turns); !errors.Is(err, ErrInvalidAgentRequest) {
		t.Errorf("65 turns: err = %v", err)
	}
	if _, err := testRequest(t, turns[:MaxAgentTurns]); err != nil {
		t.Errorf("64 turns: %v", err)
	}
	// 64 student texts of 4 KiB are exactly 256 KiB: with the other fields the request is over the limit.
	big := ok(StudentTurn(strings.Repeat("b", MaxStudentTextBytes)))
	for i := range turns {
		turns[i] = big
	}
	if _, err := testRequest(t, turns[:MaxAgentTurns]); !errors.Is(err, ErrAgentRequestTooLarge) || !errors.Is(err, ErrInvalidAgentRequest) {
		t.Errorf("over 256 KiB: err = %v, want ErrAgentRequestTooLarge", err)
	}
	if _, err := testRequest(t, turns[:MaxAgentTurns-1]); err != nil {
		t.Errorf("under 256 KiB: %v", err)
	}
}

func TestAgentFingerprint(t *testing.T) {
	r, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	fp := r.Fingerprint()
	if len(fp) != 64 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint %q is not lower-case hex SHA-256", fp)
	}
	same, _ := NewAgentRequest("other-key", "th-1", ProfilePanel, 1, testBase(t), testScope(t), history(t), 0)
	if same.Fingerprint() != fp {
		t.Fatal("the key must not change the fingerprint")
	}
	ok := mustTurn(t)
	h := history(t)
	resealed := append([]Turn(nil), h...)
	resealed[1] = ok(CallsTurn(h[1].Calls(), []byte{1, 2, 4}))
	errResult := append([]Turn(nil), h...)
	errResult[2] = ok(ResultsTurn([]ToolResult{mustResult(t, "toolu_01", `[]`), func() ToolResult {
		r, _ := NewToolResult("toolu_02", `{"lessons":[]}`, true)
		return r
	}()}))
	otherBase, _ := NewAgentBase(nil, "2026-10-22", "Europe/Moscow")
	otherScope, _ := NewAgentScope("subj-2", ScreenToday, "", "asg-1")
	for name, other := range map[string]func() (AgentRequest, error){
		"thread": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-2", ProfilePanel, 1, testBase(t), testScope(t), h, 0)
		},
		"version": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 2, testBase(t), testScope(t), h, 0)
		},
		"base": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, otherBase, testScope(t), h, 0)
		},
		"scope": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, testBase(t), otherScope, h, 0)
		},
		"turns": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, testBase(t), testScope(t), h[:1], 0)
		},
		"seal": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, testBase(t), testScope(t), resealed, 0)
		},
		"error": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, testBase(t), testScope(t), errResult, 0)
		},
		"output": func() (AgentRequest, error) {
			return NewAgentRequest("k", "th-1", ProfilePanel, 1, testBase(t), testScope(t), h, 5)
		},
	} {
		o, err := other()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o.Fingerprint() == fp {
			t.Errorf("%s: same fingerprint for a different request", name)
		}
	}
}

// TestFingerprintsAreDomainSeparated: an AgentStep request never shares a fingerprint with a CompletionSpec, even
// when the texts line up (ADR-0023).
func TestFingerprintsAreDomainSeparated(t *testing.T) {
	r, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewSpec(r.Key(), "summary", "default", r.ThreadID(), "panel", 0)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Fingerprint() == r.Fingerprint() {
		t.Fatal("AgentStep and Complete fingerprints match")
	}
	if agentFingerprintDomain == fingerprintDomain {
		t.Fatal("the fingerprint domains are equal")
	}
}

func TestAgentRequestRendersContextAsData(t *testing.T) {
	r, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := baseHeader + `{"today":"2026-10-22","time_zone":"Europe/Moscow","subjects":[{"id":"subj-1","name":"Матанализ (лекции)"},` +
		`{"id":"subj-2","name":"Physics, 2-nd term."}]}`; r.BaseText() != want {
		t.Fatalf("base = %s", r.BaseText())
	}
	if want := scopeHeader + `{"subject":"subj-1","screen":"today","assignment":"asg-1"}`; r.ScopeText() != want {
		t.Fatalf("scope = %s", r.ScopeText())
	}
	empty, _ := NewAgentScope("", "", "", "")
	none, _ := NewAgentRequest("k", "th", ProfilePanel, 1, testBase(t), empty, history(t), 0)
	if want := scopeHeader + `{"subject":"none","screen":"unspecified"}`; none.ScopeText() != want {
		t.Fatalf("empty scope = %s", none.ScopeText())
	}
}

func TestAgentValuesPrintNoText(t *testing.T) {
	ok := mustTurn(t)
	c := mustCall(t, "a", "reply", `{"text":"secret-args"}`)
	res, _ := NewToolResult("a", `"secret-result"`, false)
	turns := []Turn{ok(StudentTurn("secret-text")), ok(CallsTurn([]ToolCall{c}, []byte("secret-seal"))), ok(ResultsTurn([]ToolResult{res}))}
	r, err := testRequest(t, turns)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{c, res, turns[0], turns[1], r} {
		for _, out := range []string{fmt.Sprint(v), fmt.Sprintf("%+v", v), fmt.Sprintf("%#v", v)} {
			if strings.Contains(out, "secret") {
				t.Errorf("printed value leaks text: %s", out)
			}
		}
	}
}

func TestEncodeDecodeCalls(t *testing.T) {
	calls := []ToolCall{mustCall(t, "toolu_1", "reply", `{"text":"Готово"}`), mustCall(t, "toolu_2", "list_subjects", `{}`)}
	got, err := DecodeCalls(EncodeCalls(calls))
	if err != nil || len(got) != 2 || got[0] != calls[0] || got[1] != calls[1] {
		t.Fatalf("round trip = %v, %v", got, err)
	}
	for name, s := range map[string]string{
		"not JSON": "{", "empty": "[]", "bad call": `[{"id":"a b","tool":"reply","arguments":"{}"}]`,
		"five": strings.Repeat(`{"id":"a","tool":"r","arguments":"{}"},`, 4) + `{"id":"a","tool":"r","arguments":"{}"}`,
	} {
		if name == "five" {
			s = "[" + s + "]"
		}
		if _, err := DecodeCalls(s); !errors.Is(err, ErrInvalidCompletion) {
			t.Errorf("%s: err = %v, want ErrInvalidCompletion", name, err)
		}
	}
}

func TestAgentCall(t *testing.T) {
	r, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	tool, err := NewToolSpec("reply", "Card.", `{"type":"object"}`)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAgentCall("claude-test", "System.", []ToolSpec{tool}, r, 400)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model() != "claude-test" || c.System() != "System." || c.Base() != r.BaseText() || c.Scope() != r.ScopeText() ||
		len(c.Tools()) != 1 || len(c.Turns()) != 4 || c.MaxOutput() != 400 {
		t.Fatalf("call = %v", c)
	}
	want := int64(len("System.")+len(r.BaseText())+len(r.ScopeText())+len("reply")+len("Card.")+len(`{"type":"object"}`)) +
		r.Bytes() - int64(len(r.Key())+len(r.ThreadID())+len("2026-10-22")+len("Europe/Moscow")) -
		int64(len("subj-1")+len("Матанализ (лекции)")+len("subj-2")+len("Physics, 2-nd term.")) - int64(len("subj-1")+len("today")+len("asg-1"))
	if c.InputBytes() != want {
		t.Fatalf("input bytes = %d, want %d", c.InputBytes(), want)
	}
	for name, build := range map[string]func() error{
		"no model":  func() error { _, err := NewAgentCall("", "S", []ToolSpec{tool}, r, 1); return err },
		"no system": func() error { _, err := NewAgentCall("m", "", []ToolSpec{tool}, r, 1); return err },
		"no tools":  func() error { _, err := NewAgentCall("m", "S", nil, r, 1); return err },
		"no turns":  func() error { _, err := NewAgentCall("m", "S", []ToolSpec{tool}, AgentRequest{}, 1); return err },
		"no output": func() error { _, err := NewAgentCall("m", "S", []ToolSpec{tool}, r, 0); return err },
	} {
		if err := build(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}
	for name, s := range map[string][3]string{
		"bad name": {"Reply", "d", "{}"}, "no description": {"reply", "", "{}"}, "array schema": {"reply", "d", "[]"},
		"long description": {"reply", strings.Repeat("d", 1025), "{}"},
	} {
		if _, err := NewToolSpec(s[0], s[1], s[2]); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("tool %s: err = %v", name, err)
		}
	}
}

func TestAgentAnswerFailure(t *testing.T) {
	u, _ := NewUsage(10, 5)
	calls := []ToolCall{mustCall(t, "a", "reply", "{}")}
	if a := NewAgentAnswer(calls, u, true); a.Failure() != nil || len(a.Calls()) != 1 || a.Usage() != u {
		t.Fatalf("tool use answer = %v, failure %v", a, a.Failure())
	}
	err := NewAgentAnswer(nil, u, false).Failure()
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Kind != KindPermanent || !errors.Is(err, ErrProviderPermanent) {
		t.Fatalf("no tool use: failure = %v, want a permanent ProviderError", err)
	}
	for name, a := range map[string]AgentAnswer{"refused": RefusedAgentAnswer(u), "malformed": MalformedAgentAnswer(u)} {
		if err := a.Failure(); !errors.As(err, &pe) || pe.Kind != KindPermanent || a.Usage() != u || len(a.Calls()) != 0 {
			t.Errorf("%s: failure = %v, answer %v", name, err, a)
		}
		if a.Refused() != (name == "refused") {
			t.Errorf("%s: refused = %t", name, a.Refused())
		}
	}
	if _, err := DecodeCalls(FailedStepOutput); err == nil {
		t.Fatal("the failed step marker decodes as calls")
	}
}

// TestAgentAnswerStop: the answer carries the provider's stop reason; a cut at the output cap fails as "output cap",
// not as "no tool use" (TASK-172).
func TestAgentAnswerStop(t *testing.T) {
	u, _ := NewUsage(10, 1024)
	calls := []ToolCall{mustCall(t, "a", "reply", "{}")}
	reason := func(a AgentAnswer) string {
		var pe *ProviderError
		if err := a.Failure(); errors.As(err, &pe) {
			return pe.Reason
		}
		return ""
	}
	for name, tc := range map[string]struct {
		answer AgentAnswer
		stop   StopReason
		reason string
	}{
		"tool use":            {NewAgentAnswer(calls, u, true), StopToolUse, ""},
		"tool use, explicit":  {NewAgentAnswer(calls, u, true).WithStop(StopToolUse), StopToolUse, ""},
		"end of turn":         {NewAgentAnswer(nil, u, false).WithStop(StopEnd), StopEnd, "no tool use"},
		"no stop reason":      {NewAgentAnswer(nil, u, false), StopOther, "no tool use"},
		"output cap":          {NewAgentAnswer(nil, u, false).WithStop(StopMaxTokens), StopMaxTokens, "output cap"},
		"output cap, broken":  {MalformedAgentAnswer(u).WithStop(StopMaxTokens), StopMaxTokens, "output cap"},
		"malformed tool use":  {MalformedAgentAnswer(u).WithStop(StopToolUse), StopToolUse, "malformed tool call"},
		"refusal":             {RefusedAgentAnswer(u), StopRefusal, "refused"},
		"unknown stop reason": {NewAgentAnswer(nil, u, false).WithStop(StopReason(99)), StopOther, "no tool use"},
	} {
		if tc.answer.Stop() != tc.stop || reason(tc.answer) != tc.reason {
			t.Errorf("%s: stop %s, failure %q; want %s, %q", name, tc.answer.Stop(), reason(tc.answer), tc.stop, tc.reason)
		}
	}
	if s := NewAgentAnswer(nil, u, false).WithStop(StopMaxTokens).String(); !strings.Contains(s, "Stop:max_tokens") {
		t.Errorf("String() = %s", s)
	}
	if EncodeCalls(nil) != FailedStepOutput {
		t.Fatal("no calls must encode as the failed step marker")
	}
}
