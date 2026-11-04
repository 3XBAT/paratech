package tests

// Size of the fixed planner prefix (ADR-0033 §6): the system prompt and the tool list the provider gets at every step
// of a catalog. No tokenizer is available offline, so the count is a deterministic estimate from the texts, not the
// provider's count; the increase of a wave is what the ADR bounds.

import (
	"math"
	"testing"
	"unicode/utf8"

	"paratech/hub/testenv"
)

// The token counts here are an offline estimate and a regression guard only: the real count comes from Anthropic
// (count_tokens or usage.input_tokens) at level 2 (TASK-326). The primary estimate takes about 3.5 characters a token
// for ASCII text and 2 for other scripts (Cyrillic, symbols); the stricter one takes 3.0 and 1.5, as compact JSON
// (quotes, braces, short keys) and Cyrillic split into more tokens. The bound is asserted on the primary estimate;
// both are logged.
const (
	asciiRunesPerToken       = 3.5
	otherRunesPerToken       = 2.0
	strictASCIIRunesPerToken = 3.0
	strictOtherRunesPerToken = 1.5
)

// maxWave10PrefixTokens is the bound of ADR-0033 §6 for the increase of wave 10 (catalog 9 -> 10).
const maxWave10PrefixTokens = 2000

// prefixSize is the text of the fixed prefix of a panel catalog: the system prompt and, per tool, its name,
// description and JSON Schema, as the provider adapter sends them.
type prefixSize struct {
	bytes, ascii, other int
}

func (p prefixSize) tokens() int {
	return int(math.Ceil(float64(p.ascii)/asciiRunesPerToken + float64(p.other)/otherRunesPerToken))
}

func (p prefixSize) strictTokens() int {
	return int(math.Ceil(float64(p.ascii)/strictASCIIRunesPerToken + float64(p.other)/strictOtherRunesPerToken))
}

func (p *prefixSize) add(s string) {
	p.bytes += len(s)
	for _, r := range s {
		if r < utf8.RuneSelf {
			p.ascii++
		} else {
			p.other++
		}
	}
}

func panelPrefix(t *testing.T, version uint32) (system, tools prefixSize) {
	t.Helper()
	text, catalog, err := testenv.Panel(version)
	if err != nil {
		t.Fatal(err)
	}
	system.add(text)
	for _, s := range catalog.Specs() {
		tools.add(s.Name())
		tools.add(s.Description())
		tools.add(s.Schema())
	}
	return system, tools
}

// TestAgentPrefixSizeOfWave10: the prefix of catalog 10 grows by at most 2000 estimated tokens over catalog 9. The
// numbers are logged for the catalog PR (go test -run PrefixSize -v ./hub/tests/).
func TestAgentPrefixSizeOfWave10(t *testing.T) {
	total, strict := map[uint32]int{}, map[uint32]int{}
	for _, v := range []uint32{8, 9, 10} {
		system, tools := panelPrefix(t, v)
		total[v] = system.tokens() + tools.tokens()
		strict[v] = system.strictTokens() + tools.strictTokens()
		t.Logf("panel v%d: system %d bytes ≈ %d tokens; tools %d bytes ≈ %d tokens; prefix ≈ %d tokens (stricter estimate %d)",
			v, system.bytes, system.tokens(), tools.bytes, tools.tokens(), total[v], strict[v])
	}
	delta := total[10] - total[9]
	t.Logf("wave 10 adds ≈ %d tokens (stricter estimate %d; bound %d on the first)", delta, strict[10]-strict[9], maxWave10PrefixTokens)
	if delta > maxWave10PrefixTokens {
		t.Fatalf("catalog 10 adds ≈ %d tokens to the prefix, over the %d of ADR-0033 §6", delta, maxWave10PrefixTokens)
	}
}
