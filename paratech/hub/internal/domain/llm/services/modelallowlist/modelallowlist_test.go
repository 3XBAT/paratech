package modelallowlist

import (
	"errors"
	"strings"
	"testing"
)

func newList(t *testing.T) *List {
	t.Helper()
	l, err := New([]Entry{
		{Alias: "default", Model: "claude-sonnet-5-5", MaxOutput: 8192},
		{Alias: "fast_1", Model: "claude-haiku-5-5", MaxOutput: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResolve(t *testing.T) {
	l := newList(t)
	tests := []struct {
		name      string
		alias     string
		requested int64
		model     string
		maxOutput int64
	}{
		{name: "requested below the cap", alias: "default", requested: 500, model: "claude-sonnet-5-5", maxOutput: 500},
		{name: "requested equals the cap", alias: "default", requested: 8192, model: "claude-sonnet-5-5", maxOutput: 8192},
		{name: "zero means the cap", alias: "default", requested: 0, model: "claude-sonnet-5-5", maxOutput: 8192},
		{name: "above the cap is clamped", alias: "fast_1", requested: 1_000_000, model: "claude-haiku-5-5", maxOutput: 1024},
		{name: "minimum", alias: "fast_1", requested: 1, model: "claude-haiku-5-5", maxOutput: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, maxOutput, err := l.Resolve(tt.alias, tt.requested)
			if err != nil {
				t.Fatal(err)
			}
			if model != tt.model || maxOutput != tt.maxOutput {
				t.Fatalf("Resolve = %q, %d; want %q, %d", model, maxOutput, tt.model, tt.maxOutput)
			}
		})
	}
}

// Test step 2: a model outside the allowlist is rejected, whatever the client sends instead of an alias.
func TestResolveRejectsModelsOutsideTheAllowlist(t *testing.T) {
	l := newList(t)
	for _, alias := range []string{
		"", "unknown", "Default", "default ", "claude-sonnet-5-5", "claude-opus-5-5",
		"https://evil.example/v1/messages", "../default", "default\x00", strings.Repeat("a", 65),
	} {
		model, maxOutput, err := l.Resolve(alias, 100)
		if !errors.Is(err, ErrModelNotAllowed) {
			t.Errorf("Resolve(%q) error = %v, want ErrModelNotAllowed", alias, err)
		}
		if model != "" || maxOutput != 0 {
			t.Errorf("Resolve(%q) = %q, %d; want nothing", alias, model, maxOutput)
		}
		if err != nil && alias != "" && strings.Contains(err.Error(), alias) {
			t.Errorf("error %q quotes the client alias", err)
		}
	}
}

func TestResolveRejectsNegativeMaxOutput(t *testing.T) {
	if _, _, err := newList(t).Resolve("default", -1); !errors.Is(err, ErrInvalidMaxOutput) {
		t.Fatalf("error = %v, want ErrInvalidMaxOutput", err)
	}
}

func TestNewValidatesEntries(t *testing.T) {
	ok := Entry{Alias: "default", Model: "claude-sonnet-5-5", MaxOutput: 8192}
	with := func(f func(*Entry)) []Entry {
		e := ok
		f(&e)
		return []Entry{e}
	}
	tests := []struct {
		name    string
		entries []Entry
	}{
		{name: "empty list", entries: nil},
		{name: "empty alias", entries: with(func(e *Entry) { e.Alias = "" })},
		{name: "alias too long", entries: with(func(e *Entry) { e.Alias = strings.Repeat("a", 65) })},
		{name: "alias upper case", entries: with(func(e *Entry) { e.Alias = "Default" })},
		{name: "alias starts with dash", entries: with(func(e *Entry) { e.Alias = "-fast" })},
		{name: "alias starts with underscore", entries: with(func(e *Entry) { e.Alias = "_fast" })},
		{name: "alias with dot", entries: with(func(e *Entry) { e.Alias = "fast.1" })},
		{name: "empty model", entries: with(func(e *Entry) { e.Model = "" })},
		{name: "model too long", entries: with(func(e *Entry) { e.Model = strings.Repeat("m", 129) })},
		{name: "model is a URL", entries: with(func(e *Entry) { e.Model = "https://evil.example/m" })},
		{name: "model with slash", entries: with(func(e *Entry) { e.Model = "a/b" })},
		{name: "model with space", entries: with(func(e *Entry) { e.Model = "claude sonnet" })},
		{name: "zero cap", entries: with(func(e *Entry) { e.MaxOutput = 0 })},
		{name: "negative cap", entries: with(func(e *Entry) { e.MaxOutput = -1 })},
		{name: "cap above the limit", entries: with(func(e *Entry) { e.MaxOutput = MaxOutputLimit + 1 })},
		{name: "duplicate alias", entries: []Entry{ok, {Alias: "default", Model: "claude-haiku-5-5", MaxOutput: 10}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if l, err := New(tt.entries); !errors.Is(err, ErrInvalidEntry) || l != nil {
				t.Fatalf("New = %v, %v; want ErrInvalidEntry", l, err)
			}
		})
	}

	edge := []Entry{
		{Alias: strings.Repeat("a", 64), Model: strings.Repeat("M", 128), MaxOutput: MaxOutputLimit},
		{Alias: "0", Model: "claude-haiku-4-5-20251001", MaxOutput: 1},
		{Alias: "a_b-c", Model: "Model_1.2-x", MaxOutput: 2},
	}
	if _, err := New(edge); err != nil {
		t.Fatalf("edge entries rejected: %v", err)
	}
}

func TestListIsACopy(t *testing.T) {
	entries := []Entry{{Alias: "default", Model: "claude-sonnet-5-5", MaxOutput: 8192}}
	l, err := New(entries)
	if err != nil {
		t.Fatal(err)
	}
	entries[0].Model = "changed"
	if model, _, _ := l.Resolve("default", 0); model != "claude-sonnet-5-5" {
		t.Fatalf("the list follows changes of the caller's slice: %q", model)
	}
}
